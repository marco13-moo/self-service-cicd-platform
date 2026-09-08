package auth

import (
	"context"
	"errors"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

// ValidationResult contains normalized identity and claims extracted from a
// validated token.
type ValidationResult struct {
	Subject string
	Tenant  string
	Claims  map[string]interface{}
	Expiry  time.Time
}

// Validator validates JWTs using a JWKSCache lookup strategy.
type Validator struct{
	// resolver fetches JWKS caches by URL or tenant; left as a small function to
	// be injected for testability.
	jwksResolver func(jwksURL string) (*JWKSCache, error)
}

func NewValidator(resolver func(jwksURL string) (*JWKSCache, error)) *Validator {
	return &Validator{jwksResolver: resolver}
}

// ValidateJWT validates the token against the provided JWKS URL and returns the
// extracted claims and subject. It will consult the current keyset and, if
// verification fails, will consult the previous keyset for a grace period.
func (v *Validator) ValidateJWT(ctx context.Context, token string, jwksURL string) (*ValidationResult, error) {
	if v.jwksResolver == nil {
		return nil, errors.New("no jwks resolver configured")
	}
	cache, err := v.jwksResolver(jwksURL)
	if err != nil {
		return nil, err
	}
	cur, prev := cache.KeySet()
	// Try current set first
	var parsed jwt.Token
	if cur != nil {
		parsed, err = jwt.Parse([]byte(token), jwt.WithKeySet(cur))
		if err == nil {
			exp := parsed.Expiration()
			return &ValidationResult{Subject: parsed.Subject(), Claims: parsed.PrivateClaims(), Expiry: exp}, nil
		}
	}
	// If current failed, try previous set if within grace period (5 minutes)
	if prev != nil {
		parsed, err = jwt.Parse([]byte(token), jwt.WithKeySet(prev))
		if err == nil {
			exp := parsed.Expiration()
			return &ValidationResult{Subject: parsed.Subject(), Claims: parsed.PrivateClaims(), Expiry: exp}, nil
		}
	}
	return nil, err
}
