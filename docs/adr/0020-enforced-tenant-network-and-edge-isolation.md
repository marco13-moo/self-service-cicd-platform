# ADR 0020: Enforced Tenant Network and Edge Isolation

## Status

Accepted

## Context

ADR 0018 made namespaces, runtime identities, quotas, admission controls, and
artifact evidence tenant-owned. Kubernetes `NetworkPolicy` objects alone do not
prove packet enforcement, however, and unconstrained DNS, arbitrary egress, or
shared ingress objects would leave viable cross-tenant and exfiltration paths.

## Decision

### Cilium is the normative policy engine

Production clusters use a declared, NetworkPolicy-enforcing CNI. Cilium is the
reference implementation and the local conformance cluster disables kind's
default CNI before installing a pinned Cilium release. A cluster is not ready
for preview workloads until Cilium agents and its operator are healthy.

Each preview namespace retains a Kubernetes default-deny policy. Cilium policy
then grants only explicitly declared flows. Absence or deletion of the generated
policy therefore fails closed; it never restores unrestricted connectivity.

### Egress is part of service intent

A service may declare exact DNS names, ports, and TCP or UDP protocols. Wildcard
DNS names are rejected by the API. Reconciliation converts the declaration into
a tenant-labelled `CiliumNetworkPolicy`: DNS queries are permitted only for the
declared names, and subsequent traffic is limited to the corresponding FQDN and
port. The policy contains no IP-wide fallback. Kubernetes API service addresses
and link-local cloud metadata addresses consequently remain unreachable.

### Preview ingress has one governed attachment point

External preview traffic enters through the platform-owned `preview-gateway`
namespace and Gateway API `Gateway`. Tenant workloads create only `HTTPRoute`
objects. Admission requires routes to attach to that Gateway and requires every
host to use the tenant-prefixed governed preview domain. Tenant namespaces
accept preview traffic from Cilium's reserved `ingress` identity, not from
arbitrary pods that happen to occupy the gateway namespace. Gateway TLS material
is platform-owned and cert-manager-labelled.

### Conformance is continuous and adversarial

The conformance harness creates two disposable tenant namespaces and proves:
same-tenant traffic succeeds; cross-tenant traffic fails; gateway traffic
succeeds; arbitrary ingress, undeclared DNS, Kubernetes API, and metadata access
fail; and deleting a generated allow policy does not open egress. A scheduled
CronJob runs the same suite from an operations image in production-like clusters.
CNI agent unavailability is a failed platform readiness condition and pages the
platform operator; preview scheduling must be suspended until enforcement is
restored.

## Consequences

- Service registration has a stable, provider-neutral egress contract.
- Exact FQDN policy is intentionally incompatible with wildcard SaaS endpoints;
  those require an explicit reviewed platform exception.
- Gateway API and Cilium CRDs are cluster prerequisites and are upgraded through
  a conformance-tested platform release, not independently by application teams.
- DNS policy reduces but cannot semantically inspect encrypted application data;
  sensitive destinations still require organizational egress proxies and DLP.

## Security invariants

- Never schedule tenant previews on a cluster whose policy CNI is unhealthy.
- Never infer egress permission from image metadata or source code.
- Never permit wildcard DNS, unrestricted CIDRs, the Kubernetes API, or cloud
  metadata through a generated tenant policy.
- Never let a tenant own the Gateway, wildcard certificate, or another tenant's
  hostname.
- Policy deletion must reduce connectivity, not increase it.

## Rollout and rollback

1. Bootstrap a non-production Cilium cluster and run packet conformance.
2. Enable Gateway API and edge admission, then migrate previews from Ingress to
   `HTTPRoute`.
3. Require egress declarations and deploy generated Cilium policies.
4. Enable scheduled conformance and Cilium health alerting before production.
5. Rollback drains preview workloads and restores the prior CNI only during a
   controlled cluster replacement; it never runs two policy engines with
   ambiguous responsibility.

## Conformance

`scripts/bootstrap-kind-cilium.sh` builds the reference cluster.
`scripts/validate-cilium-tenant-isolation.sh` provides packet-level assertions.
Go tests verify egress contract validation and deterministic policy generation.
`infra/k8s/network-isolation-conformance-cronjob.yaml` supplies the continuous
execution boundary; production promotion replaces its local image reference
with the signed digest built from `infra/conformance/Dockerfile`.
