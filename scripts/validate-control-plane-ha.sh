#!/usr/bin/env bash
set -euo pipefail

source_container=${HA_SOURCE_CONTAINER:-self-service-cicd-postgres-source}
recovery_container=${HA_RECOVERY_CONTAINER:-self-service-cicd-postgres-recovery}
source_port=${HA_SOURCE_PORT:-56432}
recovery_port=${HA_RECOVERY_PORT:-56433}
postgres_image=${HA_POSTGRES_IMAGE:-postgres:17.6-alpine}
database=platform
password=${HA_POSTGRES_PASSWORD:-ha-conformance-only}
tenant_role=platform_app
tenant_password=${HA_TENANT_DATABASE_PASSWORD:-tenant-conformance-only}
dump_file=$(mktemp /tmp/self-service-cicd-postgres.dump.XXXXXX)
legacy_file=$(mktemp /tmp/self-service-cicd-state.json.XXXXXX)
audit_archive=$(mktemp /tmp/self-service-cicd-audit.wal.XXXXXX)
repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

cleanup() {
  docker rm -f "$source_container" "$recovery_container" >/dev/null 2>&1 || true
  rm -f "$dump_file"
  rm -f "$legacy_file"
  rm -f "$audit_archive"
}
trap cleanup EXIT

docker rm -f "$source_container" "$recovery_container" >/dev/null 2>&1 || true
docker run -d --name "$source_container" -e POSTGRES_PASSWORD="$password" -e POSTGRES_DB="$database" -p "127.0.0.1:${source_port}:5432" "$postgres_image" -c wal_level=logical -c max_replication_slots=4 >/dev/null

for _ in $(seq 1 60); do
  if docker exec "$source_container" pg_isready -U postgres -d "$database" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker exec "$source_container" pg_isready -U postgres -d "$database" >/dev/null

source_url="postgres://postgres:${password}@127.0.0.1:${source_port}/${database}?sslmode=disable"
printf '%s\n' '{"services":{"import-proof":{"name":"import-proof","repo_url":"https://github.com/acme/import-proof","version":1}},"environments":{"import-proof-pr-1":{"version":1,"spec":{"name":"import-proof-pr-1","service":"import-proof"}}}}' >"$legacy_file"
(
  cd "$repository_root/control-plane"
  DATABASE_URL="$source_url" GOCACHE=/tmp/self-service-cicd-go-cache \
    go run ./cmd/state-migrate -state-path "$legacy_file"
)

# Production archives may consume this insert-only publication directly or use
# equivalent WAL shipping. The logical slot assertion proves that every new
# audit event is externally observable without weakening table immutability.
audit_correlation="wal-export-$(date +%s)"
docker exec "$source_container" psql -U postgres -d "$database" -v ON_ERROR_STOP=1 \
  -c "CREATE PUBLICATION platform_audit_events FOR TABLE audit_events WITH (publish='insert')" \
  -c "SELECT * FROM pg_create_logical_replication_slot('audit_archive_conformance','test_decoding')" >/dev/null
docker exec "$source_container" psql -U postgres -d "$database" -v ON_ERROR_STOP=1 \
  -c "INSERT INTO audit_events(id,tenant_id,correlation_id,actor,event_type,resource_type,resource_name,outcome) VALUES(gen_random_uuid(),'default','${audit_correlation}','conformance','audit.exported','tenant','default','succeeded')" >/dev/null
docker exec "$source_container" psql -U postgres -d "$database" -Atc \
  "SELECT data FROM pg_logical_slot_get_changes('audit_archive_conformance',NULL,NULL)" >"$audit_archive"
if ! grep -Fq "$audit_correlation" "$audit_archive"; then
  echo "logical audit archive omitted the committed conformance event" >&2
  exit 1
fi
publication_mode=$(docker exec "$source_container" psql -U postgres -d "$database" -Atc \
  "SELECT pubinsert::text || ':' || pubupdate::text || ':' || pubdelete::text FROM pg_publication WHERE pubname='platform_audit_events'")
if [ "$publication_mode" != "true:false:false" ]; then
  echo "audit publication is not insert-only: $publication_mode" >&2
  exit 1
fi
chmod 0444 "$audit_archive"
audit_archive_digest=$(shasum -a 256 "$audit_archive" | awk '{print $1}')
docker exec "$source_container" psql -U postgres -d "$database" -v ON_ERROR_STOP=1 \
  -c "CREATE ROLE ${tenant_role} LOGIN PASSWORD '${tenant_password}'" \
  -c "GRANT USAGE ON SCHEMA public TO ${tenant_role}" \
  -c "GRANT SELECT ON schema_migrations TO ${tenant_role}" \
  -c "GRANT SELECT,INSERT,UPDATE ON tenants TO ${tenant_role}" \
  -c "GRANT SELECT,INSERT,UPDATE,DELETE ON services,environments,scm_deliveries,scm_commands,tenant_auths TO ${tenant_role}" \
  -c "GRANT SELECT,INSERT ON audit_events TO ${tenant_role}" >/dev/null
source_tenant_url="postgres://${tenant_role}:${tenant_password}@127.0.0.1:${source_port}/${database}?sslmode=disable"
(
  cd "$repository_root/control-plane"
  TEST_DATABASE_URL="$source_url" TEST_TENANT_DATABASE_URL="$source_tenant_url" GOCACHE=/tmp/self-service-cicd-go-cache \
    go test ./internal/api -run 'TestPostgresAuthoritativeStateAndReplicaHandoff|TestConcurrentMigrationStartup|TestPostgresRowLevelTenantIsolation' -count=1
  TEST_DATABASE_URL="$source_url" GOCACHE=/tmp/self-service-cicd-go-cache \
    go test ./internal/reconciler -run TestPostgresReconciliationContinuesAfterReplicaTermination -count=1
)

# pg_dump's custom format preserves a transactionally coherent logical image of
# all authoritative tables and their migration ledger.
docker exec "$source_container" pg_dump -U postgres -d "$database" -Fc >"$dump_file"
docker run -d --name "$recovery_container" -e POSTGRES_PASSWORD="$password" -e POSTGRES_DB="$database" -p "127.0.0.1:${recovery_port}:5432" "$postgres_image" >/dev/null
for _ in $(seq 1 60); do
  if docker exec "$recovery_container" pg_isready -U postgres -d "$database" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker exec "$recovery_container" psql -U postgres -d "$database" -v ON_ERROR_STOP=1 \
  -c "CREATE ROLE ${tenant_role} LOGIN PASSWORD '${tenant_password}'" >/dev/null
docker exec -i "$recovery_container" pg_restore -U postgres -d "$database" --clean --if-exists <"$dump_file"
docker exec "$recovery_container" psql -U postgres -d "$database" -v ON_ERROR_STOP=1 \
  -c "GRANT USAGE ON SCHEMA public TO ${tenant_role}" \
  -c "GRANT SELECT ON schema_migrations TO ${tenant_role}" \
  -c "GRANT SELECT,INSERT,UPDATE ON tenants TO ${tenant_role}" \
  -c "GRANT SELECT,INSERT,UPDATE,DELETE ON services,environments,scm_deliveries,scm_commands,tenant_auths TO ${tenant_role}" \
  -c "GRANT SELECT,INSERT ON audit_events TO ${tenant_role}" >/dev/null
restored_services=$(docker exec "$recovery_container" psql -U postgres -d "$database" -Atc "SELECT count(*) FROM services WHERE name='import-proof'")
restored_environments=$(docker exec "$recovery_container" psql -U postgres -d "$database" -Atc "SELECT count(*) FROM environments WHERE name='import-proof-pr-1'")
if [ "$restored_services" != 1 ] || [ "$restored_environments" != 1 ]; then
  echo "restored database does not contain the imported authoritative state" >&2
  exit 1
fi

recovery_url="postgres://postgres:${password}@127.0.0.1:${recovery_port}/${database}?sslmode=disable"
recovery_tenant_url="postgres://${tenant_role}:${tenant_password}@127.0.0.1:${recovery_port}/${database}?sslmode=disable"
(
  cd "$repository_root/control-plane"
  TEST_DATABASE_URL="$recovery_url" TEST_TENANT_DATABASE_URL="$recovery_tenant_url" GOCACHE=/tmp/self-service-cicd-go-cache \
    go test ./internal/api -run 'TestPostgresAuthoritativeStateAndReplicaHandoff|TestConcurrentMigrationStartup|TestPostgresRowLevelTenantIsolation' -count=1
  TEST_DATABASE_URL="$recovery_url" GOCACHE=/tmp/self-service-cicd-go-cache \
    go test ./internal/reconciler -run TestPostgresReconciliationContinuesAfterReplicaTermination -count=1
)

echo "Control-plane handoff, PostgreSQL backup/restore, and immutable WAL audit export conformance passed (${audit_archive_digest})"
