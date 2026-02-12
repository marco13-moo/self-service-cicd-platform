package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/config"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/logging"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/server"
)

func main() {

	//-----------------------------------------
	// Load configuration
	//-----------------------------------------

	cfg := config.Load()

	//-----------------------------------------
	// Initialize logger
	//-----------------------------------------

	logger, err := logging.New(cfg.Log.Level)
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = logger.Sync()
	}()

	logger.Info("starting control plane",
		zap.String("service", cfg.ServiceName),
		zap.String("environment", cfg.Environment),
	)

	//-----------------------------------------
	// Construct server (composition root)
	//-----------------------------------------

	srv, err := server.New(
		cfg.HTTP.Address,
		cfg.HTTP.ReadTimeout,
		cfg.HTTP.WriteTimeout,
	)
	if err != nil {
		logger.Fatal("failed to construct server", zap.Error(err))
	}

	//-----------------------------------------
	// Start server asynchronously
	//-----------------------------------------

	go func() {

		logger.Info("http server listening",
			zap.String("address", cfg.HTTP.Address),
		)

		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("http server failed", zap.Error(err))
		}
	}()

	//-----------------------------------------
	// Graceful shutdown
	//-----------------------------------------

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	logger.Info("shutdown signal received")

	ctx, cancel := context.WithTimeout(
		context.Background(),
		cfg.HTTP.ShutdownTimeout,
	)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", zap.Error(err))
	}

	logger.Info("control plane stopped cleanly")
}
