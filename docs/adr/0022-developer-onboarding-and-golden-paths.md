# ADR 0022: Developer Onboarding and Golden Paths

- Status: Accepted
- Date: 2026-09-09

## Context

Production promotion is operationally credible, but repository registration and
preview diagnosis still require platform expertise. A golden path must lower
cognitive load without coupling the domain model to one source-control vendor.

## Decision

Services are declared with the versioned `platform.service/v1` contract. The
platform API exposes a tenant-scoped catalog and deterministic diagnostics, and
`platformctl` applies declarations or queries those APIs. Repository identity,
revision status, and webhook delivery use provider-neutral domain objects;
GitHub and Bitbucket are adapters, not privileged concepts.

Tenant onboarding and offboarding use explicit, resumable workflows: establish
identity, database tenancy, namespace policy, quota, signing scope, audit export,
and conformance before activation; suspend ingress and mutation before evidence
retention and eventual deletion. Self-service errors carry stable codes and
remediation guidance while excluding secrets and cross-tenant information.

An end-to-end conformance test must prove that a tenant can apply a service,
discover it in the catalog, create a preview, obtain diagnostics/status, and
offboard without affecting a second tenant.

## Consequences

- The YAML contract, API, CLI, and templates share one semantic model.
- New SCM providers implement adapters without altering lifecycle semantics.
- Golden paths are defaults with declared escape hatches, not hidden policy.
- Tenant activation is contingent on conformance rather than API success alone.
