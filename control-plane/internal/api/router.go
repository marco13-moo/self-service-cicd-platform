package api

import (
	"context"
	"net/http"

	"go.uber.org/zap"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/providers"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
)

// NewRouter declares the complete public HTTP surface using method-aware Go
// 1.22 patterns, eliminating ambiguous suffix parsing in individual handlers.
func NewRouter(store *ServiceStore, commandStore SCMCommandStore, envOrchestrator orchestrator.EnvironmentOrchestrator, argoLinks *orchestrator.ArgoLinks, repositories providers.RepositoryProvider, webhookAdapters map[scm.Provider]scm.WebhookAdapter, logger *zap.Logger, configuredAuthorizer ...RequestAuthorizer) http.Handler {
	handlers := NewHandlers(store, commandStore, envOrchestrator, argoLinks, repositories, webhookAdapters, logger)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handlers.Healthz)
	mux.HandleFunc("GET /readyz", handlers.Readyz)
	mux.HandleFunc("POST /api/v1/webhooks/{provider}", handlers.SCMWebhook)
	mux.HandleFunc("GET /metrics", handlers.Metrics)
	mux.HandleFunc("GET /api/v1/admin/scm/commands", handlers.ListSCMCommands)
	// Tests and local embedders retain a default-tenant boundary when no
	// authorizer is supplied. The production composition root always supplies
	// an explicitly configured authorizer.
	var authorizer RequestAuthorizer = NewEmptyTenantAuthorizer()
	if len(configuredAuthorizer) != 0 && configuredAuthorizer[0] != nil {
		authorizer = configuredAuthorizer[0]
	} else {
		return developmentTenantRouter(mux, handlers)
	}
	mux.Handle("POST /api/v1/services", authorizer.Require(TenantDeveloper, http.HandlerFunc(handlers.CreateService)))
	mux.Handle("GET /api/v1/services", authorizer.Require(TenantViewer, http.HandlerFunc(handlers.ListServices)))
	mux.Handle("POST /api/v1/environments", authorizer.Require(TenantDeveloper, http.HandlerFunc(handlers.CreateEnvironment)))
	mux.Handle("GET /api/v1/environments/{name}", authorizer.Require(TenantViewer, http.HandlerFunc(handlers.GetEnvironment)))
	mux.Handle("DELETE /api/v1/environments/{name}", authorizer.Require(TenantDeveloper, http.HandlerFunc(handlers.DeleteEnvironment)))
	mux.Handle("GET /api/v1/environments/{name}/logs", authorizer.Require(TenantViewer, http.HandlerFunc(handlers.GetEnvironmentLogs)))
	mux.Handle("GET /api/v1/tenant/auth", authorizer.Require(TenantAdmin, http.HandlerFunc(handlers.GetTenantAuth)))
	mux.Handle("PUT /api/v1/tenant/auth", authorizer.Require(TenantAdmin, http.HandlerFunc(handlers.UpdateTenantAuth)))
	mux.Handle("GET /api/v1/tenant/audit", authorizer.Require(TenantAdmin, http.HandlerFunc(handlers.ListTenantAuditEvents)))
	mux.Handle("POST /api/v1/admin/tenants", authorizer.Require(TenantAdmin, http.HandlerFunc(handlers.ProvisionTenant)))
	mux.Handle("PATCH /api/v1/admin/tenants/{tenant}/status", authorizer.Require(TenantAdmin, http.HandlerFunc(handlers.ChangeTenantStatus)))
	mux.Handle("POST /api/v1/admin/repository-transfers", authorizer.Require(TenantAdmin, http.HandlerFunc(handlers.TransferRepository)))
	return mux
}

func developmentTenantRouter(mux *http.ServeMux, handlers *Handlers) http.Handler {
	principal := Principal{Subject: "development", TenantID: DefaultTenantID, Role: TenantAdmin}
	wrap := func(handler http.HandlerFunc) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handler.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalContextKey{}, principal)))
		})
	}
	mux.Handle("POST /api/v1/services", wrap(handlers.CreateService))
	mux.Handle("GET /api/v1/services", wrap(handlers.ListServices))
	mux.Handle("POST /api/v1/environments", wrap(handlers.CreateEnvironment))
	mux.Handle("GET /api/v1/environments/{name}", wrap(handlers.GetEnvironment))
	mux.Handle("DELETE /api/v1/environments/{name}", wrap(handlers.DeleteEnvironment))
	mux.Handle("GET /api/v1/environments/{name}/logs", wrap(handlers.GetEnvironmentLogs))
	return mux
}
