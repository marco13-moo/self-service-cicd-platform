#!/usr/bin/env bash
set -euo pipefail

kubectl -n argo delete job vault-kms-sign vault-kms-attest vault-kms-verify vault-kms-verify-attestation --ignore-not-found
kubectl -n argo delete configmap vault-kms-policy-predicate --ignore-not-found
kubectl -n argo delete secret preview-cosign-public-conformance preview-cosign-public-candidate-conformance --ignore-not-found
kubectl -n argo delete rolebinding kyverno-vault-kms-conformance-key-reader --ignore-not-found
kubectl -n argo delete role kyverno-vault-kms-conformance-key-reader --ignore-not-found
kubectl delete clusterpolicy vault-kms-conformance vault-kms-rotation-conformance registry-evidence-recovery-conformance --ignore-not-found
kubectl delete clusterrolebinding vault-kms-conformance-token-reviewer --ignore-not-found
kubectl delete namespace kms-conformance --ignore-not-found

echo "Vault KMS conformance resources removed"
