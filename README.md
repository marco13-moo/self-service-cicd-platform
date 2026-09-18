# Self-Service CI/CD Platform

An API-driven control plane for registering services and orchestrating ephemeral
environments through Kubernetes and Argo Workflows. The control plane expresses
validated intent; Argo remains the authoritative execution and lifecycle engine.

The reference deployment is sovereign and on-premises: Kubernetes, Cilium,
Harbor, Vault, CloudNativePG, Keycloak, cert-manager, external-dns, Argo CD,
Argo Workflows, MinIO, Kyverno, and Prometheus are installed under explicit,
version-pinned contracts. Runtime images are digest-pinned and mirrored into the
site registry; GitOps reconciliation uses a site-local Git endpoint.

## Start here

| Goal | Documentation |
| --- | --- |
| Understand the platform product boundary and roadmap | [`docs/platform-product-architecture.md`](docs/platform-product-architecture.md) |
| Understand the system | [`docs/architecture.md`](docs/architecture.md) |
| Install and operate on premises | [`docs/guides/on-prem-usage-guide.md`](docs/guides/on-prem-usage-guide.md) |
| Review the installation decision | [`ADR 0023`](docs/adr/0023-on-prem-platform-installation-and-day-2-operations.md) |
| Execute incident and recovery procedures | [`on-prem-platform-operations.md`](docs/runbooks/on-prem-platform-operations.md) |
| Register and diagnose services | [`config/service.schema.json`](config/service.schema.json) and [`examples/services`](examples/services) |
| Inspect certification evidence | [`docs/evidence`](docs/evidence) |
| Review the completion boundary | [`docs/project-completion.md`](docs/project-completion.md) |

For a local source build:

```bash
cd control-plane
go test ./...
go run ./cmd/control-plane
```

### Local Kind quickstart

The control plane requires a reachable Kubernetes API and Argo Workflows for
readiness. The following commands create the repository's local Kind cluster,
install Argo, and run the control plane with file-backed state. Run commands
from separate terminals where indicated.

1. From the repository root, create the local cluster:

  ```bash
  ./scripts/bootstrap-kind-cilium.sh
  ```

2. Install the pinned Argo Workflows chart and apply the workflow templates:

  ```bash
  helm repo add argo https://argoproj.github.io/argo-helm
  helm repo update
  helm upgrade --install argo-workflows argo/argo-workflows \
    --version 2.0.5 \
    --namespace argo \
    --create-namespace \
    --set crds.full=false \
    --set server.enabled=false \
    --wait \
    --timeout 10m
  kubectl apply -n argo -f argo/workflowtemplates/
   kubectl apply \
     -f infra/k8s/argo-env-admin-sa.yaml \
     -f infra/k8s/argo-env-admin-clusterrole.yaml \
     -f infra/k8s/argo-env-admin-clusterrolebinding.yaml
  ```

3. In terminal 1, start the control plane. Set `GITHUB_TOKEN` in the shell
  first when registering a GitHub repository; its value is intentionally not
  shown here.

  ```bash
  cd control-plane
  mkdir -p .local
  export GITHUB_TOKEN="your-github-token"
  LOCAL_TOKEN="replace-with-a-local-development-token"
  TENANT_AUTH_TOKENS="{\"$LOCAL_TOKEN\":{\"subject\":\"local-user\",\"tenant_id\":\"default\",\"role\":\"admin\",\"platform_admin\":true}}" \
  STATE_PATH="$PWD/.local/state.json" \
  go run ./cmd/control-plane
  ```

4. In terminal 2, configure the local API endpoint and verify both probes:

  ```bash
  cd control-plane
  export PLATFORM_ENDPOINT=http://localhost:8080
  export PLATFORM_TOKEN="replace-with-a-local-development-token"
  curl -i "$PLATFORM_ENDPOINT/healthz"
  curl -i "$PLATFORM_ENDPOINT/readyz"
  ```

  Both endpoints should return `200 OK`. Keep terminal 1 running.

5. Create a local service declaration. Copy
  `examples/services/go-api.yaml` to `.local/my-service.yaml`, then replace
  its example repository with a real accessible GitHub or Bitbucket
  repository. The repository must expose a recognized root manifest such as
  `go.mod`, `package.json`, or `pyproject.toml`.

  ```bash
  mkdir -p .local
  cp examples/services/go-api.yaml .local/my-service.yaml
  ```

6. Register and inspect the service from terminal 2:

  ```bash
  go run ./cmd/platformctl \
    -endpoint "$PLATFORM_ENDPOINT" \
    -token "$PLATFORM_TOKEN" \
    -file ../.local/my-service.yaml \
    apply
  go run ./cmd/platformctl \
    -endpoint "$PLATFORM_ENDPOINT" \
    -token "$PLATFORM_TOKEN" \
    catalog
  go run ./cmd/platformctl \
    -endpoint "$PLATFORM_ENDPOINT" \
    -token "$PLATFORM_TOKEN" \
    diagnose orders-api
  ```

The local `TENANT_AUTH_TOKENS` and `PLATFORM_TOKEN` values above are
development-only placeholders and must match. Never commit credentials or paste
token values into the repository. If the Kind cluster is recreated, restart the
control plane so it loads the new kubeconfig endpoint.

For the complete on-premises lifecycle, use the usage guide. Installation is
not considered accepted until `validate-on-prem-platform.sh` emits a signed,
passing `platform.installation-evidence/v1` bill of materials.

## Current status

The planned platform capabilities through ADR 0023 are implemented. The
repository is in operational-maintenance status: future work is provider
recertification, dependency maintenance, and site-specific deployment rather
than unfinished product scope.

Implemented capabilities include:

- Go control-plane service with explicit API, orchestration, execution, and provider boundaries
- Service registration with PostgreSQL-authoritative production state
- Ephemeral environment create, TTL cleanup, and destroy workflows
- Live workflow status inspection and execution-plane log navigation
- Typed Argo Workflows Go SDK integration with no CLI subprocess dependency
- Three-replica Kubernetes Deployment, RBAC, Service, topology spreading, and disruption budget
- Process liveness plus PostgreSQL- and Argo-aware readiness probes
- GitHub repository validation and root-manifest project detection
- HMAC-authenticated GitHub webhook ingestion with durable delivery deduplication
- GitHub and Bitbucket Cloud webhook adapters over a provider-neutral SCM domain
- Durable PR lifecycle command leasing, retry, and reconciliation
- Separate GitHub App and Bitbucket OAuth authentication implementations

Phase 8 now includes provider-neutral webhook ingestion, durable commands,
authentication boundaries, and preview-environment reconciliation. Exact source
revisions are built into OCI images by rootless BuildKit, deployed into their
preview namespaces, and exposed through either Ingress or cluster-local Service
DNS. Images carry OCI-native SPDX SBOM and maximal provenance attestations,
pass a configurable Trivy vulnerability gate, and are deployed exclusively by
registry-returned digest. PostgreSQL-backed distributed command leasing, durable
TTL enforcement, and generation-safe publication of deployed revisions, images,
attestation subjects, and URLs are implemented.

ADR 0016 closes the artifact-evidence lifecycle with recursive registry backup
and restore conformance, garbage-collection survival checks, overlap-based KMS
key rotation, scheduled signature and policy-attestation re-verification, and
automatic quarantine of unverifiable preview workloads.

ADR 0017 removes the remaining single-writer constraint: services,
environments, deliveries, and commands share an authoritative PostgreSQL
failure domain; environment writes use optimistic concurrency; startup
migrations are transactionally serialized; and the control plane runs as three
interchangeable, gracefully draining replicas.

ADR 0018 introduces immutable tenant ownership, role-gated API middleware,
tenant-scoped webhook and reconciliation state, composite PostgreSQL identities,
forced Row-Level Security, tenant-owned Kubernetes namespaces, least-privilege
tenant deployer identities, quota and network isolation, restricted Pod Security,
and tenant-partitioned artifact trust, exceptions, audit metadata, and evidence.

ADR 0019 adds dual-mode bootstrap/OIDC authentication, bounded JWKS rollover,
tenant lifecycle controls, and append-only tenant audit events protected by
forced PostgreSQL Row-Level Security.

ADR 0020 standardizes enforced tenant networking on Cilium, makes exact
destination egress part of service intent, and isolates preview ingress behind a
platform-owned Gateway API boundary with adversarial packet conformance.

ADR 0021 adds build-once digest releases, signed SBOM/provenance, fail-closed
provider certification, production PostgreSQL/WAL recovery, OIDC-only ordinary
access, cert-manager/external-dns/Cilium edge composition, SLO alerts, and staged
GitOps promotion with rollback. ADR 0022 adds the versioned service declaration,
tenant catalog, actionable diagnostics, `platformctl`, provider-neutral revision
status adapters for GitHub and Bitbucket, tenant lifecycle automation, and
end-to-end onboarding conformance.

## Architecture

```text
HTTP API
   │
   ▼
Environment orchestrator  ──► durable intent/workflow references
   │
   ▼
WorkflowExecutor
   │
   ▼
Kubernetes API ──► Argo controller ──► environment namespaces
```

Architectural decisions and trust boundaries are documented in
[`docs/architecture.md`](docs/architecture.md) and [`docs/adr`](docs/adr).

## API

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/healthz` | Process liveness |
| `GET` | `/readyz` | Argo API connectivity and authorization |
| `GET` | `/metrics` | Prometheus command-state metrics |
| `GET` | `/api/v1/admin/scm/commands` | Bearer-authenticated command inspection |
| `POST` | `/api/v1/services` | Register a service |
| `GET` | `/api/v1/services` | List registered services |
| `PATCH` | `/api/v1/services/{name}` | Update lifecycle intent with optimistic concurrency |
| `GET` | `/api/v1/catalog/services` | Tenant-scoped developer catalog |
| `GET` | `/api/v1/services/{name}/diagnostics` | Actionable, secret-free service diagnostics |

Service registration accepts the versioned `platform.service/v1` declaration
shape shown in [`examples/services/platform-service-v1.yaml`](examples/services/platform-service-v1.yaml).
Responses include tenant-scoped desired and observed status; the legacy flat
registration fields remain accepted during migration.
| `POST` | `/api/v1/environments` | Submit create and TTL workflows |
| `GET` | `/api/v1/environments/{name}` | Retrieve intent and live workflow state |
| `DELETE` | `/api/v1/environments/{name}` | Submit and retain a destroy workflow reference |
| `GET` | `/api/v1/environments/{name}/logs` | Return Argo UI links and CLI log hints |
| `POST` | `/api/v1/webhooks/{provider}` | Authenticate, deduplicate, and normalize SCM deliveries |
| `GET` | `/api/v1/tenant/auth` | Read the authenticated tenant's OIDC configuration |
| `PUT` | `/api/v1/tenant/auth` | Replace the authenticated tenant's OIDC configuration |
| `GET` | `/api/v1/tenant/audit` | Export the authenticated tenant's append-only audit events |
| `POST` | `/api/v1/admin/tenants` | Provision a tenant using a platform-administrator capability |
| `PATCH` | `/api/v1/admin/tenants/{tenant}/status` | Suspend, reactivate, or offboard a tenant |
| `POST` | `/api/v1/admin/repository-transfers` | Transfer an idle service between active tenants |

Example environment request:

```json
{
  "name": "checkout-pr-42",
  "service": "checkout",
  "ttl": "2h"
}
```

Preview-capable services declare their container contract when registered:

```json
{
  "name": "checkout",
  "owner": "payments-platform",
  "repo_url": "https://github.com/acme/checkout",
  "environment": "production",
  "deployment": {
    "container_port": 8080,
    "dockerfile": "Dockerfile",
    "egress": [
      {"dns_name": "api.example.com", "port": 443, "protocol": "TCP"}
    ]
  }
}
```

The deployment block is optional; its defaults are port `8080`, a root-level
`Dockerfile`, and no external egress. Egress destinations must be exact DNS
names with an explicit port; wildcard domains are rejected.

The golden path is declarative and works identically for supported SCMs:

```bash
cd control-plane
go run ./cmd/platformctl -endpoint https://platform.example \
  -file ../examples/services/go-api.yaml apply
go run ./cmd/platformctl -endpoint https://platform.example catalog
go run ./cmd/platformctl -endpoint https://platform.example diagnose orders-api
```

Use an OIDC token through `PLATFORM_TOKEN`; do not place credentials in service
declarations. The authoritative schema is [`service.schema.json`](config/service.schema.json).

## Production release and certification

`release-platform-images.sh` builds, scans, signs, attests, and publishes the
control-plane and network-conformance images by digest. A signed release manifest
is then rendered with site-specific production values and promoted without a
rebuild. `certify-production-providers.sh` executes the existing KMS, registry,
PostgreSQL HA/recovery, and Cilium packet suites; any failure prevents promotion.
The manifests in [`infra/production`](infra/production) are templates, not a
claim that an unspecified provider has passed certification. Operational steps,
RPO/RTO criteria, rollback, disaster recovery, and break-glass governance are in
[`production-promotion.md`](docs/runbooks/production-promotion.md).

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `HTTP_ADDRESS` | `:8080` | API listen address |
| `ENVIRONMENT` | `local` | Runtime environment label |
| `LOG_LEVEL` | `info` | Structured log level |
| `STATE_PATH` | `/var/lib/control-plane/state.json` | Single-replica development fallback and one-time migration source |
| `ARGO_NAMESPACE` | `argo` | Workflow namespace |
| `ARGO_UI_BASE_URL` | `http://argo-server.argo.svc` | Base URL returned by log navigation |
| `GITHUB_TOKEN` | unset | Optional token for private repositories or higher API limits |
| `GITHUB_WEBHOOK_SECRET` | unset | Required HMAC secret for GitHub webhook ingestion |
| `GITHUB_APP_ID` | unset | GitHub App identifier for installation authentication |
| `GITHUB_PRIVATE_KEY_PATH` | unset | Mounted GitHub App RSA private-key path |
| `BITBUCKET_WEBHOOK_SECRET` | unset | Required HMAC secret for Bitbucket Cloud webhook ingestion |
| `BITBUCKET_TOKEN` | unset | Optional bearer token for private Bitbucket repository inspection |
| `BITBUCKET_OAUTH_CLIENT_ID` | unset | Bitbucket OAuth consumer client ID |
| `BITBUCKET_OAUTH_CLIENT_SECRET` | unset | Bitbucket OAuth consumer secret |
| `PREVIEW_ENVIRONMENT_TTL` | `2h` | TTL assigned by the SCM command reconciler |
| `PREVIEW_IMAGE_REPOSITORY` | unset | Required OCI repository prefix for preview images |
| `PREVIEW_BUILDER_IMAGE` | `moby/buildkit:v0.33.0-rootless` | Rootless BuildKit executor image |
| `PREVIEW_REGISTRY_SECRET` | `registry-credentials` | Optional Docker config Secret mounted into builds |
| `PREVIEW_REGISTRY_INSECURE` | `false` | Permit HTTP/insecure registry transport for local clusters only |
| `PREVIEW_BASE_DOMAIN` | unset | Wildcard DNS suffix; when unset, publish cluster-local Service URLs |
| `PREVIEW_URL_SCHEME` | `https` | Scheme used for externally routed preview URLs |
| `PREVIEW_SCANNER_IMAGE` | `aquasec/trivy:0.74.0` | Trivy image used for inventory and vulnerability evaluation |
| `PREVIEW_VULNERABILITY_SEVERITIES` | `CRITICAL` | Comma-separated severities that block deployment |
| `PREVIEW_VULNERABILITY_IGNORE_UNFIXED` | `true` | Ignore blocking findings that have no available fix |
| `PREVIEW_TARGET_PLATFORM` | `linux/amd64` | OCI build and scan platform; use `linux/arm64` for ARM clusters |
| `PREVIEW_COSIGN_IMAGE` | `ghcr.io/sigstore/cosign/cosign:v2.6.4` | Maintained Cosign 2 executor used for digest signing and Kyverno-compatible verification |
| `PREVIEW_COSIGN_SIGNER` | `/cosign-private/cosign.key` | Cosign file or KMS signer URI; multi-tenant KMS profiles must contain a `{tenant}` placeholder |
| `PREVIEW_SIGNING_PROFILE` | `key` | `key` for development or `kms` to require a supported KMS signer URI |
| `PREVIEW_COSIGN_AUTH_MODE` | `ambient` | `ambient` cloud credentials or short-lived `vault-kubernetes` authentication |
| `PREVIEW_VAULT_IMAGE` | `hashicorp/vault:1.20.4` | Vault client used only by the Kubernetes login init container |
| `PREVIEW_VAULT_ADDR` | unset | Required Vault/OpenBao API address for `vault-kubernetes` authentication |
| `PREVIEW_VAULT_ROLE` | `self-service-cicd-signer` | Vault Kubernetes-auth role bound to the Argo ServiceAccount |
| `PREVIEW_COSIGN_PRIVATE_KEY_SECRET` | `preview-cosign-private` | Base name for the tenant-suffixed Argo Secret containing `cosign.key` and optional `password` |
| `PREVIEW_COSIGN_PUBLIC_KEY_SECRET` | `preview-cosign-public` | Base name for the tenant-suffixed public-only Argo Secret containing `cosign.pub` |
| `PREVIEW_POLICY_PREDICATE_TYPE` | `https://self-service-cicd.dev/attestations/vulnerability-policy/v1` | Versioned signed vulnerability-policy predicate type |
| `PREVIEW_VEX_CONFIGMAP` | `preview-vex-none` | Base name for an optional tenant-suffixed governed `preview-vex-*` ConfigMap; the default intentionally does not exist |
| `DATABASE_URL` | unset | PostgreSQL authority for all production state; required by the Kubernetes Deployment |
| `TENANT_AUTH_TOKENS` | unset | Bootstrap/break-glass JSON map of bearer tokens to `{subject,tenant_id,role,platform_admin?}` principals; optional after PostgreSQL-backed OIDC is configured |
| `CONTROL_PLANE_ADMIN_TOKEN` | unset | Bearer token enabling administrative command inspection |

The fallback file-backed repository is intentionally local and single-writer.
Production has no state volume: its three replicas require the
`control-plane-database` Secret and share PostgreSQL. Before the first cutover,
stop the legacy writer and import its JSON snapshot into an empty database:

```bash
cd control-plane
DATABASE_URL='postgres://...' go run ./cmd/state-migrate \
  -state-path /path/to/state.json
```

The import refuses a non-empty target. See
[`control-plane-ha.md`](docs/runbooks/control-plane-ha.md) for cutover, backup,
restore, and failover acceptance criteria.

Migration 5 audit rows are exported through the insert-only logical publication
defined in [`audit-logical-replication.sql`](infra/postgres/audit-logical-replication.sql).
The HA harness proves logical decoding completeness and emits the sealed archive
segment digest alongside backup/restore and replica-handoff results.

Run the disposable real-provider identity lifecycle with:

```bash
./scripts/validate-keycloak-oidc.sh
```

It provisions Keycloak over a self-signed loopback HTTPS boundary and proves
issuance, RSA JWKS rotation, group revocation, and expiry. Its TLS bypass is
confined to the standalone conformance command and rejects non-loopback hosts;
the control-plane server retains strict public-CA verification.

For the bootstrap tenant credential shape, copy
[`control-plane-tenant-auth-example.yaml`](infra/k8s/control-plane-tenant-auth-example.yaml),
replace the illustrative token through a secret manager, and apply it without
committing the rendered Secret. Production PostgreSQL API credentials must use
a non-superuser role because superusers bypass Row-Level Security.

Preview namespaces are deterministic tenant-owned security domains. The create
workflow installs the tenant deployer RoleBinding, quota and limits, default-deny
networking, restricted Pod Security labels, and tenant-local admission policy.
Run the disposable two-tenant escape test with:

```bash
./scripts/validate-tenant-kubernetes-isolation.sh
```

The API/RBAC test proves that tenant alpha can deploy only into its own namespace
and that server-side Pod Security rejects a privileged workload. Packet-level
enforcement uses the pinned Cilium reference cluster:

```bash
./scripts/bootstrap-kind-cilium.sh
./scripts/validate-cilium-tenant-isolation.sh
```

That suite proves cross-tenant, arbitrary ingress, undeclared DNS, Kubernetes
API, metadata, and policy-deletion denial while retaining same-tenant and
gateway-authorized traffic.

The scheduled variant is built from `infra/conformance/Dockerfile` and declared
in `infra/k8s/network-isolation-conformance-cronjob.yaml`. Production promotion
must replace the local image name with its signed immutable digest.

For production signing, copy
[`preview-trust-config-example.yaml`](infra/k8s/preview-trust-config-example.yaml),
replace its illustrative KMS URI, and bind the `argo-env-admin` ServiceAccount
to the corresponding cloud workload identity. The ConfigMap contains only key
identity and predicate metadata; cloud credentials and private key material do
not belong in it.

The provider-neutral Vault/OpenBao conformance lane is bootstrapped with
[`bootstrap-vault-kms-conformance.sh`](scripts/bootstrap-vault-kms-conformance.sh)
and executed by [`validate-kms-signing.sh`](scripts/validate-kms-signing.sh) with
a cluster-reachable digest in `KMS_TEST_IMAGE`. It validates audience-bound
Kubernetes authentication, non-exportable signing, signed policy attestations,
admission, and revocation. Remove its isolated resources with
[`teardown-vault-kms-conformance.sh`](scripts/teardown-vault-kms-conformance.sh).
Provider status and evidence requirements are documented in
[`kms-provider-certification.md`](docs/kms-provider-certification.md).

Exercise the complete evidence lifecycle against a BuildKit-produced, signed
digest containing SPDX and SLSA attestations:

```bash
EVIDENCE_TEST_IMAGE=registry.example.test/previews/service@sha256:... \
  ./scripts/validate-registry-evidence-lifecycle.sh
```

The harness uses two disposable registries: primary-to-backup recursive copy,
garbage collection in the backup, then restoration into an independently empty
registry. It validates the recovered SBOM, provenance, signature, signed policy
attestation, and server-side admission decision. It never garbage-collects the
primary registry.

Validate overlap-first Vault/OpenBao rotation with:

```bash
ROTATION_TEST_IMAGE=registry.example.test/previews/service@sha256:... \
  ./scripts/rotate-vault-signing-key.sh
```

Install [`evidence-reverification.yaml`](argo/cronworkflows/evidence-reverification.yaml)
to re-verify managed Deployments every six hours. Verification failures annotate
and label the namespace, then patch the affected Deployment to zero replicas.
The workflow intentionally exits unsuccessfully after containment so operations
alerting cannot mistake quarantine for a healthy run.
The isolated quarantine contract can be replayed with
[`validate-evidence-reverification.sh`](scripts/validate-evidence-reverification.sh).

When `DATABASE_URL` is configured, all desired state, delivery deduplication,
and command leasing use PostgreSQL transactions. `FOR UPDATE SKIP LOCKED`,
durable lease expiry, and versioned compare-and-set mutations permit multiple
reconcilers without lost updates. The HA backup/restore and replica-termination
harness is [`validate-control-plane-ha.sh`](scripts/validate-control-plane-ha.sh).
The lifecycle smoke harness is
[`scripts/validate-preview-lifecycle.sh`](scripts/validate-preview-lifecycle.sh).

## Development

The module requires Go 1.25 or newer.

```bash
cd control-plane
go test ./...
go vet ./...
go build ./cmd/control-plane
```

Container builds run from the repository root:

```bash
docker build -t self-service-cicd-control-plane:local .
```

See [`docs/demo.md`](docs/demo.md) for the environment lifecycle walkthrough.
The webhook security and idempotency boundary is specified in
[`ADR 0008`](docs/adr/0008-authenticated-github-webhook-ingestion.md).
The provider-neutral domain, adapter, authentication, and reconciliation model is
specified in [`ADR 0009`](docs/adr/0009-provider-neutral-source-control-boundary.md).
Revision convergence and failure semantics are specified in
[`ADR 0010`](docs/adr/0010-revision-aware-preview-reconciliation.md).

The GitOps installation, air-gap contract, complete on-prem acceptance
transaction, and day-two recovery model are specified in
[`ADR 0023`](docs/adr/0023-on-prem-platform-installation-and-day-2-operations.md).
Render and install the certified on-prem platform with
`scripts/render-on-prem-platform.sh` and `scripts/install-on-prem-platform.sh`;
`scripts/validate-on-prem-platform.sh` is its singular acceptance entry point.
TTL enforcement and generation-safe deployment observation are specified in
[`ADR 0011`](docs/adr/0011-ttl-enforcement-and-deployment-observation.md).
OCI construction, namespace deployment, and preview routing are specified in
[`ADR 0012`](docs/adr/0012-oci-preview-build-deployment-and-routing.md).
Digest-pinned deployment, OCI attestations, and vulnerability admission are
specified in [`ADR 0013`](docs/adr/0013-digest-pinned-artifacts-attestations-and-vulnerability-policy.md).
Cosign trust, admission enforcement, and expiring OpenVEX exceptions are
specified in [`ADR 0014`](docs/adr/0014-signed-artifacts-admission-and-vex-governance.md).
Production KMS identity, trust rotation, signed policy evidence, and registry
retention are specified in [`ADR 0015`](docs/adr/0015-production-trust-and-evidence-lifecycle.md).
Evidence retention, overlap-safe rotation, continuous re-verification,
quarantine, and disaster recovery are specified in
[`ADR 0016`](docs/adr/0016-evidence-retention-rotation-and-recovery.md).
Tenant identity, database and Kubernetes isolation, and tenant-partitioned
artifact governance are specified in
[`ADR 0018`](docs/adr/0018-tenant-identity-authorization-and-row-isolation.md).
Federated identity, lifecycle administration, and immutable audit are specified
in [`ADR 0019`](docs/adr/0019-federated-tenant-identity-lifecycle-and-audit.md).
Operational execution and rollback are documented in the
[`artifact evidence runbook`](docs/runbooks/artifact-evidence-operations.md).
