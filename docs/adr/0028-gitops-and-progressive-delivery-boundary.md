# ADR 0028: GitOps and Progressive Delivery Boundary

- Status: Accepted
- Date: 2026-09-18

The platform publishes validated, immutable desired artifacts and promotion
intent. GitOps/controllers and delivery providers execute that intent and
report evidence; they do not bypass catalog policy or tenant authorization.
Progressive rollout, rollback, and approval gates belong to delivery, not the
catalog API. Existing Argo workflows remain the execution integration.

## Related decisions

ADR 0005, ADR 0012, ADR 0021, and ADR 0022 constrain execution and promotion.
