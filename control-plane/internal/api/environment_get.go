package api

import (
	"encoding/json"
	"net/http"

	wf "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
)

// GetEnvironment returns a unified, live view of an environment.
//
// It combines durable control-plane intent and observations with current Argo
// workflow state. Execution state is queried on demand and never cached here.
func (h *Handlers) GetEnvironment(w http.ResponseWriter, r *http.Request) {
	envName := r.PathValue("name")
	if envName == "" {
		http.Error(w, "environment name is required", http.StatusBadRequest)
		return
	}

	env, err := h.store.GetEnvironment(envName)
	if err != nil {
		http.Error(w, "environment not found", http.StatusNotFound)
		return
	}

	// ---- query live workflow statuses ----

	createStatus, err := h.envOrchestrator.GetCreateStatus(r.Context(), env)
	if err != nil {
		http.Error(w, "failed to query create workflow", http.StatusBadGateway)
		return
	}
	ttlStatus, err := h.envOrchestrator.GetTTLStatus(r.Context(), env)
	if err != nil {
		http.Error(w, "failed to query ttl workflow", http.StatusBadGateway)
		return
	}

	// ---- assemble response (inline, no extra types) ----

	resp := map[string]interface{}{
		"environment": map[string]interface{}{
			"name":        env.Spec.Name,
			"service":     env.Spec.Service,
			"ttl_seconds": int64(env.Spec.TTL.Seconds()),
			"expires_at":  env.Spec.ExpiresAt,
			"parameters":  env.Spec.Parameters,
			"source":      env.Spec.Source,
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

	if env.DeployWorkflow != nil {
		deployStatus, err := h.envOrchestrator.GetDeployStatus(r.Context(), env)
		if err != nil {
			http.Error(w, "failed to query deploy workflow", http.StatusBadGateway)
			return
		}
		resp["workflows"].(map[string]interface{})["deploy"] = map[string]interface{}{
			"reference": ToWorkflowReferenceResponse(*env.DeployWorkflow),
			"status":    toWorkflowStatusResponse(deployStatus),
		}
	}

	// ---- write response ----

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
