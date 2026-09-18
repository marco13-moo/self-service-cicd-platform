# ADR 0030: Failure, Recovery, and Idempotency Rules

- Status: Accepted
- Date: 2026-09-18

Writes are transactional and return explicit errors. Reconciliation is
retry-safe, generation-aware, and converges from durable desired state after a
restart. Provider calls use bounded timeouts and retained operation identity;
duplicate declarations for the same tenant/name are conflicts unless an
explicit idempotency key and equivalent payload are supported. Recovery uses
the authoritative PostgreSQL state and documented backup/WAL procedures.

## Related decisions

ADR 0016, ADR 0017, ADR 0018, and ADR 0021 define recovery, HA, isolation, and
evidence requirements.
