package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
	"go.uber.org/zap"
)

func TestGitHubWebhookRejectsInvalidSignature(t *testing.T) {
	router, _ := newWebhookTestRouter()
	request := webhookRequest([]byte(`{"zen":"secure boundaries"}`), "delivery-invalid", "ping", "sha256=deadbeef")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.Code)
	}
}

func TestGitHubWebhookRejectsMissingSignature(t *testing.T) {
	router, _ := newWebhookTestRouter()
	response := httptest.NewRecorder()
	router.ServeHTTP(response, webhookRequest([]byte(`{}`), "delivery-missing", "ping", ""))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.Code)
	}
}

func TestGitHubWebhookRequiresConfiguredSecret(t *testing.T) {
	store := NewServiceStore()
	router := NewRouter(store, &fakeEnvironmentOrchestrator{}, orchestrator.NewArgoLinks("https://argo.example.test"), fakeRepositoryProvider{}, "", zap.NewNop())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, webhookRequest([]byte(`{}`), "delivery-unconfigured", "ping", ""))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", response.Code)
	}
}

func TestGitHubWebhookRejectsOversizedPayload(t *testing.T) {
	router, _ := newWebhookTestRouter()
	body := bytes.Repeat([]byte("x"), maxGitHubWebhookBody+1)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, webhookRequest(body, "delivery-large", "ping", webhookSignature(body, "test-secret")))
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", response.Code)
	}
}

func TestGitHubWebhookCreatesOneDeterministicCommand(t *testing.T) {
	router, store := newWebhookTestRouter()
	body := []byte(`{"action":"opened","number":42,"installation":{"id":987},"repository":{"name":"Checkout_API","full_name":"acme/checkout-api"},"pull_request":{"head":{"sha":"abc123"}}}`)
	signature := webhookSignature(body, "test-secret")

	for iteration := 0; iteration < 2; iteration++ {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, webhookRequest(body, "delivery-42", "pull_request", signature))
		if response.Code != http.StatusAccepted {
			t.Fatalf("iteration %d returned %d: %s", iteration, response.Code, response.Body.String())
		}
	}
	commands := store.GitHubCommands()
	if len(commands) != 1 {
		t.Fatalf("expected one command, got %d", len(commands))
	}
	command := commands[0]
	if command.Type != "upsert_preview_environment" {
		t.Fatalf("unexpected command type %q", command.Type)
	}
	if command.Environment != "checkout-api-pr-42" {
		t.Fatalf("unexpected environment %q", command.Environment)
	}
	if command.HeadSHA != "abc123" || command.InstallationID != 987 {
		t.Fatalf("command identity was not preserved: %#v", command)
	}
}

func TestGitHubWebhookAcceptsUnsupportedEventsWithoutCommand(t *testing.T) {
	router, store := newWebhookTestRouter()
	body := []byte(`{"ref":"refs/heads/main"}`)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, webhookRequest(body, "delivery-push", "push", webhookSignature(body, "test-secret")))
	if response.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", response.Code)
	}
	if commands := store.GitHubCommands(); len(commands) != 0 {
		t.Fatalf("expected no commands, got %d", len(commands))
	}
}

func TestUnsupportedPullRequestActionCreatesNoCommand(t *testing.T) {
	router, store := newWebhookTestRouter()
	body := []byte(`{"action":"labeled"}`)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, webhookRequest(body, "delivery-label", "pull_request", webhookSignature(body, "test-secret")))
	if response.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", response.Code, response.Body.String())
	}
	if len(store.GitHubCommands()) != 0 {
		t.Fatal("unsupported action created a command")
	}
}

func TestClosedPullRequestCreatesDestroyCommandWithoutHeadSHA(t *testing.T) {
	router, store := newWebhookTestRouter()
	body := []byte(`{"action":"closed","number":7,"installation":{"id":987},"repository":{"name":"checkout","full_name":"acme/checkout"},"pull_request":{"head":{"sha":""}}}`)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, webhookRequest(body, "delivery-close", "pull_request", webhookSignature(body, "test-secret")))
	if response.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", response.Code, response.Body.String())
	}
	commands := store.GitHubCommands()
	if len(commands) != 1 || commands[0].Type != "destroy_preview_environment" {
		t.Fatalf("unexpected commands: %#v", commands)
	}
}

func newWebhookTestRouter() (http.Handler, *ServiceStore) {
	store := NewServiceStore()
	router := NewRouter(store, &fakeEnvironmentOrchestrator{}, orchestrator.NewArgoLinks("https://argo.example.test"), fakeRepositoryProvider{}, "test-secret", zap.NewNop())
	return router, store
}

func webhookRequest(body []byte, deliveryID, event, signature string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(body))
	request.Header.Set("X-GitHub-Delivery", deliveryID)
	request.Header.Set("X-GitHub-Event", event)
	request.Header.Set("X-Hub-Signature-256", signature)
	return request
}

func webhookSignature(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
