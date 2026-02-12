package executor

import (
	"context"

	wf "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
)

// Executor is the transport boundary between the control plane
// and the execution plane (Argo).
//
// CRITICAL RULE:
//
// The executor submits intent.
// It does NOT interpret workflow state.
// It does NOT implement retries.
// It does NOT orchestrate.
//
// Argo owns execution semantics.
type WorkflowExecutor interface {

	// SubmitFromTemplate creates a Workflow CR from a WorkflowTemplate.
	//
	// generateName should follow Kubernetes conventions:
	//   env-create-
	//   ci-run-
	//   deploy-
	//
	// Returns the FULL workflow object so callers can extract:
	//   - Name
	//   - Namespace
	//   - UID
	SubmitFromTemplate(
		ctx context.Context,
		templateName string,
		generateName string,
		parameters map[string]string,
		labels map[string]string,
	) (*wf.Workflow, error)

	// GetWorkflow retrieves the live workflow object from Argo.
	//
	// IMPORTANT:
	// This is a READ — not state ownership.
	GetWorkflow(
		ctx context.Context,
		name string,
	) (*wf.Workflow, error)

	// Cancel terminates a running workflow.
	//
	// Implemented via:
	//   spec.shutdown = "Terminate"
	//
	// This preserves Argo as the execution authority.
	Cancel(
		ctx context.Context,
		name string,
	) error
}
