#!/usr/bin/env bash
set -euo pipefail

cluster=${KIND_CLUSTER_NAME:-self-service-cicd-cilium}
repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cilium_version=${CILIUM_VERSION:-1.20.1}
gateway_api_version=${GATEWAY_API_VERSION:-v1.6.2}

if kind get clusters | grep -Fxq "$cluster"; then
  echo "kind cluster $cluster already exists"
else
  kind create cluster --name "$cluster" --config "$repository_root/infra/kind/cilium-cluster.yaml"
fi
kubectl config use-context "kind-$cluster" >/dev/null

kubectl apply --server-side -f "https://github.com/kubernetes-sigs/gateway-api/releases/download/${gateway_api_version}/standard-install.yaml"
helm upgrade --install cilium cilium/cilium \
  --version "$cilium_version" \
  --namespace kube-system \
  --set ipam.mode=kubernetes \
  --set kubeProxyReplacement=true \
  --set "k8sServiceHost=${cluster}-control-plane" \
  --set k8sServicePort=6443 \
  --set gatewayAPI.enabled=true \
  --set operator.replicas=1 \
  --wait --timeout 10m

kubectl wait --for=condition=Ready pods -n kube-system -l k8s-app=cilium --timeout=5m
kubectl apply -f "$repository_root/infra/kind/cilium-load-balancer-pool.yaml"
kubectl apply -f "$repository_root/infra/k8s/preview-gateway.yaml"

# The local HTTPS listener uses disposable cert-manager-labelled material. A
# production cluster replaces this with its cert-manager Certificate output.
certificate_directory=$(mktemp -d /tmp/self-service-cicd-preview-tls.XXXXXX)
trap 'rm -f "$certificate_directory/tls.key" "$certificate_directory/tls.crt"; rmdir "$certificate_directory" >/dev/null 2>&1 || true' EXIT
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=preview.example.test \
  -addext subjectAltName=DNS:preview.example.test,DNS:*.preview.example.test \
  -keyout "$certificate_directory/tls.key" -out "$certificate_directory/tls.crt" >/dev/null 2>&1
kubectl -n preview-gateway create secret tls preview-wildcard-tls \
  --cert="$certificate_directory/tls.crt" --key="$certificate_directory/tls.key" \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl -n preview-gateway label secret preview-wildcard-tls \
  app.kubernetes.io/managed-by=cert-manager platform.tenant=platform --overwrite >/dev/null
kubectl apply -f "$repository_root/infra/k8s/preview-edge-governance-policy.yaml"
kubectl wait --for=condition=Accepted gateway/platform-preview -n preview-gateway --timeout=3m
kubectl wait --for=condition=Programmed gateway/platform-preview -n preview-gateway --timeout=3m
echo "kind cluster $cluster is enforcing Cilium NetworkPolicy and Gateway API"
