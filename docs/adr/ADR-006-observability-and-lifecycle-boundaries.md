ADR-006: Observability & Environment Lifecycle (Phase 6)
Status

Accepted (Partially Implemented)

Context

By the end of Phase 5, the control plane can create and destroy ephemeral environments by submitting intent to Argo WorkflowTemplates. Each environment creation submits:

An environment creation workflow

A TTL cleanup workflow

This establishes a clean separation of concerns:

The control plane expresses intent

Argo executes workflows

Phase 6 was introduced to improve visibility, lifecycle ownership, and operational safety without collapsing this separation.

Problem Statement

The system currently lacks lifecycle awareness and observability:

The control plane does not track workflow names or states

Cleanup workflows may run indefinitely

There is no reliable signal for:

Environment readiness

Failure

Completion

TTL semantics are expressed but not enforced

Despite these gaps, the execution plane behaves deterministically and correctly according to submitted intent.

Decision

Phase 6 explicitly does not fully solve TTL enforcement or workflow lifecycle management.

Instead, the following decisions are locked:

WorkflowTemplates remain the execution contract

No inline workflow YAML

No controllers or CRDs introduced

No GitOps or reconciliation loops

The control plane remains stateless

No database is introduced in Phase 6

Workflow names are not persisted yet

TTL semantics are acknowledged but deferred

expires_at is passed to workflows

Cleanup workflows are bounded but not time-accurate

Long-running or infinite workflows are considered unacceptable

Observability is best-effort

No strong guarantees on environment readiness

No user-facing lifecycle state machine yet

What Is Explicitly Missing (Known Gaps)

The following are intentional omissions, not oversights:

Accurate TTL enforcement

Workflow completion guarantees

Environment status polling

Log aggregation or streaming

Argo UI deep-linking

Failure propagation to API consumers

Persistent environment records

Auth/RBAC per environment

These are deferred to later phases.

Rationale

Attempting to fully solve lifecycle management before:

In-cluster deployment

Stable Argo SDK integration

Persistent state

would introduce premature coupling and architectural churn.

Phase 6 is therefore treated as a boundary phase: sufficient observability to operate safely, but no durable lifecycle ownership.

Consequences

Positive

Execution and control planes remain cleanly separated

No premature controllers or reconciliation logic

System remains debuggable and evolvable

Negative

TTL semantics are approximate

Manual cleanup may be required

Observability is incomplete

These tradeoffs are accepted to unblock Phase 7.