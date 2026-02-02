package api

import (
	"encoding/json"
	"net/http"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
)

// EnvironmentSummaryResponse is a read-only, live view of an environment.
// It is assembled on demand and never persisted.
type EnvironmentSummaryResponse struct {
	Environment EnvironmentSpecResponse      `json:"environment"`
	Workflows   EnvironmentWorkflowsResponse `json:"workflows"`
}

type EnvironmentSpecResponse struct {
	Name       string            `json:"name"`
	Service    string            `json:"service"`
	TTLSeconds int64             `json:"ttl_seconds"`
	Parameters map[string]string `json:"parameters,omitempty"`
}

type EnvironmentWorkflowsResponse struct {
	Create  WorkflowWithStatusResponse  `json:"create"`
	Destroy *WorkflowWithStatusResponse `json:"destroy,omitempty"`
	TTL     *WorkflowWithStatusResponse `json:"ttl,omitempty"`
}

type WorkflowWithStatusResponse struct {
	Reference WorkflowReferenceResponse `json:"reference"`
	Status    *WorkflowStatusResponse   `json:"status,omitempty"`
}

type WorkflowReferenceResponse struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Template  string `json:"template"`
}

type WorkflowStatusResponse struct {
	Phase string `json:"phase"`
}

// GetEnvironment returns a unified, live view of an environment.
//
// It:
// - Does NOT read from storage
// - Does NOT cache
// - Queries execution state on demand
func (h *Handler) GetEnvironment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// ---- resolve environment name ----
	envName := r.PathValue("name")
	if envName == "" {
		http.Error(w, "environment name is required", http.StatusBadRequest)
		return
	}

	// ---- resolve environment from request context ----
	// Assumption: environment was attached by earlier middleware or handler
	env, ok := ctx.Value("environment").(*orchestrator.Environment)
	if !ok || env == nil {
		http.Error(w, "environment not found", http.StatusNotFound)
		return
	}

	// ---- query live workflow statuses ----

	createStatus, _ := h.orchestrator.GetCreateStatus(ctx, env)
	ttlStatus, _ := h.orchestrator.GetTTLStatus(ctx, env)

	// ---- assemble response ----

	resp := EnvironmentSummaryResponse{
		Environment: EnvironmentSpecResponse{
			Name:       env.Spec.Name,
			Service:    env.Spec.Service,
			TTLSeconds: int64(env.Spec.TTL.Seconds()),
			Parameters: env.Spec.Parameters,
		},
		Workflows: EnvironmentWorkflowsResponse{
			Create: WorkflowWithStatusResponse{
				Reference: toWorkflowReferenceResponse(env.CreateWorkflow),
				Status:    toWorkflowStatusResponse(createStatus),
			},
		},
	}

	if env.DestroyWorkflow != nil {
		resp.Workflows.Destroy = &WorkflowWithStatusResponse{
			Reference: toWorkflowReferenceResponse(*env.DestroyWorkflow),
		}
	}

	if env.TTLWorkflow != nil {
		resp.Workflows.TTL = &WorkflowWithStatusResponse{
			Reference: toWorkflowReferenceResponse(*env.TTLWorkflow),
			Status:    toWorkflowStatusResponse(ttlStatus),
		}
	}

	// ---- write response ----

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// ---- mapping helpers ----

func toWorkflowReferenceResponse(
	ref orchestrator.WorkflowReference,
) WorkflowReferenceResponse {
	return WorkflowReferenceResponse{
		Name:      ref.Name,
		Namespace: ref.Namespace,
		Template:  ref.Template,
	}
}

func toWorkflowStatusResponse(
	status *orchestrator.WorkflowStatusView,
) *WorkflowStatusResponse {
	if status == nil {
		return nil
	}

	return &WorkflowStatusResponse{
		Phase: status.Phase,
	}
}
