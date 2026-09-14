#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
: "${RELEASE_MANIFEST:?set RELEASE_MANIFEST to the certified platform.release/v1 manifest}"
: "${PREVIEW_BUILDER_IMAGE:?set PREVIEW_BUILDER_IMAGE to a resolved digest}"
: "${PREVIEW_SCANNER_IMAGE:?set PREVIEW_SCANNER_IMAGE to a resolved digest}"
: "${PREVIEW_COSIGN_IMAGE:?set PREVIEW_COSIGN_IMAGE to a resolved digest}"
: "${PREVIEW_VAULT_IMAGE:?set PREVIEW_VAULT_IMAGE to a resolved digest}"
output=${1:-$root/config/airgap/on-prem-images.lock.json}
for command in jq kubectl; do command -v "$command" >/dev/null || { echo "missing required command: $command" >&2; exit 1; }; done

temporary=$(mktemp /tmp/on-prem-images.XXXXXX)
trap 'rm -f "$temporary"' EXIT
jq -r '.artifacts | to_entries[] | [.key,.value.cluster_image] | @tsv' "$RELEASE_MANIFEST" >"$temporary"
for pair in "preview-builder=$PREVIEW_BUILDER_IMAGE" "preview-scanner=$PREVIEW_SCANNER_IMAGE" "preview-cosign=$PREVIEW_COSIGN_IMAGE" "preview-vault=$PREVIEW_VAULT_IMAGE"; do
  name=${pair%%=*}; image=${pair#*=}
  [[ "$image" =~ @sha256:[a-f0-9]{64}$ ]] || { echo "$name is not digest pinned" >&2; exit 1; }
  printf '%s\t%s\n' "$name" "$image" >>"$temporary"
done

# imageID is the runtime-resolved identity. Prefer it over PodSpec image names,
# which may contain mutable chart defaults.
kubectl get pods -A -o json | jq -r '
  .items[] as $pod | $pod.status.containerStatuses[]? |
  select(.imageID | test("@sha256:[a-f0-9]{64}$")) |
  [$pod.metadata.namespace + "/" + $pod.metadata.name + "/" + .name,
   (.imageID | sub("^(docker-pullable|containerd)://"; ""))] | @tsv' >>"$temporary"

awk -F '\t' '!seen[$2]++' "$temporary" | while IFS=$'\t' read -r name image; do
  [[ "$image" =~ @sha256:[a-f0-9]{64}$ ]] || { echo "mutable or unresolved image: $name=$image" >&2; exit 1; }
  jq -nc --arg name "$name" --arg source "$image" '{name:$name,source:$source}'
done | jq -s --arg generated "$(date -u +%FT%TZ)" \
  '{schema:"platform.airgap-images/v1",profile:"on-prem-reference",generated_at:$generated,images:.}' >"$output"

jq -e '.images | length > 0 and all(.source | test("@sha256:[a-f0-9]{64}$"))' "$output" >/dev/null
echo "air-gap image lock written: $output"
