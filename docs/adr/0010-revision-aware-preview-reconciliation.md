# ADR 0010: Revision-Aware Preview Reconciliation

## Status

Accepted

## Context

Provider-neutral pull-request events previously treated an existing environment
as converged, even when a synchronize event selected a different commit. The
platform also lacked explicit repository identity, command supersession,
terminal failure classification, and observable queue state.

## Decision

Services persist a canonical repository identity comprising provider, workspace,
and repository name. Existing service records derive this identity from their
legacy repository URL during state loading.

Environments persist a source revision containing provider, repository, pull
request, desired SHA, deployed SHA, and monotonically increasing generation.
The reconciler applies these transitions:

| Event | Desired transition |
| --- | --- |
| open/reopen | provision if absent, then submit exact-SHA deployment |
| synchronize with new SHA | increment generation and submit deployment |
| duplicate desired SHA | no-op |
| close | submit destruction once |

Newer pending commands for an environment supersede older pending or failed
commands. Processing uses recoverable leases. Transient failures retry with
bounded exponential backoff; five failed attempts move a command to the
`dead_letter` terminal state.

Argo's `env-deploy-template` checks out the exact requested revision as an input
artifact. Language-specific build and runtime deployment remain template
contracts and must not be embedded in the control plane.

Queue state is exported as Prometheus text metrics at `/metrics`. Detailed
administrative command inspection is deferred until an authenticated operator
API exists; exposing command payloads anonymously is prohibited.

When `DATABASE_URL` is present, webhook deduplication and command state use
PostgreSQL transactions. Leasing uses `FOR UPDATE SKIP LOCKED` and excludes
environments with an active lease, allowing multiple reconcilers without
concurrent mutations of the same preview. Without it, the file repository
remains the single-process fallback.

## Consequences

- Pull-request updates are no longer mistaken for convergence.
- Repository matching no longer depends solely on URL suffixes.
- Work survives process termination and stale leases are recoverable.
- A successful reconciliation currently means workflow submission succeeded;
  deployed SHA promotion requires observing the deployment workflow result.
- The file state backend remains single-writer until the PostgreSQL backend is
  selected operationally.

## Follow-up

- Promote `desired_sha` to `deployed_sha` only after Argo reports success.
- Add authenticated dead-letter replay and cancellation operations.
- Supply project-specific build/deploy templates and preview URL discovery.
