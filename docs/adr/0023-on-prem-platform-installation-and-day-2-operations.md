# ADR 0023: On-Prem Platform Installation and Day-2 Operations

- Status: Accepted
- Date: 2026-09-14

## Context

ADR 0021 certified the on-prem provider substrate and ADR 0022 established the
developer golden path. Certification did not, however, install the control
plane itself as one continuously reconciled, digest-pinned system. Provider
readiness without control-plane installation, upgrade, recovery, and a genuine
tenant transaction is insufficient evidence of an operable platform.

## Decision

### GitOps is the installation authority

The on-prem platform is composed by the Kustomize overlay under
`infra/on-prem/platform`. Argo CD applies that overlay from a site-local Git
endpoint. The overlay reuses the canonical RBAC, WorkflowTemplates, admission
policies, observability rules, and control-plane workload; it does not fork
their semantics. Release automation replaces the control-plane image with the
digest from a verified `platform.release/v1` manifest before the revision is
made available to Argo CD.

PostgreSQL is the sole authoritative production state. The CloudNativePG
application secret is copied into the workload namespace by the installation
procedure without persisting its value in Git. JSON/PVC state is forbidden in
this profile. Database migrations execute during normal startup under the
transaction-scoped advisory lock defined by ADR 0017, so concurrent replicas
cannot race schema evolution.

### Availability and lifecycle

The control plane runs three replicas with zero-unavailable rolling updates, a
two-replica disruption budget, topology spreading, pod anti-affinity, readiness
and liveness probes, explicit resources, restricted container privileges, and a
45-second graceful-termination interval. Readiness covers PostgreSQL and Argo;
liveness remains process-local. Reconciliation leases persist in PostgreSQL and
expire after interruption, allowing another replica to resume work.

### Air-gap closure

`config/airgap/on-prem-images.lock.json` is the machine-readable image lock.
Every entry must be a digest reference. `mirror-on-prem-images.sh` copies each
image and its referrers to Harbor and emits a deterministic Kustomize image
rewrite file. The GitOps fixture is repository-local and is served through the
site Git endpoint; acceptance must neither fetch GitHub nor depend on another
SaaS provider.

### Acceptance and installation evidence

`validate-on-prem-platform.sh` is the singular acceptance entry point. It
verifies installation invariants, executes a two-tenant golden-path transaction,
interrupts reconciliation, proves lease and delivery recovery, exercises
digest-pinned upgrade and failed-rollout rollback, runs PostgreSQL backup and
restoration, and emits `platform.installation-evidence/v1`.

The resulting installation bill of materials binds the profile and release,
exact image digests, manifest hashes, Kubernetes and provider versions, schema
migration version, gate results, and creation time. It is signed as a blob with
the certified release key. Sanitized evidence may be committed; credentials,
private keys, bearer tokens, generated secret directories, and raw logs may not.

## Operational invariants

- Production desired state is reconciled by Argo CD from a site-local source.
- Every executable image is addressed by digest and present in the image lock.
- PostgreSQL is authoritative; no production PVC-backed JSON state exists.
- Installation never weakens tenant RLS, Pod Security, Kyverno, Cilium, or
  artifact-signing policy.
- Upgrade failure restores the preceding certified digest automatically.
- A passing provider-substrate certificate cannot substitute for a passing
  installation acceptance bundle.

## Consequences

- A certified provider profile becomes an installable and recoverable platform,
  rather than a collection of independently passing dependencies.
- Installation is reproducible without public registries or hosted source
  control after the mirror has been populated.
- Secret synchronization remains an explicit privileged installation step and
  is deliberately excluded from Git.
- Changing any locked image, provider version, manifest, or release invalidates
  the installation evidence and requires acceptance to be rerun.

## Rollout and rollback

1. Verify and mirror the certified release and image lock into site Harbor.
2. Materialize the candidate workspace and fixture into the site-local Git
   endpoint with `install-on-prem-platform.sh`.
3. Apply the Argo CD bootstrap `Application` and wait for health and sync.
4. Run the complete acceptance suite and sign its installation BOM.
5. Promote only the accepted digest. On failure, Argo CD and the acceptance
   harness restore the previous signed digest and retain failure evidence.
