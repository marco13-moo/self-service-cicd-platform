#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
lock=${1:-$root/config/airgap/on-prem-images.lock.json}
: "${AIRGAP_REGISTRY:?set AIRGAP_REGISTRY to the site Harbor hostname/project}"
command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }
registry_host=${AIRGAP_REGISTRY%%/*}
registry_user=${AIRGAP_REGISTRY_USERNAME:-admin}
registry_password=${AIRGAP_REGISTRY_PASSWORD:-}
if command -v regctl >/dev/null; then
  regctl=(regctl)
elif command -v docker >/dev/null; then
  regctl=(docker run --rm --network kind regclient/regctl:v0.9.2)
else
  echo "regctl or Docker is required" >&2
  exit 1
fi
host_options=(--host "reg=$registry_host,tls=disabled")
if [[ -n "$registry_password" ]]; then
  host_options=(--host "reg=$registry_host,tls=disabled,user=$registry_user,pass=$registry_password")
fi
jq -e '.schema == "platform.airgap-images/v1" and (.images | all(.source | test("@sha256:[a-f0-9]{64}$")))' "$lock" >/dev/null

rewrites=$(mktemp /tmp/on-prem-image-rewrites.XXXXXX)
trap 'rm -f "$rewrites"' EXIT
while IFS=$'\t' read -r name source; do
  digest=${source##*@}
  destination="${AIRGAP_REGISTRY%/}/${name}@${digest}"
  # Release evidence uses the host-forwarded Harbor address; containers on the
  # kind network must address Harbor through its node endpoint instead.
  if [[ -n ${AIRGAP_SOURCE_REWRITE_FROM:-} && "$source" == "$AIRGAP_SOURCE_REWRITE_FROM"* ]]; then
    source="${AIRGAP_SOURCE_REWRITE_TO:?set AIRGAP_SOURCE_REWRITE_TO}${source#"$AIRGAP_SOURCE_REWRITE_FROM"}"
  fi
  "${regctl[@]}" "${host_options[@]}" image copy --digest-tags --referrers "$source" "$destination"
  jq -nc --arg name "$name" --arg newName "${AIRGAP_REGISTRY%/}/$name" --arg digest "$digest" \
    '{name:$name,newName:$newName,digest:$digest}' >>"$rewrites"
done < <(jq -r '.images[] | [.name,.source] | @tsv' "$lock")

jq -s '{apiVersion:"platform.airgap-rewrites/v1",images:.}' "$rewrites" >"${AIRGAP_REWRITE_OUTPUT:-$root/certification-evidence/airgap-image-rewrites.json}"
echo "air-gap images and OCI referrers mirrored to $AIRGAP_REGISTRY"
