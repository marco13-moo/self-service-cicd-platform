package api

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
)

func TestPersistentServiceStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := NewPersistentServiceStore(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(CreateServiceRequest{Name: "checkout", Owner: "platform", RepoURL: "https://github.com/acme/checkout"}, "go", scm.RepositoryIdentity{Provider: scm.ProviderGitHub, Workspace: "acme", Name: "checkout"})
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
	command := &scm.LifecycleCommand{ID: "github:delivery-1", Provider: scm.ProviderGitHub, DeliveryID: "delivery-1", Type: scm.EnsurePreviewEnvironment, Repository: "acme/checkout", PullRequest: 42}
	if duplicate, err := store.RecordSCMDelivery(scm.ProviderGitHub, "delivery-1", command, time.Now().UTC()); err != nil || duplicate {
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
	if commands := reloaded.SCMCommands(); len(commands) != 1 || commands[0].DeliveryID != "delivery-1" {
		t.Fatalf("GitHub command was not restored: %#v", commands)
	}
	if duplicate, err := reloaded.RecordSCMDelivery(scm.ProviderGitHub, "delivery-1", command, time.Now().UTC()); err != nil || !duplicate {
		t.Fatalf("delivery idempotency was not restored: duplicate=%v err=%v", duplicate, err)
	}
}

func TestObserveDeploymentRejectsStaleGenerationAndPromotesCurrentSHA(t *testing.T) {
	store := NewServiceStore()
	env := &orchestrator.Environment{
		Spec: orchestrator.EnvironmentSpec{
			Name: "pr-42",
			Source: &orchestrator.SourceRevision{
				DesiredSHA: "new-sha", DeployedSHA: "old-sha", DesiredImage: "registry.test/app:new-sha",
				DeployedImage: "registry.test/app:old-sha", DesiredPreviewURL: "https://pr-42.example.test",
				Generation: 2, DeploymentPhase: "Pending",
			},
		},
		DeployWorkflow: &orchestrator.WorkflowReference{Name: "deploy-new", Namespace: "argo"},
	}
	if err := store.PutEnvironment(env); err != nil {
		t.Fatal(err)
	}

	updated, err := store.ObserveDeployment("pr-42", "deploy-old", 1, "Succeeded", "stale", time.Now(), DeploymentEvidence{})
	if err != nil || updated {
		t.Fatalf("stale observation: updated=%v err=%v", updated, err)
	}
	immutableImage := "registry.test/app@sha256:" + strings.Repeat("a", 64)
	updated, err = store.ObserveDeployment("pr-42", "deploy-new", 2, "Succeeded", "complete", time.Now(), DeploymentEvidence{
		ImageDigest: "sha256:" + strings.Repeat("a", 64), DeployedImage: immutableImage,
		SBOMReference: immutableImage, ProvenanceReference: immutableImage, VulnerabilityPolicy: "passed",
	})
	if err != nil || !updated {
		t.Fatalf("current observation: updated=%v err=%v", updated, err)
	}
	got, _ := store.GetEnvironment("pr-42")
	if got.Spec.Source.DeployedSHA != "new-sha" || got.Spec.Source.DeployedImage != immutableImage || got.Spec.Source.ImageDigest == "" || got.Spec.Source.SBOMReference != immutableImage || got.Spec.Source.ProvenanceReference != immutableImage || got.Spec.Source.VulnerabilityPolicy != "passed" || got.Spec.Source.PreviewURL != "https://pr-42.example.test" || got.Spec.Source.DeploymentPhase != "Succeeded" || got.Spec.Source.ObservedAt == nil {
		t.Fatalf("current deployment was not promoted: %#v", got.Spec.Source)
	}

	// Returned values are detached snapshots; mutation cannot bypass persistence.
	got.Spec.Source.DeployedSHA = "corrupt"
	stored, _ := store.GetEnvironment("pr-42")
	if stored.Spec.Source.DeployedSHA != "new-sha" {
		t.Fatalf("external mutation escaped snapshot boundary: %#v", stored.Spec.Source)
	}
}
