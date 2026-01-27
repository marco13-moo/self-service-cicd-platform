package api

import (
	"time"

	"github.com/google/uuid"
)

// Service is the authoritative domain entity managed by the control plane.
// This is NOT a transport object.
type Service struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Owner       string    `json:"owner"`
	RepoURL     string    `json:"repo_url"`
	Environment string    `json:"environment"`
	CreatedAt   time.Time `json:"created_at"`
}

// NewService constructs a new immutable Service from an API contract.
func NewService(req CreateServiceRequest) Service {
	return Service{
		ID:          uuid.New(),
		Name:        req.Name,
		Owner:       req.Owner,
		RepoURL:     req.RepoURL,
		Environment: req.Environment,
		CreatedAt:   time.Now().UTC(),
	}
}
