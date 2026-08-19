package orchestrator

import (
	"context"
	"fmt"
	"time"

	wf "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/executor"
)

type ArgoEnvironmentOrchestrator struct {
	exec executor.WorkflowExecutor
}

func NewArgoEnvironmentOrchestrator(
	exec executor.WorkflowExecutor,
) *ArgoEnvironmentOrchestrator {
	return &ArgoEnvironmentOrchestrator{
		exec: exec,
	}
}

// Create submits intent to create an environment namespace and schedule cleanup.
// The control plane stores workflow references only; Argo owns lifecycle.
func (e *ArgoEnvironmentOrchestrator) Create(
	ctx context.Context,
	spec EnvironmentSpec,
) (*Environment, error) {
	expiresAt := time.Now().Add(spec.TTL).Format(time.RFC3339)

	createParams := map[string]string{
		"env_name":   spec.Name,
		"service":    spec.Service,
		"expires_at": expiresAt,
	}

	createLabels := NewLabelBuilder(
		WorkflowTypeEnvCreate,
		spec.Service,
	).
		WithEnvironment(spec.Name).
		WithTrigger(TriggerAPI).
		WithTemplate("env-create-template").
		Build()

	createWf, err := e.exec.SubmitFromTemplate(
		ctx,
		"env-create-template",
		"env-create-",
		createParams,
		createLabels,
	)
	if err != nil {
		return nil, fmt.Errorf("submit env create workflow: %w", err)
	}

	ttlParams := map[string]string{
		"env_name":   spec.Name,
		"expires_at": expiresAt,
	}

	ttlLabels := NewLabelBuilder(
		WorkflowTypeEnvTTL,
		spec.Service,
	).
		WithEnvironment(spec.Name).
		WithTrigger(TriggerSystem).
		WithTemplate("env-ttl-cleanup-template").
		Build()

	ttlWf, err := e.exec.SubmitFromTemplate(
		ctx,
		"env-ttl-cleanup-template",
		"env-ttl-",
		ttlParams,
		ttlLabels,
	)
	if err != nil {
		return nil, fmt.Errorf("submit ttl workflow: %w", err)
	}

	return &Environment{
		Spec:           spec,
		CreateWorkflow: toWorkflowReference(createWf),
		TTLWorkflow:    toWorkflowReferencePtr(ttlWf),
	}, nil
}

// Destroy submits intent to delete an environment.
func (e *ArgoEnvironmentOrchestrator) Destroy(
	ctx context.Context,
	name string,
	service string,
) (*WorkflowReference, error) {
	params := map[string]string{
		"env_name": name,
	}

	labels := NewLabelBuilder(
		WorkflowTypeEnvDestroy,
		service,
	).
		WithEnvironment(name).
		WithTrigger(TriggerAPI).
		WithTemplate("env-destroy-template").
		Build()

	wfObj, err := e.exec.SubmitFromTemplate(
		ctx,
		"env-destroy-template",
		"env-destroy-",
		params,
		labels,
	)
	if err != nil {
		return nil, fmt.Errorf("submit env destroy workflow: %w", err)
	}

	ref := toWorkflowReference(wfObj)
	return &ref, nil
}

func (e *ArgoEnvironmentOrchestrator) GetCreateStatus(
	ctx context.Context,
	env *Environment,
) (*wf.WorkflowStatus, error) {
	w, err := e.exec.GetWorkflow(
		ctx,
		env.CreateWorkflow.Name,
	)
	if err != nil {
		return nil, err
	}

	return &w.Status, nil
}

func (e *ArgoEnvironmentOrchestrator) GetTTLStatus(
	ctx context.Context,
	env *Environment,
) (*wf.WorkflowStatus, error) {
	if env.TTLWorkflow == nil {
		return nil, nil
	}

	w, err := e.exec.GetWorkflow(
		ctx,
		env.TTLWorkflow.Name,
	)
	if err != nil {
		return nil, err
	}

	return &w.Status, nil
}

func toWorkflowReference(w *wf.Workflow) WorkflowReference {
	return WorkflowReference{
		Name:        w.Name,
		Namespace:   w.Namespace,
		UID:         string(w.UID),
		Template:    w.Labels[LabelWorkflowTemplate],
		SubmittedAt: w.CreationTimestamp.Time,
	}
}

func toWorkflowReferencePtr(w *wf.Workflow) *WorkflowReference {
	ref := toWorkflowReference(w)
	return &ref
}
