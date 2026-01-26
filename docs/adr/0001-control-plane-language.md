# ADR-0001: Control Plane Implementation Language

## Status
Accepted

## Context

The control plane is responsible for:
- API exposure
- Orchestration logic
- Provider integrations
- Policy enforcement

It must be:
- Operationally predictable
- Resource-efficient
- Easy to statically analyze
- Widely deployable in containerized environments

## Decision

The control plane will be implemented in **Go**.

## Rationale

- Strong concurrency primitives
- Fast startup and low memory footprint
- Excellent ecosystem for cloud-native systems
- First-class support for Kubernetes and CNCF tooling
- Simple static binaries ease deployment and security review

## Consequences

- Faster iteration on orchestration logic
- Clear separation between control and execution planes
- Higher initial verbosity compared to scripting languages

