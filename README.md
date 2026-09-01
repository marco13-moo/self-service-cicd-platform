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

The next product milestone is Phase 8: GitHub App authentication, webhook
ingestion, and PR-driven preview environments. The GitHub provider currently
supports read-only repository inspection; it does not create installations or
webhooks.

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
| `POST` | `/api/v1/services` | Register a service |
| `GET` | `/api/v1/services` | List registered services |
| `POST` | `/api/v1/environments` | Submit create and TTL workflows |
| `GET` | `/api/v1/environments/{name}` | Retrieve intent and live workflow state |
| `DELETE` | `/api/v1/environments/{name}` | Submit and retain a destroy workflow reference |
| `GET` | `/api/v1/environments/{name}/logs` | Return Argo UI links and CLI log hints |

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

The file-backed state repository is intentionally single-writer. The Kubernetes
deployment mounts a `ReadWriteOnce` PVC. A multi-replica deployment requires a
transactional shared datastore before horizontal scaling.

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
