#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
: "${RELEASE_MANIFEST:?set RELEASE_MANIFEST to an on-prem platform.release/v1 manifest}"
profile=$root/config/certification/on-prem-reference.yaml
output=${CERTIFICATION_OUTPUT:-$root/certification-evidence/on-prem-certification-$(date -u +%Y%m%d%H%M%S).json}
mkdir -p "$(dirname "$output")"
[[ $(jq -r '.certification_tier' "$RELEASE_MANIFEST") == on-prem-reference ]] || { echo "release is not bound to on-prem-reference" >&2; exit 1; }

raw=$(mktemp); raw_array="${raw}.array"; logs=$(mktemp -d /tmp/on-prem-certification.XXXXXX)
cleanup() {
  rm -f "$raw" "$raw_array"
  find "$logs" -type f -delete
  rmdir "$logs"
  docker rm -f on-prem-kms-registry on-prem-harbor-backup on-prem-harbor-recovery >/dev/null 2>&1 || true
}
trap cleanup EXIT
overall=pass
gate() {
  local name=$1; shift
  local started completed status=pass
  started=$(date -u +%FT%TZ)
  if ! "$@" >"$logs/$name.log" 2>&1; then status=fail; overall=fail; fi
  completed=$(date -u +%FT%TZ)
  jq -n --arg name "$name" --arg status "$status" --arg started "$started" --arg completed "$completed" \
    --arg log_sha256 "$(shasum -a 256 "$logs/$name.log" | awk '{print $1}')" '{name:$name,status:$status,started_at:$started,completed_at:$completed,log_sha256:$log_sha256}' >>"$raw"
  [[ "$status" == pass ]] || { echo "gate failed: $name" >&2; sed -n '1,100p' "$logs/$name.log" >&2; }
}

component_readiness() {
  kubectl wait --for=condition=Ready nodes --all --timeout=5m
  for target in cert-manager/deployment/cert-manager cnpg-system/deployment/cloudnative-pg cnpg-system/deployment/plugin-barman-cloud harbor/deployment/harbor-core kyverno/deployment/kyverno-admission-controller identity-system/deployment/keycloak dns-system/deployment/external-dns argocd/deployment/argo-cd-server monitoring/deployment/monitoring-kube-prometheus-operator; do
    namespace=${target%%/*}; resource=${target#*/}; kubectl -n "$namespace" rollout status "$resource" --timeout=10m
  done
}
certificate_dns() {
  kubectl create namespace on-prem-edge-conformance --dry-run=client -o yaml | kubectl apply -f -
  kubectl -n on-prem-edge-conformance apply -f - <<'EOF'
apiVersion: cert-manager.io/v1
kind: Certificate
metadata: {name: edge, namespace: on-prem-edge-conformance}
spec: {secretName: edge-tls, dnsNames: [edge.preview.internal], issuerRef: {name: on-prem-ca, kind: ClusterIssuer}}
---
apiVersion: v1
kind: Service
metadata:
  name: edge
  namespace: on-prem-edge-conformance
  annotations:
    external-dns.alpha.kubernetes.io/hostname: edge.preview.internal
    external-dns.alpha.kubernetes.io/target: 192.0.2.80
spec: {selector: {app: absent}, ports: [{port: 80}]}
EOF
  kubectl -n on-prem-edge-conformance wait --for=condition=Ready certificate/edge --timeout=5m
  for _ in $(seq 1 60); do kubectl -n dns-system exec deployment/bind -- dig +short @127.0.0.1 edge.preview.internal A | grep -Fxq 192.0.2.80 && return; sleep 2; done
  return 1
}
gitops_drift() {
  kubectl -n argocd apply -f - <<EOF
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata: {name: on-prem-fixture, namespace: argocd}
spec:
  project: default
  source: {repoURL: https://github.com/argoproj/argocd-example-apps.git, targetRevision: master, path: guestbook}
  destination: {server: https://kubernetes.default.svc, namespace: on-prem-gitops-conformance}
  syncPolicy: {automated: {prune: true, selfHeal: true}, syncOptions: [CreateNamespace=true]}
EOF
  for _ in $(seq 1 120); do [[ $(kubectl -n argocd get application on-prem-fixture -o jsonpath='{.status.health.status}:{.status.sync.status}' 2>/dev/null) == Healthy:Synced ]] && break; sleep 2; done
  [[ $(kubectl -n argocd get application on-prem-fixture -o jsonpath='{.status.health.status}:{.status.sync.status}') == Healthy:Synced ]]
  local desired_image
  desired_image=$(kubectl -n on-prem-gitops-conformance get deployment guestbook-ui -o jsonpath='{.spec.template.spec.containers[0].image}')
  kubectl -n on-prem-gitops-conformance set image deployment/guestbook-ui guestbook-ui=nginx:alpine >/dev/null
  for _ in $(seq 1 60); do
    [[ $(kubectl -n on-prem-gitops-conformance get deployment guestbook-ui -o jsonpath='{.spec.template.spec.containers[0].image}') == "$desired_image" ]] && return
    sleep 2
  done
  return 1
}
observability() {
  kubectl -n platform-system get prometheusrule platform-production-slos >/dev/null
  kubectl -n monitoring get prometheus,alertmanager >/dev/null
}
vault_evidence() {
  local source_image source_registry digest repository password kms_image
  source_image=$(jq -er '.artifacts.control_plane.cluster_image' "$RELEASE_MANIFEST")
  source_registry=${source_image%%/*}
  repository=${source_image#*/}; repository=${repository%@*}
  digest=${source_image##*@}
  password=$(cat "$root/.on-prem-secrets/harbor-admin")
  docker inspect on-prem-kms-registry >/dev/null 2>&1 || \
    docker run -d --name on-prem-kms-registry --network kind registry:2 >/dev/null
  docker run --rm --network kind regclient/regctl:v0.9.2 \
    --host "reg=$source_registry,tls=disabled,user=admin,pass=$password" \
    --host 'reg=on-prem-kms-registry:5000,tls=disabled' image copy --digest-tags --referrers \
    "$source_image" "on-prem-kms-registry:5000/$repository@$digest"
  kms_image="on-prem-kms-registry:5000/$repository@$digest"
  KMS_TEST_IMAGE="$kms_image" "$root/scripts/validate-kms-signing.sh"
}
registry_recovery() {
  local image digest repository
  image=$(jq -er '.artifacts.control_plane.cluster_image' "$RELEASE_MANIFEST")
  digest=${image##*@}; repository=${image#*/}; repository=${repository%@*}
  EVIDENCE_TEST_IMAGE="on-prem-kms-registry:5000/$repository@$digest" \
    SKIP_EVIDENCE_BASELINE=true REQUIRE_BUILDKIT_EVIDENCE=false \
    "$root/scripts/validate-registry-evidence-lifecycle.sh"
}
harbor_recovery() {
  local source_image source_registry digest repository password recovered
  source_image=$(jq -er '.artifacts.control_plane.cluster_image' "$RELEASE_MANIFEST")
  source_registry=${source_image%%/*}; digest=${source_image##*@}
  repository=${source_image#*/}; repository=${repository%@*}
  password=$(cat "$root/.on-prem-secrets/harbor-admin")
  docker rm -f on-prem-harbor-backup on-prem-harbor-recovery >/dev/null 2>&1 || true
  docker run -d --name on-prem-harbor-backup --network kind -e REGISTRY_STORAGE_DELETE_ENABLED=true registry:2 >/dev/null
  docker run -d --name on-prem-harbor-recovery --network kind -p 127.0.0.1:5005:5000 registry:2 >/dev/null
  docker run --rm --network kind regclient/regctl:v0.9.2 \
    --host "reg=$source_registry,tls=disabled,user=admin,pass=$password" \
    --host 'reg=on-prem-harbor-backup:5000,tls=disabled' image copy --digest-tags --referrers \
    "$source_image" "on-prem-harbor-backup:5000/$repository@$digest"
  docker exec on-prem-harbor-backup registry garbage-collect /etc/docker/registry/config.yml >/dev/null
  docker run --rm --network kind regclient/regctl:v0.9.2 \
    --host 'reg=on-prem-harbor-backup:5000,tls=disabled' \
    --host 'reg=on-prem-harbor-recovery:5000,tls=disabled' image copy --digest-tags --referrers \
    "on-prem-harbor-backup:5000/$repository@$digest" "on-prem-harbor-recovery:5000/$repository@$digest"
  recovered="127.0.0.1:5005/$repository@$digest"
  cosign verify --allow-insecure-registry --key "$root/.on-prem-secrets/release-signing.pub" "$recovered" >/dev/null
  cosign verify-attestation --allow-insecure-registry --key "$root/.on-prem-secrets/release-signing.pub" \
    --type spdxjson "$recovered" >/dev/null
  cosign verify-attestation --allow-insecure-registry --key "$root/.on-prem-secrets/release-signing.pub" \
    --type slsaprovenance "$recovered" >/dev/null
}

gate component-readiness component_readiness
gate release-evidence cosign verify-blob --key "$root/.on-prem-secrets/release-signing.pub" --bundle "$(dirname "$RELEASE_MANIFEST")/release.bundle.json" "$RELEASE_MANIFEST"
gate on-prem-vault-workload-identity "$root/scripts/validate-on-prem-vault.sh"
gate vault-workload-identity vault_evidence
gate registry-retention-restore registry_recovery
gate harbor-evidence-backup-restore harbor_recovery
gate postgres-failover-wal-backup "$root/scripts/validate-on-prem-postgres.sh"
gate oidc-lifecycle "$root/scripts/validate-keycloak-oidc.sh"
gate cilium-edge-isolation "$root/scripts/validate-cilium-tenant-isolation.sh"
gate certificate-dns certificate_dns
gate gitops-drift gitops_drift
gate observability-alerts observability
gate developer-onboarding "$root/scripts/validate-developer-onboarding.sh"

jq -s . "$raw" >"$raw_array"
profile_digest=$(shasum -a 256 "$profile" | awk '{print $1}')
release_digest=$(shasum -a 256 "$RELEASE_MANIFEST" | awk '{print $1}')
jq -n --arg schema platform.provider-certification/v1 --arg tier on-prem-reference --arg status "$overall" \
  --arg profile_sha256 "$profile_digest" --arg release_sha256 "$release_digest" --arg completed "$(date -u +%FT%TZ)" --slurpfile gates "$raw_array" \
  '{schema:$schema,certification_tier:$tier,status:$status,profile_sha256:$profile_sha256,release_sha256:$release_sha256,completed_at:$completed,gates:$gates[0]}' >"$output"
[[ "$overall" == pass ]] || { echo "on-prem certification failed: $output" >&2; exit 1; }
echo "on-prem reference certification passed: $output"
