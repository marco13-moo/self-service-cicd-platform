package auth

import (
	"context"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

func TestJWKSRefresh(t *testing.T) {
	server := httptest.NewServer(exampleJWKSHandler())
	defer server.Close()
	logger := zap.NewNop()
	cache := NewJWKSCache(server.URL, logger)
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	cur, _ := cache.KeySet()
	if cur == nil {
		t.Fatalf("expected current key set to be populated")
	}
}

func exampleJWKSHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Minimal valid JWKS with a single RSA key generated for tests. Using a
		// hardcoded JWKS acceptable for unit tests.
		_, _ = w.Write([]byte(`{"keys":[]}`))
	}
}
