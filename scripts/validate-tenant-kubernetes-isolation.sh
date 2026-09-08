#!/usr/bin/env bash
set -euo pipefail

alpha_namespace=${TENANT_ALPHA_NAMESPACE:-tenant-isolation-alpha}
beta_namespace=${TENANT_BETA_NAMESPACE:-tenant-isolation-beta}
alpha_service_account=${TENANT_ALPHA_SERVICE_ACCOUNT:-tenant-isolation-alpha}

cleanup() {
  kubectl delete namespace "$alpha_namespace" "$beta_namespace" --ignore-not-found --wait=false >/dev/null 2>&1 || true
  kubectl -n argo delete serviceaccount "$alpha_service_account" --ignore-not-found >/dev/null 2>&1 || true
}
trap cleanup EXIT
cleanup

kubectl create namespace "$alpha_namespace" >/dev/null
kubectl create namespace "$beta_namespace" >/dev/null
for specification in "$alpha_namespace:alpha" "$beta_namespace:beta"; do
  namespace=${specification%%:*}
  tenant=${specification##*:}
  kubectl label namespace "$namespace" platform.tenant="$tenant" managed-by=self-service-cicd \
    pod-security.kubernetes.io/enforce=restricted pod-security.kubernetes.io/enforce-version=latest --overwrite >/dev/null
  kubectl -n "$namespace" apply -f - >/dev/null <<EOF
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny
spec:
  podSelector: {}
  policyTypes: [Ingress, Egress]
---
apiVersion: v1
kind: ResourceQuota
metadata:
  name: tenant-budget
spec:
  hard:
    pods: "1"
    requests.cpu: 200m
    requests.memory: 256Mi
    limits.cpu: "1"
    limits.memory: 1Gi
EOF
done

kubectl -n argo create serviceaccount "$alpha_service_account" >/dev/null
kubectl -n "$alpha_namespace" apply -f - >/dev/null <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: tenant-deployer
rules:
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "create", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: tenant-deployer
subjects:
  - kind: ServiceAccount
    name: "$alpha_service_account"
    namespace: argo
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: tenant-deployer
EOF

identity="system:serviceaccount:argo:${alpha_service_account}"
test "$(kubectl auth can-i --as="$identity" create deployments.apps -n "$alpha_namespace")" = yes
test "$(kubectl auth can-i --as="$identity" create deployments.apps -n "$beta_namespace")" = no

privileged_pod=$(mktemp /tmp/tenant-privileged-pod.XXXXXX.yaml)
trap 'rm -f "$privileged_pod"; cleanup' EXIT
printf '%s\n' "apiVersion: v1
kind: Pod
metadata:
  name: forbidden-privileged
  namespace: $alpha_namespace
spec:
  containers:
    - name: shell
      image: busybox:1.36.1
      securityContext:
        privileged: true" >"$privileged_pod"
if kubectl apply --dry-run=server -f "$privileged_pod" >/dev/null 2>&1; then
  echo "restricted Pod Security admitted a privileged tenant pod" >&2
  exit 1
fi

for namespace in "$alpha_namespace" "$beta_namespace"; do
  test "$(kubectl -n "$namespace" get networkpolicy default-deny -o jsonpath='{.spec.policyTypes[*]}')" = "Ingress Egress"
  test "$(kubectl -n "$namespace" get resourcequota tenant-budget -o jsonpath='{.spec.hard.pods}')" = 1
done

echo "Tenant namespace ownership, cross-tenant RBAC denial, quota installation, default-deny networking, and restricted Pod Security conformance passed"
