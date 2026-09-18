# ADR 0027: Tenancy, Quota, and Isolation Invariants

- Status: Accepted
- Date: 2026-09-18

Tenant ownership is immutable at the service boundary. Every catalog and
diagnostic query is scoped to the authenticated tenant; cross-tenant reads,
writes, repository transfers, network access, and artifact trust require an
explicit platform-admin workflow. Quotas are admission controls and must fail
closed when durable accounting is unavailable.

## Related decisions

ADR 0018, ADR 0019, and ADR 0020 already define identity, RLS, lifecycle, and
network isolation. This ADR sets the catalog invariants without duplicating
their implementation.
