package executor

import (
	"context"

	wf "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
)

// WorkflowExecutor is the transport boundary between the control plane and
// Argo. It submits intent, but does not interpret state, retry, or orchestrate.
type WorkflowExecutor interface {
	// SubmitFromTemplate creates a Workflow CR from a WorkflowTemplate.
	//
	// generateName should follow Kubernetes conventions:
	//   env-create-
	//   ci-run-
	//   deploy-
	//
	// The full workflow object lets callers retain name, namespace, and UID.
	SubmitFromTemplate(
		ctx context.Context,
		templateName string,
		generateName string,
		parameters map[string]string,
		labels map[string]string,
	) (*wf.Workflow, error)

	// GetWorkflow retrieves the live workflow object from Argo.
	GetWorkflow(
		ctx context.Context,
		name string,
	) (*wf.Workflow, error)

	// Cancel terminates a running workflow by setting spec.shutdown.
	Cancel(
		ctx context.Context,
		name string,
	) error
}
