package api

import "time"

type WorkflowReferenceResponse struct {
	Name        string    `json:"name"`
	Namespace   string    `json:"namespace"`
	Template    string    `json:"template"`
	SubmittedAt time.Time `json:"submitted_at"`
}
