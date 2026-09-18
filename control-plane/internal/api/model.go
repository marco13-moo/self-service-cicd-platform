package api

import (
	"time"

	"github.com/google/uuid"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/catalog"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
)

// Service is the authoritative domain entity managed by the control plane.
// This is NOT a transport object.
type Service struct {
	TenantID    TenantID               `json:"tenant_id"`
	ID          uuid.UUID              `json:"id"`
	Name        string                 `json:"name"`
	Owner       string                 `json:"owner"`
	RepoURL     string                 `json:"repo_url"`
	Repository  scm.RepositoryIdentity `json:"repository"`
	ProjectType string                 `json:"project_type"`
	Environment string                 `json:"environment"`
	Deployment  ServiceDeployment      `json:"deployment"`
	CreatedAt   time.Time              `json:"created_at"`
	Version     int64                  `json:"version"`
	Status      ServiceStatus          `json:"status"`
	Declaration *catalog.Declaration   `json:"declaration,omitempty"`
}

type ServiceStatus struct {
	DesiredGeneration  int64  `json:"desired_generation"`
	ObservedGeneration int64  `json:"observed_generation"`
	DesiredState       string `json:"desired_state"`
	ObservedState      string `json:"observed_state"`
	Message            string `json:"message,omitempty"`
}

type ServiceDeployment struct {
	ContainerPort int                 `json:"container_port"`
	Dockerfile    string              `json:"dockerfile"`
	Egress        []ServiceEgressRule `json:"egress,omitempty"`
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
		deployment.Egress = append([]ServiceEgressRule(nil), req.Deployment.Egress...)
	}
	service := Service{
		TenantID:    DefaultTenantID,
		ID:          uuid.New(),
		Name:        req.Name,
		Owner:       req.Owner,
		RepoURL:     req.RepoURL,
		Repository:  repository,
		ProjectType: projectType,
		Environment: req.Environment,
		Deployment:  deployment,
		CreatedAt:   time.Now().UTC(),
		Version:     1,
		Status: ServiceStatus{
			DesiredGeneration: 1,
			DesiredState:      "active",
			ObservedState:     "pending",
			Message:           "desired service declaration accepted; reconciliation pending",
		},
	}
	if req.Spec != nil {
		declaration := catalog.Declaration{
			APIVersion: req.APIVersion,
			Kind:       req.Kind,
			Metadata:   catalog.Metadata{Name: req.Name},
			Spec:       *req.Spec,
		}
		service.Declaration = &declaration
	}
	return service
}
