package api

import (
	"net/http"

	"go.uber.org/zap"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/providers"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
)

// NewRouter declares the complete public HTTP surface using method-aware Go
// 1.22 patterns, eliminating ambiguous suffix parsing in individual handlers.
func NewRouter(store *ServiceStore, commandStore SCMCommandStore, envOrchestrator orchestrator.EnvironmentOrchestrator, argoLinks *orchestrator.ArgoLinks, repositories providers.RepositoryProvider, webhookAdapters map[scm.Provider]scm.WebhookAdapter, logger *zap.Logger) http.Handler {
	handlers := NewHandlers(store, commandStore, envOrchestrator, argoLinks, repositories, webhookAdapters, logger)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handlers.Healthz)
	mux.HandleFunc("GET /readyz", handlers.Readyz)
	mux.HandleFunc("POST /api/v1/services", handlers.CreateService)
	mux.HandleFunc("GET /api/v1/services", handlers.ListServices)
	mux.HandleFunc("POST /api/v1/environments", handlers.CreateEnvironment)
	mux.HandleFunc("GET /api/v1/environments/{name}", handlers.GetEnvironment)
	mux.HandleFunc("DELETE /api/v1/environments/{name}", handlers.DeleteEnvironment)
	mux.HandleFunc("GET /api/v1/environments/{name}/logs", handlers.GetEnvironmentLogs)
	mux.HandleFunc("POST /api/v1/webhooks/{provider}", handlers.SCMWebhook)
	mux.HandleFunc("GET /metrics", handlers.Metrics)
	mux.HandleFunc("GET /api/v1/admin/scm/commands", handlers.ListSCMCommands)
	return mux
}
