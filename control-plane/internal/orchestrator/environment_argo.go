package orchestrator

import (
	"context"
)

type ArgoEnvironmentOrchestrator struct {
	argo *ArgoExecutor
}

func NewArgoEnvironmentOrchestrator(namespace string) *ArgoEnvironmentOrchestrator {
	return &ArgoEnvironmentOrchestrator{
		argo: NewArgoExecutor(namespace),
	}
}

func (e *ArgoEnvironmentOrchestrator) Create(
	ctx context.Context,
	spec EnvironmentSpec,
) error {

	params := map[string]string{
		// MUST match WorkflowTemplate parameter names
		"env_name":   spec.Name,
		"service":    spec.Service,
		"expires_at": spec.TTL.String(),
	}

	return e.argo.submitFromTemplate(
		ctx,
		"env-create-template",
		params,
	)
}

func (e *ArgoEnvironmentOrchestrator) Destroy(
	ctx context.Context,
	name string,
) error {

	return e.argo.submitFromTemplate(
		ctx,
		"env-destroy-template",
		map[string]string{
			// MUST match WorkflowTemplate parameter names
			"env_name": name,
		},
	)
}
