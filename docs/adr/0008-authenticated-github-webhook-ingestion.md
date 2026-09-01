# ADR 0008: Authenticated GitHub Webhook Ingestion

## Status

Accepted

## Context

Phase 8 introduces pull-request-driven preview environments. GitHub webhooks are
an untrusted Internet-facing input boundary: deliveries can be forged, replayed,
duplicated, reordered, or retried after the control plane restarts. Coupling the
HTTP handler directly to Argo submission would amplify those transport semantics
into duplicate infrastructure mutations and make acknowledgment latency depend
on the execution plane.

The platform therefore requires an ingress contract that:

- authenticates the exact raw request body using the GitHub App webhook secret;
- bounds request size before parsing;
- deduplicates GitHub delivery IDs durably and atomically;
- translates supported events into provider-neutral lifecycle intent;
- acknowledges unsupported events without side effects; and
- remains independent from workflow submission and reconciliation.

## Decision

The control plane exposes `POST /api/v1/webhooks/github` and validates
`X-Hub-Signature-256` as an HMAC-SHA-256 digest of the raw body. Comparisons use
constant-time equality. Missing, malformed, or incorrect signatures receive
`401 Unauthorized`. Payloads are limited to 1 MiB.

Every authenticated request must include `X-GitHub-Delivery`. The atomic state
repository records delivery IDs before acknowledging the request. Delivery IDs
are retained for seven days, covering normal GitHub retry behavior without
unbounded state growth. A repeated ID returns `202 Accepted` and creates no
additional command.

Supported `pull_request` actions translate as follows:

| GitHub action | Durable command |
| --- | --- |
| `opened` | `upsert_preview_environment` |
| `synchronize` | `upsert_preview_environment` |
| `reopened` | `upsert_preview_environment` |
| `closed` | `destroy_preview_environment` |

Commands retain the delivery ID, installation ID, repository identity, pull
request number, head SHA, receipt time, and deterministic environment name.
Environment names are normalized to Kubernetes DNS-label syntax and limited to
63 characters.

Other GitHub event types and unsupported pull-request actions are authenticated,
deduplicated, and acknowledged with `202 Accepted`, but create no command.

This phase intentionally stops at durable command production. The HTTP handler
must not submit Argo workflows, obtain installation tokens, or perform repository
mutations. A subsequent reconciler will consume commands asynchronously and
apply idempotent desired-state transitions.

## Security boundaries

- `GITHUB_WEBHOOK_SECRET` is supplied through a Kubernetes Secret and is never
  logged, serialized into state, or returned by the API.
- Signature verification occurs before JSON parsing or delivery persistence.
- Response bodies do not disclose authentication or persistence internals.
- GitHub App private keys and installation-token minting remain a separate
  capability with narrower authorization and are deferred to the reconciler.

## Consequences

### Positive

- Forged webhook traffic cannot create lifecycle intent.
- GitHub retries are harmless across process restarts.
- HTTP acknowledgment remains independent of Argo availability.
- Event transport and environment reconciliation can evolve independently.
- Tests require neither GitHub credentials nor a Kubernetes cluster.

### Trade-offs

- The current JSON repository remains single-writer and unsuitable for multiple
  control-plane replicas.
- Commands are durable but do not yet have consumption, acknowledgment, retry,
  or dead-letter states.
- Seven-day deduplication is a bounded operational policy rather than an eternal
  replay guarantee.
- Closing a PR records a destroy command but does not destroy an environment
  until the Phase 8 reconciler exists.

## Alternatives considered

### Submit workflows synchronously in the webhook handler

Rejected because retries could duplicate infrastructure and Argo latency would
determine webhook acknowledgment behavior.

### Trust GitHub source addresses

Rejected because network origin is not message authenticity. HMAC verification
is GitHub's explicit payload-authentication mechanism.

### Keep delivery IDs only in memory

Rejected because a restart during GitHub retry windows would erase idempotency.

### Introduce a message broker immediately

Deferred. A broker becomes appropriate with multiple replicas, higher delivery
volume, or independent workers; it is unnecessary for the present single-writer
architecture.

## Follow-up

The next increment will add GitHub App installation authentication and a
reconciler that leases durable commands, resolves repository configuration, and
idempotently creates or destroys preview-environment workflows.
