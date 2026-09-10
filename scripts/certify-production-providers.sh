#!/usr/bin/env bash
set -euo pipefail

: "${RELEASE_MANIFEST:?set RELEASE_MANIFEST to the verified platform.release/v1 document}"
: "${CERTIFICATION_OUTPUT:=provider-certification.json}"
: "${CERTIFICATION_TIER:=managed-provider}"
for command in kubectl jq cosign; do command -v "$command" >/dev/null || { echo "missing required command: $command" >&2; exit 1; }; done

started=$(date -u +%FT%TZ)
release_tier=$(jq -er '.certification_tier' "$RELEASE_MANIFEST")
[[ "$release_tier" == "$CERTIFICATION_TIER" ]] || { echo "release tier $release_tier cannot satisfy $CERTIFICATION_TIER certification" >&2; exit 1; }
run_gate() {
  local name=$1; shift
  local log
  log=$(mktemp)
  if "$@" >"$log" 2>&1; then jq -n --arg name "$name" '{name:$name,status:"pass"}'; else
    sed -n '1,80p' "$log" >&2
    jq -n --arg name "$name" '{name:$name,status:"fail"}'; return 1
  fi
}

results=$(mktemp)
status=pass
raw_results=$(mktemp)
run_gate kms ./scripts/validate-kms-signing.sh >>"$raw_results" || status=fail
run_gate registry ./scripts/validate-registry-evidence-lifecycle.sh >>"$raw_results" || status=fail
run_gate postgresql ./scripts/validate-control-plane-ha.sh >>"$raw_results" || status=fail
run_gate cilium ./scripts/validate-cilium-tenant-isolation.sh >>"$raw_results" || status=fail
jq -s . "$raw_results" >"$results"
if command -v sha256sum >/dev/null; then release_digest=$(sha256sum "$RELEASE_MANIFEST" | awk '{print $1}'); else release_digest=$(shasum -a 256 "$RELEASE_MANIFEST" | awk '{print $1}'); fi
jq -n --arg schema platform.provider-certification/v1 --arg release "$release_digest" --arg tier "$CERTIFICATION_TIER" \
  --arg started "$started" --arg completed "$(date -u +%FT%TZ)" --arg status "$status" --slurpfile gates "$results" \
  '{schema:$schema,certification_tier:$tier,release_sha256:$release,started_at:$started,completed_at:$completed,status:$status,gates:$gates[0]}' >"$CERTIFICATION_OUTPUT"
[[ "$status" == pass ]] || { echo "provider certification failed; evidence: $CERTIFICATION_OUTPUT" >&2; exit 1; }
echo "provider certification passed: $CERTIFICATION_OUTPUT"
