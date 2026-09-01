package api

import (
	"net/http"

	"go.uber.org/zap"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/providers"
)

// NewRouter declares the complete public HTTP surface using method-aware Go
// 1.22 patterns, eliminating ambiguous suffix parsing in individual handlers.
func NewRouter(store *ServiceStore, envOrchestrator orchestrator.EnvironmentOrchestrator, argoLinks *orchestrator.ArgoLinks, repositories providers.RepositoryProvider, logger *zap.Logger) http.Handler {
	handlers := NewHandlers(store, envOrchestrator, argoLinks, repositories, logger)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handlers.Healthz)
	mux.HandleFunc("GET /readyz", handlers.Readyz)
	mux.HandleFunc("POST /api/v1/services", handlers.CreateService)
	mux.HandleFunc("GET /api/v1/services", handlers.ListServices)
	mux.HandleFunc("POST /api/v1/environments", handlers.CreateEnvironment)
	mux.HandleFunc("GET /api/v1/environments/{name}", handlers.GetEnvironment)
	mux.HandleFunc("DELETE /api/v1/environments/{name}", handlers.DeleteEnvironment)
	mux.HandleFunc("GET /api/v1/environments/{name}/logs", handlers.GetEnvironmentLogs)
	return mux
}
