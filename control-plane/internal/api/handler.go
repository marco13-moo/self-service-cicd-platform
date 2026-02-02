package api

import (
	"go.uber.org/zap"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
)

type Handler struct {
	logger          *zap.Logger
	envOrchestrator orchestrator.EnvironmentOrchestrator
}
