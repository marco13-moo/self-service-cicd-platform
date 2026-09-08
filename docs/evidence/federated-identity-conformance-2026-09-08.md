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
- The architecture graph contains 690 nodes and 1,378 edges with no missing,
  dangling, duplicate, self-loop, or collapsed edges.

## Prepared PostgreSQL controls

Migration 5 enables and forces RLS on `tenant_auths`, creates the tenant status
state machine and append-only `audit_events`, and installs a trigger rejecting
audit update or deletion. The integration suite now exercises auth configuration
scope and audit immutability using the non-owner application role. The HA harness
also grants only the table privileges required by these new tests and preserves
the records through backup and restoration.

## Environmental limitation

The Docker-based PostgreSQL HA harness could not execute because the local
container runtime stopped responding before it created the source container.
The kind API subsequently timed out during TLS negotiation, corroborating a
local runtime outage rather than a test failure. The stuck harness was terminated
without modifying repository or cluster state. Real PostgreSQL migration, RLS,
trigger, backup, and restore execution must be rerun when the runtime is healthy.
