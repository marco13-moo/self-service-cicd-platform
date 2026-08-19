package api

import (
	"encoding/json"
	"net/http"
	"strings"

	wf "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
)

// GetEnvironment returns a unified, live view of an environment.
//
// Stored environment metadata is combined with execution state queried on
// demand. Workflow status is never cached in the control-plane store.
func (h *Handlers) GetEnvironment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// ---- resolve environment name ----
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		http.Error(w, "environment name is required", http.StatusBadRequest)
		return
	}
	envName := parts[len(parts)-1]

	// ---- resolve environment from store ----
	env, err := h.store.GetEnvironment(envName)
	if err != nil {
		http.Error(w, "environment not found", http.StatusNotFound)
		return
	}

	createStatus, err := h.envOrchestrator.GetCreateStatus(ctx, env)
	if err != nil {
		http.Error(w, "failed to get create workflow status", http.StatusBadGateway)
		return
	}

	ttlStatus, err := h.envOrchestrator.GetTTLStatus(ctx, env)
	if err != nil {
		http.Error(w, "failed to get ttl workflow status", http.StatusBadGateway)
		return
	}

	// ---- assemble response (inline, no extra types) ----

	resp := map[string]interface{}{
		"environment": map[string]interface{}{
			"name":        env.Spec.Name,
			"service":     env.Spec.Service,
			"ttl_seconds": int64(env.Spec.TTL.Seconds()),
			"parameters":  env.Spec.Parameters,
		},
		"workflows": map[string]interface{}{
			"create": map[string]interface{}{
				"reference": ToWorkflowReferenceResponse(env.CreateWorkflow),
				"status":    toWorkflowStatusResponse(createStatus),
			},
		},
	}

	if env.DestroyWorkflow != nil {
		resp["workflows"].(map[string]interface{})["destroy"] = map[string]interface{}{
			"reference": ToWorkflowReferenceResponse(*env.DestroyWorkflow),
		}
	}

	if env.TTLWorkflow != nil {
		resp["workflows"].(map[string]interface{})["ttl"] = map[string]interface{}{
			"reference": ToWorkflowReferenceResponse(*env.TTLWorkflow),
			"status":    toWorkflowStatusResponse(ttlStatus),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func toWorkflowStatusResponse(
	status *wf.WorkflowStatus,
) map[string]interface{} {

	if status == nil {
		return nil
	}

	return map[string]interface{}{
		"phase":      string(status.Phase),
		"message":    status.Message,
		"startedAt":  status.StartedAt,
		"finishedAt": status.FinishedAt,
	}
}
