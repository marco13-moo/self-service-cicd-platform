# On-Prem Platform Installation and Operations

## Installation

Verify the signed `platform.release/v1` manifest, generate the complete image
lock from the certified cluster and mirror it with
`mirror-on-prem-images.sh`. Render with `render-on-prem-platform.sh`; then
`install-on-prem-platform.sh` materializes the exact candidate workspace as an
in-cluster Git bundle, synchronizes the CNPG credential and locally governed
bootstrap credentials, applies the platform, and establishes the Argo CD
application. Argo Workflows is a distinct, version-pinned provider and must be
installed by `bootstrap-on-prem-reference.sh` before this step. Run
`validate-on-prem-platform.sh`; installation is incomplete until its signed BOM
reports every gate passing.

Never commit `.on-prem-secrets`, bearer tokens, database URIs, private keys, or
acceptance logs. PostgreSQL is authoritative; do not mount the legacy JSON PVC.

## Configuration and diagnosis

Non-secret endpoints are held in the `on-prem-platform` ConfigMap. Credentials
originate in their owning provider and enter Kubernetes Secrets through the
installation boundary. Inspect Argo CD application health, Deployment rollout,
`/readyz`, `/metrics`, tenant-scoped diagnostics, SCM command state, Argo
Workflow phase, and PostgreSQL lease rows—in that order. Preserve correlation
IDs when joining API, audit, and workflow events.

## Rotation

- Rotate OIDC keys overlap-first. Retain one predecessor JWKS set only for the
  bounded rollover interval, then rerun identity conformance.
- Rotate Vault signing roots with `rotate-vault-signing-key.sh`; establish
  overlapping admission trust before changing the signer and retain historical
  public verification material for governed evidence.
- Rotate registry, database, and break-glass credentials at their provider,
  resynchronize Kubernetes Secrets, restart one replica at a time, and verify
  readiness before continuing.

## Upgrades and rollback

Build once, sign, attest, scan, mirror, and render the new digest. Review the
server-side diff and let Argo CD perform a zero-unavailable rollout. Acceptance
terminates a replica during reconciliation and proves lease recovery. A failed
rollout is reverted to the preceding certified digest; never rebuild during
rollback. Any provider-version change invalidates certification.

## PostgreSQL backup and restoration

Follow `control-plane-ha.md`. Confirm WAL archival before backup, restore into
an independently created cluster, compare schema migration and tenant/service/
environment/command counts, then run the PostgreSQL suite against the restored
endpoint. Do not promote a restore inferred only from object existence.

## Provider recovery

- **Identity:** restore Keycloak state, validate issuer/audience/JWKS reachability,
  rotation, revocation, and token expiry before enabling tenant mutations.
- **Registry:** restore the digest and all OCI referrers, then verify signature,
  SBOM, provenance, and policy attestations before scaling workloads up.
- **DNS/certificates:** validate the authoritative RFC2136 record, certificate
  readiness, Gateway attachment, and TLS hostname. Do not bypass governed edge
  admission during an incident.
- **Control plane:** if readiness fails, distinguish PostgreSQL from Argo
  dependency failure. Liveness must not restart all replicas for a shared
  dependency outage. Expired PostgreSQL leases are recoverable by another
  replica; do not mutate queue rows manually.
