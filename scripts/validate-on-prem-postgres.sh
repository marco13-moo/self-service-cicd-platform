#!/usr/bin/env bash
set -euo pipefail

namespace=platform-database
cluster=platform-postgres
maximum_rto=${ON_PREM_MAXIMUM_RTO_SECONDS:-1800}
marker="certification-$(date +%s)"
kubectl -n "$namespace" wait --for=condition=Ready "cluster/$cluster" --timeout=15m
primary=$(kubectl -n "$namespace" get pod -l cnpg.io/cluster="$cluster",role=primary -o jsonpath='{.items[0].metadata.name}')
kubectl -n "$namespace" exec "$primary" -- psql -U postgres -d postgres -v ON_ERROR_STOP=1 -c 'CREATE TABLE IF NOT EXISTS certification_markers(value text primary key, created_at timestamptz default now())' >/dev/null
kubectl -n "$namespace" exec "$primary" -- psql -U postgres -d postgres -v ON_ERROR_STOP=1 -c "INSERT INTO certification_markers(value) VALUES('$marker')" >/dev/null

backup="certification-$(date +%s)"
kubectl -n "$namespace" apply -f - <<EOF
apiVersion: postgresql.cnpg.io/v1
kind: Backup
metadata: {name: $backup}
spec:
  cluster: {name: $cluster}
  method: plugin
  pluginConfiguration: {name: barman-cloud.cloudnative-pg.io}
EOF
for _ in $(seq 1 180); do
  phase=$(kubectl -n "$namespace" get backup "$backup" -o jsonpath='{.status.phase}' 2>/dev/null || true)
  [[ "$phase" == completed ]] && break
  [[ "$phase" == failed ]] && { kubectl -n "$namespace" describe backup "$backup" >&2; exit 1; }
  sleep 2
done
[[ "${phase:-}" == completed ]] || { echo "backup did not complete" >&2; exit 1; }

started=$(date +%s)
kubectl -n "$namespace" delete pod "$primary" --wait=false >/dev/null
for _ in $(seq 1 "$maximum_rto"); do
  candidate=$(kubectl -n "$namespace" get pod -l cnpg.io/cluster="$cluster",role=primary -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
  if [[ -n "$candidate" && "$candidate" != "$primary" ]] && kubectl -n "$namespace" exec "$candidate" -- pg_isready -U postgres >/dev/null 2>&1; then break; fi
  sleep 1
done
rto=$(( $(date +%s) - started ))
[[ -n "${candidate:-}" && "$candidate" != "$primary" && "$rto" -le "$maximum_rto" ]] || { echo "PostgreSQL failover exceeded RTO" >&2; exit 1; }
observed=$(kubectl -n "$namespace" exec "$candidate" -- psql -U postgres -d postgres -Atc "SELECT value FROM certification_markers WHERE value='$marker'")
[[ "$observed" == "$marker" ]] || { echo "synchronous failover lost committed marker" >&2; exit 1; }
jq -n --arg marker "$marker" --arg backup "$backup" --argjson rto_seconds "$rto" '{status:"pass",committed_marker:$marker,backup:$backup,rto_seconds:$rto_seconds,observed_rpo_seconds:0}'
