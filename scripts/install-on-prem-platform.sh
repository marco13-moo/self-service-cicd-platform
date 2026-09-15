#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
: "${RENDERED_PLATFORM_MANIFEST:?set RENDERED_PLATFORM_MANIFEST}"
for command in kubectl git tar jq openssl; do command -v "$command" >/dev/null || { echo "missing required command: $command" >&2; exit 1; }; done

secret_dir=${ON_PREM_SECRET_DIR:-$root/.on-prem-secrets}
mkdir -p "$secret_dir"; chmod 0700 "$secret_dir"
secret_value() { local file=$1; [[ -s "$file" ]] || { umask 077; openssl rand -hex 32 >"$file"; }; cat "$file"; }
admin_token=$(secret_value "$secret_dir/platform-admin-token")
tenant_a_token=$(secret_value "$secret_dir/adr23-alpha-token")
tenant_b_token=$(secret_value "$secret_dir/adr23-beta-token")
webhook_secret=$(secret_value "$secret_dir/github-webhook-secret")
tenant_tokens=$(jq -nc --arg admin "$admin_token" --arg alpha "$tenant_a_token" --arg beta "$tenant_b_token" \
  '{($admin):{subject:"platform-bootstrap",tenant_id:"default",role:"admin",platform_admin:true},($alpha):{subject:"adr23-alpha-admin",tenant_id:"adr23-alpha",role:"admin"},($beta):{subject:"adr23-beta-admin",tenant_id:"adr23-beta",role:"admin"}}')

# Materialize the exact candidate workspace as an immutable Git bundle. This
# makes Argo CD independent of public SaaS while retaining Git as the sole
# reconciliation substrate, including for pre-commit acceptance candidates.
bundle_work=$(mktemp -d /tmp/on-prem-gitops.XXXXXX)
trap 'rm -rf "$bundle_work"' EXIT
git -C "$bundle_work" init -q -b main
git -C "$root" ls-files --cached --others --exclude-standard -z \
  | tar -C "$root" --null -T - -cf - \
  | tar -C "$bundle_work" -xf -
git -C "$bundle_work" add .
git -C "$bundle_work" -c user.name=platform-bootstrap -c user.email=platform-bootstrap@invalid.example \
  commit -q -m 'bootstrap: materialize candidate platform configuration'
git -C "$bundle_work" bundle create "$bundle_work/platform.bundle" main
kubectl create namespace git-system --dry-run=client -o yaml | kubectl apply -f -
kubectl -n git-system create configmap platform-repository-bundle \
  --from-file=platform.bundle="$bundle_work/platform.bundle" --dry-run=client -o yaml | kubectl apply -f -

# CNPG-generated credentials are copied in memory; secret material is never
# written to the repository or to an intermediate plaintext file.
kubectl create namespace control-plane --dry-run=client -o yaml | kubectl apply -f -
database_url=$(kubectl -n platform-database get secret platform-postgres-app -o jsonpath='{.data.uri}')
[[ -n "$database_url" ]] || { echo "CloudNativePG application URI is unavailable" >&2; exit 1; }
kubectl -n control-plane create secret generic control-plane-database \
  --from-literal=url="$(printf '%s' "$database_url" | base64 -d)" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n control-plane create secret generic control-plane-tenant-auth \
  --from-literal=tokens.json="$tenant_tokens" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n control-plane create secret generic control-plane-admin \
  --from-literal=token="$admin_token" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n control-plane create secret generic github-app \
  --from-literal=webhook-secret="$webhook_secret" --dry-run=client -o yaml | kubectl apply -f -
kubectl apply --server-side --field-manager=platform-bootstrap -f "$RENDERED_PLATFORM_MANIFEST"
kubectl -n git-system rollout status deployment/airgap-git --timeout=5m
kubectl apply --server-side --field-manager=platform-bootstrap -f "$root/infra/on-prem/platform/argocd-application.yaml"
kubectl -n control-plane rollout status deployment/control-plane --timeout=10m
kubectl -n argocd wait application/self-service-cicd-platform \
  --for=jsonpath='{.status.sync.status}'=Synced --timeout=10m
