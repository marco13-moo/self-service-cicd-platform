## Architecture Overview

This project implements a self-service CI/CD control plane that allows
developers to register repositories and automatically receive
infrastructure-backed pipelines.

The repository is structured to reflect intentional architectural
boundaries:

- `cmd/` — binary entrypoints and lifecycle management
- `internal/api/` — HTTP routing, API contracts, and domain modeling
- `internal/providers/` — source control abstractions (GitHub, GitLab, etc)
- `internal/server/` — HTTP server lifecycle and graceful shutdown
- `docs/adr/` — architectural decision records

Early scaffold phases were intentionally collapsed to accelerate validation
of runtime behavior. Architectural intent is preserved through explicit
interfaces, layering boundaries, and ADRs.

Future phases introduce:
- GitHub App authentication
- Webhook ingestion
- Dynamic pipeline generation
- Ephemeral environment provisioning via Terraform and Argo Workflows