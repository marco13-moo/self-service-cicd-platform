package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	wf "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
	"go.uber.org/zap"
)

type fakeEnvironmentOrchestrator struct {
	createdSpec     orchestrator.EnvironmentSpec
	destroyName     string
	destroyService  string
	createStatus    *wf.WorkflowStatus
	createStatusErr error
	ttlStatus       *wf.WorkflowStatus
	ttlStatusErr    error
}

func (f *fakeEnvironmentOrchestrator) Create(
	_ context.Context,
	spec orchestrator.EnvironmentSpec,
) (*orchestrator.Environment, error) {
	f.createdSpec = spec

	return &orchestrator.Environment{
		Spec: spec,
		CreateWorkflow: orchestrator.WorkflowReference{
			Name:        "env-create-abc",
			Namespace:   "argo",
			UID:         "uid-create",
			Template:    "env-create-template",
			SubmittedAt: time.Date(2026, 8, 18, 17, 0, 0, 0, time.UTC),
		},
		TTLWorkflow: &orchestrator.WorkflowReference{
			Name:        "env-ttl-abc",
			Namespace:   "argo",
			UID:         "uid-ttl",
			Template:    "env-ttl-cleanup-template",
			SubmittedAt: time.Date(2026, 8, 18, 17, 0, 1, 0, time.UTC),
		},
	}, nil
}

func (f *fakeEnvironmentOrchestrator) Destroy(
	_ context.Context,
	name string,
	service string,
) (*orchestrator.WorkflowReference, error) {
	f.destroyName = name
	f.destroyService = service

	return &orchestrator.WorkflowReference{
		Name:        "env-destroy-abc",
		Namespace:   "argo",
		UID:         "uid-destroy",
		Template:    "env-destroy-template",
		SubmittedAt: time.Date(2026, 8, 18, 17, 5, 0, 0, time.UTC),
	}, nil
}

func (f *fakeEnvironmentOrchestrator) GetCreateStatus(
	_ context.Context,
	_ *orchestrator.Environment,
) (*wf.WorkflowStatus, error) {
	if f.createStatusErr != nil {
		return nil, f.createStatusErr
	}

	return f.createStatus, nil
}

func (f *fakeEnvironmentOrchestrator) GetTTLStatus(
	_ context.Context,
	_ *orchestrator.Environment,
) (*wf.WorkflowStatus, error) {
	if f.ttlStatusErr != nil {
		return nil, f.ttlStatusErr
	}

	return f.ttlStatus, nil
}

func TestCreateEnvironmentSubmitsIntentAndStoresReference(t *testing.T) {
	fake := &fakeEnvironmentOrchestrator{}
	store := NewServiceStore()
	handlers := NewHandlers(store, fake, zap.NewNop())

	body := bytes.NewBufferString(`{"name":"pr-42","service":"payments","ttl":"2h"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", body)
	rec := httptest.NewRecorder()

	handlers.CreateEnvironment(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if fake.createdSpec.Name != "pr-42" {
		t.Fatalf("created spec name = %q, want pr-42", fake.createdSpec.Name)
	}
	if fake.createdSpec.Service != "payments" {
		t.Fatalf("created spec service = %q, want payments", fake.createdSpec.Service)
	}
	if fake.createdSpec.TTL != 2*time.Hour {
		t.Fatalf("created spec ttl = %s, want 2h", fake.createdSpec.TTL)
	}

	env, err := store.GetEnvironment("pr-42")
	if err != nil {
		t.Fatalf("stored environment not found: %v", err)
	}
	if env.CreateWorkflow.Name != "env-create-abc" {
		t.Fatalf("stored create workflow = %q, want env-create-abc", env.CreateWorkflow.Name)
	}
}

func TestCreateEnvironmentRejectsInvalidTTL(t *testing.T) {
	fake := &fakeEnvironmentOrchestrator{}
	handlers := NewHandlers(NewServiceStore(), fake, zap.NewNop())

	body := bytes.NewBufferString(`{"name":"pr-42","service":"payments","ttl":"soon"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", body)
	rec := httptest.NewRecorder()

	handlers.CreateEnvironment(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if fake.createdSpec.Name != "" {
		t.Fatalf("orchestrator should not be called for invalid ttl")
	}
}

func TestDeleteEnvironmentUsesStoredServiceForDestroyIntent(t *testing.T) {
	fake := &fakeEnvironmentOrchestrator{}
	store := NewServiceStore()
	store.PutEnvironment(&orchestrator.Environment{
		Spec: orchestrator.EnvironmentSpec{
			Name:    "pr-42",
			Service: "payments",
			TTL:     time.Hour,
		},
	})
	handlers := NewHandlers(store, fake, zap.NewNop())

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/environments/pr-42", nil)
	rec := httptest.NewRecorder()

	handlers.DeleteEnvironment(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if fake.destroyName != "pr-42" {
		t.Fatalf("destroy name = %q, want pr-42", fake.destroyName)
	}
	if fake.destroyService != "payments" {
		t.Fatalf("destroy service = %q, want payments", fake.destroyService)
	}

	env, err := store.GetEnvironment("pr-42")
	if err != nil {
		t.Fatalf("stored environment not found: %v", err)
	}
	if env.DestroyWorkflow == nil || env.DestroyWorkflow.Name != "env-destroy-abc" {
		t.Fatalf("destroy workflow was not stored: %#v", env.DestroyWorkflow)
	}
}

func TestGetEnvironmentReturnsLiveWorkflowStatuses(t *testing.T) {
	fake := &fakeEnvironmentOrchestrator{
		createStatus: &wf.WorkflowStatus{Phase: wf.WorkflowSucceeded, Message: "created"},
		ttlStatus:    &wf.WorkflowStatus{Phase: wf.WorkflowRunning, Message: "waiting"},
	}
	store := NewServiceStore()
	store.PutEnvironment(&orchestrator.Environment{
		Spec: orchestrator.EnvironmentSpec{
			Name:    "pr-42",
			Service: "payments",
			TTL:     90 * time.Minute,
		},
		CreateWorkflow: orchestrator.WorkflowReference{
			Name:      "env-create-abc",
			Namespace: "argo",
			Template:  "env-create-template",
		},
		TTLWorkflow: &orchestrator.WorkflowReference{
			Name:      "env-ttl-abc",
			Namespace: "argo",
			Template:  "env-ttl-cleanup-template",
		},
	})
	handlers := NewHandlers(store, fake, zap.NewNop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments/pr-42", nil)
	rec := httptest.NewRecorder()

	handlers.GetEnvironment(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	workflows := payload["workflows"].(map[string]interface{})
	create := workflows["create"].(map[string]interface{})
	createStatus := create["status"].(map[string]interface{})
	if createStatus["phase"] != string(wf.WorkflowSucceeded) {
		t.Fatalf("create phase = %q, want %q", createStatus["phase"], wf.WorkflowSucceeded)
	}

	ttl := workflows["ttl"].(map[string]interface{})
	ttlStatus := ttl["status"].(map[string]interface{})
	if ttlStatus["phase"] != string(wf.WorkflowRunning) {
		t.Fatalf("ttl phase = %q, want %q", ttlStatus["phase"], wf.WorkflowRunning)
	}
}

func TestGetEnvironmentReturnsBadGatewayWhenStatusLookupFails(t *testing.T) {
	fake := &fakeEnvironmentOrchestrator{
		createStatusErr: errors.New("argo unavailable"),
	}
	store := NewServiceStore()
	store.PutEnvironment(&orchestrator.Environment{
		Spec: orchestrator.EnvironmentSpec{
			Name:    "pr-42",
			Service: "payments",
			TTL:     time.Hour,
		},
		CreateWorkflow: orchestrator.WorkflowReference{
			Name:      "env-create-abc",
			Namespace: "argo",
			Template:  "env-create-template",
		},
	})
	handlers := NewHandlers(store, fake, zap.NewNop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments/pr-42", nil)
	rec := httptest.NewRecorder()

	handlers.GetEnvironment(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusBadGateway, rec.Body.String())
	}
}
