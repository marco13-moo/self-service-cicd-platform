package orchestrator

import (
	"context"
	"fmt"
	"time"
)

type ArgoEnvironmentOrchestrator struct {
	argo *ArgoExecutor
}

func NewArgoEnvironmentOrchestrator(namespace string) *ArgoEnvironmentOrchestrator {
	return &ArgoEnvironmentOrchestrator{
		argo: NewArgoExecutor(namespace),
	}
}

// Create submits intent to:
// 1. Create an environment (namespace)
// 2. Schedule TTL cleanup via a separate workflow
//
// It returns an Environment containing immutable workflow references.
// No execution state is tracked or stored.
func (e *ArgoEnvironmentOrchestrator) Create(
	ctx context.Context,
	spec EnvironmentSpec,
) (*Environment, error) {

	// ---- submit environment create workflow ----

	createParams := map[string]string{
		// MUST match WorkflowTemplate parameter names
		"env_name":   spec.Name,
		"service":    spec.Service,
		"expires_at": time.Now().Add(spec.TTL).Format(time.RFC3339),
	}

	createRef, err := e.argo.submitFromTemplate(
		ctx,
		"env-create-template",
		createParams,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to submit env create workflow: %w", err)
	}

	// ---- submit TTL cleanup workflow ----

	ttlParams := map[string]string{
		// MUST match WorkflowTemplate parameter names
		"env_name":   spec.Name,
		"expires_at": time.Now().Add(spec.TTL).Format(time.RFC3339),
	}

	ttlRef, err := e.argo.submitFromTemplate(
		ctx,
		"env-ttl-cleanup-template",
		ttlParams,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to submit env TTL workflow: %w", err)
	}

	// ---- assemble control-plane view ----

	env := &Environment{
		Spec: spec,

		CreateWorkflow: *createRef,
		TTLWorkflow:    ttlRef,
	}

	return env, nil
}

// Destroy submits intent to destroy an environment.
// It returns a WorkflowReference for observability.
func (e *ArgoEnvironmentOrchestrator) Destroy(
	ctx context.Context,
	name string,
) (*WorkflowReference, error) {

	destroyRef, err := e.argo.submitFromTemplate(
		ctx,
		"env-destroy-template",
		map[string]string{
			// MUST match WorkflowTemplate parameter names
			"env_name": name,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to submit env destroy workflow: %w", err)
	}

	return destroyRef, nil
}

// ---- Phase 6 read-only observability ----

// GetCreateStatus queries the execution plane for the current
// status of the environment creation workflow.
func (e *ArgoEnvironmentOrchestrator) GetCreateStatus(
	ctx context.Context,
	env *Environment,
) (*WorkflowStatusView, error) {

	return e.argo.GetWorkflowStatus(env.CreateWorkflow)
}

// GetTTLStatus queries the execution plane for the current
// status of the TTL cleanup workflow (if present).
func (e *ArgoEnvironmentOrchestrator) GetTTLStatus(
	ctx context.Context,
	env *Environment,
) (*WorkflowStatusView, error) {

	if env.TTLWorkflow == nil {
		return nil, nil
	}

	return e.argo.GetWorkflowStatus(*env.TTLWorkflow)
}
