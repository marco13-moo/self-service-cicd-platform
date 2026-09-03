#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
source_image=${EVIDENCE_TEST_IMAGE:-${KMS_TEST_IMAGE:-}}
backup_registry=${EVIDENCE_BACKUP_REGISTRY:-self-service-cicd-backup-registry:5000}
backup_container=${EVIDENCE_BACKUP_CONTAINER:-self-service-cicd-backup-registry}
recovery_registry=${EVIDENCE_RECOVERY_REGISTRY:-self-service-cicd-recovery-registry:5000}
recovery_container=${EVIDENCE_RECOVERY_CONTAINER:-self-service-cicd-recovery-registry}
regctl_image=${REGCTL_IMAGE:-regclient/regctl:v0.9.2}

if [[ ! "$source_image" =~ @sha256:[a-f0-9]{64}$ ]]; then
  echo "EVIDENCE_TEST_IMAGE must be a cluster-reachable digest reference" >&2
  exit 1
fi
for binary in docker jq kubectl sed; do
  command -v "$binary" >/dev/null || { echo "$binary is required" >&2; exit 1; }
done

source_repository=${source_image%@sha256:*}
source_registry=${source_repository%%/*}
digest=${source_image##*@}
repository_path=${source_repository#*/}
backup_repository="$backup_registry/$repository_path"
backup_image="$backup_repository@$digest"
recovered_repository="$recovery_registry/$repository_path"
recovered_image="$recovered_repository@$digest"
temporary_files=()
manifest_get() {
  registry=$1
  reference=$2
  docker run --rm --network kind "$regctl_image" \
    --host "reg=$registry,tls=disabled" manifest get "$reference" --format raw-body
}
cleanup() {
  kubectl -n argo delete job registry-evidence-verify registry-evidence-verify-attestation --ignore-not-found >/dev/null 2>&1 || true
  kubectl delete clusterpolicy registry-evidence-recovery-conformance --ignore-not-found >/dev/null 2>&1 || true
  kubectl delete namespace evidence-recovery-conformance --ignore-not-found >/dev/null 2>&1 || true
  for file in ${temporary_files[*]-}; do rm -f "$file"; done
  if [ "${KEEP_EVIDENCE_RECOVERY_REGISTRY:-false}" != true ]; then
    docker rm -f "$backup_container" >/dev/null 2>&1 || true
    docker rm -f "$recovery_container" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

"$repo_root/scripts/bootstrap-vault-kms-conformance.sh" >/dev/null

# Establish the pre-backup cryptographic baseline before copying any evidence.
# Iterative diagnostics may skip this already-proven stage explicitly, while the
# default conformance path always repeats it.
if [ "${SKIP_EVIDENCE_BASELINE:-false}" != true ]; then
  KMS_TEST_IMAGE="$source_image" "$repo_root/scripts/validate-kms-signing.sh" >/dev/null
fi

source_manifest=$(manifest_get "$source_registry" "$source_image")
source_attestation_digest=$(jq -r '.manifests[]? | select(.annotations["vnd.docker.reference.type"] == "attestation-manifest") | .digest' <<<"$source_manifest" | head -1)
if [ "${REQUIRE_BUILDKIT_EVIDENCE:-true}" = true ]; then
  test -n "$source_attestation_digest"
  source_attestation=$(manifest_get "$source_registry" "$source_repository@$source_attestation_digest")
  jq -e '.layers[] | select(.annotations["in-toto.io/predicate-type"] == "https://spdx.dev/Document")' <<<"$source_attestation" >/dev/null
  jq -e '.layers[] | select(.annotations["in-toto.io/predicate-type"] == "https://slsa.dev/provenance/v1")' <<<"$source_attestation" >/dev/null
fi

for registry_spec in "$backup_container" "$recovery_container"; do
  if ! docker inspect "$registry_spec" >/dev/null 2>&1; then
    docker run -d --name "$registry_spec" --network kind \
      -e REGISTRY_STORAGE_DELETE_ENABLED=true registry:2 >/dev/null
  fi
done

# Recursive copy is the backup primitive: the image graph, digest tags used by
# legacy Cosign storage, and OCI 1.1 referrers must move as one aggregate.
docker run --rm --network kind "$regctl_image" \
  --host "reg=$source_registry,tls=disabled" \
  --host "reg=$backup_registry,tls=disabled" image copy \
  --digest-tags --referrers "$source_image" "$backup_image"

# Exercise garbage collection only in the disposable backup registry. This
# deliberately avoids mutating the primary registry under test.
docker exec "$backup_container" registry garbage-collect /etc/docker/registry/config.yml >/dev/null

# Restore into an independently empty registry; verification never relies on
# the backup registry remaining online.
docker run --rm --network kind "$regctl_image" \
  --host "reg=$backup_registry,tls=disabled" \
  --host "reg=$recovery_registry,tls=disabled" image copy \
  --digest-tags --referrers "$backup_image" "$recovered_image"

recovered_manifest=$(manifest_get "$recovery_registry" "$recovered_image")
recovered_attestation_digest=$(jq -r '.manifests[]? | select(.annotations["vnd.docker.reference.type"] == "attestation-manifest") | .digest' <<<"$recovered_manifest" | head -1)
if [ "${REQUIRE_BUILDKIT_EVIDENCE:-true}" = true ]; then
  test "$recovered_attestation_digest" = "$source_attestation_digest"
  recovered_attestation=$(manifest_get "$recovery_registry" "$recovered_repository@$recovered_attestation_digest")
  jq -e '.layers[] | select(.annotations["in-toto.io/predicate-type"] == "https://spdx.dev/Document")' <<<"$recovered_attestation" >/dev/null
  jq -e '.layers[] | select(.annotations["in-toto.io/predicate-type"] == "https://slsa.dev/provenance/v1")' <<<"$recovered_attestation" >/dev/null
fi

verify_manifest=$(mktemp /tmp/registry-evidence-verify.XXXXXX)
policy_manifest=$(mktemp /tmp/registry-evidence-policy.XXXXXX)
pod_manifest=$(mktemp /tmp/registry-evidence-pod.XXXXXX)
temporary_files+=("$verify_manifest" "$policy_manifest" "$pod_manifest")
sed "s|EVIDENCE_TEST_IMAGE|$recovered_image|g" \
  "$repo_root/infra/k8s/registry-evidence-verify.yaml" > "$verify_manifest"
sed "s|EVIDENCE_REGISTRY_PATTERN|${recovery_registry}/*|g" \
  "$verify_manifest" > "$policy_manifest"

kubectl create namespace evidence-recovery-conformance >/dev/null
kubectl label namespace evidence-recovery-conformance \
  platform.evidence-recovery-conformance=enabled --overwrite >/dev/null
kubectl apply -f "$policy_manifest" >/dev/null
kubectl -n argo wait --for=condition=complete job/registry-evidence-verify --timeout=5m >/dev/null
kubectl -n argo wait --for=condition=complete job/registry-evidence-verify-attestation --timeout=5m >/dev/null

kubectl -n evidence-recovery-conformance run recovered-subject \
  --image="$recovered_image" --restart=Never --dry-run=client -o yaml > "$pod_manifest"
kubectl apply --dry-run=server -f "$pod_manifest" >/dev/null

echo "Registry retention, recursive backup, garbage collection, restore verification, and admission replay passed for $recovered_image"
