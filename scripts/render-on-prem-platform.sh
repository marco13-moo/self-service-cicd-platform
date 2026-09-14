#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
: "${1:?usage: render-on-prem-platform.sh RELEASE_JSON RELEASE_BUNDLE OUTPUT}"
: "${2:?release bundle is required}"
: "${3:?output path is required}"
: "${COSIGN_PUBLIC_KEY:?set COSIGN_PUBLIC_KEY to the certified release verification key}"
: "${AIRGAP_IMAGE_LOCK:?set AIRGAP_IMAGE_LOCK to the complete generated image lock}"
: "${AIRGAP_REGISTRY:?set AIRGAP_REGISTRY to the site Harbor hostname/project}"
for command in jq cosign kubectl sed; do command -v "$command" >/dev/null || { echo "missing required command: $command" >&2; exit 1; }; done

cosign verify-blob --key "$COSIGN_PUBLIC_KEY" --bundle "$2" "$1" >/dev/null
image=$(jq -er '.artifacts.control_plane.cluster_image' "$1")
[[ "$image" =~ @sha256:[a-f0-9]{64}$ ]] || { echo "control-plane image is not digest pinned" >&2; exit 1; }
locked_image() {
  local name=$1 source digest
  source=$(jq -er --arg name "$name" '.images[] | select(.name == $name) | .source' "$AIRGAP_IMAGE_LOCK")
  digest=${source##*@}
  [[ "$digest" =~ ^sha256:[a-f0-9]{64}$ ]] || { echo "$name has no immutable digest" >&2; exit 1; }
  printf '%s/%s@%s' "${AIRGAP_REGISTRY%/}" "$name" "$digest"
}
builder=$(locked_image preview-builder)
scanner=$(locked_image preview-scanner)
signer=$(locked_image preview-cosign)
vault=$(locked_image preview-vault)
kubectl kustomize "$root" | sed -E \
  -e "s#image: [^[:space:]]*/library/control-plane@sha256:[a-f0-9]{64}#image: $image#" \
  -e "s#value: moby/buildkit:v0.33.0-rootless#value: $builder#" \
  -e "s#value: aquasec/trivy:0.74.0#value: $scanner#" \
  -e "s#value: ghcr.io/sigstore/cosign/cosign:v2.6.4#value: $signer#" \
  -e "s#value: hashicorp/vault:1.20.4#value: $vault#" >"$3"
grep -Fq "image: $image" "$3" || { echo "release image was not rendered" >&2; exit 1; }
if grep -Eq '(image|value):[[:space:]]+[^[:space:]@]+:[^[:space:]]+$' "$3"; then
  echo "rendered installation contains a mutable image tag" >&2
  exit 1
fi
