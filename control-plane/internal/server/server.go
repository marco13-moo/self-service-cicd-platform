package server

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
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
	database             *sql.DB
	authenticators       map[scm.Provider]scm.Authenticator
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
	var commandStore api.SCMCommandStore = store
	var database *sql.DB
	if cfg.Database.URL != "" {
		database, err = sql.Open("pgx", cfg.Database.URL)
		if err != nil {
			return nil, fmt.Errorf("open PostgreSQL command store: %w", err)
		}
		if err = database.PingContext(context.Background()); err != nil {
			_ = database.Close()
			return nil, fmt.Errorf("connect PostgreSQL command store: %w", err)
		}
		commandStore, err = api.NewPostgresCommandStore(context.Background(), database)
		if err != nil {
			_ = database.Close()
			return nil, err
		}
	}
	argoLinks := orchestrator.NewArgoLinks(cfg.Argo.UIBaseURL)
	authenticators := map[scm.Provider]scm.Authenticator{}
	if cfg.GitHub.AppID != "" && cfg.GitHub.PrivateKeyPath != "" {
		key, readErr := os.ReadFile(cfg.GitHub.PrivateKeyPath)
		if readErr != nil {
			return nil, fmt.Errorf("read GitHub App private key: %w", readErr)
		}
		auth, authErr := githubscm.NewAuthenticator(cfg.GitHub.AppID, key)
		if authErr != nil {
			return nil, authErr
		}
		authenticators[scm.ProviderGitHub] = auth
	}
	if cfg.Bitbucket.OAuthClientID != "" && cfg.Bitbucket.OAuthClientSecret != "" {
		authenticators[scm.ProviderBitbucket] = bitbucketscm.NewAuthenticator(cfg.Bitbucket.OAuthClientID, cfg.Bitbucket.OAuthClientSecret)
	}

	//-----------------------------------------
	// Router
	//-----------------------------------------

	handler := api.NewRouter(
		store,
		commandStore,
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
		httpServer: httpSrv,
		reconciler: reconciler.NewSCMCommandReconciler(store, commandStore, envOrchestrator, cfg.Reconciler.PreviewTTL, reconciler.PreviewRuntimeConfig{
			ImageRepository: cfg.Preview.ImageRepository, BaseDomain: cfg.Preview.BaseDomain,
			URLScheme: cfg.Preview.URLScheme, BuilderImage: cfg.Preview.BuilderImage,
			RegistrySecretName: cfg.Preview.RegistrySecretName, RegistryInsecure: cfg.Preview.RegistryInsecure,
			ScannerImage: cfg.Preview.ScannerImage, VulnerabilitySeverities: cfg.Preview.VulnerabilitySeverities,
			IgnoreUnfixed:  cfg.Preview.IgnoreUnfixed,
			TargetPlatform: cfg.Preview.TargetPlatform,
		}, logger),
		reconcileContext:     reconcileContext,
		cancelReconciliation: cancelReconciliation,
		database:             database,
		authenticators:       authenticators,
	}, nil
}

func (s *Server) Start() error {
	go s.reconciler.Run(s.reconcileContext)
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.cancelReconciliation()
	err := s.httpServer.Shutdown(ctx)
	if s.database != nil {
		_ = s.database.Close()
	}
	return err
}
