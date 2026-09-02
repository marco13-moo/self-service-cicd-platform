package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	wf "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
	githubscm "github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm/github"
	"go.uber.org/zap"
)

type fakeEnvironmentOrchestrator struct{ readyErr error }

type fakeRepositoryProvider struct{}

func (fakeRepositoryProvider) ValidateRepo(string) error                { return nil }
func (fakeRepositoryProvider) DetectProjectType(string) (string, error) { return "go", nil }

func (f *fakeEnvironmentOrchestrator) Create(_ context.Context, spec orchestrator.EnvironmentSpec) (*orchestrator.Environment, error) {
	return &orchestrator.Environment{Spec: spec, CreateWorkflow: orchestrator.WorkflowReference{Name: "create-1", Namespace: "argo"}}, nil
}
func (f *fakeEnvironmentOrchestrator) Destroy(_ context.Context, name, _ string) (*orchestrator.WorkflowReference, error) {
	return &orchestrator.WorkflowReference{Name: "destroy-" + name, Namespace: "argo"}, nil
}
func (f *fakeEnvironmentOrchestrator) Deploy(context.Context, *orchestrator.Environment, orchestrator.PreviewDeployment) (*orchestrator.WorkflowReference, error) {
	return &orchestrator.WorkflowReference{Name: "deploy-1", Namespace: "argo"}, nil
}
func (f *fakeEnvironmentOrchestrator) GetCreateStatus(context.Context, *orchestrator.Environment) (*wf.WorkflowStatus, error) {
	return &wf.WorkflowStatus{Phase: wf.WorkflowRunning}, nil
}
func (f *fakeEnvironmentOrchestrator) GetTTLStatus(context.Context, *orchestrator.Environment) (*wf.WorkflowStatus, error) {
	return nil, nil
}
func (f *fakeEnvironmentOrchestrator) GetDeployStatus(context.Context, *orchestrator.Environment) (*wf.WorkflowStatus, error) {
	return nil, nil
}
func (f *fakeEnvironmentOrchestrator) Ready(context.Context) error { return f.readyErr }

func TestEnvironmentLifecycleRoutes(t *testing.T) {
	store := NewServiceStore()
	router := NewRouter(store, store, &fakeEnvironmentOrchestrator{}, orchestrator.NewArgoLinks("https://argo.example.test"), fakeRepositoryProvider{}, map[scm.Provider]scm.WebhookAdapter{scm.ProviderGitHub: githubscm.NewWebhookAdapter("test-secret")}, zap.NewNop())

	create := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(`{"name":"pr-42","service":"checkout","ttl":"1h"}`))
	created := httptest.NewRecorder()
	router.ServeHTTP(created, create)
	if created.Code != http.StatusAccepted {
		t.Fatalf("create returned %d: %s", created.Code, created.Body.String())
	}
	if _, err := store.GetEnvironment("pr-42"); err != nil {
		t.Fatalf("environment was not stored: %v", err)
	}

	for _, endpoint := range []string{"/api/v1/environments/pr-42", "/api/v1/environments/pr-42/logs"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, endpoint, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s returned %d: %s", endpoint, response.Code, response.Body.String())
		}
	}

	deleted := httptest.NewRecorder()
	router.ServeHTTP(deleted, httptest.NewRequest(http.MethodDelete, "/api/v1/environments/pr-42", nil))
	if deleted.Code != http.StatusAccepted {
		t.Fatalf("delete returned %d: %s", deleted.Code, deleted.Body.String())
	}
	env, _ := store.GetEnvironment("pr-42")
	if env.DestroyWorkflow == nil {
		t.Fatal("destroy workflow reference was not persisted")
	}
}

func TestReadinessReflectsExecutionPlane(t *testing.T) {
	orchestratorFake := &fakeEnvironmentOrchestrator{readyErr: errors.New("unavailable")}
	store := NewServiceStore()
	router := NewRouter(store, store, orchestratorFake, orchestrator.NewArgoLinks("https://argo.example.test"), fakeRepositoryProvider{}, map[scm.Provider]scm.WebhookAdapter{scm.ProviderGitHub: githubscm.NewWebhookAdapter("test-secret")}, zap.NewNop())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness returned %d", response.Code)
	}
}

func TestCreateServiceRejectsUnsafeDeploymentContract(t *testing.T) {
	store := NewServiceStore()
	router := NewRouter(store, store, &fakeEnvironmentOrchestrator{}, orchestrator.NewArgoLinks("https://argo.example.test"), fakeRepositoryProvider{}, nil, zap.NewNop())
	tests := []string{
		`{"name":"Invalid_Name","repo_url":"https://example.test/acme/app"}`,
		`{"name":"app","repo_url":"https://example.test/acme/app","deployment":{"container_port":70000}}`,
		`{"name":"app","repo_url":"https://example.test/acme/app","deployment":{"dockerfile":"../Dockerfile"}}`,
	}
	for _, body := range tests {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/services", bytes.NewBufferString(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("unsafe service contract returned %d for %s", response.Code, body)
		}
	}
}
