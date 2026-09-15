#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cluster=${KIND_CLUSTER_NAME:-self-service-cicd-cilium}
for command in docker kind kubectl helm openssl envsubst jq; do command -v "$command" >/dev/null || { echo "missing required command: $command" >&2; exit 1; }; done

kubectl config use-context "kind-$cluster" >/dev/null
kubectl get nodes >/dev/null
secret_dir=${ON_PREM_SECRET_DIR:-$root/.on-prem-secrets}
mkdir -p "$secret_dir"; chmod 0700 "$secret_dir"
secret_value() { local file=$1; [[ -s "$file" ]] || { umask 077; openssl rand -base64 32 | tr -d '\n' >"$file"; }; cat "$file"; }
harbor_password=$(secret_value "$secret_dir/harbor-admin")
harbor_node_ip=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "${cluster}-worker2")
minio_user=platform-backup
minio_password=$(secret_value "$secret_dir/minio-root")
keycloak_password=$(secret_value "$secret_dir/keycloak-admin")
rfc2136_secret=$(secret_value "$secret_dir/rfc2136-tsig")

helm upgrade --install cert-manager jetstack/cert-manager --version v1.21.1 -n cert-manager --create-namespace --set crds.enabled=true --wait --timeout 10m
kubectl apply -f "$root/infra/on-prem/internal-pki.yaml"
kubectl -n cert-manager wait --for=condition=Ready certificate/on-prem-root --timeout=5m

helm upgrade --install minio minio/minio --version 5.4.0 -n minio --create-namespace -f "$root/infra/on-prem/minio-values.yaml" \
  --set rootUser="$minio_user" --set rootPassword="$minio_password" --wait --timeout 10m
helm upgrade --install cloudnative-pg cnpg/cloudnative-pg --version 0.29.0 -n cnpg-system --create-namespace --wait --timeout 10m
helm upgrade --install plugin-barman-cloud cnpg/plugin-barman-cloud --version 0.8.0 -n cnpg-system --wait --timeout 10m
kubectl create namespace harbor --dry-run=client -o yaml | kubectl apply -f -
HARBOR_DATABASE_PASSWORD=$harbor_password envsubst '${HARBOR_DATABASE_PASSWORD}' \
  <"$root/infra/on-prem/harbor-postgres.yaml.tmpl" | kubectl apply -f -
kubectl -n harbor wait --for=condition=Ready cluster/harbor-postgres --timeout=10m
MINIO_ROOT_USER=$minio_user MINIO_ROOT_PASSWORD=$minio_password envsubst '${MINIO_ROOT_USER} ${MINIO_ROOT_PASSWORD}' \
  <"$root/infra/on-prem/postgres.yaml.tmpl" | kubectl apply -f -

helm upgrade --install harbor harbor/harbor --version 1.18.4 -n harbor --create-namespace -f "$root/infra/on-prem/harbor-values.yaml" \
  --set harborAdminPassword="$harbor_password" --set-string externalURL="http://${harbor_node_ip}:30002" --wait --timeout 15m
helm upgrade --install kyverno kyverno/kyverno --version 3.9.0 -n kyverno --create-namespace \
  -f "$root/infra/on-prem/kyverno-values.yaml" --wait --timeout 15m
helm upgrade --install vault hashicorp/vault --version 0.34.1 -n vault --create-namespace -f "$root/infra/on-prem/vault-values.yaml" --timeout 10m
"$root/scripts/configure-on-prem-vault.sh"

kubectl create namespace identity-system --dry-run=client -o yaml | kubectl apply -f -
kubectl -n identity-system create secret generic keycloak-bootstrap --from-literal=username=admin --from-literal=password="$keycloak_password" --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f "$root/infra/on-prem/keycloak.yaml"

RFC2136_TSIG_SECRET=$rfc2136_secret envsubst '${RFC2136_TSIG_SECRET}' <"$root/infra/on-prem/dns.yaml.tmpl" | kubectl apply -f -
dns_values=$(mktemp /tmp/on-prem-external-dns.XXXXXX)
trap 'rm -f "$dns_values"' EXIT
RFC2136_TSIG_SECRET=$rfc2136_secret envsubst '${RFC2136_TSIG_SECRET}' <"$root/infra/on-prem/external-dns-values.yaml.tmpl" >"$dns_values"
helm upgrade --install external-dns external-dns/external-dns --version 1.21.1 -n dns-system -f "$dns_values" --wait --timeout 10m
helm upgrade --install argo-cd argo/argo-cd --version 10.8.4 -n argocd --create-namespace -f "$root/infra/on-prem/argocd-values.yaml" --wait --timeout 15m
helm upgrade --install argo-workflows argo/argo-workflows --version 2.0.5 -n argo --create-namespace \
  --set crds.full=false --set server.enabled=false --wait --timeout 15m
helm upgrade --install monitoring prometheus-community/kube-prometheus-stack --version 90.0.0 -n monitoring --create-namespace -f "$root/infra/on-prem/monitoring-values.yaml" --wait --timeout 15m
kubectl apply -f "$root/infra/observability/platform-prometheus-rules.yaml"

kubectl -n platform-database wait --for=condition=Ready cluster/platform-postgres --timeout=15m
kubectl -n identity-system rollout status deployment/keycloak --timeout=10m
kubectl -n dns-system rollout status deployment/bind --timeout=5m
echo "on-prem reference providers installed; generated credentials are restricted to $secret_dir"
