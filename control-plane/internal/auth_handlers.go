package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"

	"go.uber.org/zap"
)

// TenantAuthConfigPayload is the API payload for tenant auth configuration.
type TenantAuthConfigPayload struct {
	Providers []struct {
		Issuer       string            `json:"issuer"`
		JWKSURL      string            `json:"jwks_url"`
		Audience     []string          `json:"audience,omitempty"`
		ClaimMapping map[string]string `json:"claim_mapping,omitempty"`
	} `json:"providers"`
}

func (h *Handlers) GetTenantAuth(w http.ResponseWriter, r *http.Request) {
	tenant := r.PathValue("tenant")
	if tenant == "" {
		h.logger.Warn("GetTenantAuth missing tenant path param")
		http.Error(w, "tenant is required", http.StatusBadRequest)
		return
	}
	store := h.scopedStore(r)
	cfg, err := store.GetTenantAuthConfig(r.Context(), TenantID(tenant))
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		h.logger.Error("failed to read tenant auth", zap.Error(err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(cfg)
}

func (h *Handlers) UpdateTenantAuth(w http.ResponseWriter, r *http.Request) {
	tenant := r.PathValue("tenant")
	if tenant == "" {
		h.logger.Warn("UpdateTenantAuth missing tenant path param")
		http.Error(w, "tenant is required", http.StatusBadRequest)
		return
	}
	var payload TenantAuthConfigPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return
	}
	store := h.scopedStore(r)
	if err := store.PutTenantAuthConfig(r.Context(), TenantID(tenant), payload); err != nil {
		h.logger.Error("failed to persist tenant auth", zap.Error(err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
