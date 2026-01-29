# ADR 0005: Ephemeral Environments via Argo WorkflowTemplates

## Status
Accepted

## Context

The platform requires support for **on-demand, short-lived (ephemeral) environments**
to enable workflows such as:

- Per-PR environments
- Preview deployments
- Isolated service testing
- Future promotion and deployment flows

At this stage of the platform (Phase 5), the goal is to introduce ephemeral environments
**without prematurely coupling** the control plane to Kubernetes internals, controllers,
or Argo SDKs.

Key constraints and guiding principles:

- The control plane must remain a **pure intent submission system**
- Execution concerns (pods, retries, namespaces) must live outside the control plane
- The solution must be production-real, not a mock
- Architectural reversibility is required (no early CRDs, controllers, or GitOps)

Argo Workflows is already in use as the execution engine for CI workflows, making it a
natural candidate for environment lifecycle orchestration.

## Decision

Ephemeral environments are implemented using **Argo WorkflowTemplates** that create and
destroy Kubernetes namespaces.

The control plane introduces:

- An `EnvironmentSpec` domain model
- An `EnvironmentOrchestrator` interface
- An Argo-backed implementation (`ArgoEnvironmentOrchestrator`)

The control plane submits **workflow intent only**, referencing pre-installed
`WorkflowTemplate` resources:

- `env-create-template`
- `env-destroy-template`

The control plane does **not**:
- Create Kubernetes resources directly
- Watch workflow status (yet)
- Own retries, pods, or execution logic

The namespace is used as the isolation boundary for an environment.

## Implementation Details

- Environments map 1:1 to Kubernetes namespaces
- Namespace lifecycle is managed by Argo workflows
- WorkflowTemplates live in the `argo/workflowtemplates/` directory
- Parameters are passed explicitly and form a stable execution contract

Example parameters:

- `env_name`
- `service`
- `expires_at`

The control plane submits workflows using the Argo CLI (not the Go SDK).

## Consequences

### Positive

- Clear separation between control plane and execution plane
- No Kubernetes API coupling in the control plane
- Execution logic is declarative and inspectable
- Easy to reason about, debug, and evolve
- Aligns with existing Argo-based CI workflows

### Trade-offs

- No automatic TTL cleanup yet
- No workflow status tracking or log streaming
- Manual cleanup is required if workflows fail
- Namespace-per-environment may be coarse-grained for some future use cases

These trade-offs are intentional and deferred to later phases.

## Alternatives Considered

### Kubernetes Controllers / CRDs
Rejected due to:
- High coupling
- Operational complexity
- Reduced reversibility

### Argo Go SDK
Deferred to Phase 7 when the control plane runs in-cluster and requires tighter integration.

### GitOps / Argo CD
Out of scope for environment lifecycle at this phase; reserved for deployment and promotion.

## Future Work

Planned follow-up phases include:

- Workflow status tracking and observability
- TTL enforcement and automated cleanup
- Control plane deployment in-cluster
- Argo Go SDK adoption
- GitHub App integration for PR-driven environments
- Promotion and deployment workflows

This ADR intentionally scopes ephemeral environments to the minimum viable,
production-real design.

## Summary

Ephemeral environments are implemented via Argo WorkflowTemplates that manage Kubernetes
namespaces. The control plane submits intent only, preserving architectural boundaries
and enabling future evolution without lock-in.