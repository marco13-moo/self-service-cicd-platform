package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
	bitbucketscm "github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm/bitbucket"
	"go.uber.org/zap"
)

func TestBitbucketWebhookNormalizesPullRequest(t *testing.T) {
	store := NewServiceStore()
	router := NewRouter(store, &fakeEnvironmentOrchestrator{}, orchestrator.NewArgoLinks("https://argo.example.test"), fakeRepositoryProvider{}, map[scm.Provider]scm.WebhookAdapter{
		scm.ProviderBitbucket: bitbucketscm.NewWebhookAdapter("bitbucket-secret"),
	}, zap.NewNop())
	body := []byte(`{"pullrequest":{"id":19,"source":{"commit":{"hash":"def456"}}},"repository":{"name":"Payments API","full_name":"acme/payments-api","uuid":"{repo-uuid}"}}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/bitbucket", nil)
	request.Body = newBytesReadCloser(body)
	request.Header.Set("X-Request-UUID", "{delivery-uuid}")
	request.Header.Set("X-Event-Key", "pullrequest:created")
	request.Header.Set("X-Hub-Signature", webhookSignature(body, "bitbucket-secret"))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", response.Code, response.Body.String())
	}
	commands := store.SCMCommands()
	if len(commands) != 1 {
		t.Fatalf("expected one command, got %d", len(commands))
	}
	if commands[0].Provider != scm.ProviderBitbucket || commands[0].Environment != "payments-api-pr-19" {
		t.Fatalf("unexpected command: %#v", commands[0])
	}
}
