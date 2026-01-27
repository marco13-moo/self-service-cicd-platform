package api

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"
)

// Handlers owns all HTTP handlers for the control-plane API.
// Dependencies are injected explicitly.
type Handlers struct {
	store  *ServiceStore
	logger *zap.Logger
}

func NewHandlers(store *ServiceStore, logger *zap.Logger) *Handlers {
	return &Handlers{
		store:  store,
		logger: logger,
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
