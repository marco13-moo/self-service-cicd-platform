package api

// CreateServiceRequest is the external API contract used by clients
// registering a service with the control plane.
type CreateServiceRequest struct {
	Name        string                    `json:"name"`
	Owner       string                    `json:"owner"`
	RepoURL     string                    `json:"repo_url"`
	Environment string                    `json:"environment"`
	Deployment  *ServiceDeploymentRequest `json:"deployment,omitempty"`
}

type ServiceDeploymentRequest struct {
	ContainerPort int    `json:"container_port,omitempty"`
	Dockerfile    string `json:"dockerfile,omitempty"`
}
