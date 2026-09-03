# Vault KMS Conformance Evidence — 2026-09-03

## Result

Passed against `kind-self-service-cicd-test` using HashiCorp Vault Transit
1.20.4, Cosign 2.6.4, Kubernetes audience-bound projected tokens, and Kyverno
1.19.0.

## Immutable subject

`self-service-cicd-registry:5000/adr0014/admission@sha256:7c8cb692ae09657cbc4a3f3cbd0e8d5a2690ba38386aaaf252dbb060bf5eb2e6`

## Verified assertions

- Transit key `preview-signing` was created with `exportable=false` and
  `allow_plaintext_backup=false`.
- `argo/argo-env-admin` exchanged an audience `vault` ServiceAccount token for
  a five-minute Vault token.
- The token was held only in a memory-backed volume, owned by Cosign's non-root
  UID, with mode `0400`.
- The digest signature and custom vulnerability-policy attestation were created
  through `hashivault://preview-signing`.
- Separate public-key Jobs verified the signature and attestation.
- Kyverno admitted the signed digest under the isolated conformance policy.
- Deleting the signer policy caused a newly authenticated signing Job to fail.
- Re-bootstrap restored the conformance policy after the revocation assertion.

## Scope

This certifies the Vault/OpenBao-compatible KMS integration contract and test
harness. The Vault server intentionally runs in dev mode inside kind; it does
not certify production Vault storage, unseal, TLS, backup, or high availability.
Managed AWS, GCP, Azure, or production Vault certification still requires an
operator-provided tenant and workload-identity trust relationship.
