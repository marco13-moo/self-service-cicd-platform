package api

import (
	"net/http"

	"go.uber.org/zap"
)

// NewRouter wires the HTTP routes for the control-plane API.
func NewRouter(logger *zap.Logger) http.Handler {
	store := NewServiceStore()
	handlers := NewHandlers(store, logger)

	mux := http.NewServeMux()

	// Platform endpoints
	mux.HandleFunc("/healthz", handlers.Healthz)
	mux.HandleFunc("/readyz", handlers.Readyz)

	// API v1
	mux.HandleFunc("/api/v1/services", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			handlers.CreateService(w, r)
		case http.MethodGet:
			handlers.ListServices(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	return mux
}
