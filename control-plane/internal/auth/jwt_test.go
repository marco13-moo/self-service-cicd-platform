package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

type rotatingJWKS struct {
	mu     sync.RWMutex
	key    *rsa.PrivateKey
	kid    string
	maxAge string
}

func (r *rotatingJWKS) handler(w http.ResponseWriter, _ *http.Request) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	if r.maxAge != "" {
		w.Header().Set("Cache-Control", r.maxAge)
	}
	n := base64.RawURLEncoding.EncodeToString(r.key.PublicKey.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(r.key.PublicKey.E)).Bytes())
	_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{"kty": "RSA", "kid": r.kid, "alg": "RS256", "use": "sig", "n": n, "e": e}}})
}

func (r *rotatingJWKS) rotate(t *testing.T, kid string) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.key, r.kid = key, kid
	r.mu.Unlock()
	return key
}

func signedToken(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	encoded, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestValidatorEnforcesIdentityAndRollover(t *testing.T) {
	keys := &rotatingJWKS{maxAge: "public, max-age=60"}
	first := keys.rotate(t, "first")
	server := httptest.NewTLSServer(http.HandlerFunc(keys.handler))
	defer server.Close()
	cache := NewJWKSCache(server.URL, server.Client(), time.Hour, 5*time.Minute, time.Hour)
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	provider := ProviderConfig{Issuer: "https://issuer.example.test", JWKSURL: server.URL, Audiences: []string{"control-plane"}, TenantID: "alpha", TenantClaim: "tenant", GroupsClaim: "groups", GroupRoles: map[string]string{"developers": "developer", "admins": "admin"}}
	now := time.Now().UTC()
	claims := jwt.MapClaims{"iss": provider.Issuer, "sub": "alice", "aud": []string{"control-plane"}, "exp": now.Add(time.Minute).Unix(), "nbf": now.Add(-time.Minute).Unix(), "tenant": "alpha", "groups": []string{"developers"}}
	identity, err := NewValidator(cache).Validate(context.Background(), signedToken(t, first, "first", claims), provider)
	if err != nil || identity.Subject != "alice" || identity.TenantID != "alpha" || identity.Role != "developer" {
		t.Fatalf("valid identity rejected or mis-mapped: %#v %v", identity, err)
	}

	second := keys.rotate(t, "second")
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := NewValidator(cache).Validate(context.Background(), signedToken(t, first, "first", claims), provider); err != nil {
		t.Fatalf("predecessor key was rejected during grace: %v", err)
	}
	secondClaims := jwt.MapClaims{"iss": provider.Issuer, "sub": "alice", "aud": "control-plane", "exp": now.Add(time.Minute).Unix(), "tenant": "alpha", "groups": []string{"admins"}}
	identity, err = NewValidator(cache).Validate(context.Background(), signedToken(t, second, "second", secondClaims), provider)
	if err != nil || identity.Role != "admin" {
		t.Fatalf("rotated key was not authoritative: %#v %v", identity, err)
	}
	cache.mu.Lock()
	cache.previousExpires = now.Add(-time.Second)
	cache.mu.Unlock()
	if _, err := NewValidator(cache).Validate(context.Background(), signedToken(t, first, "first", claims), provider); err == nil {
		t.Fatal("expired predecessor key remained trusted")
	}
}

func TestValidatorRejectsClaimAndAlgorithmConfusion(t *testing.T) {
	keys := &rotatingJWKS{}
	key := keys.rotate(t, "primary")
	server := httptest.NewTLSServer(http.HandlerFunc(keys.handler))
	defer server.Close()
	cache := NewJWKSCache(server.URL, server.Client(), time.Minute, time.Minute, time.Hour)
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	provider := ProviderConfig{Issuer: "https://issuer.example.test", JWKSURL: server.URL, Audiences: []string{"control-plane"}, TenantID: "alpha", TenantClaim: "tenant", GroupRoles: map[string]string{"developers": "developer"}}
	base := jwt.MapClaims{"iss": provider.Issuer, "sub": "alice", "aud": "control-plane", "exp": time.Now().Add(time.Minute).Unix(), "tenant": "alpha", "groups": []string{"developers"}}
	tests := []struct {
		name   string
		mutate func(jwt.MapClaims)
	}{
		{"wrong issuer", func(c jwt.MapClaims) { c["iss"] = "https://attacker.example.test" }},
		{"wrong audience", func(c jwt.MapClaims) { c["aud"] = "other" }},
		{"wrong tenant", func(c jwt.MapClaims) { c["tenant"] = "beta" }},
		{"expired", func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Minute).Unix() }},
		{"unmapped group", func(c jwt.MapClaims) { c["groups"] = []string{"outsiders"} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			claims := jwt.MapClaims{}
			for name, value := range base {
				claims[name] = value
			}
			test.mutate(claims)
			if _, err := NewValidator(cache).Validate(context.Background(), signedToken(t, key, "primary", claims), provider); err == nil {
				t.Fatal("invalid JWT was accepted")
			}
		})
	}
	none := jwt.NewWithClaims(jwt.SigningMethodNone, base)
	none.Header["kid"] = "primary"
	encoded, err := none.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewValidator(cache).Validate(context.Background(), encoded, provider); err == nil {
		t.Fatal("unsigned JWT was accepted")
	}
}

func TestProviderConfigurationRejectsUnsafeJWKSURLs(t *testing.T) {
	base := ProviderConfig{Issuer: "https://issuer.example.test", JWKSURL: "http://metadata.internal/keys", Audiences: []string{"control-plane"}, TenantID: "alpha", GroupRoles: map[string]string{"group": "viewer"}}
	if err := base.Validate(false); err == nil {
		t.Fatal("plaintext non-loopback JWKS URL was accepted")
	}
	base.JWKSURL = "https://user:password@issuer.example.test/keys"
	if err := base.Validate(false); err == nil {
		t.Fatal("credential-bearing JWKS URL was accepted")
	}
}
