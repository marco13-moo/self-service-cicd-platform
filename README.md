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
authentication boundaries, and preview-environment reconciliation. Revision-aware
preview deployment, PostgreSQL-backed distributed command leasing, durable Argo
TTL enforcement, and generation-safe deployed-revision observation are implemented.
Project-specific workload deployment and preview URL discovery remain future
increments.

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
| `DATABASE_URL` | unset | PostgreSQL connection URL for distributed command leasing; file queue is the fallback |
| `CONTROL_PLANE_ADMIN_TOKEN` | unset | Bearer token enabling administrative command inspection |

The fallback file-backed state repository is intentionally single-writer. The
Kubernetes deployment mounts a `ReadWriteOnce` PVC; configure PostgreSQL before
running multiple reconcilers.

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
