package api

import (
	"fmt"
	"net/http"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
)

// GetEnvironmentLogs returns stable execution-plane navigation metadata. The
// control plane deliberately does not proxy potentially voluminous pod logs.
func (h *Handlers) GetEnvironmentLogs(w http.ResponseWriter, r *http.Request) {
	env, err := h.store.GetEnvironment(r.PathValue("name"))
	if err != nil {
		http.Error(w, "environment not found", http.StatusNotFound)
		return
	}
	workflows := make(map[string]interface{})
	add := func(kind string, ref orchestrator.WorkflowReference) {
		workflows[kind] = map[string]interface{}{
			"reference": ToWorkflowReferenceResponse(ref),
			"argo_ui":   h.argoLinks.WorkflowURL(ref),
			"cli_hint":  fmt.Sprintf("argo logs -n %s %s", ref.Namespace, ref.Name),
		}
	}
	add("create", env.CreateWorkflow)
	if env.TTLWorkflow != nil {
		add("ttl", *env.TTLWorkflow)
	}
	if env.DestroyWorkflow != nil {
		add("destroy", *env.DestroyWorkflow)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"environment": env.Spec.Name, "workflows": workflows})
}
