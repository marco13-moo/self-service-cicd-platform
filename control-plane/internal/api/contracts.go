package api

// CreateServiceRequest is the external API contract used by clients
// registering a service with the control plane.
type CreateServiceRequest struct {
	Name        string `json:"name"`
	Owner       string `json:"owner"`
	RepoURL     string `json:"repo_url"`
	Environment string `json:"environment"`
}
