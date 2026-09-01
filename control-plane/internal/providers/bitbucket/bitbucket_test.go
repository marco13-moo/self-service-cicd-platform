package bitbucket

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestProviderValidatesAndDetectsProject(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := "{}"
		if strings.Contains(request.URL.Path, "/src/HEAD/") {
			body = `{"values":[{"path":"package.json","type":"commit_file"}]}`
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: request}, nil
	})
	provider := &Provider{client: &http.Client{Timeout: time.Second, Transport: transport}, baseURL: "https://api.bitbucket.test/2.0"}
	if err := provider.ValidateRepo("https://bitbucket.org/acme/checkout.git"); err != nil {
		t.Fatal(err)
	}
	kind, err := provider.DetectProjectType("git@bitbucket.org:acme/checkout.git")
	if err != nil {
		t.Fatal(err)
	}
	if kind != "node" {
		t.Fatalf("expected node, got %q", kind)
	}
}
