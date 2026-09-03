# ADR 0016: Evidence Retention, Trust Rotation, and Recovery

## Status

Accepted

## Context

ADR 0015 makes deployment contingent on a KMS-backed signature and signed
vulnerability-policy evidence. That admission decision remains trustworthy only
while the OCI subject, its referrers, the verification roots, and the audit trail
survive their independent operational lifecycles. Registry garbage collection,
an incomplete backup, an unsafe verifier cutover, or post-deployment revocation
can otherwise convert a valid deployment into unverifiable or unauthorized
state.

## Decision

### Evidence is one recoverable aggregate

The digest-pinned image manifest, every referenced platform manifest, Cosign
signature tags, SBOM and provenance attestations, vulnerability-policy
attestations, and registry access metadata form one evidence aggregate. Retention
and backup implementations MUST preserve the aggregate for at least as long as
the workload can run or be reconstructed. A digest is not recoverable when only
its image layers survive.

Registry conformance recursively copies the subject, digest tags, and OCI 1.1
referrers into an isolated recovery repository. It verifies signatures and policy
attestations before backup, after retention or garbage collection, and after
restore. Recovery is accepted only when server-side admission accepts the
restored digest.

### Rotation is an overlap protocol

Trust rotation is a four-state protocol:

1. **Prepared** — create a new non-exportable KMS key version and publish its
   public key as the candidate verifier.
2. **Overlapping** — admission trusts both current and candidate roots while the
   signer emits signatures from the candidate version.
3. **Promoted** — a fresh subject is signed, independently verified, and admitted
   using the candidate root.
4. **Retired** — the previous verifier is removed only after the overlap
   observation window and rollback checkpoint have elapsed.

Failure before promotion restores the previous admission policy. Emergency
revocation may skip the observation window, but MUST quarantine subjects whose
only valid signature chains to the revoked root.

### Continuous re-verification is fail closed

An Argo CronWorkflow periodically enumerates managed preview Deployments and
creates isolated Cosign verification Jobs. Both the image signature and the
versioned vulnerability-policy attestation must verify against the active public
trust bundle. A failure labels and annotates the namespace, scales the affected
Deployment to zero, and leaves recovery as an explicit operator decision.

The re-verifier has no signing credential, Vault token, or KMS permission. Its
authority is limited to reading managed Deployments, creating short-lived
verification Jobs in `argo`, and quarantining the failed Deployment and
namespace.

### Recovery objectives and evidence

Production operators define `EVIDENCE_RPO` and `EVIDENCE_RTO`; recommended
initial objectives are a 24-hour RPO and four-hour RTO. Each exercise records:

- immutable source and recovered subjects;
- registry implementation and version;
- retained signature, SBOM, provenance, and policy-attestation checks;
- admission replay result;
- rotation key versions and overlap timestamps;
- quarantine and revocation outcomes; and
- measured RPO and RTO.

Credentials, identity tokens, KMS private material, and registry authorization
headers are never retained as evidence.

## Consequences

- Garbage collection and disaster recovery become security controls rather than
  storage-only procedures.
- Key rotation does not introduce an admission outage or silently orphan running
  subjects.
- A subject that becomes unverifiable is automatically contained.
- Recursive OCI copy support is a production registry acceptance requirement.
- Quarantine is intentionally not self-clearing; restoration requires a newly
  verified artifact or an audited operator action.

## Operational invariants

- Never delete a subject while a running Deployment or retained environment
  references its digest.
- Never retire the old verifier before a candidate-signed subject passes direct
  verification and server-side admission.
- Never grant KMS signing permission to the re-verification identity.
- Never treat a successful blob restore as evidence recovery without signature,
  attestation, and admission replay.
- Never garbage-collect the primary registry as part of an unscoped test; use an
  isolated conformance registry or an explicitly approved maintenance window.
- Every quarantine transition must be attributable and idempotent.

## Conformance

The executable contracts are:

- `scripts/validate-registry-evidence-lifecycle.sh` for retention, recursive
  backup, restore, verification, and admission replay;
- `scripts/rotate-vault-signing-key.sh` for overlap-based Transit rotation; and
- `argo/cronworkflows/evidence-reverification.yaml` for periodic verification
  and quarantine.

The conformance manifests deliberately use isolated namespaces, repositories,
and trust Secrets. Managed-provider and production-registry certification must
record results in `docs/evidence/` before the combination is declared supported.
