# Phase 7 — Argo SDK Migration: Typed Execution for the Control Plane

## Overview

Phase 7 transitions the control plane from CLI-based workflow submission to a fully typed execution model using the Argo Workflows Go SDK.

This marks a foundational shift in platform maturity: the system no longer shells out to user-oriented tooling but instead communicates directly with the Kubernetes API server — the authoritative execution substrate.

The control plane continues to express intent while delegating lifecycle ownership to Argo.

---

## Objectives

* Eliminate subprocess-based workflow submission
* Establish a typed execution boundary
* Improve security posture by removing the Argo CLI dependency
* Enable deterministic workflow lifecycle control
* Strengthen architectural separation between intent and execution
* Prepare the platform for PR environments, promotion workflows, and tenancy

---

## Key Architectural Shift

### Previous Execution Path

```
Control Plane
   ↓
exec.Command("argo submit")
   ↓
Argo CLI
   ↓
Kubernetes API
```

### New Execution Path

```
Control Plane
   ↓
WorkflowExecutor (interface)
   ↓
Argo Go Client
   ↓
Kubernetes API
   ↓
Argo Controller
```

The change is strictly transport-level. Execution authority remains external.

---

## Implementation Summary

### 1. Introduced Typed Kubernetes and Argo Clients

A client factory was added to construct:

* in-cluster configuration for production
* kubeconfig fallback for local development

Clients are injected at startup to avoid global state and hidden dependencies.

---

### 2. Formalized the Execution Boundary

A `WorkflowExecutor` interface now defines the transport contract:

* Submit workflows from templates
* Retrieve workflow state
* Cancel workflows via shutdown patch

This isolates orchestration logic from execution mechanics and preserves future backend optionality.

---

### 3. Implemented the Argo SDK Executor

The new executor:

* Creates Workflow CRs directly
* Injects parameters safely
* Applies platform labels
* Returns typed Workflow objects

No subprocesses are spawned.

No CLI parsing exists.

---

### 4. Migrated to a Composition Root

Dependency construction moved into the server bootstrap layer.

**Before:**

* Orchestrators constructed executors
* Routers created infrastructure
* Dependencies were implicit

**After:**

* Server wires executor → orchestrator → router
* Construction is deterministic
* Infrastructure ownership is explicit

This aligns the control plane with mature Go service patterns.

---

### 5. Removed CLI Artifacts

The following were permanently deleted:

* CLI executor
* subprocess status readers
* JSON parsing helpers
* wrapper status types
* Argo binary dependency

This reduced container size, attack surface, and operational complexity.

---

### 6. Returned Native Workflow Types

The platform no longer wraps execution status.

Instead, it returns:

```
*wf.Workflow
*wf.WorkflowStatus
```

Benefits include:

* compile-time safety
* richer metadata access
* zero mapping drift
* simpler observability

Control planes should not editorialize execution state.

---

### 7. Enabled Deterministic Lifecycle Control

The SDK unlocks capabilities previously impractical with CLI execution:

* safe workflow cancellation
* structured status queries
* label-based discovery
* metadata inspection

These primitives are prerequisites for higher-order platform behaviors.

---

## Security Improvements

Removing the CLI produced immediate benefits:

* smaller container image
* reduced CVE exposure
* no shell injection vector
* fewer supply-chain dependencies

Infrastructure binaries should only exist when strictly required.

---

## Tradeoffs

### Increased Dependency Graph

Kubernetes and Argo client libraries introduce substantial transitive dependencies. This is an acceptable cost for type safety and platform correctness.

### Closer Kubernetes Coupling

The control plane now interacts directly with CRDs. Given Kubernetes is the execution substrate, this coupling is intentional.

### Migration Complexity

Interface drift required coordinated updates across orchestrators, handlers, router wiring, and server bootstrap. This was a one-time structural cost.

---

## Guardrails Established

The control plane must never:

* shell out to the Argo CLI
* parse workflow JSON manually
* recreate wrapper status types
* assume execution ownership

Argo remains the lifecycle authority.

The control plane submits intent — nothing more.

---

## Architectural Outcome

The platform now exhibits proper control-plane topology:

```
Control Plane → expresses intent
Kubernetes API → authoritative state
Argo Controller → reconciles execution
```

This is categorically different from a scripting model.

---

## Capabilities Unlocked

Phase 7 enables the safe construction of advanced platform features:

* PR-driven ephemeral environments
* promotion workflows
* label-indexed execution queries
* multi-tenant isolation
* policy enforcement
* automated cleanup strategies

Subsequent phases build directly on this execution foundation.

---

## Strategic Impact

This migration represents the transition from:

> “a service that triggers workflows”

to:

> **a control plane operating over Kubernetes resources.**

Typed execution is the prerequisite for every serious internal developer platform.

Phase 7 establishes that prerequisite.

---

## Next Steps

With the execution boundary stabilized, the platform is prepared for:

**Phase 8 — GitHub App Integration and PR-Driven Environments**

This will introduce secure installation authentication, webhook ingestion, lifecycle orchestration, and automated preview environments.

---

## Suggested Commit Message

```
feat(control-plane): migrate workflow execution from argo CLI to Go SDK

- introduce typed argo client
- formalize WorkflowExecutor interface
- implement sdk-backed executor
- remove CLI artifacts and subprocess parsing
- adopt composition root for dependency wiring
- return native workflow types
- enable cancellation and structured status queries
- improve security by eliminating argo binary
```
