package api

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
)

func TestPersistentServiceStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := NewPersistentServiceStore(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(CreateServiceRequest{Name: "checkout", Owner: "platform", RepoURL: "https://github.com/acme/checkout"}, "go")
	if err := store.Put(service); err != nil {
		t.Fatal(err)
	}
	env := &orchestrator.Environment{
		Spec:           orchestrator.EnvironmentSpec{Name: "pr-42", Service: "checkout", TTL: time.Hour},
		CreateWorkflow: orchestrator.WorkflowReference{Name: "env-create-abc", Namespace: "argo"},
	}
	if err := store.PutEnvironment(env); err != nil {
		t.Fatal(err)
	}
	command := &GitHubWebhookCommand{DeliveryID: "delivery-1", Type: "upsert_preview_environment", Repository: "acme/checkout", PullRequest: 42}
	if duplicate, err := store.RecordGitHubDelivery("delivery-1", command, time.Now().UTC()); err != nil || duplicate {
		t.Fatalf("record delivery: duplicate=%v err=%v", duplicate, err)
	}

	reloaded, err := NewPersistentServiceStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reloaded.Get("checkout"); err != nil {
		t.Fatalf("service was not restored: %v", err)
	}
	got, err := reloaded.GetEnvironment("pr-42")
	if err != nil {
		t.Fatalf("environment was not restored: %v", err)
	}
	if got.CreateWorkflow.Name != "env-create-abc" {
		t.Fatalf("unexpected workflow reference: %q", got.CreateWorkflow.Name)
	}
	if commands := reloaded.GitHubCommands(); len(commands) != 1 || commands[0].DeliveryID != "delivery-1" {
		t.Fatalf("GitHub command was not restored: %#v", commands)
	}
	if duplicate, err := reloaded.RecordGitHubDelivery("delivery-1", command, time.Now().UTC()); err != nil || !duplicate {
		t.Fatalf("delivery idempotency was not restored: duplicate=%v err=%v", duplicate, err)
	}
}
