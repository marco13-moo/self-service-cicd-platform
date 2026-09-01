package bitbucket

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAuthenticatorCachesOAuthToken(t *testing.T) {
	calls := 0
	auth := NewAuthenticator("client", "secret")
	auth.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(`{"access_token":"token-value","expires_in":3600}`)), Header: make(http.Header), Request: request}, nil
	})}
	fixed := time.Unix(1_700_000_000, 0)
	auth.now = func() time.Time { return fixed }
	for range 2 {
		token, err := auth.Token(context.Background(), "workspace")
		if err != nil || token.Value != "token-value" {
			t.Fatalf("token=%#v err=%v", token, err)
		}
	}
	if calls != 1 {
		t.Fatalf("expected one exchange, got %d", calls)
	}
}
