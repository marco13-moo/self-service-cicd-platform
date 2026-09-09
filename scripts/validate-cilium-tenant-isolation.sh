#!/usr/bin/env bash
set -euo pipefail

alpha=${NETWORK_ALPHA_NAMESPACE:-network-alpha}
beta=${NETWORK_BETA_NAMESPACE:-network-beta}
control=${NETWORK_CONTROL_NAMESPACE:-network-control-plane}

if ! kubectl -n kube-system wait --for=condition=Ready pod -l k8s-app=cilium --timeout=90s >/dev/null; then
  echo "Cilium agents are not ready; network enforcement cannot be asserted" >&2
  exit 1
fi

cleanup() {
  # Wait for finalizers so consecutive scheduled executions cannot collide with
  # namespaces that still exist in a terminating state.
  kubectl delete namespace "$alpha" "$beta" "$control" --ignore-not-found --wait=true >/dev/null 2>&1 || true
}
trap cleanup EXIT
cleanup

for specification in "$alpha:alpha" "$beta:beta" "$control:platform"; do
  namespace=${specification%%:*}
  tenant=${specification##*:}
  kubectl create namespace "$namespace" >/dev/null
  kubectl label namespace "$namespace" platform.tenant="$tenant" app.kubernetes.io/managed-by=self-service-cicd \
    pod-security.kubernetes.io/enforce=restricted --overwrite >/dev/null
  kubectl -n "$namespace" apply -f - >/dev/null <<EOF
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny
spec:
  podSelector: {}
  policyTypes: [Ingress, Egress]
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-same-namespace
spec:
  podSelector: {}
  policyTypes: [Ingress, Egress]
  ingress:
    - from:
        - podSelector: {}
  egress:
    - to:
        - podSelector: {}
EOF
done

for namespace in "$alpha" "$beta"; do
  kubectl -n "$namespace" apply -f - >/dev/null <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: echo
spec:
  replicas: 1
  selector:
    matchLabels: {app: echo}
  template:
    metadata:
      labels: {app: echo, app.kubernetes.io/name: preview, platform.tenant: ${namespace#network-}}
    spec:
      automountServiceAccountToken: false
      securityContext:
        runAsNonRoot: true
        runAsUser: 1000
        seccompProfile: {type: RuntimeDefault}
      containers:
        - name: echo
          image: busybox:1.36.1
          command: [sh, -c, "echo tenant-${namespace#network-} >/tmp/index.html; httpd -f -p 8080 -h /tmp"]
          securityContext:
            allowPrivilegeEscalation: false
            capabilities: {drop: [ALL]}
          resources:
            requests: {cpu: 10m, memory: 8Mi}
            limits: {cpu: 100m, memory: 32Mi}
---
apiVersion: v1
kind: Service
metadata:
  name: echo
spec:
  selector: {app: echo}
  ports: [{port: 8080, targetPort: 8080}]
---
apiVersion: v1
kind: Pod
metadata:
  name: probe
  labels: {app: probe, app.kubernetes.io/name: preview, platform.tenant: ${namespace#network-}}
spec:
  automountServiceAccountToken: false
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    seccompProfile: {type: RuntimeDefault}
  containers:
    - name: probe
      image: busybox:1.36.1
      command: [sleep, "3600"]
      securityContext:
        allowPrivilegeEscalation: false
        capabilities: {drop: [ALL]}
      resources:
        requests: {cpu: 10m, memory: 8Mi}
        limits: {cpu: 100m, memory: 32Mi}
EOF
done

kubectl -n "$alpha" apply -f - >/dev/null <<EOF
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: declared-egress
  labels: {platform.tenant: alpha}
spec:
  endpointSelector:
    matchLabels:
      app: probe
  egress:
    - toEndpoints:
        - matchLabels:
            k8s:io.kubernetes.pod.namespace: kube-system
            k8s:k8s-app: kube-dns
      toPorts:
        - ports:
            - {port: "53", protocol: UDP}
            - {port: "53", protocol: TCP}
          rules:
            dns:
              - matchName: example.com
    - toFQDNs:
        - matchName: example.com
      toPorts:
        - ports:
            - {port: "443", protocol: TCP}
EOF

kubectl -n "$alpha" apply -f - >/dev/null <<EOF
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: allow-preview-gateway
  labels: {platform.tenant: alpha}
spec:
  endpointSelector:
    matchLabels: {app: echo}
  ingress:
    - fromEntities: [ingress]
EOF

# Edge ownership is enforced before any route is persisted. The valid route
# uses the alpha prefix; a beta-owned host must fail admission.
kubectl -n "$alpha" apply -f - >/dev/null <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: governed-preview
  labels: {platform.tenant: alpha}
spec:
  parentRefs:
    - {name: platform-preview, namespace: preview-gateway}
  hostnames: [t-alpha-echo.preview.example.test]
  rules:
    - backendRefs: [{name: echo, port: 8080}]
EOF
if kubectl -n "$alpha" apply -f - >/dev/null 2>&1 <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: foreign-host
  labels: {platform.tenant: alpha}
spec:
  parentRefs:
    - {name: platform-preview, namespace: preview-gateway}
  hostnames: [t-beta-stolen.preview.example.test]
  rules:
    - backendRefs: [{name: echo, port: 8080}]
EOF
then
  echo "tenant claimed another tenant's preview hostname" >&2; exit 1
fi

kubectl -n preview-gateway apply -f - >/dev/null <<'EOF'
apiVersion: v1
kind: Pod
metadata:
  name: network-probe
spec:
  automountServiceAccountToken: false
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    seccompProfile: {type: RuntimeDefault}
  containers:
    - name: probe
      image: busybox:1.36.1
      command: [sleep, "3600"]
      securityContext:
        allowPrivilegeEscalation: false
        capabilities: {drop: [ALL]}
      resources:
        requests: {cpu: 10m, memory: 8Mi}
        limits: {cpu: 100m, memory: 32Mi}
EOF
trap 'kubectl -n preview-gateway delete pod network-probe --ignore-not-found --wait=false >/dev/null 2>&1 || true; cleanup' EXIT

kubectl wait --for=condition=Available deployment/echo -n "$alpha" --timeout=3m >/dev/null
kubectl wait --for=condition=Available deployment/echo -n "$beta" --timeout=3m >/dev/null
kubectl wait --for=condition=Ready pod/probe -n "$alpha" --timeout=3m >/dev/null
kubectl wait --for=condition=Ready pod/probe -n "$beta" --timeout=3m >/dev/null
kubectl wait --for=condition=Ready pod/network-probe -n preview-gateway --timeout=3m >/dev/null

alpha_ip=$(kubectl -n "$alpha" get service echo -o jsonpath='{.spec.clusterIP}')
beta_ip=$(kubectl -n "$beta" get service echo -o jsonpath='{.spec.clusterIP}')
kubernetes_ip=$(kubectl -n default get service kubernetes -o jsonpath='{.spec.clusterIP}')

kubectl -n "$alpha" exec probe -- wget -q -T 3 -O- "http://${alpha_ip}:8080" | grep -q tenant-alpha
if kubectl -n "$alpha" exec probe -- wget -q -T 3 -O- "http://${beta_ip}:8080" >/dev/null 2>&1; then
  echo "alpha reached beta across the tenant boundary" >&2; exit 1
fi
if kubectl -n "$beta" exec probe -- wget -q -T 3 -O- "http://${alpha_ip}:8080" >/dev/null 2>&1; then
  echo "beta reached alpha without gateway authority" >&2; exit 1
fi
gateway_ip=$(kubectl -n preview-gateway get service cilium-gateway-platform-preview -o jsonpath='{.spec.clusterIP}')
kubectl -n preview-gateway exec network-probe -- wget -q -T 5 -O- \
  --header 'Host: t-alpha-echo.preview.example.test' "http://${gateway_ip}" | grep -q tenant-alpha
if kubectl -n "$alpha" exec probe -- wget -q -T 3 -O- "https://${kubernetes_ip}:443" --no-check-certificate >/dev/null 2>&1; then
  echo "tenant reached the Kubernetes control plane" >&2; exit 1
fi
if kubectl -n "$alpha" exec probe -- wget -q -T 3 -O- http://169.254.169.254/latest/meta-data/ >/dev/null 2>&1; then
  echo "tenant reached cloud instance metadata" >&2; exit 1
fi
kubectl -n "$alpha" exec probe -- nslookup example.com >/dev/null
if kubectl -n "$alpha" exec probe -- nslookup "exfil-$(date +%s).invalid" >/dev/null 2>&1; then
  echo "undeclared DNS query escaped the Cilium DNS policy" >&2; exit 1
fi

# Removing an allow policy must contract connectivity because Kubernetes
# default-deny remains authoritative underneath the generated Cilium policy.
kubectl -n "$alpha" delete ciliumnetworkpolicy declared-egress >/dev/null
if kubectl -n "$alpha" exec probe -- nslookup example.com >/dev/null 2>&1; then
  echo "policy deletion unexpectedly opened DNS egress" >&2; exit 1
fi

echo "Cilium enforced tenant traffic, governed edge ownership, exact DNS egress, control-plane/metadata denial, and fail-closed policy deletion"
