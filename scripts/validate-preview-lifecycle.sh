#!/usr/bin/env bash
set -euo pipefail

: "${CONTROL_PLANE_URL:?set CONTROL_PLANE_URL}"
: "${GITHUB_WEBHOOK_SECRET:?set GITHUB_WEBHOOK_SECRET}"
: "${SCM_REPOSITORY:?set SCM_REPOSITORY to an already registered owner/repository}"
: "${SCM_REPOSITORY_NAME:?set SCM_REPOSITORY_NAME}"
: "${SCM_COMMIT_SHA:?set SCM_COMMIT_SHA}"

WORKFLOW_TIMEOUT="${WORKFLOW_TIMEOUT:-600s}"
SCM_PULL_REQUEST="${SCM_PULL_REQUEST:-9001}"

command -v curl >/dev/null
command -v kubectl >/dev/null

delivery="smoke-$(date +%s)"
payload="{\"action\":\"opened\",\"number\":${SCM_PULL_REQUEST},\"installation\":{\"id\":1},\"repository\":{\"name\":\"${SCM_REPOSITORY_NAME}\",\"full_name\":\"${SCM_REPOSITORY}\"},\"pull_request\":{\"head\":{\"sha\":\"${SCM_COMMIT_SHA}\"}}}"
signature="sha256=$(printf '%s' "$payload" | openssl dgst -sha256 -hmac "$GITHUB_WEBHOOK_SECRET" -hex | awk '{print $2}')"

curl --fail-with-body -X POST "${CONTROL_PLANE_URL}/api/v1/webhooks/github" \
  -H "X-GitHub-Delivery: ${delivery}" \
  -H "X-GitHub-Event: pull_request" \
  -H "X-Hub-Signature-256: ${signature}" \
  -H "Content-Type: application/json" \
  --data "$payload"

wait_for_workflow() {
  selector="$1"
  deadline=$((SECONDS + 60))
  while [ "$SECONDS" -lt "$deadline" ]; do
    if [ -n "$(kubectl get workflow -n argo -l "$selector" -o name)" ]; then
      return 0
    fi
    sleep 1
  done
  echo "Timed out waiting for workflow matching ${selector}" >&2
  return 1
}

# The TTL workflow is deliberately long-lived and must not be included in the
# convergence predicate. Validate provisioning and revision deployment as two
# independent execution-plane contracts.
create_selector="platform.environment=${SCM_REPOSITORY_NAME}-pr-${SCM_PULL_REQUEST},platform.workflow.type=environment-create"
wait_for_workflow "$create_selector"
kubectl wait --for=jsonpath='{.status.phase}'=Succeeded workflow \
  -n argo \
  -l "$create_selector" \
  --timeout="$WORKFLOW_TIMEOUT"

deploy_selector="platform.environment=${SCM_REPOSITORY_NAME}-pr-${SCM_PULL_REQUEST},platform.workflow.type=environment-deploy"
wait_for_workflow "$deploy_selector"
kubectl wait --for=jsonpath='{.status.phase}'=Succeeded workflow \
  -n argo \
  -l "$deploy_selector" \
  --timeout="$WORKFLOW_TIMEOUT"

echo "Preview lifecycle submission verified for ${SCM_REPOSITORY_NAME}-pr-${SCM_PULL_REQUEST}"
