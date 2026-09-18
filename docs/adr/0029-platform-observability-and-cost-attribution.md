# ADR 0029: Platform Observability and Cost Attribution

- Status: Accepted
- Date: 2026-09-18

Every reconciliation and delivery operation emits tenant, service, generation,
correlation, outcome, and provider dimensions. Metrics and traces are
tenant-safe; logs and diagnostics are secret-free. Cost attribution is based on
metered workload/resource evidence and remains separate from quota admission
until the metering source is authoritative.

## Related decisions

ADR 0006 and ADR 0021 define the existing observability and production evidence
boundaries.
