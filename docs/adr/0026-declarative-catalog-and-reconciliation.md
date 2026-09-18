# ADR 0026: Declarative Service Catalog and Reconciliation

- Status: Accepted
- Date: 2026-09-18

Catalog registration persists desired intent with a generation. Reconciliation
observes execution-plane state and projects an observed generation/state without
mutating the declaration. Stale observations are ignored, repeated applies are
idempotent where the same identity is supplied, and conflicting ownership or
name writes return a conflict. The existing `ServiceStore` is the first durable
repository; a queue is a follow-up slice.

## Related decisions

This builds on ADR 0010, ADR 0011, ADR 0017, ADR 0018, and ADR 0022.
