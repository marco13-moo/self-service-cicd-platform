package orchestrator

import "time"

// WorkflowStatusView is derived, read-only data.
type WorkflowStatusView struct {
	Phase      string
	StartedAt  *time.Time
	FinishedAt *time.Time
}
