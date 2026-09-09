package github

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
)

type statusAuth struct{}

func (statusAuth) Provider() scm.Provider { return scm.ProviderGitHub }
func (statusAuth) Token(context.Context, string) (scm.InstallationToken, error) {
	return scm.InstallationToken{Value: "ephemeral", ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func TestStatusReporterPublishesCanonicalRevision(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/repos/acme/orders/statuses/abcdef1" || r.Header.Get("Authorization") != "Bearer ephemeral" {
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusCreated, Status: "201 Created", Body: http.NoBody, Header: make(http.Header), Request: r}, nil
	})}
	reporter := NewStatusReporter(statusAuth{}, client)
	reporter.baseURL = "https://github.invalid"
	if err := reporter.Report(context.Background(), scm.RevisionStatus{Repository: "acme/orders", SHA: "abcdef1", State: scm.RevisionSuccess}); err != nil {
		t.Fatal(err)
	}
}
