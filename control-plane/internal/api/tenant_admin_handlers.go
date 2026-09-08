package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

type tenantLifecycleRequest struct {
	TenantID string `json:"tenant_id"`
	Status   string `json:"status,omitempty"`
}

type repositoryTransferRequest struct {
	SourceTenant string `json:"source_tenant"`
	TargetTenant string `json:"target_tenant"`
	Service      string `json:"service"`
}

func platformAdministrator(r *http.Request) (Principal, bool) {
	principal, ok := PrincipalFromContext(r.Context())
	return principal, ok && principal.PlatformAdmin && principal.Role == TenantAdmin
}

func (h *Handlers) ProvisionTenant(w http.ResponseWriter, r *http.Request) {
	principal, ok := platformAdministrator(r)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var request tenantLifecycleRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return
	}
	tenantID := TenantID(request.TenantID)
	if err := h.store.ProvisionTenant(r.Context(), tenantID); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	h.appendPlatformAudit(r, principal, tenantID, "tenant.provisioned", "tenant", request.TenantID, nil)
	writeJSON(w, http.StatusCreated, map[string]string{"tenant_id": request.TenantID, "status": "active"})
}

func (h *Handlers) ChangeTenantStatus(w http.ResponseWriter, r *http.Request) {
	principal, ok := platformAdministrator(r)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	tenantID := TenantID(r.PathValue("tenant"))
	var request tenantLifecycleRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return
	}
	if err := h.store.SetTenantStatus(r.Context(), tenantID, strings.ToLower(request.Status)); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	h.appendPlatformAudit(r, principal, tenantID, "tenant.status.changed", "tenant", string(tenantID), map[string]any{"status": request.Status})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) TransferRepository(w http.ResponseWriter, r *http.Request) {
	principal, ok := platformAdministrator(r)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var request repositoryTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return
	}
	if err := h.store.TransferService(r.Context(), TenantID(request.SourceTenant), TenantID(request.TargetTenant), request.Service); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	h.appendPlatformAudit(r, principal, TenantID(request.TargetTenant), "repository.transferred", "service", request.Service, map[string]any{"source_tenant": request.SourceTenant})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) appendPlatformAudit(r *http.Request, principal Principal, tenantID TenantID, eventType, resourceType, resourceName string, metadata map[string]any) {
	correlationID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
	if correlationID == "" {
		correlationID = uuid.NewString()
	}
	_ = h.store.AppendPlatformAuditEvent(r.Context(), AuditEvent{TenantID: tenantID, CorrelationID: correlationID, Actor: principal.Subject, EventType: eventType, ResourceType: resourceType, ResourceName: resourceName, Outcome: "succeeded", Metadata: metadata})
}
