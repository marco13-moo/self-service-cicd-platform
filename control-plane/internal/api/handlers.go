package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/providers"
)

// Handlers owns all HTTP handlers for the control-plane API.
// Dependencies are injected explicitly.
type Handlers struct {
	store               *ServiceStore
	envOrchestrator     orchestrator.EnvironmentOrchestrator
	argoLinks           *orchestrator.ArgoLinks
	repositories        providers.RepositoryProvider
	githubWebhookSecret string
	logger              *zap.Logger
}

func NewHandlers(
	store *ServiceStore,
	envOrchestrator orchestrator.EnvironmentOrchestrator,
	argoLinks *orchestrator.ArgoLinks,
	repositories providers.RepositoryProvider,
	githubWebhookSecret string,
	logger *zap.Logger,
) *Handlers {
	return &Handlers{
		store:               store,
		envOrchestrator:     envOrchestrator,
		argoLinks:           argoLinks,
		repositories:        repositories,
		githubWebhookSecret: githubWebhookSecret,
		logger:              logger,
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

func (h *Handlers) Readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := h.envOrchestrator.Ready(ctx); err != nil {
		h.logger.Warn("execution plane readiness probe failed", zap.Error(err))
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "reason": "execution plane unavailable"})
		return
	}
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

	if err := h.repositories.ValidateRepo(req.RepoURL); err != nil {
		http.Error(w, "repository validation failed", http.StatusUnprocessableEntity)
		return
	}
	projectType, err := h.repositories.DetectProjectType(req.RepoURL)
	if err != nil {
		http.Error(w, "project type detection failed", http.StatusUnprocessableEntity)
		return
	}
	service := NewService(req, projectType)
	if err := h.store.Put(service); err != nil {
		h.logger.Error("failed to persist service", zap.Error(err))
		http.Error(w, "failed to persist service", http.StatusInternalServerError)
		return
	}

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
	h.logger.Info("CreateEnvironment called")

	var req CreateEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("failed to decode request", zap.Error(err))
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return
	}

	h.logger.Info("parsed request",
		zap.String("name", req.Name),
		zap.String("service", req.Service),
		zap.String("ttl", req.TTL),
	)

	ttl, err := time.ParseDuration(req.TTL)
	if err != nil {
		h.logger.Error("invalid ttl", zap.Error(err))
		http.Error(w, "invalid ttl", http.StatusBadRequest)
		return
	}

	h.logger.Info("submitting environment to orchestrator")

	env, err := h.envOrchestrator.Create(r.Context(), orchestrator.EnvironmentSpec{
		Name:    req.Name,
		Service: req.Service,
		TTL:     ttl,
	})
	if err != nil {
		h.logger.Error("failed to create environment", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := h.store.PutEnvironment(env); err != nil {
		h.logger.Error("environment submitted but reference persistence failed", zap.Error(err))
		http.Error(w, "environment submitted but state persistence failed", http.StatusInternalServerError)
		return
	}

	h.logger.Info("environment creation accepted")
	writeJSON(w, http.StatusAccepted, env)
}

func (h *Handlers) DeleteEnvironment(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	ctx := r.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	env, err := h.store.GetEnvironment(name)
	if err != nil {
		h.logger.Error("environment not found", zap.Error(err))
		http.Error(w, "environment not found", http.StatusNotFound)
		return
	}

	destroyRef, err := h.envOrchestrator.Destroy(
		ctx,
		name,
		env.Spec.Service,
	)
	if err != nil {
		h.logger.Error("failed to delete environment", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	env.DestroyWorkflow = destroyRef
	if err := h.store.PutEnvironment(env); err != nil {
		h.logger.Error("destroy submitted but reference persistence failed", zap.Error(err))
		http.Error(w, "destroy submitted but state persistence failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]interface{}{"environment": name, "destroy_workflow": destroyRef})
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
