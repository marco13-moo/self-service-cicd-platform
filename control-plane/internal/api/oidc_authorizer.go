package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	platformauth "github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/auth"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/telemetry"
)

type HybridAuthorizer struct {
	static *TenantAuthorizer
	store  *ServiceStore
	client *http.Client
	mu     sync.Mutex
	caches map[string]*platformauth.JWKSCache
}

func NewHybridAuthorizer(static *TenantAuthorizer, store *ServiceStore) *HybridAuthorizer {
	return &HybridAuthorizer{static: static, store: store, client: &http.Client{Timeout: 10 * time.Second}, caches: map[string]*platformauth.JWKSCache{}}
}

func (a *HybridAuthorizer) Require(minimum TenantRole, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if token == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		principal, ok := a.static.Authenticate(token)
		if !ok {
			var err error
			principal, err = a.authenticateOIDC(r.Context(), token)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		active, err := a.store.TenantActive(r.Context(), principal.TenantID)
		if err != nil || !active {
			a.recordAuthentication(r.Context(), principal, "denied", "tenant_inactive")
			http.Error(w, "tenant unavailable", http.StatusForbidden)
			return
		}
		if roleRank(principal.Role) < roleRank(minimum) {
			a.recordAuthentication(r.Context(), principal, "denied", "insufficient_role")
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		a.recordAuthentication(r.Context(), principal, "succeeded", "authenticated")
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalContextKey{}, principal)))
	})
}

func (a *HybridAuthorizer) authenticateOIDC(ctx context.Context, token string) (Principal, error) {
	issuer, err := platformauth.ExtractIssuer(token)
	if err != nil {
		return Principal{}, err
	}
	tenantID, payload, err := a.store.ResolveTenantAuthByIssuer(ctx, issuer)
	if err != nil {
		return Principal{}, err
	}
	var selected *platformauth.ProviderConfig
	for index := range payload.Providers {
		if payload.Providers[index].Issuer == issuer {
			selected = &payload.Providers[index]
			break
		}
	}
	if selected == nil || TenantID(selected.TenantID) != tenantID {
		return Principal{}, sql.ErrNoRows
	}
	cache := a.cache(selected.JWKSURL)
	if cache.RefreshDue(time.Now().UTC()) {
		if err := cache.Refresh(ctx); err != nil {
			telemetry.RecordJWKSRefreshFailure()
			return Principal{}, err
		}
	}
	identity, err := platformauth.NewValidator(cache).Validate(ctx, token, *selected)
	if err != nil {
		a.recordAuthentication(ctx, Principal{TenantID: tenantID, Subject: "unknown"}, "denied", "invalid_oidc_token")
		return Principal{}, err
	}
	principal := Principal{Subject: identity.Subject, TenantID: TenantID(identity.TenantID), Role: TenantRole(identity.Role)}
	if !validTenantID(principal.TenantID) || roleRank(principal.Role) == 0 {
		return Principal{}, errors.New("OIDC identity produced an invalid tenant capability")
	}
	return principal, nil
}

func (a *HybridAuthorizer) cache(url string) *platformauth.JWKSCache {
	a.mu.Lock()
	defer a.mu.Unlock()
	cache := a.caches[url]
	if cache == nil {
		cache = platformauth.NewJWKSCache(url, a.client, 5*time.Minute, 5*time.Minute, 15*time.Minute)
		a.caches[url] = cache
		go cache.Run(context.Background())
	}
	return cache
}

func (a *HybridAuthorizer) recordAuthentication(ctx context.Context, principal Principal, outcome, reason string) {
	if principal.TenantID == "" {
		return
	}
	_ = a.store.AppendPlatformAuditEvent(ctx, AuditEvent{
		TenantID: principal.TenantID, CorrelationID: uuid.NewString(), Actor: principal.Subject,
		EventType: "authentication.decision", ResourceType: "tenant", ResourceName: string(principal.TenantID), Outcome: outcome,
		Metadata: map[string]any{"reason": reason},
	})
}
