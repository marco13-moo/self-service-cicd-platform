#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
"$root/scripts/configure-on-prem-vault.sh" >/dev/null
jwt=$(kubectl -n platform-system create token vault-signer --audience=vault --duration=10m)
login=$(kubectl -n vault exec vault-0 -- env VAULT_ADDR=http://127.0.0.1:8200 vault write -format=json auth/kubernetes/login role=platform-release jwt="$jwt")
client_token=$(jq -er '.auth.client_token' <<<"$login")
digest=$(printf 'on-prem-reference-certification' | openssl dgst -sha256 -binary | base64)
signature=$(kubectl -n vault exec vault-0 -- env VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN="$client_token" vault write -format=json transit/sign/platform-release input="$digest" | jq -er '.data.signature')
[[ "$signature" =~ ^vault:v[0-9]+: ]] || { echo "Vault Transit returned an invalid signature" >&2; exit 1; }
echo "Vault Kubernetes workload identity and non-exportable Transit signing passed"
