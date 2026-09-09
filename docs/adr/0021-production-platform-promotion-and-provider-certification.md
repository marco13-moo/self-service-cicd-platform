# ADR 0021: Production Platform Promotion and Provider Certification

- Status: Accepted
- Date: 2026-09-09

## Context

The control plane has conformance coverage for high availability, identity,
artifact trust, and tenant network isolation. Those capabilities are not a
production release until the exact binaries, provider versions, configuration,
and recovery evidence are bound to an immutable promotion record.

## Decision

Every releasable control-plane and network-conformance image is built once,
published by digest, scanned, accompanied by SPDX SBOM and SLSA provenance,
signed with the production workload identity, and recorded in a release
manifest. Staging and production consume that manifest; mutable image tags and
unverified substitutions are rejected.

Production uses a three-instance PostgreSQL operator deployment with synchronous
replication, continuous WAL archival, encrypted backups, automated failover,
and measured RPO/RTO. Human traffic uses OIDC. Static credentials are prohibited
except for a separately stored, time-bound, audited break-glass principal.

Preview ingress is composed from cert-manager, external-dns, Gateway API, and a
site-owned Cilium LoadBalancer implementation. KMS, registry, PostgreSQL, and
Cilium selections are not called certified until `certify-production-providers.sh`
has emitted a passing evidence bundle for the precise release and versions.

GitOps promotion proceeds development to staging to production, verifies policy
and manifest drift before mutation, uses rollout health and conformance gates,
and automatically reapplies the preceding signed release on failure. Disaster
recovery is exercised, not inferred from backup existence.

## Consequences

- A release can be promoted without rebuilding it.
- Production values and credentials remain site configuration, never examples
  committed as authoritative secrets.
- Provider upgrades invalidate certification and require a fresh evidence run.
- Promotion stops when provenance, vulnerability policy, drift, SLO, or recovery
  evidence is absent.
