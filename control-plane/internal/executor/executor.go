// internal/executor/executor.go
package executor

type WorkflowExecutor interface {
	SubmitWorkflowTemplate(
		templateName string,
		parameters map[string]string,
	) (string, error)
}
