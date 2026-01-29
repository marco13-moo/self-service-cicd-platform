package api

import (
	"net/http"

	"go.uber.org/zap"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
)

// NewRouter wires the HTTP routes for the control-plane API.
func NewRouter(logger *zap.Logger) http.Handler {
	store := NewServiceStore()

	// Phase 5: Argo-backed environment orchestrator (namespace-scoped)
	envOrchestrator := orchestrator.NewArgoEnvironmentOrchestrator("argo")

	handlers := NewHandlers(store, envOrchestrator, logger)

	mux := http.NewServeMux()

	// Platform endpoints
	mux.HandleFunc("/healthz", handlers.Healthz)
	mux.HandleFunc("/readyz", handlers.Readyz)

	// API v1 — services
	mux.HandleFunc("/api/v1/services", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			handlers.CreateService(w, r)
		case http.MethodGet:
			handlers.ListServices(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// API v1 — environments
	mux.HandleFunc("/api/v1/environments", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			handlers.CreateEnvironment(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/v1/environments/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodDelete:
			handlers.DeleteEnvironment(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	return mux
}
