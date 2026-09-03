#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
image=${KMS_TEST_IMAGE:-}
if [[ ! "$image" =~ @sha256:[a-f0-9]{64}$ ]]; then
  echo "KMS_TEST_IMAGE must be a cluster-reachable digest reference" >&2
  exit 1
fi

"$repo_root/scripts/bootstrap-vault-kms-conformance.sh"
job_manifest=$(mktemp /tmp/vault-kms-sign-job.XXXXXX)
verify_manifest=$(mktemp /tmp/vault-kms-verify.XXXXXX)
trap 'rm -f "$job_manifest" "$verify_manifest"' EXIT
sed "s|KMS_TEST_IMAGE|$image|g" "$repo_root/infra/k8s/vault-kms-sign-job.yaml" > "$job_manifest"
sed "s|KMS_TEST_IMAGE|$image|g" "$repo_root/infra/k8s/vault-kms-verify.yaml" > "$verify_manifest"

kubectl -n argo delete job vault-kms-sign vault-kms-attest vault-kms-verify vault-kms-verify-attestation --ignore-not-found
kubectl apply -f "$job_manifest"
kubectl -n argo wait --for=condition=complete job/vault-kms-sign --timeout=5m
kubectl -n argo wait --for=condition=complete job/vault-kms-attest --timeout=5m
kubectl apply -f "$verify_manifest"
kubectl -n argo wait --for=condition=complete job/vault-kms-verify --timeout=5m
kubectl -n argo wait --for=condition=complete job/vault-kms-verify-attestation --timeout=5m

pod_manifest=$(mktemp /tmp/vault-kms-pod.XXXXXX)
trap 'rm -f "$job_manifest" "$verify_manifest" "$pod_manifest"' EXIT
kubectl -n kms-conformance run signed-subject --image="$image" --restart=Never --dry-run=client -o yaml > "$pod_manifest"
kubectl apply --dry-run=server -f "$pod_manifest" >/dev/null

# Revocation is tested against a newly authenticated workload. Existing tokens
# cannot regain a deleted policy, and no static credential is available.
vault_pod=$(kubectl -n kms-conformance get pod -l app=vault -o jsonpath='{.items[0].metadata.name}')
kubectl -n kms-conformance exec "$vault_pod" -- env VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN=conformance-root vault policy delete self-service-cicd-signer >/dev/null
kubectl -n argo delete job vault-kms-sign --ignore-not-found
kubectl apply -f "$job_manifest" >/dev/null
if kubectl -n argo wait --for=condition=complete job/vault-kms-sign --timeout=45s >/dev/null 2>&1; then
  echo "revoked signer unexpectedly retained signing authority" >&2
  exit 1
fi
kubectl -n argo wait --for=condition=failed job/vault-kms-sign --timeout=90s >/dev/null

"$repo_root/scripts/bootstrap-vault-kms-conformance.sh" >/dev/null
echo "Vault KMS identity, image and attestation signing, verification, admission, and revocation passed for $image"
