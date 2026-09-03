#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
namespace=evidence-reverification-conformance
cron_name=preview-evidence-reverification-conformance
manifest=$(mktemp /tmp/evidence-reverification.XXXXXX)

cleanup() {
  kubectl -n argo delete cronworkflow "$cron_name" --ignore-not-found >/dev/null 2>&1 || true
  kubectl -n argo delete workflow \
    -l "workflows.argoproj.io/cron-workflow=$cron_name" --ignore-not-found >/dev/null 2>&1 || true
  kubectl delete namespace "$namespace" --ignore-not-found >/dev/null 2>&1 || true
  rm -f "$manifest"
}
trap cleanup EXIT

for binary in kubectl sed; do
  command -v "$binary" >/dev/null || { echo "$binary is required" >&2; exit 1; }
done

sed "s/name: preview-evidence-reverification/name: $cron_name/" \
  "$repo_root/argo/cronworkflows/evidence-reverification.yaml" > "$manifest"
kubectl apply -f "$repo_root/infra/k8s/argo-env-admin-clusterrole.yaml" >/dev/null
kubectl apply -f "$manifest" >/dev/null
kubectl -n argo patch cronworkflow "$cron_name" --type=json -p='[
  {"op":"replace","path":"/spec/schedule","value":"* * * * *"},
  {"op":"replace","path":"/spec/workflowSpec/arguments/parameters/4/value","value":"platform.evidence-reverification-conformance=true"}
]' >/dev/null

kubectl create namespace "$namespace" >/dev/null
kubectl -n "$namespace" create deployment unverifiable \
  --image='invalid.example.test/platform/subject@sha256:0000000000000000000000000000000000000000000000000000000000000000' >/dev/null
kubectl -n "$namespace" label deployment unverifiable \
  platform.evidence-reverification-conformance=true --overwrite >/dev/null

attempts=0
while [ "$attempts" -lt 24 ]; do
  replicas=$(kubectl -n "$namespace" get deployment unverifiable -o jsonpath='{.spec.replicas}')
  quarantined=$(kubectl get namespace "$namespace" -o jsonpath='{.metadata.labels.platform\.preview-quarantine}')
  if [ "$replicas" = 0 ] && [ "$quarantined" = true ]; then
    reason=$(kubectl get namespace "$namespace" -o jsonpath='{.metadata.annotations.platform\.preview-quarantine-reason}')
    test "$reason" = signature-verification-failed
    phase_attempts=0
    while [ "$phase_attempts" -lt 12 ]; do
      workflow_phase=$(kubectl -n argo get workflow \
        -l "workflows.argoproj.io/cron-workflow=$cron_name" \
        -o jsonpath='{.items[0].status.phase}')
      [ "$workflow_phase" = Failed ] && break
      phase_attempts=$((phase_attempts + 1))
      sleep 2
    done
    test "${workflow_phase:-}" = Failed
    echo "Periodic evidence re-verification quarantined and scaled the forged subject to zero"
    exit 0
  fi
  attempts=$((attempts + 1))
  sleep 5
done

echo "Timed out waiting for evidence quarantine" >&2
exit 1
