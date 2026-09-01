# ADR 0009: Provider-Neutral Source-Control Boundary

## Status

Accepted

## Context

ADR 0008 established authenticated GitHub webhook ingestion, but its initial
implementation placed GitHub delivery identifiers and GitHub-shaped commands in
the API and persistence layers. That coupling would force every source-control
provider to emulate GitHub semantics and would make the control-plane domain a
function of its first adapter.

The platform is a self-service delivery control plane, not a GitHub application.
GitHub, Bitbucket, GitLab, and future systems must remain replaceable ingress and
authentication adapters.

## Decision

The control plane introduces a provider-neutral `internal/scm` domain containing:

- canonical pull-request events;
- canonical lifecycle commands;
- provider-scoped delivery keys;
- command status, leasing, retry, and completion semantics;
- webhook adapter and authenticator interfaces; and
- deterministic preview-environment naming.

Provider adapters exclusively own native headers, signature algorithms, payload
schemas, action names, and credential exchanges. GitHub and Bitbucket Cloud are
the first adapters.

```text
GitHub webhook ──► GitHub adapter ───┐
                                     ├─► PullRequestEvent
Bitbucket webhook ► Bitbucket adapter┘          │
                                                ▼
                                      LifecycleCommand
                                                │
                                                ▼
                                         SCM reconciler
                                                │
                                                ▼
                                  EnvironmentOrchestrator
```

### Webhook routes

All providers enter through `POST /api/v1/webhooks/{provider}`. An adapter:

1. authenticates the exact raw request body;
2. extracts a provider-native delivery identifier;
3. normalizes supported pull-request events; and
4. returns no event for authenticated but unsupported deliveries.

GitHub uses `X-Hub-Signature-256`, `X-GitHub-Delivery`, and
`X-GitHub-Event`. Bitbucket Cloud uses `X-Hub-Signature`, `X-Request-UUID`,
and `X-Event-Key`.

### Durable commands

Idempotency keys are `<provider>:<delivery-id>`, preventing cross-provider
collisions. Commands transition through `pending`, `leased`, `succeeded`, and
`failed`. Expired leases are recoverable after process termination. Failures use
bounded exponential backoff.

The reconciler is provider-neutral. It resolves a registered service by canonical
repository identity, then calls `EnvironmentOrchestrator`. Existing environments
and existing destroy references are treated as successful idempotent outcomes.

### Authentication

Authentication is separate from webhook admission:

- GitHub App authentication signs short-lived RS256 JWTs and exchanges them for
  installation access tokens.
- Bitbucket Cloud authentication exchanges OAuth consumer credentials using the
  client-credentials grant.
- tokens are cached in memory until one minute before expiry;
- private keys and tokens never enter durable commands or state; and
- concurrent requests are coalesced by the authenticator cache lock.

Authentication is used only when reconciliation requires provider API access.
Environment lifecycle submission itself does not require an SCM token.

### State migration

The state loader recognizes ADR 0008's `github_deliveries` and
`github_commands` fields and migrates them into provider-neutral records. New
writes contain only `scm_deliveries` and `scm_commands`.

## Consequences

### Positive

- The platform domain no longer depends on GitHub vocabulary.
- GitHub and Bitbucket produce identical lifecycle semantics.
- Additional providers require adapters, not control-plane rewrites.
- Delivery retries and worker crashes are recoverable.
- Credential authority remains narrower than environment authority.

### Trade-offs

- The file-backed command queue remains single-writer.
- Repository URL canonicalization is deliberately conservative and should become
  an explicit repository identity during a future service-schema migration.
- A pull-request update currently treats an existing preview environment as
  converged; revision-aware redeployment requires deployment workflow support.
- Bitbucket Server/Data Center requires a separate adapter from Bitbucket Cloud.

## Supersession

This ADR supersedes ADR 0008's GitHub-specific domain and persistence model.
ADR 0008 remains authoritative for GitHub HMAC verification and ingress threat
modeling.

## Follow-up

- Introduce explicit provider and repository identity fields on `Service`.
- Add revision-aware preview deployment workflows.
- Replace the single-writer file queue before horizontal scaling.
- Add GitLab and Bitbucket Data Center adapters as product demand requires.
