# Federated tenant identity conformance — 2026-09-08

## Verified controls

- The complete Go test, vet, and control-plane build gates pass.
- OIDC validation rejects wrong issuer, audience, tenant, expiry, role mapping,
  unsigned tokens, unknown keys, and unsupported algorithms.
- RSA trust roots below 2048 bits and unsafe JWKS endpoint configurations fail
  closed.
- JWKS rollover accepts one predecessor set only during its configured grace
  interval; identical refreshes do not evict that predecessor prematurely.
- A tenant administrator cannot acquire platform lifecycle privileges, and the
  obsolete tenant-selector authentication route is absent.
- Kubernetes deployment and bootstrap Secret manifests pass client-side schema
  construction.
- A disposable Keycloak 26.7.3 realm issued HTTPS-issuer tokens that passed the
  production validator before and after RSA JWKS rotation. Removing the user's
  mapped group made the next token fail closed, and the prior token failed after
  its four-second TTL plus the validator's bounded 30-second clock-skew window.
- The hybrid authorizer retained an independently tested static platform-admin
  credential as the bounded rollback path.
- The architecture graph contains 690 nodes and 1,378 edges with no missing,
  dangling, duplicate, self-loop, or collapsed edges.

## PostgreSQL and audit-export controls

Migration 5 enables and forces RLS on `tenant_auths`, creates the tenant status
state machine and append-only `audit_events`, and installs a trigger rejecting
audit update or deletion. The integration suite now exercises auth configuration
scope and audit immutability using the non-owner application role. The HA harness
also grants only the table privileges required by these new tests and preserves
the records through backup and restoration.

## Operational execution — 2026-09-09

The restored Docker runtime executed the complete HA harness against PostgreSQL
17.6. Migration 5, forced RLS through the non-owner application role, issuer
uniqueness, tenant suspension, lease exclusion, audit mutation denial, command
handoff after replica termination, backup, restoration, and the post-restore
suite all passed. An insert-only logical publication and decoding slot observed
the uniquely correlated audit event; the sealed segment SHA-256 was
`e394ccc943e2766c53196abf7d42e1b75372b2547cab6d4434b4613478378625`.
