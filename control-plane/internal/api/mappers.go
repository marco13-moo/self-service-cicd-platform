package api

import "github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"

func ToWorkflowReferenceResponse(
	ref orchestrator.WorkflowReference,
) WorkflowReferenceResponse {
	return WorkflowReferenceResponse{
		Name:        ref.Name,
		Namespace:   ref.Namespace,
		Template:    ref.Template,
		SubmittedAt: ref.SubmittedAt,
	}
}
