package api

import "time"

type WorkflowReferenceResponse struct {
	Name        string    `json:"name"`
	Namespace   string    `json:"namespace"`
	UID         string    `json:"uid"`
	Template    string    `json:"template"`
	SubmittedAt time.Time `json:"submitted_at"`
}
