# ADR 0017: Highly Available Control Plane and Authoritative PostgreSQL State

## Status

Accepted

## Context

PostgreSQL already durably deduplicates SCM deliveries and leases lifecycle
commands, but service and environment intent remained in an atomic JSON file on
a `ReadWriteOnce` volume. That bifurcated authority constrained the Deployment
to one replica and made desired state, observation, and command execution
subject to different failure domains.

## Decision

PostgreSQL is the sole production authority for services, environments, SCM
deliveries, and commands. JSON persistence remains a local-development fallback
and migration source; the Kubernetes Deployment requires `DATABASE_URL`, mounts
no state PVC, and runs three interchangeable replicas.

Schema evolution uses an ordered migration ledger inside a transaction guarded
by a PostgreSQL transaction-scoped advisory lock. Every replica may invoke the
migrator at startup, but exactly one serial history is committed. Existing JSON
state is transferred before cutover with the explicit `state-migrate` command,
which refuses a non-empty target rather than synthesizing a potentially
incoherent merge.

Environment documents carry a monotonically increasing version. Mutations are
compare-and-set updates against that version, and stale writers receive a
conflict. Deployment observation additionally matches workflow identity and
desired generation, preventing a late completion from an obsolete workflow
from promoting stale evidence.

SCM delivery identity is globally unique by provider and delivery identifier.
Commands are leased with `FOR UPDATE SKIP LOCKED`; an unfinished lease becomes
eligible after its durable expiry, allowing another replica to continue after
termination without concurrently executing an unexpired lease.

Readiness is dependency-aware and fails closed unless both PostgreSQL and Argo
are reachable. Liveness remains process-local so a shared dependency incident
removes replicas from traffic without inducing a restart storm. SIGTERM cancels
reconciliation, drains HTTP traffic, waits for the reconciler, and then closes
the database pool.

The Deployment uses three replicas, zero-unavailable rolling updates, a
two-replica PodDisruptionBudget, topology spreading, preferred pod
anti-affinity, a readiness stabilization interval, and a bounded termination
grace period. The database endpoint itself must provide a stable writer address
and synchronous durability commensurate with the platform's RPO.

Database backup captures the migration ledger and all four authoritative
tables. Restore and failover certification must prove schema recovery,
service/environment visibility, delivery deduplication, expired-lease reclaim,
and post-failover mutation against an independently restored database.

## Consequences

- Any ready replica can serve API traffic and reconciliation work.
- PostgreSQL availability is now an explicit prerequisite for production
  readiness and startup.
- Optimistic concurrency exposes conflicting writers instead of silently losing
  an update; callers must re-read and retry intentionally.
- A three-replica application does not make a single PostgreSQL instance highly
  available. Production must supply managed or operator-governed PostgreSQL
  failover, backups, monitoring, and a stable writer endpoint.
- JSON rollback after cutover is prohibited because it no longer contains the
  authoritative generation history.

## Operational invariants

- Never run `state-migrate` against a database containing service or environment
  rows.
- Never route traffic to a replica whose PostgreSQL or Argo readiness check is
  failing.
- Never lower command lease duration below the maximum safe execution handoff
  interval without proving idempotency of the affected operation.
- Never restore only service/environment tables; delivery and command history
  are part of the same consistency boundary.
- Never acknowledge a database failover until a write, read, deduplication, and
  lease-recovery probe has passed through the promoted writer endpoint.

## Conformance

`scripts/validate-control-plane-ha.sh` creates isolated source and recovery
PostgreSQL instances, runs concurrent-startup and replica-handoff integration
tests, performs a logical backup and restore, and repeats the suite against the
restored authority. `docs/runbooks/control-plane-ha.md` specifies production
cutover, backup, restoration, and failover acceptance.
