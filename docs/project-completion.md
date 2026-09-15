# Project Completion Record

- Status: Implementation complete; operational maintenance
- Completion baseline: `main` at or after ADR 0023
- Scope boundary: sovereign on-premises reference platform

## Delivered outcome

The repository implements the architectural sequence through ADR 0023: an
API-driven, highly available, multi-tenant control plane; PostgreSQL-authoritative
state; Argo Workflow lifecycle execution; authenticated provider-neutral SCM
ingestion; digest-only preview builds; signed artifact evidence; tenant identity,
authorization, network, and edge isolation; provider certification; developer
golden paths; and a reproducible on-premises GitOps installation.

The final installation slice adds a signed release boundary, complete air-gap
image lock, Harbor mirroring with OCI referrers, internal Git service, pinned
Argo CD and Argo Workflows providers, secret synchronization, upgrade and
rollback validation, PostgreSQL recovery validation, and a signed installation
bill of materials.

## Definition of done

Repository implementation is complete when all of the following remain true:

- ADRs 0001–0023 are accepted and represented by executable code or manifests.
- `main` contains no uncommitted completion work.
- Go tests and manifest/shell static validation pass.
- Pushes and pull requests are protected by the repository quality workflow.
- Every production executable image is digest-addressed in the generated lock.
- The on-premises install path has no runtime SaaS dependency.
- Operations, recovery, and user procedures are documented.

Each site installation has a separate acceptance boundary. It is complete only
when `validate-on-prem-platform.sh` produces a signed, passing
`platform.installation-evidence/v1` document for that site's exact Kubernetes,
provider versions, release, image lock, and rendered manifests. Site evidence
must not be generalized to a different cluster or release.

## Maintenance mode

Subsequent changes fall into one of four categories:

1. Security or dependency maintenance.
2. Provider or Kubernetes recertification.
3. Defect correction with regression evidence.
4. A new architectural capability governed by a new ADR.

Changing a provider version, locked image, release digest, rendered manifest,
or database migration invalidates prior installation evidence. The responsible
change must rerun the affected conformance suites and, before promotion, the
singular installation acceptance suite.

## Canonical handoff

- Entry point: [`README.md`](../README.md)
- Detailed usage: [`docs/guides/on-prem-usage-guide.md`](guides/on-prem-usage-guide.md)
- Architecture: [`docs/architecture.md`](architecture.md)
- Installation decision: [`docs/adr/0023-on-prem-platform-installation-and-day-2-operations.md`](adr/0023-on-prem-platform-installation-and-day-2-operations.md)
- Operations: [`docs/runbooks/on-prem-platform-operations.md`](runbooks/on-prem-platform-operations.md)
- Certification inputs: [`config/certification/on-prem-reference.yaml`](../config/certification/on-prem-reference.yaml)
- Immutable image inventory: [`config/airgap/on-prem-images.lock.json`](../config/airgap/on-prem-images.lock.json)
