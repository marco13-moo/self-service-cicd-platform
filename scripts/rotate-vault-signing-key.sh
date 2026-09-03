#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
image=${ROTATION_TEST_IMAGE:-${KMS_TEST_IMAGE:-}}
candidate_key=${VAULT_CANDIDATE_KEY:-preview-signing-candidate-$(date -u +%Y%m%d%H%M%S)}
vault_namespace=kms-conformance

if [[ ! "$image" =~ @sha256:[a-f0-9]{64}$ ]]; then
  echo "ROTATION_TEST_IMAGE must be a cluster-reachable digest reference" >&2
  exit 1
fi
for binary in kubectl jq sed; do
  command -v "$binary" >/dev/null || { echo "$binary is required" >&2; exit 1; }
done

"$repo_root/scripts/bootstrap-vault-kms-conformance.sh" >/dev/null
vault_pod=$(kubectl -n "$vault_namespace" get pod -l app=vault -o jsonpath='{.items[0].metadata.name}')
vault_exec=(kubectl -n "$vault_namespace" exec "$vault_pod" -- env VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN=conformance-root vault)

# A distinct candidate key lets admission overlap precede signer cutover. Vault
# key-version rotation alone would make the signer switch before the verifier is
# staged, creating a small but genuine availability hazard.
"${vault_exec[@]}" write "transit/keys/$candidate_key" \
  type=ecdsa-p256 exportable=false allow_plaintext_backup=false >/dev/null
candidate_public=$("${vault_exec[@]}" read -format=json "transit/keys/$candidate_key" | jq -r '.data.keys["1"].public_key')
test -n "$candidate_public" && test "$candidate_public" != null
kubectl -n argo create secret generic preview-cosign-public-candidate-conformance \
  --from-literal=cosign.pub="$candidate_public" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

policy_file=$(mktemp /tmp/vault-kms-rotation-policy.XXXXXX)
sign_manifest=$(mktemp /tmp/vault-kms-rotation-sign.XXXXXX)
verify_manifest=$(mktemp /tmp/vault-kms-rotation-verify.XXXXXX)
pod_manifest=$(mktemp /tmp/vault-kms-rotation-pod.XXXXXX)
cleanup() {
  rm -f "$policy_file" "$sign_manifest" "$verify_manifest" "$pod_manifest"
  kubectl -n argo delete job vault-kms-rotate-sign vault-kms-rotate-attest vault-kms-rotate-verify vault-kms-rotate-verify-attestation --ignore-not-found >/dev/null 2>&1 || true
  kubectl delete clusterpolicy vault-kms-rotation-conformance --ignore-not-found >/dev/null 2>&1 || true
  kubectl -n argo delete secret preview-cosign-public-candidate-conformance --ignore-not-found >/dev/null 2>&1 || true
}
trap cleanup EXIT

printf '%s\n' \
  'path "transit/keys/preview-signing" { capabilities = ["read"] }' \
  'path "transit/sign/preview-signing" { capabilities = ["update"] }' \
  'path "transit/sign/preview-signing/*" { capabilities = ["update"] }' \
  "path \"transit/keys/$candidate_key\" { capabilities = [\"read\"] }" \
  "path \"transit/sign/$candidate_key\" { capabilities = [\"update\"] }" \
  "path \"transit/sign/$candidate_key/*\" { capabilities = [\"update\"] }" > "$policy_file"
kubectl -n "$vault_namespace" cp "$policy_file" "$vault_pod:/tmp/rotation-signer.hcl" >/dev/null
"${vault_exec[@]}" policy write self-service-cicd-signer /tmp/rotation-signer.hcl >/dev/null

sed -e 's/name: vault-kms-sign/name: vault-kms-rotate-sign/' \
  -e 's/name: vault-kms-attest/name: vault-kms-rotate-attest/' \
  -e "s|hashivault://preview-signing|hashivault://$candidate_key|g" \
  -e "s|KMS_TEST_IMAGE|$image|g" "$repo_root/infra/k8s/vault-kms-sign-job.yaml" > "$sign_manifest"
sed -e 's/name: vault-kms-verify/name: vault-kms-rotate-verify/' \
  -e 's/name: vault-kms-verify-attestation/name: vault-kms-rotate-verify-attestation/' \
  -e 's/preview-cosign-public-conformance/preview-cosign-public-candidate-conformance/g' \
  -e "s|KMS_TEST_IMAGE|$image|g" "$repo_root/infra/k8s/vault-kms-verify.yaml" > "$verify_manifest"

# Stage overlap first, then switch the conformance signer to the candidate.
kubectl apply -f "$repo_root/infra/k8s/vault-kms-rotation-policy.yaml" >/dev/null
kubectl apply -f "$sign_manifest" >/dev/null
kubectl -n argo wait --for=condition=complete job/vault-kms-rotate-sign --timeout=5m >/dev/null
kubectl -n argo wait --for=condition=complete job/vault-kms-rotate-attest --timeout=5m >/dev/null
kubectl apply -f "$verify_manifest" >/dev/null
kubectl -n argo wait --for=condition=complete job/vault-kms-rotate-verify --timeout=5m >/dev/null
kubectl -n argo wait --for=condition=complete job/vault-kms-rotate-verify-attestation --timeout=5m >/dev/null

kubectl -n kms-conformance run rotation-subject --image="$image" \
  --restart=Never --dry-run=client -o yaml > "$pod_manifest"
kubectl apply --dry-run=server -f "$pod_manifest" >/dev/null

# Promotion updates the canonical verifier only after direct verification and
# admission have succeeded. The old key remains in Vault for audited rollback.
kubectl -n argo create secret generic preview-cosign-public-conformance \
  --from-literal=cosign.pub="$candidate_public" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl apply -f "$repo_root/infra/k8s/vault-kms-policy.yaml" >/dev/null
kubectl -n argo delete secret preview-cosign-public-candidate-conformance --ignore-not-found >/dev/null

echo "Vault signing-key overlap, candidate cutover, verification, admission, and promotion passed for $image"
