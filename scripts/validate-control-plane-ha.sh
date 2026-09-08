#!/usr/bin/env bash
set -euo pipefail

source_container=${HA_SOURCE_CONTAINER:-self-service-cicd-postgres-source}
recovery_container=${HA_RECOVERY_CONTAINER:-self-service-cicd-postgres-recovery}
source_port=${HA_SOURCE_PORT:-56432}
recovery_port=${HA_RECOVERY_PORT:-56433}
postgres_image=${HA_POSTGRES_IMAGE:-postgres:17.6-alpine}
database=platform
password=${HA_POSTGRES_PASSWORD:-ha-conformance-only}
dump_file=$(mktemp /tmp/self-service-cicd-postgres.XXXXXX.dump)
legacy_file=$(mktemp /tmp/self-service-cicd-state.XXXXXX.json)
repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

cleanup() {
  docker rm -f "$source_container" "$recovery_container" >/dev/null 2>&1 || true
  rm -f "$dump_file"
  rm -f "$legacy_file"
}
trap cleanup EXIT

docker rm -f "$source_container" "$recovery_container" >/dev/null 2>&1 || true
docker run -d --name "$source_container" -e POSTGRES_PASSWORD="$password" -e POSTGRES_DB="$database" -p "127.0.0.1:${source_port}:5432" "$postgres_image" >/dev/null

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
(
  cd "$repository_root/control-plane"
  TEST_DATABASE_URL="$source_url" GOCACHE=/tmp/self-service-cicd-go-cache \
    go test ./internal/api -run 'TestPostgresAuthoritativeStateAndReplicaHandoff|TestConcurrentMigrationStartup' -count=1
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
docker exec -i "$recovery_container" pg_restore -U postgres -d "$database" --clean --if-exists <"$dump_file"
restored_services=$(docker exec "$recovery_container" psql -U postgres -d "$database" -Atc "SELECT count(*) FROM services WHERE name='import-proof'")
restored_environments=$(docker exec "$recovery_container" psql -U postgres -d "$database" -Atc "SELECT count(*) FROM environments WHERE name='import-proof-pr-1'")
if [ "$restored_services" != 1 ] || [ "$restored_environments" != 1 ]; then
  echo "restored database does not contain the imported authoritative state" >&2
  exit 1
fi

recovery_url="postgres://postgres:${password}@127.0.0.1:${recovery_port}/${database}?sslmode=disable"
(
  cd "$repository_root/control-plane"
  TEST_DATABASE_URL="$recovery_url" GOCACHE=/tmp/self-service-cicd-go-cache \
    go test ./internal/api -run 'TestPostgresAuthoritativeStateAndReplicaHandoff|TestConcurrentMigrationStartup' -count=1
  TEST_DATABASE_URL="$recovery_url" GOCACHE=/tmp/self-service-cicd-go-cache \
    go test ./internal/reconciler -run TestPostgresReconciliationContinuesAfterReplicaTermination -count=1
)

echo "Control-plane replica handoff and PostgreSQL backup/restore conformance passed"
