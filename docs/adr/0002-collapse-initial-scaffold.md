# ADR 0002: Collapse Initial Control Plane Scaffold

## Status
Accepted

## Context
The original design separated Phase 3.1 (control plane scaffold) from
later phases that introduce domain modeling and persistence.

Phase 3.1 was intended to validate:
- Binary compilation
- HTTP server lifecycle
- Health endpoints
- Directory structure

During early development, these concerns were validated implicitly while
building richer API behavior, resulting in Phase 3.1 being subsumed by
later work.

## Decision
We accept the collapse of Phase 3.1 into subsequent phases and document
the scaffold as an implicit milestone rather than a distinct commit.

The control plane binary, HTTP lifecycle, and health endpoints are now
validated as part of a more complete API slice.

## Consequences
- Early architectural intent remains preserved through directory layout
  and layering boundaries.
- Historical simplicity is traded for faster validation.
- Future contributors can understand the intended evolution through ADRs
  rather than commit archaeology.