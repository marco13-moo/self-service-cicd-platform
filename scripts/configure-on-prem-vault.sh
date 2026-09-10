#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
secret_dir=${ON_PREM_SECRET_DIR:-$root/.on-prem-secrets}
mkdir -p "$secret_dir"; chmod 0700 "$secret_dir"
kubectl -n vault wait --for=jsonpath='{.status.phase}'=Running pod/vault-0 --timeout=5m
if [[ ! -s "$secret_dir/vault-init.json" ]]; then
  umask 077
  kubectl -n vault exec vault-0 -- vault operator init -key-shares=1 -key-threshold=1 -format=json >"$secret_dir/vault-init.json"
fi
unseal=$(jq -er '.unseal_keys_b64[0]' "$secret_dir/vault-init.json")
root_token=$(jq -er '.root_token' "$secret_dir/vault-init.json")
kubectl -n vault exec vault-0 -- vault operator unseal "$unseal" >/dev/null 2>&1 || true
vault=(kubectl -n vault exec vault-0 -- env VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN="$root_token" vault)
"${vault[@]}" secrets enable transit >/dev/null 2>&1 || true
"${vault[@]}" write transit/keys/platform-release type=ecdsa-p256 exportable=false allow_plaintext_backup=false >/dev/null
"${vault[@]}" auth enable kubernetes >/dev/null 2>&1 || true
"${vault[@]}" write auth/kubernetes/config kubernetes_host=https://kubernetes.default.svc >/dev/null
kubectl create clusterrolebinding on-prem-vault-token-review --clusterrole=system:auth-delegator --serviceaccount=vault:vault --dry-run=client -o yaml | kubectl apply -f -
kubectl -n platform-system create serviceaccount vault-signer --dry-run=client -o yaml | kubectl apply -f -
policy=$(mktemp /tmp/on-prem-vault-policy.XXXXXX)
trap 'rm -f "$policy"' EXIT
printf '%s\n' 'path "transit/sign/platform-release" { capabilities = ["update"] }' 'path "transit/keys/platform-release" { capabilities = ["read"] }' >"$policy"
kubectl -n vault cp "$policy" vault-0:/tmp/platform-release.hcl
"${vault[@]}" policy write platform-release /tmp/platform-release.hcl >/dev/null
"${vault[@]}" write auth/kubernetes/role/platform-release bound_service_account_names=vault-signer bound_service_account_namespaces=platform-system audience=vault policies=platform-release token_ttl=5m token_max_ttl=10m >/dev/null
echo "on-prem Vault Transit initialized, unsealed, and bound to the vault-signer workload identity"
