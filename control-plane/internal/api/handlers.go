package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/providers"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
)

// Handlers owns all HTTP handlers for the control-plane API.
// Dependencies are injected explicitly.
type Handlers struct {
	store           *ServiceStore
	commandStore    SCMCommandStore
	envOrchestrator orchestrator.EnvironmentOrchestrator
	argoLinks       *orchestrator.ArgoLinks
	repositories    providers.RepositoryProvider
	webhookAdapters map[scm.Provider]scm.WebhookAdapter
	logger          *zap.Logger
}

func (h *Handlers) scopedStore(r *http.Request) *ServiceStore {
	return h.store.ForTenant(tenantFromRequest(r))
}

func (h *Handlers) scopedCommands(r *http.Request) SCMCommandStore {
	if scoped, ok := h.commandStore.(TenantScopedCommandStore); ok {
		return scoped.CommandsForTenant(tenantFromRequest(r))
	}
	return h.commandStore
}

func (h *Handlers) audit(r *http.Request, eventType, resourceType, resourceName, outcome string, metadata map[string]any) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		return
	}
	correlationID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
	if correlationID == "" {
		correlationID = uuid.NewString()
	}
	if err := h.store.ForTenant(principal.TenantID).AppendAuditEvent(r.Context(), AuditEvent{
		TenantID: principal.TenantID, CorrelationID: correlationID, Actor: principal.Subject,
		EventType: eventType, ResourceType: resourceType, ResourceName: resourceName, Outcome: outcome, Metadata: metadata,
	}); err != nil {
		h.logger.Error("failed to append audit event", zap.Error(err), zap.String("event_type", eventType))
	}
}

func NewHandlers(
	store *ServiceStore,
	commandStore SCMCommandStore,
	envOrchestrator orchestrator.EnvironmentOrchestrator,
	argoLinks *orchestrator.ArgoLinks,
	repositories providers.RepositoryProvider,
	webhookAdapters map[scm.Provider]scm.WebhookAdapter,
	logger *zap.Logger,
) *Handlers {
	return &Handlers{
		store:           store,
		commandStore:    commandStore,
		envOrchestrator: envOrchestrator,
		argoLinks:       argoLinks,
		repositories:    repositories,
		webhookAdapters: webhookAdapters,
		logger:          logger,
	}
}

// --- Platform endpoints ---

func (h *Handlers) Healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

func (h *Handlers) Readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := h.store.Ready(ctx); err != nil {
		h.logger.Warn("state plane readiness probe failed", zap.Error(err))
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "reason": "state plane unavailable"})
		return
	}
	if err := h.envOrchestrator.Ready(ctx); err != nil {
		h.logger.Warn("execution plane readiness probe failed", zap.Error(err))
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "reason": "execution plane unavailable"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "ready",
	})
}

// --- Service registry endpoints ---

func (h *Handlers) CreateService(w http.ResponseWriter, r *http.Request) {
	var req CreateServiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return
	}
	if problems := validation.IsDNS1123Label(req.Name); len(problems) != 0 {
		http.Error(w, "service name must be a Kubernetes DNS label", http.StatusBadRequest)
		return
	}
	if req.Deployment != nil {
		if req.Deployment.ContainerPort < 0 || req.Deployment.ContainerPort > 65535 {
			http.Error(w, "deployment container_port must be between 1 and 65535", http.StatusBadRequest)
			return
		}
		dockerfile := strings.TrimSpace(req.Deployment.Dockerfile)
		if dockerfile != "" && (path.IsAbs(dockerfile) || path.Clean(dockerfile) == ".." || strings.HasPrefix(path.Clean(dockerfile), "../")) {
			http.Error(w, "deployment dockerfile must be a repository-relative path", http.StatusBadRequest)
			return
		}
		req.Deployment.Dockerfile = dockerfile
		for index, rule := range req.Deployment.Egress {
			protocol := strings.ToUpper(strings.TrimSpace(rule.Protocol))
			if protocol == "" {
				protocol = "TCP"
			}
			if !validEgressDNSName(rule.DNSName) || rule.Port < 1 || rule.Port > 65535 || (protocol != "TCP" && protocol != "UDP") {
				http.Error(w, "egress rules require an exact DNS name, valid port, and TCP or UDP protocol", http.StatusBadRequest)
				return
			}
			req.Deployment.Egress[index].DNSName = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(rule.DNSName), "."))
			req.Deployment.Egress[index].Protocol = protocol
		}
	}

	if err := h.repositories.ValidateRepo(req.RepoURL); err != nil {
		http.Error(w, "repository validation failed", http.StatusUnprocessableEntity)
		return
	}
	projectType, err := h.repositories.DetectProjectType(req.RepoURL)
	if err != nil {
		http.Error(w, "project type detection failed", http.StatusUnprocessableEntity)
		return
	}
	repository, err := scm.ParseRepositoryIdentity(req.RepoURL)
	if err != nil {
		http.Error(w, "unsupported repository identity", http.StatusUnprocessableEntity)
		return
	}
	service := NewService(req, projectType, repository)
	service.TenantID = tenantFromRequest(r)
	if err := h.scopedStore(r).Put(service); err != nil {
		h.logger.Error("failed to persist service", zap.Error(err))
		http.Error(w, "failed to persist service", http.StatusInternalServerError)
		return
	}
	h.audit(r, "service.registered", "service", service.Name, "succeeded", map[string]any{"repository": service.Repository.Canonical()})

	h.logger.Info("service registered",
		zap.String("service_id", service.ID.String()),
		zap.String("name", service.Name),
		zap.String("owner", service.Owner),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(service)
}

var egressDNSName = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)

func validEgressDNSName(value string) bool {
	value = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
	return len(value) <= 253 && egressDNSName.MatchString(value) && !strings.Contains(value, "*")
}

func (h *Handlers) ListServices(w http.ResponseWriter, r *http.Request) {
	services := h.scopedStore(r).List()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(services)
}

// CatalogService is deliberately presentation-oriented: it gives developers a
// stable inventory view without exposing persistence documents or credentials.
type CatalogService struct {
	Name                string                 `json:"name"`
	Owner               string                 `json:"owner"`
	ProjectType         string                 `json:"project_type"`
	Repository          scm.RepositoryIdentity `json:"repository"`
	PreviewEnvironments int                    `json:"preview_environments"`
}

func (h *Handlers) ListCatalogServices(w http.ResponseWriter, r *http.Request) {
	store := h.scopedStore(r)
	counts := make(map[string]int)
	for _, environment := range store.ListEnvironments() {
		counts[environment.Spec.Service]++
	}
	result := make([]CatalogService, 0)
	for _, service := range store.List() {
		result = append(result, CatalogService{Name: service.Name, Owner: service.Owner,
			ProjectType: service.ProjectType, Repository: service.Repository,
			PreviewEnvironments: counts[service.Name]})
	}
	writeJSON(w, http.StatusOK, result)
}

type DiagnosticCheck struct {
	Code        string `json:"code"`
	Status      string `json:"status"`
	Summary     string `json:"summary"`
	Remediation string `json:"remediation,omitempty"`
}

type ServiceDiagnostics struct {
	Service string            `json:"service"`
	Checks  []DiagnosticCheck `json:"checks"`
}

// GetServiceDiagnostics returns deterministic, secret-free and tenant-scoped
// checks suitable for a CLI, portal, or SCM status adapter.
func (h *Handlers) GetServiceDiagnostics(w http.ResponseWriter, r *http.Request) {
	service, err := h.scopedStore(r).Get(r.PathValue("name"))
	if err != nil {
		http.Error(w, "service not found", http.StatusNotFound)
		return
	}
	checks := []DiagnosticCheck{
		{Code: "repository.identity", Status: "pass", Summary: service.Repository.Canonical()},
		{Code: "deployment.contract", Status: "pass", Summary: "declarative deployment contract is valid"},
	}
	if len(service.Deployment.Egress) == 0 {
		checks = append(checks, DiagnosticCheck{Code: "network.egress", Status: "pass", Summary: "no external egress declared"})
	} else {
		checks = append(checks, DiagnosticCheck{Code: "network.egress", Status: "pass", Summary: "explicit egress policy will be generated"})
	}
	for _, environment := range h.scopedStore(r).ListEnvironments() {
		if environment.Spec.Service == service.Name && environment.Spec.Source != nil && environment.Spec.Source.DeploymentPhase == "Failed" {
			checks = append(checks, DiagnosticCheck{Code: "preview.deployment", Status: "fail", Summary: "a preview deployment failed", Remediation: "inspect the environment logs endpoint and retry after correcting the repository build"})
		}
	}
	writeJSON(w, http.StatusOK, ServiceDiagnostics{Service: service.Name, Checks: checks})
}

// --- Environment endpoints (Phase 5) ---

type CreateEnvironmentRequest struct {
	Name    string `json:"name"`
	Service string `json:"service"`
	TTL     string `json:"ttl"`
}

func (h *Handlers) CreateEnvironment(w http.ResponseWriter, r *http.Request) {
	h.logger.Info("CreateEnvironment called")

	var req CreateEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("failed to decode request", zap.Error(err))
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return
	}

	h.logger.Info("parsed request",
		zap.String("name", req.Name),
		zap.String("service", req.Service),
		zap.String("ttl", req.TTL),
	)

	ttl, err := time.ParseDuration(req.TTL)
	if err != nil {
		h.logger.Error("invalid ttl", zap.Error(err))
		http.Error(w, "invalid ttl", http.StatusBadRequest)
		return
	}
	store := h.scopedStore(r)
	if _, err := store.Get(req.Service); err != nil {
		http.Error(w, "service not found", http.StatusNotFound)
		return
	}

	h.logger.Info("submitting environment to orchestrator")

	env, err := h.envOrchestrator.Create(r.Context(), orchestrator.EnvironmentSpec{
		TenantID:  string(tenantFromRequest(r)),
		Name:      req.Name,
		Namespace: orchestrator.NamespaceForTenant(string(tenantFromRequest(r)), req.Name),
		Service:   req.Service,
		TTL:       ttl,
	})
	if err != nil {
		h.logger.Error("failed to create environment", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	env.TenantID = string(tenantFromRequest(r))
	if err := store.PutEnvironment(env); err != nil {
		h.logger.Error("environment submitted but reference persistence failed", zap.Error(err))
		if errors.Is(err, ErrVersionConflict) {
			http.Error(w, "environment changed concurrently; retry with fresh state", http.StatusConflict)
			return
		}
		http.Error(w, "environment submitted but state persistence failed", http.StatusInternalServerError)
		return
	}
	h.audit(r, "environment.created", "environment", env.Spec.Name, "succeeded", map[string]any{"namespace": env.Spec.Namespace})

	h.logger.Info("environment creation accepted")
	writeJSON(w, http.StatusAccepted, env)
}

func (h *Handlers) DeleteEnvironment(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	ctx := r.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	store := h.scopedStore(r)
	env, err := store.GetEnvironment(name)
	if err != nil {
		h.logger.Error("environment not found", zap.Error(err))
		http.Error(w, "environment not found", http.StatusNotFound)
		return
	}

	destroyRef, err := h.envOrchestrator.Destroy(
		ctx,
		env.Spec.Namespace,
		env.Spec.Service,
		env.Spec.TenantID,
	)
	if err != nil {
		h.logger.Error("failed to delete environment", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	env.DestroyWorkflow = destroyRef
	if err := store.PutEnvironment(env); err != nil {
		h.logger.Error("destroy submitted but reference persistence failed", zap.Error(err))
		if errors.Is(err, ErrVersionConflict) {
			http.Error(w, "environment changed concurrently; retry with fresh state", http.StatusConflict)
			return
		}
		http.Error(w, "destroy submitted but state persistence failed", http.StatusInternalServerError)
		return
	}
	h.audit(r, "environment.destroy.requested", "environment", name, "succeeded", map[string]any{"namespace": env.Spec.Namespace})
	writeJSON(w, http.StatusAccepted, map[string]interface{}{"environment": name, "destroy_workflow": destroyRef})
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
