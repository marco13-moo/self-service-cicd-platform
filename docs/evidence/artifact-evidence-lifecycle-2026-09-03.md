# Artifact Evidence Lifecycle Conformance — 2026-09-03

## Scope

- Cluster: `kind`, Kubernetes 1.34 local conformance cluster
- Primary registry: Distribution Registry 2 on the kind Docker network
- Backup and recovery: two independently empty, disposable Registry 2 instances
- KMS: Vault 1.20.4 Transit with Kubernetes audience-bound authentication
- Verifier: Cosign 2.6.4
- Admission: Kyverno ClusterPolicy conformance boundary
- Subject: `self-service-cicd-registry:5000/previews/pms-wiki@sha256:0bf6406a676cd047ce124f75c631956696d154e915d8a48b120687762d109d4f`

## Verified assertions

### Evidence retention and recovery

- The immutable OCI index contains a BuildKit attestation manifest.
- The attestation manifest contains both an SPDX predicate and SLSA provenance
  v1 predicate.
- Recursive copy preserved the image graph, digest tags, and OCI referrers in an
  isolated backup registry.
- Registry garbage collection completed against the isolated backup, never the
  primary registry.
- A second recursive copy restored the aggregate into an independently empty
  recovery registry.
- Recovered SPDX and provenance descriptor digests matched the source.
- The recovered Cosign signature and signed vulnerability-policy attestation
  verified with the public-only conformance trust root.
- Kyverno server-side admission replay accepted the recovered digest.

### Rotation

- A distinct non-exportable Transit candidate key was created.
- The current and candidate verifiers overlapped before signer cutover.
- The candidate signed both the image and policy predicate through a short-lived
  Vault token obtained by the Argo ServiceAccount.
- Direct signature and attestation verification passed with the candidate root.
- Admission accepted the candidate-signed digest before promotion.
- The candidate public key replaced the canonical conformance verifier only
  after all checkpoints passed; the former KMS key remained available for
  audited rollback.

### Continuous re-verification and quarantine

- A scheduled Argo re-verification run discovered a managed Deployment carrying
  a forged digest.
- Signature verification failed closed.
- The namespace received `platform.preview-quarantine=true`, the failure reason,
  and an observation timestamp.
- The offending Deployment recorded its image and was patched to zero replicas.
- The workflow itself finished Failed after containment, preserving an alertable
  operational signal.
- The production schedule was restored to six-hour execution and all isolated
  quarantine-test resources were removed.

## Limitations

This certifies the provider-neutral executable contract in a local cluster. It
does not certify a production registry's retention configuration, object-store
versioning, geographic replication, managed KMS, recovery time, or recovery
point. Those properties require the same harness plus provider-specific backup
evidence in the target environment. Kyverno also reports that `ClusterPolicy`
is deprecated; migration to `ImageValidatingPolicy` remains gated on the chosen
production Kyverno and registry compatibility matrix.
