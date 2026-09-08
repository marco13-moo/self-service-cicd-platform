Design: JWKS rotation and OIDC/JWT authentication

Status: Draft

Overview

This design captures the initial approach to replace static TENANT_AUTH_TOKENS with OIDC/JWT authentication and to provide per-tenant JWKS rotation support.

Goals
- Per-tenant OIDC configuration: issuer URI, JWKS URL(s), audience, and claim-to-role mappings.
- Validate incoming JWTs against tenant-specific JWKS with graceful rotation support.
- Provide a short-lived client credential issuance path (when machine identities are required).
- Cache and rotate JWKS with a configurable refresh interval and backoff, supporting key rollover grace windows.
- Emit audit events for validation decisions (accepted, rejected, reason) and JWKS refreshes/failures.

Components
1) Config schema
- TenantAuthConfig: per-tenant configuration stored in the tenant DB (issuer, jwks_url, audiences, claim mappings).

2) JWKS fetcher & cache
- Periodic background refresh per distinct JWKS URL with jittered interval.
- On change, validate new keys and keep previous keyset for a configured grace window to support key rollover.
- Expose a thread-safe lookup for key by kid.

3) JWT validator
- Validate signature using key from JWKS cache, check exp/nbf, aud, issuer.
- Extract claims, normalize usernames/emails, and map groups/claims to tenant-scoped roles via a configurable mapping table.
- Produce an auth context that includes identity, subject, roles, and raw claims.

4) Short-lived machine credentials
- Optional control-plane client credential flow for machine identities with compact tokens and short TTLs. These tokens are issued by the control-plane and validated via the same JWT validator.

5) Audit hooks
- Emit structured audit events for token validations and JWKS refresh outcomes. Ensure events contain correlation_id, actor, tenant_id, event_type, and non-sensitive metadata.

Migration notes
- Start in a dual-mode validation: accept both static TENANT_AUTH_TOKENS and JWTs, with a deprecation timeline for static tokens.
- Provide a migration tool that can create tenant auth records from existing metadata and notify tenant admins.

Next steps
- Add API contract for tenant auth configuration and claim-to-role mappings (PATCH/GET tenant auth endpoints).
- Implement a lightweight JWKS cache and JWT validator in control-plane/internal with unit and e2e tests.

"Design created programmatically by an AI assistant using Copilot CLI runtime in VS Code."