package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/orchestrator"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
	"go.uber.org/zap"
)

func TestTenantAuthorizationAndAPIIsolation(t *testing.T) {
	authorizer, err := NewTenantAuthorizer(`{
      "alpha-developer-token":{"subject":"alice","tenant_id":"alpha","role":"developer"},
      "alpha-viewer-token":{"subject":"auditor","tenant_id":"alpha","role":"viewer"},
      "beta-developer-token":{"subject":"bob","tenant_id":"beta","role":"developer"}
    }`)
	if err != nil {
		t.Fatal(err)
	}
	store := NewServiceStore()
	router := NewRouter(store, store, &fakeEnvironmentOrchestrator{}, orchestrator.NewArgoLinks("https://argo.example.test"), fakeRepositoryProvider{}, map[scm.Provider]scm.WebhookAdapter{}, zap.NewNop(), authorizer)

	createService := func(token, repository string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"name": "checkout", "repo_url": repository})
		request := httptest.NewRequest(http.MethodPost, "/api/v1/services", bytes.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	if response := createService("alpha-developer-token", "https://github.com/acme/alpha-checkout"); response.Code != http.StatusCreated {
		t.Fatalf("alpha create returned %d: %s", response.Code, response.Body.String())
	}
	if response := createService("beta-developer-token", "https://github.com/acme/beta-checkout"); response.Code != http.StatusCreated {
		t.Fatalf("beta create with the same resource name returned %d: %s", response.Code, response.Body.String())
	}
	if response := createService("alpha-viewer-token", "https://github.com/acme/forbidden"); response.Code != http.StatusForbidden {
		t.Fatalf("viewer mutation returned %d", response.Code)
	}
	if response := createService("", "https://github.com/acme/anonymous"); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous mutation returned %d", response.Code)
	}

	for token, expectedRepository := range map[string]string{"alpha-developer-token": "https://github.com/acme/alpha-checkout", "beta-developer-token": "https://github.com/acme/beta-checkout"} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/services", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("tenant list returned %d", response.Code)
		}
		var services []Service
		if err := json.Unmarshal(response.Body.Bytes(), &services); err != nil {
			t.Fatal(err)
		}
		if len(services) != 1 || services[0].RepoURL != expectedRepository {
			t.Fatalf("cross-tenant service disclosure for %s: %#v", token, services)
		}
	}

	createEnvironment := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(`{"name":"alpha-preview","service":"checkout","ttl":"1h"}`))
	createEnvironment.Header.Set("Authorization", "Bearer alpha-developer-token")
	createdEnvironment := httptest.NewRecorder()
	router.ServeHTTP(createdEnvironment, createEnvironment)
	if createdEnvironment.Code != http.StatusAccepted {
		t.Fatalf("alpha environment create returned %d: %s", createdEnvironment.Code, createdEnvironment.Body.String())
	}
	for token, expectedStatus := range map[string]int{"alpha-developer-token": http.StatusOK, "beta-developer-token": http.StatusNotFound} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/environments/alpha-preview", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != expectedStatus {
			t.Fatalf("tenant environment read for %s returned %d, want %d", token, response.Code, expectedStatus)
		}
	}
}

func TestTenantAdministrationRequiresExplicitPlatformCapability(t *testing.T) {
	authorizer, err := NewTenantAuthorizer(`{
      "tenant-admin":{"subject":"alice","tenant_id":"alpha","role":"admin"},
      "platform-admin":{"subject":"operator","tenant_id":"default","role":"admin","platform_admin":true}
    }`)
	if err != nil {
		t.Fatal(err)
	}
	store := NewServiceStore()
	router := NewRouter(store, store, &fakeEnvironmentOrchestrator{}, orchestrator.NewArgoLinks("https://argo.example.test"), fakeRepositoryProvider{}, nil, zap.NewNop(), authorizer)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tenants", bytes.NewBufferString(`{"tenant_id":"beta"}`))
	request.Header.Set("Authorization", "Bearer tenant-admin")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("tenant administrator acquired a platform capability: %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/tenants", bytes.NewBufferString(`{"tenant_id":"beta"}`))
	request.Header.Set("Authorization", "Bearer platform-admin")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("platform capability did not reach the PostgreSQL lifecycle boundary: %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodPut, "/api/v1/tenants/beta/auth", bytes.NewBufferString(`{}`))
	request.Header.Set("Authorization", "Bearer platform-admin")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("obsolete cross-tenant authentication route remained exposed: %d", response.Code)
	}
}
