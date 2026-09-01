package server

import (
	"context"
	"fmt"
	"net/http"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/api"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/config"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/executor"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/providers"
	bitbucketprovider "github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/providers/bitbucket"
	githubprovider "github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/providers/github"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/reconciler"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
	bitbucketscm "github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm/bitbucket"
	githubscm "github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm/github"
	"go.uber.org/zap"
)

type Server struct {
	httpServer           *http.Server
	reconciler           *reconciler.SCMCommandReconciler
	reconcileContext     context.Context
	cancelReconciliation context.CancelFunc
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
		providers.NewResolver(githubprovider.New(), bitbucketprovider.New()),
		map[scm.Provider]scm.WebhookAdapter{
			scm.ProviderGitHub:    githubscm.NewWebhookAdapter(cfg.GitHub.WebhookSecret),
			scm.ProviderBitbucket: bitbucketscm.NewWebhookAdapter(cfg.Bitbucket.WebhookSecret),
		},
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
	reconcileContext, cancelReconciliation := context.WithCancel(context.Background())

	return &Server{
		httpServer:           httpSrv,
		reconciler:           reconciler.NewSCMCommandReconciler(store, envOrchestrator, cfg.Reconciler.PreviewTTL, logger),
		reconcileContext:     reconcileContext,
		cancelReconciliation: cancelReconciliation,
	}, nil
}

func (s *Server) Start() error {
	go s.reconciler.Run(s.reconcileContext)
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.cancelReconciliation()
	return s.httpServer.Shutdown(ctx)
}
