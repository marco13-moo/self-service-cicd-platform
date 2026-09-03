#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
vault_namespace=kms-conformance
vault_address=http://vault.kms-conformance.svc:8200

for binary in kubectl jq; do
  command -v "$binary" >/dev/null || { echo "$binary is required" >&2; exit 1; }
done

kubectl apply -f "$repo_root/infra/k8s/vault-kms-conformance.yaml"
kubectl -n "$vault_namespace" rollout status deployment/vault --timeout=5m
vault_pod=$(kubectl -n "$vault_namespace" get pod -l app=vault -o jsonpath='{.items[0].metadata.name}')

vault_exec=(kubectl -n "$vault_namespace" exec "$vault_pod" -- env VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN=conformance-root vault)
"${vault_exec[@]}" secrets enable transit >/dev/null 2>&1 || true
"${vault_exec[@]}" write transit/keys/preview-signing type=ecdsa-p256 exportable=false allow_plaintext_backup=false >/dev/null
"${vault_exec[@]}" auth enable kubernetes >/dev/null 2>&1 || true
"${vault_exec[@]}" write auth/kubernetes/config kubernetes_host=https://kubernetes.default.svc >/dev/null

policy_file=$(mktemp /tmp/vault-kms-policy.XXXXXX)
trap 'rm -f "$policy_file"' EXIT
printf '%s\n' \
  'path "transit/keys/preview-signing" { capabilities = ["read"] }' \
  'path "transit/sign/preview-signing" { capabilities = ["update"] }' \
  'path "transit/sign/preview-signing/*" { capabilities = ["update"] }' > "$policy_file"
kubectl -n "$vault_namespace" cp "$policy_file" "$vault_pod:/tmp/signer.hcl"
"${vault_exec[@]}" policy write self-service-cicd-signer /tmp/signer.hcl >/dev/null
"${vault_exec[@]}" write auth/kubernetes/role/self-service-cicd-signer \
  bound_service_account_names=argo-env-admin \
  bound_service_account_namespaces=argo \
  audience=vault policies=self-service-cicd-signer token_ttl=5m token_max_ttl=10m >/dev/null

public_key=$("${vault_exec[@]}" read -format=json transit/keys/preview-signing | jq -r '.data.keys["1"].public_key')
test -n "$public_key" && test "$public_key" != null
kubectl -n argo create secret generic preview-cosign-public-conformance \
  --from-literal=cosign.pub="$public_key" --dry-run=client -o yaml | kubectl apply -f -

cat <<EOF
Vault Transit conformance signer is ready.
  signer: hashivault://preview-signing
  address: $vault_address
  auth role: self-service-cicd-signer
EOF
