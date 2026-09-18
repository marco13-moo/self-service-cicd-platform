# Confidential compute threat model

The control plane admits a workload only after tenant policy verification. The
decision is bound to a policy digest and is projected into the Argo workflow
and namespace metadata. Workloads receive an external workload-identity
reference with a credential lifetime of at most 15 minutes; secret fields are
never accepted as policy values. Tenant encryption keys are named and checked
against the tenant identity. Namespaces carry default-deny network isolation,
residency, confidentiality, attestation, identity, and key metadata. Audit
events are append-only and hash-chained per tenant.

## Addressed threats

| Threat | Control |
| --- | --- |
| Compromised workload | Default-deny network policy, tenant-scoped namespace/service account, short-lived identity, no automatic service-account token mounting, and Kyverno identity/image admission. |
| Compromised node | Confidential workloads require an attestation profile and the admission decision is carried into execution metadata; deployment remains fail-closed when policy is incomplete. |
| Malicious tenant | Tenant-scoped storage/RLS, tenant-prefixed key validation, exact egress declarations, residency allow-lists, and tenant-bound workflow labels. |
| Stolen credential | Credentials are external-provider references with a bounded lifetime; the platform does not mint or persist long-lived workload secrets. |
| Supply-chain attack | Existing digest-pinned build/admission, signature verification, vulnerability policy, provenance/SBOM, and policy-attestation workflow stages remain prerequisites for deployment. |

## Not addressed by this slice

Attestation verification is still performed by the configured cluster admission
provider; the control plane validates that a profile is declared but does not
implement a TEE verifier. Cloud-provider IAM policy correctness, compromised
hypervisors, malicious control-plane operators, and compromise of the external
Vault/KMS/identity provider remain operational or provider responsibilities.
Hash chaining detects mutation or omission when the chain is exported, but does
not prevent a database administrator from deleting the entire database or
rewriting both rows and an unsealed copy. Export and independent sealing remain
required for forensic durability.
