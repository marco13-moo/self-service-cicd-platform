package orchestrator

import "fmt"

// ArgoLinks encapsulates construction of execution-plane URLs.
// This prevents UI logic from leaking into handlers.
type ArgoLinks struct {
	baseURL string
}

// NewArgoLinks creates a helper for building Argo UI links.
//
// baseURL example:
//
//	https://argo.example.com
func NewArgoLinks(baseURL string) *ArgoLinks {
	return &ArgoLinks{
		baseURL: baseURL,
	}
}

// WorkflowURL returns a direct link to a workflow in the Argo UI.
//
// Result format:
//
//	<baseURL>/workflows/<namespace>/<workflow-name>
func (a *ArgoLinks) WorkflowURL(ref WorkflowReference) string {
	return fmt.Sprintf(
		"%s/workflows/%s/%s",
		a.baseURL,
		ref.Namespace,
		ref.Name,
	)
}
