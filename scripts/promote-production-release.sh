#!/usr/bin/env bash
set -euo pipefail

: "${1:?usage: promote-production-release.sh RENDERED_MANIFEST}"
: "${PROMOTION_NAMESPACE:=platform-system}"
manifest=$1
kubectl diff --server-side --field-manager=platform-gitops -f "$manifest" || diff_status=$?
if [[ ${diff_status:-0} -gt 1 ]]; then echo "unable to evaluate policy drift" >&2; exit 1; fi
previous=$(kubectl -n "$PROMOTION_NAMESPACE" get deployment control-plane -o jsonpath='{.spec.template.spec.containers[0].image}' 2>/dev/null || true)
kubectl apply --server-side --field-manager=platform-gitops -f "$manifest"
if ! kubectl -n "$PROMOTION_NAMESPACE" rollout status deployment/control-plane --timeout=10m; then
  if [[ "$previous" =~ @sha256:[a-f0-9]{64}$ ]]; then kubectl -n "$PROMOTION_NAMESPACE" set image deployment/control-plane control-plane="$previous"; fi
  echo "rollout failed; preceding immutable image restored" >&2; exit 1
fi
RELEASE_MANIFEST="${RELEASE_MANIFEST:?}" ./scripts/certify-production-providers.sh
