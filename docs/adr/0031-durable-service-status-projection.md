# ADR 0031: Durable Service Status Projection

- Status: Accepted
- Date: 2026-09-18

## Context

The versioned catalog records desired service intent, while execution-plane
observations previously appeared only in diagnostics. That made the catalog
status incomplete after a restart and forced every client to reconstruct
reconciliation state from environments.

## Decision

The reconciler projects a compact, tenant-scoped service status into the
authoritative service document. It records observed generation, state, and a
secret-free message while leaving desired declaration fields unchanged.
Observations are compare-and-set protected in PostgreSQL and idempotent in the
file-backed store. `ready`, `reconciling`, and `degraded` are projections, not
new execution-plane authorities.

## Consequences

Catalog consumers can read status consistently without replaying workflow
history. A later slice can add durable reconciliation queues and richer
conditions without changing the ownership boundary.

## Related decisions

Builds on ADR 0026, ADR 0029, and ADR 0030.
