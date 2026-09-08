# Control-plane high availability

## JSON-to-PostgreSQL cutover

1. Scale the legacy control plane to zero and retain a snapshot of its PVC.
2. Provision PostgreSQL with a stable writer endpoint, TLS, automated backups,
   and a tested failover mechanism.
3. Export `DATABASE_URL` without placing credentials in shell history, then run
   `go run ./cmd/state-migrate -state-path /path/to/state.json` from
   `control-plane/`.
4. Verify service and environment counts and retain the import output with the
   change record.
5. deploy the database Secret, Deployment, Service, and PodDisruptionBudget;
   wait for all three replicas to become Ready.
6. Exercise an authenticated read and one disposable environment lifecycle.
   Do not remount or resume writes to the legacy PVC after cutover.

## Backup and restore

Back up the entire database, including `schema_migrations`, `services`,
`environments`, `scm_deliveries`, and `scm_commands`, with the PostgreSQL
provider's transactionally consistent mechanism. Encrypt backups, restrict
restore authority, record WAL/backup retention, and validate a restore in an
isolated database on the configured schedule.

A restore is accepted only after migrations are current, representative desired
state is readable, a fresh CAS update succeeds, duplicate delivery insertion is
rejected, and an expired command lease can be reclaimed exactly once.

## Failover

During writer failover, `/readyz` must return unavailable and Kubernetes must
remove affected pods from Service endpoints. Do not restart-loop replicas;
liveness is intentionally independent. After the database endpoint promotes a
writer, verify readiness, a service/environment read-write cycle, webhook
deduplication, command leasing, and reconciliation convergence before resolving
the incident. Compare the promoted database timestamp with the last accepted
delivery to quantify actual RPO and record time-to-ready as RTO.

Run `./scripts/validate-control-plane-ha.sh` before changing PostgreSQL versions,
backup machinery, failover topology, or control-plane leasing semantics.
