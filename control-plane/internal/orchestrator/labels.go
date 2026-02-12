package orchestrator

func BaseWorkflowLabels(
	workflowType string,
	service string,
) map[string]string {

	return map[string]string{
		"platform.workflow.type": workflowType,
		"platform.service":       service,
	}
}
