# ADR 0024: Platform Product Boundaries and Domain Model

- Status: Accepted
- Date: 2026-09-18

## Decision

The platform product is organized around catalog, tenancy, policy,
reconciliation, provisioning, delivery, and telemetry bounded contexts.
`platform.service/v1` is the developer-facing declaration; the control plane
owns intent and status, while Kubernetes/Argo/SCM remain execution adapters.
Existing environment and webhook capabilities remain inside those boundaries.

## Consequences

The next changes can add convergence without coupling API contracts to a
provider. A portal and autonomous remediation are explicitly out of scope until
the catalog and reconciliation boundaries have durable evidence.
