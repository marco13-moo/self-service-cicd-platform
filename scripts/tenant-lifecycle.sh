#!/usr/bin/env bash
set -euo pipefail

: "${PLATFORM_ENDPOINT:?set PLATFORM_ENDPOINT}"
: "${PLATFORM_TOKEN:?set an OIDC platform-administrator token}"
: "${1:?usage: tenant-lifecycle.sh onboard|suspend|offboard TENANT}"
: "${2:?tenant identifier is required}"
action=$1 tenant=$2
[[ "$tenant" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || { echo "invalid tenant identifier" >&2; exit 1; }
umask 077
auth_header=$(mktemp)
trap 'rm -f "$auth_header"' EXIT
printf 'Authorization: Bearer %s\n' "$PLATFORM_TOKEN" >"$auth_header"
headers=(-H "@$auth_header" -H 'Content-Type: application/json')
case "$action" in
  onboard)
    curl --fail-with-body --silent --show-error "${headers[@]}" -X POST "$PLATFORM_ENDPOINT/api/v1/admin/tenants" -d "{\"tenant_id\":\"$tenant\"}"
    echo "tenant database and Kubernetes boundaries requested; activation requires tenant isolation, identity, signing, audit, and onboarding conformance" >&2
    ;;
  suspend)
    curl --fail-with-body --silent --show-error "${headers[@]}" -X PATCH "$PLATFORM_ENDPOINT/api/v1/admin/tenants/$tenant/status" -d '{"status":"suspended"}'
    ;;
  offboard)
    # Offboarding is intentionally two-phase: suspension immediately revokes
    # mutation and ingress, while evidence retention/deletion remains governed.
    curl --fail-with-body --silent --show-error "${headers[@]}" -X PATCH "$PLATFORM_ENDPOINT/api/v1/admin/tenants/$tenant/status" -d '{"status":"suspended"}'
    echo "tenant suspended; complete export/retention verification before destructive deletion" >&2
    ;;
  *) echo "unsupported action: $action" >&2; exit 1;;
esac
