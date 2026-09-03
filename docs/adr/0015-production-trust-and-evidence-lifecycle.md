# ADR 0015: Production Trust and Evidence Lifecycle

## Status

Accepted

## Context

ADR 0014 establishes cryptographic authorization and governed VEX exceptions,
but its development profile keeps a file-backed signing key and records only
the image subject. Production operation requires non-exportable signing
material, workload-scoped authorization, independently verifiable policy
results, rotation without downtime, and evidence whose retention is decoupled
from Workflow pods.

## Decision

### Workload-identity-backed signing

The control plane supplies an opaque Cosign signer URI. Development may retain
`/cosign-private/cosign.key`; production MUST use a supported KMS URI such as
`awskms://`, `gcpkms://`, `azurekms://`, or `hashivault://`. Credentials are
never accepted through API objects or workflow parameters. The
`argo-env-admin` ServiceAccount obtains narrowly scoped KMS authorization from
the cluster's workload-identity mechanism.

The KMS authorization MUST permit signing with one designated key but not key
creation, deletion, policy mutation, or unrelated cryptographic operations.
Registry credentials remain independently scoped. Compromise of either
credential alone is therefore insufficient to introduce an admitted artifact.

### Signed policy evidence

After the digest-pinned vulnerability gate succeeds, Argo emits a versioned
in-toto predicate containing the subject digest, source revision, scanner
identity, blocking severity set, unfixed policy, VEX document identity, and
`passed` result. Cosign signs and publishes that attestation against the same
OCI subject. A distinct workflow node verifies the attestation before rollout.

The OCI registry is the durable evidence store for image signatures, BuildKit
SBOM/provenance, and vulnerability-policy attestations. Environment state stores
immutable subject references for discovery. Registry retention policy MUST keep
referrers and legacy signature tags for at least the corresponding image's
retention period; garbage collection MUST treat them as a unit.

### Trust rotation and revocation

The public-key Secret is the admission trust bundle and may contain overlapping
old and new public keys during rotation. Rotation proceeds add new verifier,
switch signer, verify new signatures, then remove old verifier. Emergency
revocation removes the compromised verifier first and triggers redeployment or
quarantine of affected subjects. Private keys are never copied between KMS and
Kubernetes.

### Transport and recovery

Production registries MUST use trusted TLS. Plain HTTP and certificate bypasses
are development-only workflow options and remain disabled in admission policy.
Registry backups must preserve manifests, referrers, signature tags, and access
logs. Recovery is complete only after Cosign verification and admission replay
against restored subjects succeed.

## Consequences

- Production signing keys become non-exportable and access is attributable to
  workload identity.
- A successful scan is independently signed evidence rather than an inferred
  consequence of workflow completion.
- Control-plane state remains compact while evidence survives Argo retention.
- The OCI registry and KMS become availability dependencies; failure is closed.
- Operators must coordinate key rotation, registry garbage collection, and
  admission trust as one lifecycle.

## Operational invariants

- Never grant the control-plane Deployment KMS signing permission; only the
  Argo execution identity signs.
- Never place cloud credentials, KMS secrets, private keys, or identity tokens
  in workflow parameters.
- Alert on attestation/signature verification failure, evidence deletion,
  signing-key use outside the Argo identity, and VEX expiry.
- Test rotation and registry restore in a non-production cluster before every
  trust-root change.

## Follow-up

- Add registry retention conformance and disaster-recovery automation.
- Migrate to Cosign 3 OCI 1.1 referrers and Kyverno ImageValidatingPolicy once
  the selected production registry passes compatibility tests.
- Add periodic evidence re-verification and compromised-subject quarantine.
