package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
    "go.uber.org/zap"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/api"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/config"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/logging"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/server"
)

func main() {
	cfg := config.Load()

	logger, err := logging.New(cfg.Log.Level)
	if err != nil {
		panic(err)
	}
	defer logger.Sync()

	logger.Info("starting control plane",
		zap.String("service", cfg.ServiceName),
		zap.String("environment", cfg.Environment),
	)

	srv := server.New(
		cfg.HTTP.Address,
		api.Router(),
		cfg.HTTP.ReadTimeout,
		cfg.HTTP.WriteTimeout,
	)

	go func() {
		if err := srv.Start(); err != nil {
			logger.Fatal("http server failed", zap.Error(err))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer cancel()

	logger.Info("shutting down control plane")
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", zap.Error(err))
	}
}
