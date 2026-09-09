# Production promotion and recovery

## Release invariant

`release-platform-images.sh` emits two digest-addressed images, SPDX SBOMs,
SLSA provenance, vulnerability results, signatures, attestations, and a signed
`platform.release/v1` manifest. Promotion verifies that manifest and every image
before rendering. A tag, missing attestation, critical unmitigated finding, or
unsigned manifest stops the release.

The image markers in `infra/k8s` are deliberately non-deployable substitutions.
Local and production operators must render them from a verified release manifest;
there is no mutable fallback image.

## Promotion

1. Run the image release in the isolated build identity.
2. Render production manifests with site-owned domain, LoadBalancer CIDR,
   backup destination, and certified public key.
3. Promote to staging; run provider certification and the onboarding suite.
4. Approve the exact manifest digest for production.
5. Promote production and retain the preceding signed manifest for rollback.

## Identity and break glass

Production pods contain no ordinary `TENANT_AUTH_TOKENS`. OIDC and workload
identity are mandatory. During an identity outage, incident command may create a
single platform-admin token secret with an expiry and ticket reference, patch it
into the deployment, and record all operations. Revoke and remove it before
closing the incident; audit export must prove its complete use.

## Backup, failover, and disaster recovery

The operator continuously archives encrypted WAL and takes scheduled encrypted
backups. Quarterly, restore the latest backup plus WAL into an isolated cluster,
verify audit continuity and application checksums, then record observed data-loss
(RPO) and service-restoration (RTO) durations in certification evidence. Monthly,
terminate the primary and verify automated failover while commands reconcile.
Targets are RPO <= 5 minutes and RTO <= 30 minutes; exceeding either blocks
promotion.
