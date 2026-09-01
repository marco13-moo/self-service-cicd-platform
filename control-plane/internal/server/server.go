package server

import (
	"context"
	"fmt"
	"net/http"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/api"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/config"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/executor"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
	githubprovider "github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/providers/github"
	"go.uber.org/zap"
)

type Server struct {
	httpServer *http.Server
}

func New(
	cfg *config.Config,
	logger *zap.Logger,
) (*Server, error) {

	//-----------------------------------------
	// Executor (Execution Plane Bridge)
	//-----------------------------------------

	clients, err := executor.NewClients()
	if err != nil {
		return nil, err
	}

	argoExecutor := executor.NewArgoSDKExecutor(
		clients,
		cfg.Argo.Namespace,
	)

	//-----------------------------------------
	// Orchestrator (Intent Layer)
	//-----------------------------------------

	// IMPORTANT:
	// Do NOT declare pointers without constructing them.
	// No `var envOrchestrator *...`
	envOrchestrator := orchestrator.NewArgoEnvironmentOrchestrator(
		argoExecutor,
	)

	store, err := api.NewPersistentServiceStore(cfg.State.Path)
	if err != nil {
		return nil, fmt.Errorf("initialize state store: %w", err)
	}
	argoLinks := orchestrator.NewArgoLinks(cfg.Argo.UIBaseURL)

	//-----------------------------------------
	// Router
	//-----------------------------------------

	handler := api.NewRouter(
		store,
		envOrchestrator, // interface satisfied
		argoLinks,
		githubprovider.New(),
		logger,
	)

	//-----------------------------------------
	// HTTP Server
	//-----------------------------------------

	httpSrv := &http.Server{
		Addr:         cfg.HTTP.Address,
		Handler:      handler,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}

	return &Server{
		httpServer: httpSrv,
	}, nil
}

func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
