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

// Create submits intent to:
//  1. Create an environment namespace
//  2. Schedule TTL cleanup
//
// The control plane stores ONLY workflow references.
// Argo owns lifecycle.
func (e *ArgoEnvironmentOrchestrator) Create(
	ctx context.Context,
	spec EnvironmentSpec,
) (*Environment, error) {

	expiresAt := time.Now().Add(spec.TTL).Format(time.RFC3339)

	//-----------------------------------------
	// Parameters (template-facing)
	//-----------------------------------------

	createParams := map[string]string{
		"env_name":   spec.Name,
		"service":    spec.Service,
		"expires_at": expiresAt,
	}

	//-----------------------------------------
	// Labels (BUILDER — NO INLINE MAPS)
	//-----------------------------------------

	createLabels := NewLabelBuilder(
		WorkflowTypeEnvCreate,
		spec.Service,
	).
		WithEnvironment(spec.Name).
		WithTrigger(TriggerAPI).
		WithTemplate("env-create-template").
		Build()

	//-----------------------------------------
	// Submit CREATE workflow
	//-----------------------------------------

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

	//-----------------------------------------
	// TTL workflow
	//-----------------------------------------

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

	//-----------------------------------------
	// Assemble control-plane view
	//-----------------------------------------

	env := &Environment{
		Spec: spec,

		CreateWorkflow: toWorkflowReference(createWf),
		TTLWorkflow:    toWorkflowReferencePtr(ttlWf),
	}

	return env, nil
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

//
// ---- Read-only execution observability ----
//

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

//
// ---- Helpers (DO NOT INLINE THESE) ----
//

func toWorkflowReference(w *wf.Workflow) WorkflowReference {
	return WorkflowReference{
		Name:      w.Name,
		Namespace: w.Namespace,
		UID:       string(w.UID),
	}
}

func toWorkflowReferencePtr(w *wf.Workflow) *WorkflowReference {
	ref := toWorkflowReference(w)
	return &ref
}
