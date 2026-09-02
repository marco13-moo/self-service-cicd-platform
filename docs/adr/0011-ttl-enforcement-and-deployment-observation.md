# ADR 0011: TTL Enforcement and Deployment Observation

## Status

Accepted

## Context

ADR 0010 introduced revision-aware preview deployment, but two lifecycle gaps
remained. The TTL workflow evaluated the expiry timestamp once and terminated
without deleting an environment that had not already expired. Deployment
workflow submission was durable, but successful execution never promoted
`desired_sha` to `deployed_sha`.

Naively correcting either gap would introduce new hazards. A control-plane timer
would make expiry dependent on process availability, while an unguarded workflow
observer could allow a late completion for generation N to mark generation N+1
as deployed.

## Decision

### TTL ownership

Argo remains the lifecycle authority. Environment creation persists an absolute
UTC `expires_at` value and submits it with the relative TTL duration. The TTL
WorkflowTemplate executes two sequential nodes:

1. an Argo `suspend` node for the requested duration; and
2. a namespace deletion node using the constrained environment-management
   service account.

The suspension is represented in Kubernetes/Argo state and therefore survives
control-plane restarts. Explicit pull-request closure may delete the namespace
earlier; TTL deletion remains idempotent through `--ignore-not-found`.

### Deployment observation

The reconciler periodically reads the current deployment Workflow from Argo and
persists its phase, message, and observation timestamp. `deployed_sha` is
promoted to the current `desired_sha` only when Argo reports `Succeeded`.
`Failed` and `Error` are terminal observations but never promote a revision.

Persistence uses a compare-and-set over all of the following values:

- environment name;
- deployment Workflow name; and
- source generation.

If any value differs from the current environment, the observation is stale and
is ignored. A newly submitted generation starts in `Pending` while retaining the
previous successfully deployed SHA.

The service store returns detached environment snapshots. Callers cannot mutate
authoritative state without passing through a persistence operation or the
generation-aware observation boundary.

## Consequences

- Environment expiry no longer depends on control-plane uptime.
- Desired and successfully deployed revisions have distinct, observable state.
- Late completion of an obsolete Workflow cannot corrupt a newer generation.
- Environment API responses expose expiry, source convergence, and live deploy
  Workflow status.
- Argo continues to own execution; the control plane records observations rather
  than interpreting pod-level mechanics.
- Relative suspend duration is fixed at environment creation. Extending a TTL
  requires a future explicit rescheduling operation rather than mutating a
  running Workflow implicitly.

## Follow-up

- Add an authenticated TTL-extension operation that replaces the existing TTL
  Workflow under an explicit compare-and-set contract.
- Record structured deployment failure classifications and expose retry policy.
- Emit convergence-latency and expired-environment metrics.
