# ADR 0018: Tenant Identity, Authorization, and Row Isolation

## Status

Accepted

## Context

ADR 0017 made PostgreSQL authoritative and allowed interchangeable control-plane
replicas, but every caller still occupied one undifferentiated trust domain.
Service names, environment names, webhook identities, commands, and observations
therefore had platform-global semantics. Application filtering alone would be an
insufficient isolation boundary: one omitted predicate could disclose or mutate
another organization's desired state.

## Decision

### Tenant identity is immutable domain data

Every service, environment, SCM delivery, and lifecycle command carries an
immutable DNS-label-compatible tenant identifier. Database identity is composite:
service and environment names are unique within a tenant, webhook deliveries are
unique within tenant and provider, and command identity is tenant-scoped. Tenant
rows are provisioned from the authenticated control-plane configuration before
traffic is served.

A source repository has exactly one tenant owner. This invariant permits a
signature-authenticated provider webhook to resolve its tenant from the
normalized repository identity without accepting a caller-controlled tenant
header. Unsupported events that yield no lifecycle command are acknowledged but
do not create unowned delivery state.

### Authentication produces a tenant capability

The initial authentication adapter consumes `TENANT_AUTH_TOKENS`, a Secret-backed
JSON map from opaque bearer tokens to subject, tenant, and role. Tokens are
hashed during initialization and never retained in plaintext. Roles are ordered
`viewer`, `developer`, and `admin`; route middleware authenticates first, checks
the minimum role, and installs an immutable principal in request context.

This static adapter is a bootstrap interface, not the terminal identity system.
An OIDC or workload-identity adapter may replace token validation without
changing authorization or repository semantics. Production startup fails when
tenant authentication is absent or malformed.

### PostgreSQL RLS is the mandatory backstop

Each pooled database operation starts a transaction and binds `app.tenant_id`
with transaction-local `set_config`. PostgreSQL Row-Level Security is enabled and
forced on all tenant-bearing tables. Both `USING` and `WITH CHECK` policies
compare rows to the bound tenant, preventing read and write escape even if an
application query omits its tenant predicate. Explicit predicates remain in the
repositories for query planning and defence in depth.

The database role used for API traffic must be non-superuser. Superusers bypass
RLS and are reserved for migrations, tenant provisioning, backup, and recovery.
The conformance suite exercises RLS through a separately authenticated,
non-owner, non-superuser role.

### Cross-tenant scheduling is narrow and auditable

The reconciler may enumerate environments and lease the next eligible command
across tenants through an internal bypass capability. Immediately after leasing,
it derives a tenant-scoped state repository and command repository from the
durable command tenant ID. Reconciliation, completion, deployment observation,
and lease exclusion then occur within that tenant. HTTP handlers never receive
the bypass capability.

The JSON backend remains a single-process development mode. It implements the
same tenant keying and filtering semantics, but PostgreSQL RLS is the production
security boundary.

## Consequences

- Tenants may use identical service and environment names without collision.
- Missing or invalid bearer credentials fail with `401`; authenticated roles
  below a route's requirement fail with `403`; inaccessible tenant resources are
  indistinguishable from absent resources.
- Repository ownership is globally unique until a governed transfer protocol is
  introduced.
- Backup and restore must preserve tenant rows, composite keys, RLS policies,
  grants, and authoritative state as one aggregate.
- Static bearer-token distribution and rotation remain an operational burden;
  federated identity is the expected successor.

## Operational invariants

- Never derive tenant scope from an untrusted request header or webhook field.
- Never run the API or reconciler with a PostgreSQL superuser credential.
- Never execute a tenant query outside a transaction-local identity scope.
- Never expose the reconciliation bypass repository to HTTP handlers.
- Never transfer repository ownership by directly editing `tenant_id`; use a
  future audited transfer transaction that quiesces webhooks and commands.
- Never treat application predicates as a substitute for RLS conformance.

## Conformance

Unit tests prove authentication, role enforcement, same-name resource isolation,
and non-disclosure across tenant APIs. `scripts/validate-control-plane-ha.sh`
creates a non-superuser application role and proves RLS against source and
restored PostgreSQL authorities. The suite also replays deduplication, command
leasing, replica termination, and reconciliation so tenant isolation cannot
regress the ADR 0017 availability guarantees.
