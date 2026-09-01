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
}
