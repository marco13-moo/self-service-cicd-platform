package api

import "github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/catalog"

// CreateServiceRequest is the external API contract used by clients
// registering a service with the control plane.
type CreateServiceRequest struct {
	APIVersion  string                    `json:"apiVersion,omitempty"`
	Kind        string                    `json:"kind,omitempty"`
	Metadata    *catalog.Metadata         `json:"metadata,omitempty"`
	Spec        *catalog.Spec             `json:"spec,omitempty"`
	Name        string                    `json:"name"`
	Owner       string                    `json:"owner"`
	RepoURL     string                    `json:"repo_url"`
	Environment string                    `json:"environment"`
	Deployment  *ServiceDeploymentRequest `json:"deployment,omitempty"`
}

// Normalize accepts the versioned declaration shape and the original flat
// request shape during the additive API transition.
func (r *CreateServiceRequest) Normalize() {
	if r.Metadata != nil {
		r.Name = r.Metadata.Name
	}
	if r.Spec != nil {
		r.Owner = r.Spec.Owner
		r.RepoURL = r.Spec.Repository
		if r.Spec.Runtime.Environment != "" {
			r.Environment = r.Spec.Runtime.Environment
		}
		r.Deployment = &ServiceDeploymentRequest{
			ContainerPort: r.Spec.Runtime.ContainerPort,
			Dockerfile:    r.Spec.Runtime.Dockerfile,
		}
	}
}

type ServiceDeploymentRequest struct {
	ContainerPort int                 `json:"container_port,omitempty"`
	Dockerfile    string              `json:"dockerfile,omitempty"`
	Egress        []ServiceEgressRule `json:"egress,omitempty"`
}

type ServiceEgressRule struct {
	DNSName  string `json:"dns_name"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol,omitempty"`
}
