# ADR 0032: Tenant Quota Admission

- Status: Accepted
- Date: 2026-09-18

## Decision

Service registration and preview-environment creation are admission points for
tenant quotas. The authoritative PostgreSQL tenant row stores service and
environment limits, while the file-backed local store uses documented
development defaults. Admission fails closed when quota state cannot be read
and returns `429 Too Many Requests` when the limit is reached.

Quota checks are tenant-scoped and occur before durable intent mutation. They
are intentionally separate from workload resource accounting; CPU, memory, and
cost budgets require a metering source and are a later slice.

## Consequences

Quota policy is enforceable without changing execution providers. Concurrent
admission still requires database-side reservation or serializable accounting
when strict oversubscription prevention is required; that is explicitly
tracked for the reconciliation/quota follow-up.

## Related decisions

Builds on ADR 0018, ADR 0027, ADR 0026, and ADR 0030.
