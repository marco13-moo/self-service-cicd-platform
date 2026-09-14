#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
: "${RENDERED_PLATFORM_MANIFEST:?set RENDERED_PLATFORM_MANIFEST}"
for command in kubectl; do command -v "$command" >/dev/null || { echo "missing required command: $command" >&2; exit 1; }; done

# CNPG-generated credentials are copied in memory; secret material is never
# written to the repository or to an intermediate plaintext file.
kubectl create namespace control-plane --dry-run=client -o yaml | kubectl apply -f -
database_url=$(kubectl -n platform-database get secret platform-postgres-app -o jsonpath='{.data.uri}')
[[ -n "$database_url" ]] || { echo "CloudNativePG application URI is unavailable" >&2; exit 1; }
kubectl -n control-plane create secret generic control-plane-database \
  --from-literal=url="$(printf '%s' "$database_url" | base64 -d)" --dry-run=client -o yaml | kubectl apply -f -
kubectl apply --server-side --field-manager=platform-bootstrap -f "$RENDERED_PLATFORM_MANIFEST"
kubectl -n control-plane rollout status deployment/control-plane --timeout=10m
