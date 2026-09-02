package api

import (
	"time"

	"github.com/google/uuid"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
)

// Service is the authoritative domain entity managed by the control plane.
// This is NOT a transport object.
type Service struct {
	ID          uuid.UUID              `json:"id"`
	Name        string                 `json:"name"`
	Owner       string                 `json:"owner"`
	RepoURL     string                 `json:"repo_url"`
	Repository  scm.RepositoryIdentity `json:"repository"`
	ProjectType string                 `json:"project_type"`
	Environment string                 `json:"environment"`
	Deployment  ServiceDeployment      `json:"deployment"`
	CreatedAt   time.Time              `json:"created_at"`
}

type ServiceDeployment struct {
	ContainerPort int    `json:"container_port"`
	Dockerfile    string `json:"dockerfile"`
}

// NewService constructs a new immutable Service from an API contract.
func NewService(req CreateServiceRequest, projectType string, repository scm.RepositoryIdentity) Service {
	deployment := ServiceDeployment{ContainerPort: 8080, Dockerfile: "Dockerfile"}
	if req.Deployment != nil {
		if req.Deployment.ContainerPort != 0 {
			deployment.ContainerPort = req.Deployment.ContainerPort
		}
		if req.Deployment.Dockerfile != "" {
			deployment.Dockerfile = req.Deployment.Dockerfile
		}
	}
	return Service{
		ID:          uuid.New(),
		Name:        req.Name,
		Owner:       req.Owner,
		RepoURL:     req.RepoURL,
		Repository:  repository,
		ProjectType: projectType,
		Environment: req.Environment,
		Deployment:  deployment,
		CreatedAt:   time.Now().UTC(),
	}
}
