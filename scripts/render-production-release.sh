#!/usr/bin/env bash
set -euo pipefail

for command in jq cosign envsubst; do command -v "$command" >/dev/null || { echo "missing required command: $command" >&2; exit 1; }; done
: "${1:?usage: render-production-release.sh RELEASE_JSON RELEASE_BUNDLE OUTPUT}"
: "${2:?release bundle is required}"
: "${3:?output path is required}"
: "${COSIGN_PUBLIC_KEY:?set COSIGN_PUBLIC_KEY to the certified verification key}"
: "${PREVIEW_DOMAIN:?set PREVIEW_DOMAIN}"
: "${LOAD_BALANCER_CIDR:?set the site-owned Cilium LoadBalancer CIDR}"
: "${BACKUP_DESTINATION:?set the encrypted object-store backup destination}"

cosign verify-blob --key "$COSIGN_PUBLIC_KEY" --bundle "$2" "$1" >/dev/null
export CONTROL_PLANE_IMAGE NETWORK_CONFORMANCE_IMAGE
CONTROL_PLANE_IMAGE=$(jq -er '.artifacts.control_plane.image' "$1")
NETWORK_CONFORMANCE_IMAGE=$(jq -er '.artifacts.network_conformance.image' "$1")
for image in "$CONTROL_PLANE_IMAGE" "$NETWORK_CONFORMANCE_IMAGE"; do
  [[ "$image" =~ @sha256:[a-f0-9]{64}$ ]] || { echo "release contains mutable image reference: $image" >&2; exit 1; }
  cosign verify --key "$COSIGN_PUBLIC_KEY" "$image" >/dev/null
done
envsubst '${CONTROL_PLANE_IMAGE} ${NETWORK_CONFORMANCE_IMAGE} ${PREVIEW_DOMAIN} ${LOAD_BALANCER_CIDR} ${BACKUP_DESTINATION}' <infra/production/platform.yaml.tmpl >"$3"
grep -Eq '@sha256:[a-f0-9]{64}' "$3" || { echo "rendered release has no immutable images" >&2; exit 1; }
