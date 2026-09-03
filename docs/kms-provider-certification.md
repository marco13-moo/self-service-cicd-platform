# KMS Provider Certification

Support is earned by executing the same digest-signing, policy-attestation,
verification, admission, denial, rotation, and revocation assertions—not by
accepting that a Cosign URI parses.

| Provider | Signer scheme | Workload identity | Local conformance | Managed certification |
| --- | --- | --- | --- | --- |
| Vault/OpenBao Transit | `hashivault://` / `openbao://` | Kubernetes auth | Passed 2026-09-03 | Pending target deployment |
| AWS KMS | `awskms://` | IRSA or EKS Pod Identity | N/A | Requires AWS tenant |
| Google Cloud KMS | `gcpkms://` | Workload Identity Federation | N/A | Requires GCP project |
| Azure Key Vault | `azurekms://` | Azure Workload Identity | N/A | Requires Azure subscription |

Certification evidence MUST record the cluster, signer key/version, immutable
test subject, signature and attestation references, denial tests, rotation and
revocation results, and execution timestamp. Credentials and tokens are never
recorded.
