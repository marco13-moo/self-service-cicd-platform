package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

var permittedAlgorithms = []string{"RS256", "RS384", "RS512", "ES256", "ES384", "ES512"}

type Identity struct {
	Subject   string
	TenantID  string
	Role      string
	ExpiresAt time.Time
}

type Validator struct {
	cache *JWKSCache
	now   func() time.Time
}

func ExtractIssuer(encoded string) (string, error) {
	claims := jwt.MapClaims{}
	_, _, err := jwt.NewParser().ParseUnverified(encoded, claims)
	if err != nil {
		return "", errors.New("malformed JWT")
	}
	issuer, err := claims.GetIssuer()
	if err != nil || strings.TrimSpace(issuer) == "" {
		return "", errors.New("JWT issuer is required")
	}
	return issuer, nil
}

func NewValidator(cache *JWKSCache) *Validator {
	return &Validator{cache: cache, now: time.Now}
}

func (v *Validator) Validate(ctx context.Context, encoded string, provider ProviderConfig) (Identity, error) {
	if err := provider.Validate(false); err != nil {
		return Identity{}, err
	}
	if v.cache == nil {
		return Identity{}, errors.New("JWKS cache is unavailable")
	}
	if v.cache.URL() != provider.JWKSURL {
		return Identity{}, errors.New("JWKS cache does not match provider configuration")
	}
	claims := jwt.MapClaims{}
	parser := jwt.NewParser(jwt.WithValidMethods(permittedAlgorithms), jwt.WithExpirationRequired(), jwt.WithIssuer(provider.Issuer), jwt.WithLeeway(30*time.Second))
	keyFunction := func(token *jwt.Token) (any, error) {
		kid, ok := token.Header["kid"].(string)
		if !ok || strings.TrimSpace(kid) == "" {
			return nil, errors.New("JWT kid is required")
		}
		algorithm := token.Method.Alg()
		if key, found := v.cache.Lookup(kid, algorithm, v.now().UTC()); found {
			return key, nil
		}
		if err := v.cache.Refresh(ctx); err != nil {
			return nil, fmt.Errorf("refresh JWKS for unknown kid: %w", err)
		}
		if key, found := v.cache.Lookup(kid, algorithm, v.now().UTC()); found {
			return key, nil
		}
		return nil, errors.New("JWT kid is not trusted")
	}
	parsed, err := parser.ParseWithClaims(encoded, claims, keyFunction)
	if err != nil || !parsed.Valid {
		return Identity{}, fmt.Errorf("validate JWT: %w", err)
	}
	// Additional audiences are accepted explicitly; the parser enforces the
	// first configured audience and this branch supports intentional aliases.
	if !claimsAudienceMatches(claims, provider.Audiences) {
		return Identity{}, errors.New("JWT audience is not allowed")
	}
	subjectClaim := provider.SubjectClaim
	if subjectClaim == "" {
		subjectClaim = "sub"
	}
	subject, _ := claims[subjectClaim].(string)
	if strings.TrimSpace(subject) == "" {
		return Identity{}, errors.New("JWT subject is required")
	}
	if provider.TenantClaim != "" {
		claimedTenant, _ := claims[provider.TenantClaim].(string)
		if claimedTenant != provider.TenantID {
			return Identity{}, errors.New("JWT tenant claim does not match provider ownership")
		}
	}
	groupsClaim := provider.GroupsClaim
	if groupsClaim == "" {
		groupsClaim = "groups"
	}
	role := mapGroupsToRole(claims[groupsClaim], provider.GroupRoles)
	if role == "" {
		return Identity{}, errors.New("JWT groups do not grant a tenant role")
	}
	expiresAt, err := claims.GetExpirationTime()
	if err != nil || expiresAt == nil {
		return Identity{}, errors.New("JWT expiration is required")
	}
	return Identity{Subject: subject, TenantID: provider.TenantID, Role: role, ExpiresAt: expiresAt.Time}, nil
}

func claimsAudienceMatches(claims jwt.MapClaims, allowed []string) bool {
	audiences, err := claims.GetAudience()
	if err != nil {
		return false
	}
	for _, actual := range audiences {
		for _, expected := range allowed {
			if actual == expected {
				return true
			}
		}
	}
	return false
}

func mapGroupsToRole(raw any, mappings map[string]string) string {
	rank := map[string]int{"viewer": 1, "developer": 2, "admin": 3}
	best := ""
	var groups []string
	switch value := raw.(type) {
	case []any:
		for _, item := range value {
			if group, ok := item.(string); ok {
				groups = append(groups, group)
			}
		}
	case []string:
		groups = value
	case string:
		groups = []string{value}
	}
	for _, group := range groups {
		if candidate := mappings[group]; rank[candidate] > rank[best] {
			best = candidate
		}
	}
	return best
}
