# ADR 0014: Signed Artifacts, Admission Enforcement, and VEX Governance

## Status

Accepted

## Context

ADR 0013 makes the image digest the common identity for construction, scanning,
attestation, and rollout. Digest identity prevents mutation, but does not prove
who authorized the artifact. Its severity gate also needs a constrained route
for legitimate non-exploitability decisions without normalizing opaque ignore
lists.

## Decision

The deployment DAG MUST sign the digest-pinned OCI index with Cosign only after
the vulnerability gate succeeds. A distinct node MUST verify that signature
with a public key before Kubernetes mutation. The signing Secret contains
`cosign.key` and an optional `password`; verification uses a separate,
public-only Secret containing `cosign.pub`. Registry credentials and signing
material are mounted read-only and never enter workflow parameters, outputs, or
control-plane state.

Preview namespaces are labelled into a fail-closed Kyverno `verifyImages`
policy. Admission independently retrieves the public-only
`argo/preview-cosign-public` Secret, requires digest references, and verifies a
signature rooted in that key. The workflow verification gate is
therefore rapid feedback, not the security boundary. The admission controller
remains authoritative if a caller bypasses Argo.

Kyverno receives a resource-name-constrained Role in `argo`: it may read only
the public-key Secret and cannot enumerate Secrets or retrieve the private key.

Vulnerability exceptions MUST be expressed as OpenVEX, never scanner-specific
ignore files. An operator opts a deployment into one named `preview-vex-*`
ConfigMap. Kubernetes admission requires explicit governance metadata, an
approver, a review ticket, an RFC3339 UTC expiry, `openvex.json`, and a mirrored
`governance.env`. The scanner refuses incomplete or expired governance data on
every execution before passing the document to Trivy. Absence of a configured
VEX document means no exceptions.

The initial signing profile uses offline-managed asymmetric keys because vanilla
Kubernetes does not provide a universally portable Fulcio-compatible workload
identity. Production installations SHOULD replace the signer with KMS or
keyless workload identity; that migration must preserve separate signing and
admission trust configuration. Rekor is not required for this private-key
profile, so both workflow and Kyverno verification explicitly ignore the
transparency log. This is not equivalent to keyless transparency-backed signing.
Cosign 2.6 and legacy OCI signature tags are pinned for compatibility with
distribution-spec registries and Kyverno's stable `verifyImages` API. A future
move to Cosign 3 OCI 1.1 referrers and Kyverno `ImageValidatingPolicy` requires
registry conformance testing and is deliberately not an implicit upgrade.

## Consequences

- Unsigned, mutable-tag, or incorrectly signed preview images are rejected even
  when submitted outside the platform workflow.
- Compromise of registry write credentials alone cannot authorize a workload.
- Signing-key rotation requires coordinated Secret rotation and may use an
  overlap policy with multiple attestors.
- VEX decisions are attributable and expire automatically at the next scan;
  Kubernetes admission validates structure but cannot autonomously evict a
  workload when an exception expires.
- Kyverno is now a required cluster dependency before preview namespaces are
  labelled for enforcement.

## Operational contract

Generate keys outside the repository and create split Secrets:

```bash
cosign generate-key-pair
kubectl -n argo create secret generic preview-cosign-private \
  --from-file=cosign.key --from-literal=password="$COSIGN_PASSWORD"
kubectl -n argo create secret generic preview-cosign-public \
  --from-file=cosign.pub
```

Never commit the generated private key or its password. Install Kyverno, apply
the signature and VEX policies, then apply the Argo templates. A VEX document is
enabled only by setting `PREVIEW_VEX_CONFIGMAP` to its reviewed ConfigMap name.
Admission verification requires a registry secured by publicly trusted TLS or
an explicitly configured private CA; the checked-in policy never permits plain
HTTP or disables certificate validation.

## Follow-up

- Adopt cloud workload identity or KMS signing with transparency evidence.
- Sign and admission-verify dedicated scan and policy-result attestations.
- Add an exception controller that alerts before expiry and revokes running
  workloads when organizational policy requires immediate reevaluation.
