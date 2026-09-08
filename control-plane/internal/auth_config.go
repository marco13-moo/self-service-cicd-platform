package auth

// Lightweight per-tenant auth configuration types used by the JWKS/JWT implementation.

// OIDCProviderConfig represents an external OIDC provider entry for a tenant.
type OIDCProviderConfig struct {
	Issuer       string            `json:"issuer"`
	JWKSURL      string            `json:"jwks_url"`
	Audience     []string          `json:"audience,omitempty"`
	ClaimMapping map[string]string `json:"claim_mapping,omitempty"` // e.g. group -> role
}

// TenantAuthConfig is the persisted per-tenant authentication configuration.
type TenantAuthConfig struct {
	TenantID  string               `json:"tenant_id"`
	Providers []OIDCProviderConfig `json:"providers"`
}
