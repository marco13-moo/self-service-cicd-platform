package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	platformauth "github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/auth"
	"go.uber.org/zap"
)

type TenantAuthConfigPayload struct {
	Providers []platformauth.ProviderConfig `json:"providers"`
}

func (h *Handlers) ListTenantAuditEvents(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, err := h.scopedStore(r).ListAuditEvents(r.Context(), limit)
	if err != nil {
		http.Error(w, "failed to read audit events", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (h *Handlers) GetTenantAuth(w http.ResponseWriter, r *http.Request) {
	tenantID := tenantFromRequest(r)
	config, err := h.store.ForTenant(tenantID).GetTenantAuthConfig(r.Context(), tenantID)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		h.logger.Error("failed to read tenant authentication configuration", zap.Error(err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, config)
}

func (h *Handlers) UpdateTenantAuth(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var payload TenantAuthConfigPayload
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil || len(payload.Providers) == 0 {
		http.Error(w, "invalid tenant authentication configuration", http.StatusBadRequest)
		return
	}
	seenIssuers := map[string]struct{}{}
	for index := range payload.Providers {
		provider := &payload.Providers[index]
		provider.TenantID = string(principal.TenantID)
		provider.Issuer = strings.TrimSuffix(strings.TrimSpace(provider.Issuer), "/")
		if err := provider.Validate(false); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if _, duplicate := seenIssuers[provider.Issuer]; duplicate {
			http.Error(w, "duplicate issuer", http.StatusBadRequest)
			return
		}
		seenIssuers[provider.Issuer] = struct{}{}
	}
	if err := h.store.ForTenant(principal.TenantID).PutTenantAuthConfig(r.Context(), principal.TenantID, payload); err != nil {
		h.logger.Error("failed to persist tenant authentication configuration", zap.Error(err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.audit(r, "tenant.auth.updated", "tenant", string(principal.TenantID), "succeeded", nil)
	w.WriteHeader(http.StatusNoContent)
}
