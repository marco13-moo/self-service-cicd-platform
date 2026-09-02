package api

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
	"go.uber.org/zap"
)

const maxSCMWebhookBody = 1 << 20

func (h *Handlers) SCMWebhook(w http.ResponseWriter, r *http.Request) {
	provider := scm.Provider(r.PathValue("provider"))
	adapter, exists := h.webhookAdapters[provider]
	if !exists {
		http.Error(w, "unsupported SCM provider", http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSCMWebhookBody))
	if err != nil {
		http.Error(w, "invalid webhook payload", http.StatusRequestEntityTooLarge)
		return
	}
	deliveryID, event, err := adapter.Normalize(r.Header, body, time.Now().UTC())
	if err != nil {
		switch {
		case errors.Is(err, scm.ErrNotConfigured):
			http.Error(w, "SCM webhook ingestion is not configured", http.StatusServiceUnavailable)
		case errors.Is(err, scm.ErrUnauthorized):
			http.Error(w, "invalid webhook signature", http.StatusUnauthorized)
		default:
			http.Error(w, "invalid webhook payload", http.StatusBadRequest)
		}
		return
	}
	var command *scm.LifecycleCommand
	if event != nil {
		command = scm.CommandFromEvent(*event)
	}
	duplicate, err := h.commandStore.RecordSCMDelivery(provider, deliveryID, command, time.Now().UTC())
	if err != nil {
		h.logger.Error("failed to persist SCM delivery", zap.String("provider", string(provider)), zap.String("delivery_id", deliveryID), zap.Error(err))
		http.Error(w, "failed to persist webhook delivery", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]interface{}{"accepted": true, "duplicate": duplicate, "command_created": command != nil && !duplicate})
}
