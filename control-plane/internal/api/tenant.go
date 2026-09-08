package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const DefaultTenantID = "default"

type TenantID string

type TenantRole string

const (
	TenantViewer    TenantRole = "viewer"
	TenantDeveloper TenantRole = "developer"
	TenantAdmin     TenantRole = "admin"
)

type Principal struct {
	Subject  string     `json:"subject"`
	TenantID TenantID   `json:"tenant_id"`
	Role     TenantRole `json:"role"`
}

type principalContextKey struct{}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}

type TenantAuthorizer struct {
	tokens map[[sha256.Size]byte]Principal
}

// NewTenantAuthorizer parses a JSON object whose keys are opaque bearer tokens
// and whose values are tenant principals. Only token hashes are retained.
func NewTenantAuthorizer(raw string) (*TenantAuthorizer, error) {
	var configured map[string]Principal
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("TENANT_AUTH_TOKENS is required")
	}
	if err := json.Unmarshal([]byte(raw), &configured); err != nil {
		return nil, fmt.Errorf("decode TENANT_AUTH_TOKENS: %w", err)
	}
	authorizer := &TenantAuthorizer{tokens: make(map[[sha256.Size]byte]Principal, len(configured))}
	for token, principal := range configured {
		if strings.TrimSpace(token) == "" || !validTenantID(principal.TenantID) || strings.TrimSpace(principal.Subject) == "" || roleRank(principal.Role) == 0 {
			return nil, errors.New("TENANT_AUTH_TOKENS contains an invalid token or principal")
		}
		authorizer.tokens[sha256.Sum256([]byte(token))] = principal
	}
	return authorizer, nil
}

func (a *TenantAuthorizer) Require(minimum TenantRole, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if provided == "" || a == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		providedHash := sha256.Sum256([]byte(provided))
		var principal Principal
		matched := false
		for tokenHash, candidate := range a.tokens {
			if subtle.ConstantTimeCompare(providedHash[:], tokenHash[:]) == 1 {
				principal, matched = candidate, true
			}
		}
		if !matched {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if roleRank(principal.Role) < roleRank(minimum) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalContextKey{}, principal)))
	})
}

func (a *TenantAuthorizer) TenantIDs() []TenantID {
	seen := map[TenantID]struct{}{}
	for _, principal := range a.tokens {
		seen[principal.TenantID] = struct{}{}
	}
	tenants := make([]TenantID, 0, len(seen))
	for tenantID := range seen {
		tenants = append(tenants, tenantID)
	}
	return tenants
}

func roleRank(role TenantRole) int {
	switch role {
	case TenantViewer:
		return 1
	case TenantDeveloper:
		return 2
	case TenantAdmin:
		return 3
	default:
		return 0
	}
}

func validTenantID(id TenantID) bool {
	value := string(id)
	if len(value) < 1 || len(value) > 63 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return false
		}
	}
	return true
}

func normalizeTenantID(id TenantID) TenantID {
	if id == "" {
		return DefaultTenantID
	}
	return id
}

func tenantFromRequest(r *http.Request) TenantID {
	if principal, ok := PrincipalFromContext(r.Context()); ok {
		return principal.TenantID
	}
	return DefaultTenantID
}
