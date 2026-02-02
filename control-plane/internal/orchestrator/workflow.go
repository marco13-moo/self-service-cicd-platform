package orchestrator

import "time"

// WorkflowReference is a pure identifier for an execution-plane workflow.
// It is immutable and never updated.
type WorkflowReference struct {
	Name        string
	Namespace   string
	Template    string
	SubmittedAt time.Time
}
