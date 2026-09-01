package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAuthenticatorCachesInstallationToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	auth, err := NewAuthenticator("123", keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Unix(1_700_000_000, 0).UTC()
	auth.now = func() time.Time { return fixed }
	calls := 0
	auth.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if !strings.HasPrefix(request.Header.Get("Authorization"), "Bearer ") {
			t.Fatal("missing app JWT")
		}
		body := `{"token":"installation-token","expires_at":"` + fixed.Add(time.Hour).Format(time.RFC3339) + `"}`
		return &http.Response{StatusCode: http.StatusCreated, Status: "201 Created", Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: request}, nil
	})}
	for range 2 {
		token, tokenErr := auth.Token(context.Background(), "987")
		if tokenErr != nil || token.Value != "installation-token" {
			t.Fatalf("token=%#v err=%v", token, tokenErr)
		}
	}
	if calls != 1 {
		t.Fatalf("expected one exchange, got %d", calls)
	}
}
