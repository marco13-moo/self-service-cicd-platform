package reconciler

import (
	"context"
	"testing"
	"time"

	wf "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/api"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
	"go.uber.org/zap"
)

type fakeOrchestrator struct{ creates, destroys int }

func (f *fakeOrchestrator) Create(_ context.Context, spec orchestrator.EnvironmentSpec) (*orchestrator.Environment, error) {
	f.creates++
	return &orchestrator.Environment{Spec: spec, CreateWorkflow: orchestrator.WorkflowReference{Name: "create", Namespace: "argo"}}, nil
}
func (f *fakeOrchestrator) Destroy(_ context.Context, name, _ string) (*orchestrator.WorkflowReference, error) {
	f.destroys++
	return &orchestrator.WorkflowReference{Name: "destroy-" + name, Namespace: "argo"}, nil
}
func (*fakeOrchestrator) GetCreateStatus(context.Context, *orchestrator.Environment) (*wf.WorkflowStatus, error) {
	return nil, nil
}
func (*fakeOrchestrator) GetTTLStatus(context.Context, *orchestrator.Environment) (*wf.WorkflowStatus, error) {
	return nil, nil
}
func (*fakeOrchestrator) Ready(context.Context) error { return nil }

func TestReconcilerCreatesAndDestroysPreviewIdempotently(t *testing.T) {
	store := api.NewServiceStore()
	if err := store.Put(api.Service{Name: "checkout", RepoURL: "https://bitbucket.org/acme/checkout"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	ensure := scm.LifecycleCommand{ID: "bitbucket:open", Provider: scm.ProviderBitbucket, DeliveryID: "open", Type: scm.EnsurePreviewEnvironment, Repository: "acme/checkout", PullRequest: 3, HeadSHA: "abc", Environment: "checkout-pr-3", Status: scm.CommandPending, AvailableAt: now, CreatedAt: now}
	if _, err := store.RecordSCMDelivery(scm.ProviderBitbucket, "open", &ensure, now); err != nil {
		t.Fatal(err)
	}
	fake := &fakeOrchestrator{}
	reconciler := NewSCMCommandReconciler(store, fake, time.Hour, zap.NewNop())
	if processed, err := reconciler.ProcessOne(context.Background(), now); err != nil || !processed {
		t.Fatalf("ensure: processed=%v err=%v", processed, err)
	}
	if fake.creates != 1 {
		t.Fatalf("expected one create, got %d", fake.creates)
	}

	closeCommand := scm.LifecycleCommand{ID: "bitbucket:close", Provider: scm.ProviderBitbucket, DeliveryID: "close", Type: scm.DestroyPreviewEnvironment, Repository: "acme/checkout", PullRequest: 3, Environment: "checkout-pr-3", Status: scm.CommandPending, AvailableAt: now, CreatedAt: now}
	if _, err := store.RecordSCMDelivery(scm.ProviderBitbucket, "close", &closeCommand, now); err != nil {
		t.Fatal(err)
	}
	if processed, err := reconciler.ProcessOne(context.Background(), now); err != nil || !processed {
		t.Fatalf("destroy: processed=%v err=%v", processed, err)
	}
	if fake.destroys != 1 {
		t.Fatalf("expected one destroy, got %d", fake.destroys)
	}
	commands := store.SCMCommands()
	for _, command := range commands {
		if command.Status != scm.CommandSucceeded {
			t.Fatalf("command not completed: %#v", command)
		}
	}
}
