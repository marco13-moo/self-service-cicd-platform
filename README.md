# Self-Service CI/CD Platform

An API-driven control plane for registering services and orchestrating ephemeral
environments through Kubernetes and Argo Workflows. The control plane expresses
validated intent; Argo remains the authoritative execution and lifecycle engine.

## Current status

Phases 1–7 are implemented:

- Go control-plane service with explicit API, orchestration, execution, and provider boundaries
- Service registration and durable local state
- Ephemeral environment create, TTL cleanup, and destroy workflows
- Live workflow status inspection and execution-plane log navigation
- Typed Argo Workflows Go SDK integration with no CLI subprocess dependency
- Kubernetes RBAC, deployment, service, service accounts, and persistent state volume
- Liveness and Argo-aware readiness probes
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
| `POST` | `/api/v1/environments` | Submit create and TTL workflows |
| `GET` | `/api/v1/environments/{name}` | Retrieve intent and live workflow state |
| `DELETE` | `/api/v1/environments/{name}` | Submit and retain a destroy workflow reference |
| `GET` | `/api/v1/environments/{name}/logs` | Return Argo UI links and CLI log hints |
| `POST` | `/api/v1/webhooks/{provider}` | Authenticate, deduplicate, and normalize SCM deliveries |

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
    "dockerfile": "Dockerfile"
  }
}
```

The deployment block is optional; its defaults are port `8080` and a root-level
`Dockerfile`.

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `HTTP_ADDRESS` | `:8080` | API listen address |
| `ENVIRONMENT` | `local` | Runtime environment label |
| `LOG_LEVEL` | `info` | Structured log level |
| `STATE_PATH` | `/var/lib/control-plane/state.json` | Atomic JSON state repository |
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
| `PREVIEW_COSIGN_SIGNER` | `/cosign-private/cosign.key` | Cosign file or KMS signer URI; production uses workload-identity-authorized KMS |
| `PREVIEW_SIGNING_PROFILE` | `key` | `key` for development or `kms` to require a supported KMS signer URI |
| `PREVIEW_COSIGN_AUTH_MODE` | `ambient` | `ambient` cloud credentials or short-lived `vault-kubernetes` authentication |
| `PREVIEW_VAULT_IMAGE` | `hashicorp/vault:1.20.4` | Vault client used only by the Kubernetes login init container |
| `PREVIEW_VAULT_ADDR` | unset | Required Vault/OpenBao API address for `vault-kubernetes` authentication |
| `PREVIEW_VAULT_ROLE` | `self-service-cicd-signer` | Vault Kubernetes-auth role bound to the Argo ServiceAccount |
| `PREVIEW_COSIGN_PRIVATE_KEY_SECRET` | `preview-cosign-private` | Argo Secret containing `cosign.key` and optional `password` |
| `PREVIEW_COSIGN_PUBLIC_KEY_SECRET` | `preview-cosign-public` | Public-only Argo Secret containing `cosign.pub` |
| `PREVIEW_POLICY_PREDICATE_TYPE` | `https://self-service-cicd.dev/attestations/vulnerability-policy/v1` | Versioned signed vulnerability-policy predicate type |
| `PREVIEW_VEX_CONFIGMAP` | `preview-vex-none` | Optional governed `preview-vex-*` ConfigMap; the default intentionally does not exist |
| `DATABASE_URL` | unset | PostgreSQL connection URL for distributed command leasing; file queue is the fallback |
| `CONTROL_PLANE_ADMIN_TOKEN` | unset | Bearer token enabling administrative command inspection |

The fallback file-backed state repository is intentionally single-writer. The
Kubernetes deployment mounts a `ReadWriteOnce` PVC; configure PostgreSQL before
running multiple reconcilers.

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

When `DATABASE_URL` is configured, delivery deduplication and command leasing use
PostgreSQL transactions with `FOR UPDATE SKIP LOCKED`, permitting multiple
reconcilers. The lifecycle smoke harness is
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
Operational execution and rollback are documented in the
[`artifact evidence runbook`](docs/runbooks/artifact-evidence-operations.md).
