# Control-plane HA conformance — 2026-09-04

## Scope

ADR 0017 was exercised with two independently initialized PostgreSQL 17.6
containers: one source authority and one recovery authority restored from a
custom-format logical backup. The repository harness removed both containers
and its temporary dump after completion.

## Verified results

- Four concurrent store initializers serialized versioned migrations without a
  duplicate or partial migration.
- Desired service and environment state written through one database pool was
  immediately visible through a second replica pool.
- A stale environment version was rejected while a fresh compare-and-set
  mutation succeeded.
- The explicit legacy JSON importer transferred one service and one environment
  into an empty database; both rows survived logical backup and restore.
- Replaying an SCM provider/delivery identity was deduplicated.
- A command leased by a terminated replica was unavailable until durable lease
  expiry, then reclaimed by a replacement replica.
- The replacement replica executed the real lifecycle reconciler, created and
  deployed the preview intent through the test orchestrator, persisted the
  environment, and completed the command.
- `pg_dump` produced a transactionally coherent custom-format backup;
  `pg_restore` reconstructed an independent database; the complete migration,
  deduplication, leasing, concurrency, and reconciliation suite passed again
  against the restored authority.
- Kubernetes server-side dry-run accepted the three-replica Deployment and its
  `policy/v1` PodDisruptionBudget against the configured cluster API.
- The complete Go test suite and `go vet ./...` passed.

## Boundary

This certifies application semantics and logical backup restoration. It does not
certify a managed PostgreSQL provider, synchronous replication topology,
continuous WAL archiving, DNS or proxy writer-endpoint promotion, production
credentials, production RPO, or production RTO. Each production database
offering must repeat the runbook acceptance probes during a provider-native
failover exercise before being declared highly available.
