package server

import (
	"context"
	"net/http"
	"time"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/api"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/executor"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
	"go.uber.org/zap"
)

type Server struct {
	httpServer *http.Server
}

func New(
	address string,
	readTimeout time.Duration,
	writeTimeout time.Duration,
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
		"argo", // move to config soon
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

	//-----------------------------------------
	// Logger (temporary bootstrap logger)
	//-----------------------------------------

	// Senior recommendation:
	// Inject this from main soon instead of constructing here.
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, err
	}

	//-----------------------------------------
	// Router
	//-----------------------------------------

	handler := api.NewRouter(
		envOrchestrator, // interface satisfied
		logger,
	)

	//-----------------------------------------
	// HTTP Server
	//-----------------------------------------

	httpSrv := &http.Server{
		Addr:         address,
		Handler:      handler,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
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
