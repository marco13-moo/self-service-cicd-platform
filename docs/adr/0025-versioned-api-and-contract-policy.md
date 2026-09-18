# ADR 0025: Versioned API and Contract Policy

- Status: Accepted
- Date: 2026-09-18

Declarations use `platform.service/v1` and additive JSON-compatible evolution.
Required fields and semantics are immutable within a version; incompatible
changes require a new version and an explicit migration. The API may retain
legacy flat fields during migration, but new examples and tooling use the
versioned shape. Schemas, HTTP behavior, and CLI serialization are tested
together.

Credentials, bearer tokens, and provider secrets are never declaration fields.
