# Tenant isolation conformance — 2026-09-08

## Verified controls

- Anonymous tenant API requests return `401`, and a viewer attempting mutation
  receives `403`.
- Alpha and beta tenants can independently create the same service name; each
  tenant's list response contains only its own repository and tenant identity.
- Tenant-scoped environment reads return not found for an environment owned by
  another tenant.
- PostgreSQL composite identities permit same-name service rows while explicit
  repository queries remain tenant constrained.
- A dedicated non-superuser `platform_app` role executed a raw same-name query
  without an application tenant predicate. Forced RLS returned exactly the row
  matching its transaction-local `app.tenant_id`.
- The RLS suite passed against both the source database and an independently
  restored logical backup.
- Existing concurrent migration, webhook deduplication, command lease handoff,
  replica termination, and reconciliation continuation tests remained green.

## Evidence commands

```text
cd control-plane
go test ./...
go vet ./...

./scripts/validate-control-plane-ha.sh
imported 1 services and 1 environments
ok  .../internal/api
ok  .../internal/reconciler
ok  .../internal/api
ok  .../internal/reconciler
Control-plane replica handoff and PostgreSQL backup/restore conformance passed
```

## Boundary

This evidence validates the bootstrap bearer-token adapter, application
authorization, and database row isolation. Namespace ownership, quotas,
NetworkPolicies, Pod Security, tenant-specific workload identities, federated
OIDC, and policy partitioning are intentionally deferred to the next ADR 0018
implementation slice.
