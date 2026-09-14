#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
: "${RELEASE_MANIFEST:?set RELEASE_MANIFEST}"
: "${PLATFORM_ENDPOINT:?set PLATFORM_ENDPOINT}"
: "${PLATFORM_ADMIN_TOKEN:?set PLATFORM_ADMIN_TOKEN}"
: "${TENANT_A_TOKEN:?set TENANT_A_TOKEN}"
: "${TENANT_B_TOKEN:?set TENANT_B_TOKEN}"
: "${TENANT_A_AUTH_JSON:?set TENANT_A_AUTH_JSON to the tenant OIDC configuration}"
: "${AIRGAP_REPOSITORY:?set AIRGAP_REPOSITORY to the canonical mirrored fixture identity}"
: "${AIRGAP_REPOSITORY_NAME:?set AIRGAP_REPOSITORY_NAME}"
: "${AIRGAP_COMMIT_SHA:?set AIRGAP_COMMIT_SHA}"
: "${GITHUB_WEBHOOK_SECRET:?set GITHUB_WEBHOOK_SECRET}"
: "${UPGRADE_CONTROL_PLANE_IMAGE:?set UPGRADE_CONTROL_PLANE_IMAGE to a distinct certified digest}"
: "${COSIGN_PRIVATE_KEY:?set COSIGN_PRIVATE_KEY to the installation evidence signing key}"

for command in curl jq kubectl cosign shasum openssl; do command -v "$command" >/dev/null || { echo "missing required command: $command" >&2; exit 1; }; done
[[ "$UPGRADE_CONTROL_PLANE_IMAGE" =~ @sha256:[a-f0-9]{64}$ ]] || { echo "upgrade image must be digest pinned" >&2; exit 1; }

evidence_dir=${INSTALLATION_EVIDENCE_DIR:-$root/certification-evidence/on-prem-platform-$(date -u +%Y%m%d%H%M%S)}
mkdir -p "$evidence_dir"
raw=$(mktemp /tmp/on-prem-platform-gates.XXXXXX)
trap 'rm -f "$raw"' EXIT
overall=pass
gate() {
  local name=$1; shift
  local started completed status=pass log="$evidence_dir/$name.log"
  started=$(date -u +%FT%TZ)
  if ! "$@" >"$log" 2>&1; then status=fail; overall=fail; fi
  completed=$(date -u +%FT%TZ)
  jq -nc --arg name "$name" --arg status "$status" --arg started "$started" --arg completed "$completed" \
    --arg log_sha256 "$(shasum -a 256 "$log" | awk '{print $1}')" \
    '{name:$name,status:$status,started_at:$started,completed_at:$completed,log_sha256:$log_sha256}' >>"$raw"
  if [[ "$status" == fail ]]; then echo "gate failed: $name" >&2; sed -n '1,120p' "$log" >&2; fi
}

api() {
  local token=$1 method=$2 path=$3 body=${4:-}
  local args=(--fail-with-body --silent --show-error -H "Authorization: Bearer $token" -H 'Content-Type: application/json' -X "$method")
  [[ -z "$body" ]] || args+=(--data "$body")
  curl "${args[@]}" "${PLATFORM_ENDPOINT%/}$path"
}

installation_invariants() {
  kubectl -n argocd get application self-service-cicd-platform -o json | jq -e '.status.sync.status == "Synced" and .status.health.status == "Healthy"' >/dev/null
  kubectl -n control-plane rollout status deployment/control-plane --timeout=10m
  [[ $(kubectl -n control-plane get deployment control-plane -o jsonpath='{.spec.replicas}') -ge 3 ]]
  kubectl -n control-plane get pdb control-plane -o json | jq -e '.spec.minAvailable == 2' >/dev/null
  image=$(kubectl -n control-plane get deployment control-plane -o jsonpath='{.spec.template.spec.containers[0].image}')
  [[ "$image" =~ @sha256:[a-f0-9]{64}$ ]]
  ! kubectl -n control-plane get deployment control-plane -o yaml | grep -q 'STATE_PATH\|control-plane-state'
  api "$TENANT_B_TOKEN" GET /readyz >/dev/null
}

send_github_event() {
  local action=$1 delivery=$2
  local payload signature
  payload=$(jq -nc --arg action "$action" --arg repository "$AIRGAP_REPOSITORY" --arg name "$AIRGAP_REPOSITORY_NAME" --arg sha "$AIRGAP_COMMIT_SHA" \
    '{action:$action,number:23001,installation:{id:1},repository:{name:$name,full_name:$repository},pull_request:{head:{sha:$sha}}}')
  signature="sha256=$(printf '%s' "$payload" | openssl dgst -sha256 -hmac "$GITHUB_WEBHOOK_SECRET" -hex | awk '{print $2}')"
  curl --fail-with-body --silent --show-error -X POST "${PLATFORM_ENDPOINT%/}/api/v1/webhooks/github" \
    -H "X-GitHub-Delivery: $delivery" -H 'X-GitHub-Event: pull_request' \
    -H "X-Hub-Signature-256: $signature" -H 'Content-Type: application/json' --data "$payload"
}

golden_path() {
  local tenant_a=adr23-alpha tenant_b=adr23-beta service=airgap-fixture environment="${AIRGAP_REPOSITORY_NAME}-pr-23001"
  api "$PLATFORM_ADMIN_TOKEN" POST /api/v1/admin/tenants "{\"tenant_id\":\"$tenant_a\"}" >/dev/null
  api "$PLATFORM_ADMIN_TOKEN" POST /api/v1/admin/tenants "{\"tenant_id\":\"$tenant_b\"}" >/dev/null
  api "$TENANT_A_TOKEN" PUT /api/v1/tenant/auth "$TENANT_A_AUTH_JSON" >/dev/null
  beta_before=$(api "$TENANT_B_TOKEN" GET /api/v1/catalog/services | shasum -a 256 | awk '{print $1}')
  api "$TENANT_A_TOKEN" POST /api/v1/services "$(jq -nc --arg name "$service" --arg repo "https://github.com/$AIRGAP_REPOSITORY" '{name:$name,owner:"platform-conformance",repo_url:$repo,environment:"preview",deployment:{container_port:8080,dockerfile:"Dockerfile"}}')" >/dev/null

  delivery="adr23-$(date +%s)"
  send_github_event opened "$delivery" >/dev/null
  send_github_event opened "$delivery" >/dev/null
  # Terminate one reconciler while the durable command is in flight. The lease
  # must expire and another replica must converge the same generation once.
  pod=$(kubectl -n control-plane get pod -l app=control-plane -o jsonpath='{.items[0].metadata.name}')
  kubectl -n control-plane delete pod "$pod" --wait=false
  kubectl -n control-plane rollout status deployment/control-plane --timeout=5m

  deadline=$((SECONDS + 900))
  while (( SECONDS < deadline )); do
    environment_json=$(api "$TENANT_A_TOKEN" GET "/api/v1/environments/$environment" 2>/dev/null || true)
    deployed=$(jq -r '.environment.source.deployed_image // empty' <<<"$environment_json" 2>/dev/null || true)
    url=$(jq -r '.environment.source.preview_url // empty' <<<"$environment_json" 2>/dev/null || true)
    [[ "$deployed" =~ @sha256:[a-f0-9]{64}$ && "$url" == https://* ]] && break
    sleep 5
  done
  [[ "$deployed" =~ @sha256:[a-f0-9]{64}$ && "$url" == https://* ]]
  api "$TENANT_A_TOKEN" GET /api/v1/catalog/services | jq -e --arg service "$service" 'any(.name == $service)' >/dev/null
  api "$TENANT_A_TOKEN" GET "/api/v1/services/$service/diagnostics" | jq -e '.checks | all(.status == "pass")' >/dev/null
  [[ $(api "$PLATFORM_ADMIN_TOKEN" GET /api/v1/admin/scm/commands | jq --arg delivery "$delivery" '[.[] | select(.delivery_id == $delivery)] | length') -eq 1 ]]

  send_github_event closed "${delivery}-close" >/dev/null
  kubectl -n argo wait --for=jsonpath='{.status.phase}'=Succeeded workflow -l "platform.environment=$environment,platform.workflow.type=environment-destroy" --timeout=10m
  api "$TENANT_A_TOKEN" DELETE "/api/v1/environments/$environment" >/dev/null
  api "$TENANT_A_TOKEN" DELETE "/api/v1/services/$service" >/dev/null
  api "$PLATFORM_ADMIN_TOKEN" PATCH "/api/v1/admin/tenants/$tenant_a/status" '{"status":"suspended"}' >/dev/null
  api "$PLATFORM_ADMIN_TOKEN" PATCH "/api/v1/admin/tenants/$tenant_a/status" '{"status":"offboarded"}' >/dev/null
  beta_after=$(api "$TENANT_B_TOKEN" GET /api/v1/catalog/services | shasum -a 256 | awk '{print $1}')
  [[ "$beta_before" == "$beta_after" ]]
}

upgrade_and_rollback() {
  previous=$(kubectl -n control-plane get deployment control-plane -o jsonpath='{.spec.template.spec.containers[0].image}')
  kubectl -n control-plane set image deployment/control-plane control-plane="$UPGRADE_CONTROL_PLANE_IMAGE"
  kubectl -n control-plane rollout status deployment/control-plane --timeout=10m
  kubectl -n control-plane set image deployment/control-plane control-plane="invalid.invalid/control-plane@sha256:$(printf '0%.0s' {1..64})"
  if kubectl -n control-plane rollout status deployment/control-plane --timeout=90s; then
    echo "deliberately invalid rollout unexpectedly succeeded" >&2
    return 1
  fi
  kubectl -n control-plane set image deployment/control-plane control-plane="$UPGRADE_CONTROL_PLANE_IMAGE"
  kubectl -n control-plane rollout status deployment/control-plane --timeout=10m
  [[ "$previous" != "$UPGRADE_CONTROL_PLANE_IMAGE" ]]
}

gate installation-invariants installation_invariants
gate golden-path-and-reconciliation-recovery golden_path
gate digest-upgrade-and-failed-rollout-rollback upgrade_and_rollback
gate postgres-backup-restore "$root/scripts/validate-on-prem-postgres.sh"

jq -s . "$raw" >"$evidence_dir/gates.json"
profile=$root/config/certification/on-prem-reference.yaml
lock=$root/config/airgap/on-prem-images.lock.json
migration=$(rg -o 'version: [0-9]+' "$root/control-plane/internal/api/database.go" | awk '{print $2}' | sort -n | tail -1)
kubectl_version=$(kubectl version -o json | jq -r '.serverVersion.gitVersion')
jq -n --arg status "$overall" --arg created "$(date -u +%FT%TZ)" --arg kubernetes "$kubectl_version" \
  --arg migration "$migration" --arg profile_sha256 "$(shasum -a 256 "$profile" | awk '{print $1}')" \
  --arg release_sha256 "$(shasum -a 256 "$RELEASE_MANIFEST" | awk '{print $1}')" \
  --arg manifest_sha256 "$(kubectl kustomize "$root" | shasum -a 256 | awk '{print $1}')" \
  --argjson profile "$(ruby -e 'require "yaml"; require "json"; puts JSON.generate(YAML.safe_load(File.read(ARGV[0])))' "$profile")" \
  --argjson images "$(jq '.images' "$lock")" --slurpfile gates "$evidence_dir/gates.json" \
  '{schema:"platform.installation-evidence/v1",status:$status,created_at:$created,installation_profile:$profile.metadata.name,profile_version:$profile.spec.components,kubernetes_version:$kubernetes,database_migration_version:($migration|tonumber),profile_sha256:$profile_sha256,release_sha256:$release_sha256,manifest_sha256:$manifest_sha256,images:$images,gates:$gates[0]}' >"$evidence_dir/installation-bom.json"
cosign sign-blob --yes --key "$COSIGN_PRIVATE_KEY" --bundle "$evidence_dir/installation-bom.bundle.json" "$evidence_dir/installation-bom.json" >/dev/null
find "$evidence_dir" -name '*.log' -delete
[[ "$overall" == pass ]] || { echo "on-prem platform acceptance failed: $evidence_dir/installation-bom.json" >&2; exit 1; }
echo "on-prem platform acceptance passed: $evidence_dir/installation-bom.json"
