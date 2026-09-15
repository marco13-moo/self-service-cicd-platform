# On-Premises Platform Usage Guide

This guide is the executable path from a source checkout to an accepted,
day-two-operable installation of the self-service CI/CD platform. It implements
the contract in [ADR 0023](../adr/0023-on-prem-platform-installation-and-day-2-operations.md).
The reference profile deliberately remains on premises and does not require a
hosted source-control, identity, registry, database, KMS, or observability
service at runtime.

## 1. Operating model

The installation has three distinct authorities:

1. A signed `platform.release/v1` document identifies releasable artifacts.
2. `config/airgap/on-prem-images.lock.json` identifies every executable image
   by digest and is the transfer manifest for the disconnected site.
3. Argo CD reconciles the rendered platform from the internal Git service.

PostgreSQL is the sole production state authority. Argo Workflows executes
environment lifecycle operations. Harbor stores release and preview artifacts;
Vault provides tenant-partitioned signing; Kyverno and Cilium enforce artifact
and network policy. Installation scripts never make these controls optional.

## 2. Prerequisites

The connected preparation workstation requires:

- Docker and `kind`
- `kubectl` and Helm
- Go matching `control-plane/go.mod`
- `jq`, `openssl`, `envsubst`, `curl`, `tar`, and Git
- `cosign`, `syft`, `trivy`, and `skopeo` for releases
- `regctl`, or Docker access for the pinned `regctl` container fallback

The reference topology needs three schedulable Kubernetes nodes. Size the host
for all stateful providers and admission controllers; 8 CPU cores and 16 GiB of
memory should be treated as a practical development minimum, not a production
capacity prescription. Production sizing must follow measured workload demand,
failure-domain requirements, RPO/RTO objectives, and provider guidance.

Confirm tools before changing cluster state:

```bash
docker version
kind version
kubectl version --client
helm version
go version
jq --version
cosign version
syft version
trivy --version
skopeo --version
```

Never commit `.on-prem-secrets/`, rendered credentials, private signing keys,
database URLs, bearer tokens, or raw acceptance logs.

## 3. Create the reference Kubernetes cluster

From the repository root:

```bash
./scripts/bootstrap-kind-cilium.sh
kubectl config use-context kind-self-service-cicd-cilium
kubectl get nodes -o wide
```

The expected result is one control-plane node and two workers, all `Ready`, with
Cilium healthy. The bootstrap is intended for disposable certification and
development clusters. For an existing production Kubernetes installation,
retain its lifecycle mechanism and certify it against the profile instead of
creating a nested `kind` topology.

## 4. Install the pinned on-premises providers

```bash
./scripts/bootstrap-on-prem-reference.sh
```

This installs the versions recorded in
`config/certification/on-prem-reference.yaml`, including Harbor, Vault,
CloudNativePG, MinIO, Keycloak, Kyverno, cert-manager, external-dns, Argo CD,
Argo Workflows, and monitoring. Generated bootstrap credentials are written
with restrictive permissions under `.on-prem-secrets/` by default. Override the
location with `ON_PREM_SECRET_DIR` when a governed secret volume is available:

```bash
export ON_PREM_SECRET_DIR=/secure/operator-owned/platform-secrets
./scripts/bootstrap-on-prem-reference.sh
```

Validate provider readiness and create signed provider evidence:

```bash
export RELEASE_MANIFEST="$PWD/docs/evidence/on-prem-reference-release-2026-09-09.json"
./scripts/certify-on-prem-reference.sh
```

Any provider-version change invalidates previous certification and requires the
complete certification suite to run again.

## 5. Build and sign a release

The release command builds both the control plane and network-conformance
artifact, scans them, emits SBOM and provenance documents, pushes them into the
reference Harbor registry, signs the OCI subjects, and signs the release JSON:

```bash
export ON_PREM_RELEASE_VERSION="on-prem-$(date -u +%Y%m%d%H%M%S)"
release_manifest=$(./scripts/release-on-prem-images.sh)
printf 'Release manifest: %s\n' "$release_manifest"
```

Treat the returned path and its sibling `release.bundle.json` as an inseparable
release record. A critical, fixed vulnerability terminates the release.

## 6. Generate and transfer the air-gap image lock

Resolve the four workflow runtime images to immutable digests, then generate
the lock from the signed release, declared platform workloads, and
runtime-resolved provider pods:

```bash
export RELEASE_MANIFEST="$release_manifest"
export PREVIEW_BUILDER_IMAGE='moby/buildkit@sha256:<digest>'
export PREVIEW_SCANNER_IMAGE='aquasec/trivy@sha256:<digest>'
export PREVIEW_COSIGN_IMAGE='ghcr.io/sigstore/cosign/cosign@sha256:<digest>'
export PREVIEW_VAULT_IMAGE='hashicorp/vault@sha256:<digest>'
./scripts/generate-airgap-image-lock.sh
```

Verify that no mutable reference survived:

```bash
jq -e '.images | length > 0 and all(.source | test("@sha256:[a-f0-9]{64}$"))' \
  config/airgap/on-prem-images.lock.json
```

Mirror the lock into site Harbor while still connected:

```bash
export AIRGAP_REGISTRY='harbor.internal.example/platform'
export AIRGAP_REGISTRY_USERNAME='admin'
export AIRGAP_REGISTRY_PASSWORD='<secret-manager-reference-or-ephemeral-value>'
./scripts/mirror-on-prem-images.sh
```

If release evidence names a host-forwarded registry address that differs from
the address visible to the mirror process, set both rewrite variables:

```bash
export AIRGAP_SOURCE_REWRITE_FROM='127.0.0.1:5002'
export AIRGAP_SOURCE_REWRITE_TO='harbor.harbor.svc.cluster.local'
./scripts/mirror-on-prem-images.sh
```

Transfer the repository revision, signed release, signature bundle, public key,
image lock, mirrored OCI content, and generated rewrite file through the site's
approved media and integrity-verification process. Referrers are part of the
transfer: signatures, SBOMs, and provenance are not optional metadata.

## 7. Render the installation

At the target site, point the renderer at the verified release and local Harbor
project:

```bash
export ON_PREM_SECRET_DIR="${ON_PREM_SECRET_DIR:-$PWD/.on-prem-secrets}"
export COSIGN_PUBLIC_KEY="$ON_PREM_SECRET_DIR/release-signing.pub"
export AIRGAP_IMAGE_LOCK="$PWD/config/airgap/on-prem-images.lock.json"
export AIRGAP_REGISTRY='harbor.internal.example/platform'
export RENDERED_PLATFORM_MANIFEST=/tmp/on-prem-platform.yaml

./scripts/render-on-prem-platform.sh \
  "$RELEASE_MANIFEST" \
  "${RELEASE_MANIFEST%/release.json}/release.bundle.json" \
  "$RENDERED_PLATFORM_MANIFEST"
```

The renderer verifies the signed release before reading its digest, substitutes
mirrored workflow images, and rejects any mutable image reference.

Inspect the candidate before applying it:

```bash
kubectl diff -f "$RENDERED_PLATFORM_MANIFEST" || test $? -eq 1
kubectl apply --dry-run=client -f "$RENDERED_PLATFORM_MANIFEST"
```

`kubectl diff` returns `1` when a legitimate difference exists; any value above
`1` is an error.

## 8. Install and establish internal GitOps

```bash
export RENDERED_PLATFORM_MANIFEST=/tmp/on-prem-platform.yaml
./scripts/install-on-prem-platform.sh
```

The installer performs four privileged boundary operations:

1. It materializes the exact candidate workspace into a complete Git bundle.
2. It places that bundle in the `git-system` namespace and starts an internal,
   read-only Git endpoint plus the conformance repository API.
3. It synchronizes the CloudNativePG application URI and locally governed
   bootstrap credentials into Kubernetes Secrets without writing them to Git.
4. It applies the rendered platform, establishes the Argo CD `Application`, and
   waits for both workload rollout and Git synchronization.

Confirm convergence:

```bash
kubectl -n argocd get application self-service-cicd-platform
kubectl -n control-plane get deployment,pods,pdb,service
kubectl -n argo get workflowtemplates
kubectl -n git-system get deployment,service
```

The Argo CD application must be `Synced` and `Healthy`; the control plane must
have three ready replicas, and the disruption budget must retain two.

## 9. Access bootstrap credentials safely

The installer creates opaque token files only in `ON_PREM_SECRET_DIR` (or
`.on-prem-secrets/`):

- `platform-admin-token`
- `adr23-alpha-token`
- `adr23-beta-token`
- `github-webhook-secret`

Load them into a transient shell without printing them:

```bash
export PLATFORM_ADMIN_TOKEN="$(<"$ON_PREM_SECRET_DIR/platform-admin-token")"
export TENANT_A_TOKEN="$(<"$ON_PREM_SECRET_DIR/adr23-alpha-token")"
export TENANT_B_TOKEN="$(<"$ON_PREM_SECRET_DIR/adr23-beta-token")"
export GITHUB_WEBHOOK_SECRET="$(<"$ON_PREM_SECRET_DIR/github-webhook-secret")"
```

These are bootstrap and conformance identities. Configure tenant OIDC, verify
it, and retire ordinary static tokens according to ADR 0019. Retain only a
separately governed break-glass capability where policy requires it.

## 10. Register and inspect a service

Forward the API for an operator workstation:

```bash
kubectl -n control-plane port-forward service/control-plane 8080:80
export PLATFORM_ENDPOINT=http://127.0.0.1:8080
export PLATFORM_TOKEN="$TENANT_A_TOKEN"
```

In another shell:

```bash
cd control-plane
go run ./cmd/platformctl -endpoint "$PLATFORM_ENDPOINT" \
  -file ../examples/services/on-prem-fixture.yaml apply
go run ./cmd/platformctl -endpoint "$PLATFORM_ENDPOINT" catalog
go run ./cmd/platformctl -endpoint "$PLATFORM_ENDPOINT" diagnose airgap-fixture
```

Service documents are validated against `config/service.schema.json`. Never put
credentials in a service declaration. Repository URLs identify source; the
provider adapter resolves and validates the exact revision.

## 11. Preview lifecycle

An authenticated pull-request event creates a durable SCM command. A reconciler
leases that command from PostgreSQL and submits Argo workflows to create the
tenant namespace, clone the exact commit, build, inventory, scan, attest, sign,
deploy, and publish the digest and URL. A close event executes the destroy
workflow and preserves lifecycle evidence.

Observe the transaction without mutating it:

```bash
kubectl -n argo get workflows -w
curl -fsS -H "Authorization: Bearer $PLATFORM_TOKEN" \
  "$PLATFORM_ENDPOINT/api/v1/catalog/services" | jq
curl -fsS -H "Authorization: Bearer $PLATFORM_TOKEN" \
  "$PLATFORM_ENDPOINT/api/v1/services/go-api/diagnostics" | jq
```

Use the environment endpoint for workflow phase and log navigation. Do not
manually edit command rows, environment generations, tenant namespaces, or
admission policies to force convergence.

## 12. Run final installation acceptance

Acceptance requires a second, distinct certified control-plane digest so it can
prove upgrade and failed-rollout recovery:

```bash
export RELEASE_MANIFEST="$release_manifest"
export PLATFORM_ENDPOINT=http://127.0.0.1:8080
export TENANT_A_AUTH_JSON='{"providers":[{"issuer":"https://identity.internal.example/realms/adr23-alpha","jwks_url":"https://identity.internal.example/realms/adr23-alpha/protocol/openid-connect/certs","audiences":["self-service-cicd"],"groups_claim":"groups","group_roles":{"platform-admins":"admin","platform-developers":"developer","platform-viewers":"viewer"}}]}'
export AIRGAP_REPOSITORY='acme/fixture'
export AIRGAP_REPOSITORY_NAME='fixture'
export AIRGAP_COMMIT_SHA="$(kubectl -n git-system exec deployment/airgap-git -c git -- \
  git --git-dir=/repositories/acme/fixture.git rev-parse HEAD)"
export UPGRADE_CONTROL_PLANE_IMAGE='harbor.internal.example/platform/control-plane@sha256:<distinct-certified-digest>'
export COSIGN_PRIVATE_KEY="$ON_PREM_SECRET_DIR/release-signing.key"

./scripts/validate-on-prem-platform.sh
```

The command must finish with `on-prem platform acceptance passed`. Its signed
`platform.installation-evidence/v1` BOM binds provider versions, Kubernetes
version, release and manifest hashes, schema migration, image digests, and every
gate outcome. Sanitized evidence may be retained; raw logs and secrets may not.

## 13. Upgrade and rollback

For an upgrade:

1. Build, scan, attest, and sign once.
2. Mirror by digest without rebuilding at the site.
3. Re-render from the signed release.
4. Review the server-side diff.
5. Update the internal Git candidate and let Argo CD reconcile.
6. Run the complete acceptance suite and retain its signed BOM.

Rollback selects the preceding certified digest. Never rebuild a historical
version, substitute a tag, disable readiness, reduce the disruption budget, or
weaken admission to make a rollout pass.

## 14. Backup and disaster recovery

Before promotion, prove that PostgreSQL WAL is archived and that Harbor retains
the release subject and all referrers. Run:

```bash
./scripts/validate-on-prem-postgres.sh
./scripts/validate-registry-evidence-lifecycle.sh
```

Restoration must target an independently created database or registry location.
Compare schema migration and tenant, service, environment, and command counts;
then run the application suite against the restored endpoint. Object existence
alone is not proof of recoverability.

## 15. Troubleshooting order

Diagnose dependency boundaries in this order:

1. `kubectl get nodes` and Cilium health.
2. Kyverno admission-controller readiness and `kyverno-svc` EndpointSlices.
3. CloudNativePG cluster readiness and application-secret availability.
4. Argo Workflows CRDs and workflow-controller readiness.
5. Argo CD repository reachability, sync, and health.
6. Control-plane rollout, `/healthz`, then `/readyz`.
7. Tenant diagnostics, durable SCM commands, and workflow phase.
8. Harbor artifact/referrer presence and Vault identity/signing state.

Useful commands:

```bash
kubectl get nodes -o wide
kubectl -n kyverno get pods,endpointslice
kubectl -n platform-database get cluster,pods
kubectl -n argo get pods,workflowtemplates,workflows
kubectl -n argocd get application self-service-cicd-platform -o yaml
kubectl -n control-plane rollout status deployment/control-plane
kubectl -n control-plane logs deployment/control-plane --tail=200
```

Fail-closed admission errors are evidence of an unavailable enforcement plane,
not permission to delete webhooks or change their failure policy. Recover the
controller and networking first.

## 16. Routine maintenance

- Rebuild and recertify after any provider, chart, Kubernetes, or locked-image
  change.
- Rotate OIDC and Vault signing keys overlap-first.
- Test PostgreSQL restore and registry referrer restore on a schedule.
- Review SLO alerts and reconciliation latency.
- Confirm the Argo CD source remains internal and read-only.
- Remove expired preview environments through their lifecycle command, not by
  deleting namespaces manually.
- Keep the signed installation BOM with the corresponding release record.

For incident-specific procedures, see
[On-Prem Platform Installation and Operations](../runbooks/on-prem-platform-operations.md),
[Control-Plane HA](../runbooks/control-plane-ha.md), and
[Artifact Evidence Operations](../runbooks/artifact-evidence-operations.md).
