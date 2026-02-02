func ToWorkflowReferenceResponse(ref orchestrator.WorkflowReference) WorkflowReferenceResponse {
	return WorkflowReferenceResponse{
		Name:        ref.Name,
		Namespace:   ref.Namespace,
		Template:    ref.Template,
		SubmittedAt: ref.SubmittedAt,
	}
}