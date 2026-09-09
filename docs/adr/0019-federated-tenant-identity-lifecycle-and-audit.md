# ADR 0019: Federated Tenant Identity, Lifecycle, and Immutable Audit

## Status

Accepted

## Context

ADR 0018 established tenant ownership across PostgreSQL, Kubernetes namespaces,
runtime identities, and artifact policy. Its bootstrap bearer-token map cannot,
however, express identity-provider membership changes, cryptographic rollover,
tenant suspension, or a durable administrative history. Authentication data is
itself tenant-sensitive and cannot rely exclusively on application predicates.

## Decision

### OIDC produces the same immutable tenant capability

The control plane accepts static bootstrap tokens and OIDC JWTs during a bounded
migration interval. An unverified issuer claim may select a candidate provider,
but never establishes identity. A Principal is created only after signature,
approved algorithm, key identifier, issuer, audience, expiration, optional
tenant claim, subject, and group-to-role mapping all validate.

OIDC issuers have exactly one tenant owner. Provider configuration is managed
through the authenticated tenant's own `/api/v1/tenant/auth` resource; callers
cannot select another tenant in a path or payload. PostgreSQL RLS is enabled and
forced on the configuration table. Cross-tenant issuer collisions fail closed.

The control plane will not mint machine credentials. Workloads use the external
identity provider's OAuth client-credentials or workload-identity flow, keeping
private signing authority and revocation semantics outside this service.

### JWKS rollover is bounded and fail-closed

Only HTTPS JWKS endpoints are accepted outside loopback tests. Responses are
time-bounded and size-bounded; keys require unique `kid`, explicit signing use,
an approved RSA or EC algorithm, and adequate key strength. Cache refresh uses
provider cache metadata subject to a local maximum, background refresh with
bounded backoff and jitter, and synchronous refresh for an unknown `kid`.

Exactly one predecessor key set remains eligible during a five-minute rollover
window. Stale keysets fail closed after fifteen minutes. Fetch failure never
replaces the last valid current set, extends predecessor trust, or accepts an
unknown key.

### Tenant lifecycle is explicit

Tenants transition between `active`, `suspended`, and `offboarded`. Suspension
denies API capabilities and prevents new command leases while preserving state
for recovery. Offboarding is permitted only after services and environments are
removed. Repository transfer is a platform-administrator transaction requiring
an active target and no extant preview environments; the service ownership and
version change atomically.

Administrative lifecycle routes require both the tenant-admin role and an
explicit `platform_admin` capability. Tenant-admin alone never grants a global
control-plane capability.

### Audit is append-only tenant state

Authentication decisions, auth-configuration changes, lifecycle transitions,
and transfers append structured events containing correlation ID, actor,
tenant, event type, resource identity, outcome, and non-sensitive metadata.
Forced RLS partitions reads and inserts. A database trigger rejects update and
delete operations, including privileged application mistakes.

Production exports this table through PostgreSQL logical replication or WAL
shipping into the organization's immutable security archive. The control plane
does not synchronously depend on an external audit vendor: loss of that vendor
cannot make an authorized API mutation partially succeed.

## Consequences

- Tenant membership and role revocation follow the external provider's token
  lifetime; short access-token TTLs are therefore mandatory.
- Issuer uniqueness prevents ambiguous tenant discovery before validation.
- Bootstrap tokens remain available for migration and break-glass recovery, but
  production operators can run without them after OIDC configuration exists.
- Suspension is reversible; offboarding is intentionally destructive only after
  owned application state reaches zero.
- Audit retention and external archival become database operational obligations.

## Security invariants

- Never derive authorization from an unverified JWT claim.
- Never accept `none`, HMAC, an implicit algorithm, a missing `kid`, or an
  undersized public key.
- Never accept a tenant identifier from an OIDC token unless it agrees with the
  provider's persisted owner.
- Never expose a tenant-selector parameter on self-service authentication APIs.
- Never permit an inactive tenant to authenticate or lease new commands.
- Never update or delete an audit event.

## Migration and rollback

1. Apply migration 5 and verify forced RLS and the audit immutability trigger.
2. Configure one OIDC provider per pilot tenant while static tokens remain live.
3. Prove JWT acceptance, rotation, revocation, and static-token fallback.
4. Remove ordinary static tokens, retaining a separately controlled break-glass
   credential until the rollback interval closes.
5. Rollback disables OIDC at the router and restores the break-glass map; schema
   and audit rows remain forward-compatible and are never deleted.

## Conformance

Unit tests cover issuer, audience, expiration, tenant, group, algorithm, key
strength, unknown-key refresh, predecessor expiry, and unsafe endpoint rejection.
API tests prove self-tenant administration and platform-capability separation.
The PostgreSQL suite proves auth-configuration RLS, issuer uniqueness, tenant
suspension, command lease exclusion, and audit immutability through a non-owner
application role.

Operational closure is automated by `scripts/validate-control-plane-ha.sh`. Its
PostgreSQL instance enables logical WAL, exposes `audit_events` through an
insert-only publication, consumes a uniquely correlated event through a logical
decoding slot, seals and hashes the resulting archive segment, and repeats the
database suite after backup restoration. The provider-facing validator suite
uses a live rotating HTTPS JWKS endpoint and proves group revocation and token
expiry in addition to predecessor-key rollover and static-token fallback.
