package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
)

// Handlers owns all HTTP handlers for the control-plane API.
// Dependencies are injected explicitly.
type Handlers struct {
	store           *ServiceStore
	envOrchestrator orchestrator.EnvironmentOrchestrator
	logger          *zap.Logger
}

func NewHandlers(
	store *ServiceStore,
	envOrchestrator orchestrator.EnvironmentOrchestrator,
	logger *zap.Logger,
) *Handlers {
	return &Handlers{
		store:           store,
		envOrchestrator: envOrchestrator,
		logger:          logger,
	}
}

// --- Platform endpoints ---

func (h *Handlers) Healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

func (h *Handlers) Readyz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "ready",
	})
}

// --- Service registry endpoints ---

func (h *Handlers) CreateService(w http.ResponseWriter, r *http.Request) {
	var req CreateServiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return
	}

	service := NewService(req)
	h.store.Add(service)

	h.logger.Info("service registered",
		zap.String("service_id", service.ID.String()),
		zap.String("name", service.Name),
		zap.String("owner", service.Owner),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(service)
}

func (h *Handlers) ListServices(w http.ResponseWriter, _ *http.Request) {
	services := h.store.List()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(services)
}

// --- Environment endpoints (Phase 5) ---

type CreateEnvironmentRequest struct {
	Name    string `json:"name"`
	Service string `json:"service"`
	TTL     string `json:"ttl"`
}

func (h *Handlers) CreateEnvironment(w http.ResponseWriter, r *http.Request) {
	var req CreateEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return
	}

	ttl, err := time.ParseDuration(req.TTL)
	if err != nil {
		http.Error(w, "invalid ttl", http.StatusBadRequest)
		return
	}

	spec := orchestrator.EnvironmentSpec{
		Name:    req.Name,
		Service: req.Service,
		TTL:     ttl,
	}

	ctx := r.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	if _, err := h.envOrchestrator.Create(ctx, spec); err != nil {
		h.logger.Error("failed to create environment", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (h *Handlers) DeleteEnvironment(w http.ResponseWriter, r *http.Request) {
	// Expected path: /api/v1/environments/{name}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		http.Error(w, "missing environment name", http.StatusBadRequest)
		return
	}

	name := parts[len(parts)-1]

	ctx := r.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	if _, err := h.envOrchestrator.Destroy(ctx, name); err != nil {
		h.logger.Error("failed to delete environment", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}
