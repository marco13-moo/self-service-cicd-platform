package auth

import "context"

// Validator is a small facade for JWT validation logic.
type Validator struct{}

func NewValidator() *Validator { return &Validator{} }

// ValidateJWT validates the token for the given tenant and returns normalized claims and role mappings.
// Produces nil, nil for the scaffold; implement real validation: fetch keys from JWKSCache, verify signature, exp/nbf, aud, issuer.
func (v *Validator) ValidateJWT(ctx context.Context, token string, tenantID string) (map[string]interface{}, error) {
	// TODO: lookup tenant auth config, find matching provider, get JWKS from cache, verify signature, map claims -> roles.
	return nil, nil
}
