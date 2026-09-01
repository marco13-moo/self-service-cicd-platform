package github

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestProviderValidatesAndDetectsProject(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := "{}"
		if request.URL.Path == "/repos/acme/checkout/contents" {
			body = `[{"name":"go.mod","type":"file"}]`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})
	provider := &Provider{client: &http.Client{Timeout: time.Second, Transport: transport}, baseURL: "https://api.github.test"}
	if err := provider.ValidateRepo("https://github.com/acme/checkout.git"); err != nil {
		t.Fatal(err)
	}
	kind, err := provider.DetectProjectType("git@github.com:acme/checkout.git")
	if err != nil {
		t.Fatal(err)
	}
	if kind != "go" {
		t.Fatalf("expected go, got %q", kind)
	}
}

func TestProviderRejectsNonGitHubURL(t *testing.T) {
	if err := New().ValidateRepo("https://example.com/acme/checkout"); err == nil {
		t.Fatal("expected validation error")
	}
}
