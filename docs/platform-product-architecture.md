# Autonomous Internal Developer Platform

This repository is the product foundation for an internal platform, not a
generic CI wrapper. Application teams are the primary persona: they declare a
service once, see tenant-scoped status and diagnostics, and use a golden path
for preview environments. Platform engineers own policy, contracts, provider
adapters, and recovery. Security and finance personas consume audit, compliance,
SLO, and cost evidence.

## Golden path and boundaries

The golden path is **declare -> validate -> reconcile -> observe -> deliver**.
The versioned `platform.service/v1` declaration is the durable boundary between
developer intent and platform implementation. The control plane owns identity,
tenancy, validation, desired state, policy, reconciliation, and status
projection. The data/execution plane (Kubernetes, Argo, SCM, registries, and
delivery systems) owns builds and workload execution; it never becomes the
source of policy or tenant identity.

Bounded contexts are catalog (service intent), tenancy (ownership and quotas),
policy (admission and compliance), reconciliation (desired/observed convergence),
provisioning (environment resources), delivery (promotion), and telemetry
(SLO, audit, and cost evidence). Provider adapters remain behind those
boundaries. Existing service registration, tenant isolation, ephemeral
environments, webhooks, diagnostics, and provider-neutral delivery are
retained. The next boundary is durable catalog intent and status projection,
not a rewrite of the existing execution workflows.

## Tenancy and lifecycle

Every service and environment has one immutable tenant owner. API reads and
writes are tenant-scoped, PostgreSQL RLS remains authoritative in production,
and quotas/isolation are policy invariants rather than UI hints. A declaration
progresses through `active`, `paused`, and `retired`; registration records
desired state immediately, while reconciliation advances observed state and
records explicit failures. No declaration contains credentials.

Initial platform objectives are 99.9% monthly API availability, p95 catalog
read latency below 250 ms, and a 15-minute reconciliation convergence target
under normal provider health. Recovery objectives and provider-specific SLOs
remain deployment-profile configuration, evidenced by the on-premise runbooks.

## Roadmap

1. **Foundation (this slice):** versioned catalog contract, tenant-scoped
   register/list/diagnose behavior, desired/observed status, ADR roadmap.
2. **Convergence:** durable reconciliation queues, policy decisions, quota
   accounting, and generation-safe status conditions.
3. **Delivery:** GitOps promotion, progressive rollout policy, rollback
   evidence, and cost/SLO attribution.
4. **Autonomy:** dependency-aware remediation and bounded automation with
   human approval for high-impact changes.

Non-goals for this slice are a portal, a new workflow engine, cloud-specific
provisioners, credentials in Git, or speculative autonomous remediation.
