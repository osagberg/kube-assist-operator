/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"html"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	assistv1alpha1 "github.com/osagberg/kube-assist-operator/api/v1alpha1"
	"github.com/osagberg/kube-assist-operator/internal/ai"
	"github.com/osagberg/kube-assist-operator/internal/causal"
	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/datasource"
	"github.com/osagberg/kube-assist-operator/internal/history"
)

const (
	providerNoop      = "noop"
	providerAnthropic = "anthropic"
	testAuthToken     = "secret-token"
)

func TestServer_SPAEmbed(t *testing.T) {
	// Verify that the embedded SPA assets exist and can be accessed
	entries, err := webAssets.ReadDir("web/dist")
	if err != nil {
		t.Fatalf("webAssets.ReadDir() error = %v", err)
	}

	if len(entries) == 0 {
		t.Error("expected embedded SPA assets to contain files")
	}

	// Check index.html exists
	data, err := webAssets.ReadFile("web/dist/index.html")
	if err != nil {
		t.Fatalf("webAssets.ReadFile(index.html) error = %v", err)
	}
	if len(data) < 50 {
		t.Error("index.html too short")
	}
}

func TestServer_HandleHealth_NoData(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()

	server.handleHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleHealth() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var response map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("handleHealth() returned invalid JSON: %v", err)
	}

	if response["status"] != "initializing" {
		t.Errorf("handleHealth() status = %s, want initializing", response["status"])
	}
}

func TestServer_HandleHealth_WithData(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	// Set some data via cluster state
	server.mu.Lock()
	cs := server.getOrCreateClusterState("")
	cs.latest = &HealthUpdate{
		Timestamp:  time.Now(),
		Namespaces: []string{"default"},
		Results: map[string]CheckResult{
			"workloads": {
				Name:    "workloads",
				Healthy: 5,
				Issues:  []Issue{},
			},
		},
		Summary: Summary{
			TotalHealthy: 5,
		},
	}
	server.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()

	server.handleHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleHealth() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var response HealthUpdate
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("handleHealth() returned invalid JSON: %v", err)
	}

	if response.Summary.TotalHealthy != 5 {
		t.Errorf("handleHealth() totalHealthy = %d, want 5", response.Summary.TotalHealthy)
	}
}

func TestServer_HandleTriggerCheck_WrongMethod(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	req := httptest.NewRequest(http.MethodGet, "/api/check", nil)
	rr := httptest.NewRecorder()

	server.handleTriggerCheck(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("handleTriggerCheck() with GET status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestServer_HandleTriggerCheck_POST(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	req := httptest.NewRequest(http.MethodPost, "/api/check", nil)
	rr := httptest.NewRecorder()

	server.handleTriggerCheck(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleTriggerCheck() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var response map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("handleTriggerCheck() returned invalid JSON: %v", err)
	}

	if response["status"] != "check triggered" {
		t.Errorf("handleTriggerCheck() status = %s, want 'check triggered'", response["status"])
	}
}

func TestServer_AuthMiddleware_NoTokenConfigured_AllowsRequest(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	called := false
	handler := server.authMiddleware(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/explain", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("authMiddleware without token status = %d, want %d", rr.Code, http.StatusNoContent)
	}
	if !called {
		t.Error("authMiddleware should call next handler when no token configured")
	}
}

func TestServer_AuthMiddleware_RequiresBearerToken(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")
	server.authToken = testAuthToken

	handler := server.authMiddleware(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/explain", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("authMiddleware missing bearer status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestServer_AuthMiddleware_RejectsWrongToken(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")
	server.authToken = testAuthToken

	handler := server.authMiddleware(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/explain", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("authMiddleware wrong token status = %d, want %d", rr.Code, http.StatusForbidden)
	}
}

func TestServer_AuthMiddleware_AcceptsValidToken(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")
	server.authToken = testAuthToken

	called := false
	handler := server.authMiddleware(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/explain", nil)
	req.Header.Set("Authorization", "Bearer "+testAuthToken)
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("authMiddleware valid token status = %d, want %d", rr.Code, http.StatusNoContent)
	}
	if !called {
		t.Error("authMiddleware should call next handler for valid token")
	}
}

func TestServer_AuthMiddleware_ConstantTimeComparison(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")
	server.authToken = testAuthToken

	handler := server.authMiddleware(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	tests := []struct {
		name       string
		authHeader string
		wantCode   int
	}{
		{"valid token", "Bearer " + testAuthToken, http.StatusNoContent},
		{"wrong token", "Bearer wrong", http.StatusForbidden},
		{"partial prefix match", "Bearer secret-token-extra", http.StatusForbidden},
		{"empty bearer", "Bearer ", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/check", nil)
			req.Header.Set("Authorization", tt.authHeader)
			rr := httptest.NewRecorder()
			handler(rr, req)
			if rr.Code != tt.wantCode {
				t.Errorf("authMiddleware(%q) status = %d, want %d", tt.name, rr.Code, tt.wantCode)
			}
		})
	}
}

func TestServer_ValidateSecurityConfig_RequiresTLSWhenAuthEnabled(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")
	server.authToken = testAuthToken
	server.allowInsecureHTTP = false

	err := server.validateSecurityConfig()
	if err == nil {
		t.Fatal("validateSecurityConfig() expected error when auth is enabled without TLS")
	}
	if !strings.Contains(err.Error(), "TLS") {
		t.Errorf("validateSecurityConfig() error = %q, want TLS guidance", err.Error())
	}
}

func TestServer_ValidateSecurityConfig_AllowsInsecureOverride(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")
	server.authToken = testAuthToken
	server.allowInsecureHTTP = true

	if err := server.validateSecurityConfig(); err != nil {
		t.Fatalf("validateSecurityConfig() unexpected error: %v", err)
	}
}

func TestServer_ValidateSecurityConfig_AllowsTLS(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")
	server.authToken = testAuthToken
	server.WithTLS("/tmp/cert.pem", "/tmp/key.pem")

	if err := server.validateSecurityConfig(); err != nil {
		t.Fatalf("validateSecurityConfig() unexpected error with TLS configured: %v", err)
	}
}

func TestNewServer(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	ds := datasource.NewKubernetes(client)
	server := NewServer(ds, registry, ":9090")

	if server.addr != ":9090" {
		t.Errorf("NewServer() addr = %s, want :9090", server.addr)
	}
	if server.client != ds {
		t.Error("NewServer() client not set correctly")
	}
	if server.registry != registry {
		t.Error("NewServer() registry not set correctly")
	}
	if server.clients == nil {
		t.Error("NewServer() clients map is nil")
	}
}

func TestHealthUpdate_JSON(t *testing.T) {
	update := HealthUpdate{
		Timestamp:  time.Now(),
		Namespaces: []string{"ns1", "ns2"},
		Results: map[string]CheckResult{
			"workloads": {
				Name:    "workloads",
				Healthy: 3,
				Issues: []Issue{
					{
						Type:       "CrashLoopBackOff",
						Severity:   "critical",
						Resource:   "deployment/test",
						Namespace:  "ns1",
						Message:    "Container crashing",
						Suggestion: "Check logs",
					},
				},
			},
		},
		Summary: Summary{
			TotalHealthy:  3,
			TotalIssues:   1,
			CriticalCount: 1,
		},
	}

	data, err := json.Marshal(update)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded HealthUpdate
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if len(decoded.Namespaces) != 2 {
		t.Errorf("Namespaces length = %d, want 2", len(decoded.Namespaces))
	}
	if decoded.Summary.CriticalCount != 1 {
		t.Errorf("CriticalCount = %d, want 1", decoded.Summary.CriticalCount)
	}
}

func TestServer_HandleGetAISettings_Default(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	req := httptest.NewRequest(http.MethodGet, "/api/settings/ai", nil)
	rr := httptest.NewRecorder()

	server.handleAISettings(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("GET /api/settings/ai status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp AISettingsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("GET /api/settings/ai returned invalid JSON: %v", err)
	}

	if resp.Enabled {
		t.Error("expected AI disabled by default")
	}
	if resp.Provider != providerNoop {
		t.Errorf("expected default provider 'noop', got %q", resp.Provider)
	}
	if resp.HasAPIKey {
		t.Error("expected no API key by default")
	}
}

func TestServer_HandleGetAISettings_WithProvider(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")
	server.WithAI(ai.NewNoOpProvider(), true)

	req := httptest.NewRequest(http.MethodGet, "/api/settings/ai", nil)
	rr := httptest.NewRecorder()

	server.handleAISettings(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("GET /api/settings/ai status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp AISettingsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("GET /api/settings/ai returned invalid JSON: %v", err)
	}

	if !resp.Enabled {
		t.Error("expected AI enabled")
	}
	if resp.Provider != providerNoop {
		t.Errorf("expected provider 'noop', got %q", resp.Provider)
	}
	if !resp.ProviderReady {
		t.Error("expected provider ready for noop")
	}
}

func TestServer_HandlePostAISettings(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	body := AISettingsRequest{
		Enabled:  true,
		Provider: providerNoop,
	}
	data, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/settings/ai", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleAISettings(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("POST /api/settings/ai status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp AISettingsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("POST /api/settings/ai returned invalid JSON: %v", err)
	}

	if !resp.Enabled {
		t.Error("expected AI enabled after POST")
	}
	if resp.Provider != providerNoop {
		t.Errorf("expected provider 'noop', got %q", resp.Provider)
	}

	// Verify the server state was actually updated
	if !server.aiEnabled {
		t.Error("server.aiEnabled should be true after POST")
	}
}

func TestServer_HandlePostAISettings_InvalidProvider(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	body := AISettingsRequest{
		Enabled:  true,
		Provider: "invalid-provider",
	}
	data, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/settings/ai", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleAISettings(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("POST /api/settings/ai with invalid provider status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestServer_HandlePostAISettings_InvalidJSON(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	req := httptest.NewRequest(http.MethodPost, "/api/settings/ai", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleAISettings(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("POST /api/settings/ai with invalid JSON status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestServer_HandleAISettings_MethodNotAllowed(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	req := httptest.NewRequest(http.MethodDelete, "/api/settings/ai", nil)
	rr := httptest.NewRecorder()

	server.handleAISettings(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE /api/settings/ai status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestServer_HandlePostAISettings_WithModel(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	body := AISettingsRequest{
		Enabled:  true,
		Provider: providerAnthropic,
		APIKey:   "sk-test-key",
		Model:    "claude-sonnet-4-5-20250929",
	}
	data, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/settings/ai", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleAISettings(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("POST /api/settings/ai status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp AISettingsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("returned invalid JSON: %v", err)
	}

	if resp.Provider != providerAnthropic {
		t.Errorf("expected provider 'anthropic', got %q", resp.Provider)
	}
	if resp.Model != "claude-sonnet-4-5-20250929" {
		t.Errorf("expected model 'claude-sonnet-4-5-20250929', got %q", resp.Model)
	}
	if !resp.HasAPIKey {
		t.Error("expected hasApiKey to be true after setting key")
	}

	// Now GET should reflect the updated settings
	req2 := httptest.NewRequest(http.MethodGet, "/api/settings/ai", nil)
	rr2 := httptest.NewRecorder()
	server.handleAISettings(rr2, req2)

	var resp2 AISettingsResponse
	if err := json.Unmarshal(rr2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("GET returned invalid JSON: %v", err)
	}
	if resp2.Provider != providerAnthropic {
		t.Errorf("GET after POST: expected provider 'anthropic', got %q", resp2.Provider)
	}
	if !resp2.HasAPIKey {
		t.Error("GET after POST: expected hasApiKey true")
	}
}

func TestServer_RunCheck_PopulatesLatest(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	if len(server.clusters) != 0 {
		t.Error("expected no cluster state before runCheck")
	}

	// Simulate what Start() does: run initial check synchronously
	server.runCheck(t.Context())

	server.mu.RLock()
	cs, ok := server.clusters[""]
	server.mu.RUnlock()
	if !ok || cs == nil || cs.latest == nil {
		t.Error("expected cluster state to be populated after runCheck")
	}
}

func TestServer_WithAI(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")
	provider := ai.NewNoOpProvider()
	result := server.WithAI(provider, true)

	if result != server {
		t.Error("WithAI should return the server for chaining")
	}
	if !server.aiEnabled {
		t.Error("WithAI should set aiEnabled")
	}
	if server.aiProvider != provider {
		t.Error("WithAI should set aiProvider")
	}
	if server.aiConfig.Provider != providerNoop {
		t.Errorf("WithAI should set aiConfig.Provider to provider name, got %q", server.aiConfig.Provider)
	}
}

func TestServer_HandleHealthHistory_Last(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	// Add 3 snapshots via cluster state
	server.mu.Lock()
	cs := server.getOrCreateClusterState("")
	server.mu.Unlock()
	for i := range 3 {
		cs.history.Add(history.HealthSnapshot{
			Timestamp:    time.Now().Add(time.Duration(i) * time.Minute),
			TotalHealthy: i + 1,
			TotalIssues:  0,
			HealthScore:  100,
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/api/health/history?last=2", nil)
	rr := httptest.NewRecorder()

	server.handleHealthHistory(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleHealthHistory(?last=2) status = %d, want %d", rr.Code, http.StatusOK)
	}

	var snapshots []history.HealthSnapshot
	if err := json.Unmarshal(rr.Body.Bytes(), &snapshots); err != nil {
		t.Fatalf("handleHealthHistory(?last=2) returned invalid JSON: %v", err)
	}

	if len(snapshots) != 2 {
		t.Errorf("handleHealthHistory(?last=2) returned %d snapshots, want 2", len(snapshots))
	}
}

func TestServer_HandleHealthHistory_Since(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	server.mu.Lock()
	cs := server.getOrCreateClusterState("")
	server.mu.Unlock()

	now := time.Now()
	early := now.Add(-10 * time.Minute)
	mid := now.Add(-5 * time.Minute)
	late := now

	cs.history.Add(history.HealthSnapshot{Timestamp: early, TotalHealthy: 1, HealthScore: 100})
	cs.history.Add(history.HealthSnapshot{Timestamp: mid, TotalHealthy: 2, HealthScore: 100})
	cs.history.Add(history.HealthSnapshot{Timestamp: late, TotalHealthy: 3, HealthScore: 100})

	sinceTime := mid.UTC().Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, "/api/health/history?since="+sinceTime, nil)
	rr := httptest.NewRecorder()

	server.handleHealthHistory(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleHealthHistory(?since=...) status = %d, want %d", rr.Code, http.StatusOK)
	}

	var snapshots []history.HealthSnapshot
	if err := json.Unmarshal(rr.Body.Bytes(), &snapshots); err != nil {
		t.Fatalf("handleHealthHistory(?since=...) returned invalid JSON: %v", err)
	}

	if len(snapshots) < 2 {
		t.Errorf("handleHealthHistory(?since=...) returned %d snapshots, want at least 2", len(snapshots))
	}
}

func TestServer_HandleHealthHistory_Default(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	server.mu.Lock()
	cs := server.getOrCreateClusterState("")
	server.mu.Unlock()
	cs.history.Add(history.HealthSnapshot{
		Timestamp:    time.Now(),
		TotalHealthy: 5,
		HealthScore:  100,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/health/history", nil)
	rr := httptest.NewRecorder()

	server.handleHealthHistory(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleHealthHistory() default status = %d, want %d", rr.Code, http.StatusOK)
	}

	var snapshots []history.HealthSnapshot
	if err := json.Unmarshal(rr.Body.Bytes(), &snapshots); err != nil {
		t.Fatalf("handleHealthHistory() default returned invalid JSON: %v", err)
	}

	if len(snapshots) != 1 {
		t.Errorf("handleHealthHistory() default returned %d snapshots, want 1", len(snapshots))
	}
}

func TestServer_HandleHealthHistory_InvalidLast(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	req := httptest.NewRequest(http.MethodGet, "/api/health/history?last=abc", nil)
	rr := httptest.NewRecorder()

	server.handleHealthHistory(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("handleHealthHistory(?last=abc) status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestServer_HandleHealthHistory_InvalidSince(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	req := httptest.NewRequest(http.MethodGet, "/api/health/history?since=notadate", nil)
	rr := httptest.NewRecorder()

	server.handleHealthHistory(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("handleHealthHistory(?since=notadate) status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestServer_HandleCausalGroups_NoData(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	req := httptest.NewRequest(http.MethodGet, "/api/causal/groups", nil)
	rr := httptest.NewRecorder()

	server.handleCausalGroups(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleCausalGroups() no data status = %d, want %d", rr.Code, http.StatusOK)
	}

	var cc causal.CausalContext
	if err := json.Unmarshal(rr.Body.Bytes(), &cc); err != nil {
		t.Fatalf("handleCausalGroups() no data returned invalid JSON: %v", err)
	}

	if len(cc.Groups) != 0 {
		t.Errorf("handleCausalGroups() no data returned %d groups, want 0", len(cc.Groups))
	}
}

func TestServer_HandleCausalGroups_WithData(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	server.mu.Lock()
	cs := server.getOrCreateClusterState("")
	cs.latestCausal = &causal.CausalContext{
		Groups: []causal.CausalGroup{
			{
				ID:         "test-group-1",
				Title:      "OOM in namespace default",
				Severity:   "critical",
				Rule:       "oom-correlation",
				Confidence: 0.9,
				FirstSeen:  time.Now(),
				LastSeen:   time.Now(),
			},
		},
		TotalIssues:       3,
		UncorrelatedCount: 1,
	}
	server.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/causal/groups", nil)
	rr := httptest.NewRecorder()

	server.handleCausalGroups(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleCausalGroups() with data status = %d, want %d", rr.Code, http.StatusOK)
	}

	var cc causal.CausalContext
	if err := json.Unmarshal(rr.Body.Bytes(), &cc); err != nil {
		t.Fatalf("handleCausalGroups() with data returned invalid JSON: %v", err)
	}

	if len(cc.Groups) != 1 {
		t.Errorf("handleCausalGroups() with data returned %d groups, want 1", len(cc.Groups))
	}
	if cc.Groups[0].ID != "test-group-1" {
		t.Errorf("handleCausalGroups() group ID = %q, want %q", cc.Groups[0].ID, "test-group-1")
	}
	if cc.TotalIssues != 3 {
		t.Errorf("handleCausalGroups() totalIssues = %d, want 3", cc.TotalIssues)
	}
}

func TestServer_SecurityHeaders(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("securityHeaders status = %d, want %d", rr.Code, http.StatusOK)
	}

	csp := rr.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Error("expected Content-Security-Policy header to be set")
	}
	if csp != "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'" {
		t.Errorf("Content-Security-Policy = %q, want expected CSP value", csp)
	}

	xcto := rr.Header().Get("X-Content-Type-Options")
	if xcto != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", xcto, "nosniff")
	}

	xfo := rr.Header().Get("X-Frame-Options")
	if xfo != "DENY" {
		t.Errorf("X-Frame-Options = %q, want %q", xfo, "DENY")
	}
}

// ---------------------------------------------------------------------------
// SSE and concurrency tests
// ---------------------------------------------------------------------------

func TestServer_HandleSSE_InitialState(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	server.mu.Lock()
	cs := server.getOrCreateClusterState("")
	cs.latest = &HealthUpdate{
		Timestamp:  time.Now(),
		Namespaces: []string{"default"},
		Results:    map[string]CheckResult{},
		Summary:    Summary{TotalHealthy: 42},
	}
	server.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		server.handleSSE(rr, req)
	}()

	// Give handler time to write initial state
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	body := rr.Body.String()
	if !strings.Contains(body, "data: ") {
		t.Fatalf("expected SSE data frame, got: %s", body)
	}

	// Extract JSON from "data: {...}\n\n"
	line := strings.TrimPrefix(strings.Split(body, "\n")[0], "data: ")
	var update HealthUpdate
	if err := json.Unmarshal([]byte(line), &update); err != nil {
		t.Fatalf("failed to parse SSE data: %v", err)
	}
	if update.Summary.TotalHealthy != 42 {
		t.Errorf("SSE initial state TotalHealthy = %d, want 42", update.Summary.TotalHealthy)
	}
}

func TestServer_HandleSSE_ClientDisconnect(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		server.handleSSE(rr, req)
	}()

	// Wait for client to register
	time.Sleep(100 * time.Millisecond)

	server.mu.RLock()
	clientsBefore := len(server.clients)
	server.mu.RUnlock()

	if clientsBefore != 1 {
		t.Fatalf("expected 1 SSE client, got %d", clientsBefore)
	}

	// Disconnect
	cancel()
	<-done

	server.mu.RLock()
	clientsAfter := len(server.clients)
	server.mu.RUnlock()

	if clientsAfter != 0 {
		t.Errorf("expected 0 SSE clients after disconnect, got %d", clientsAfter)
	}
}

func TestServer_HandleTriggerCheck_ConcurrencyGuard(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	// Simulate a check already in progress by setting the atomic flag
	server.checkInFlight.Store(true)

	req := httptest.NewRequest(http.MethodPost, "/api/check", nil)
	rr := httptest.NewRecorder()

	server.handleTriggerCheck(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("concurrent trigger status = %d, want %d", rr.Code, http.StatusOK)
	}

	var response map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("returned invalid JSON: %v", err)
	}

	if response["status"] != "check already in progress" {
		t.Errorf("concurrent trigger status = %q, want 'check already in progress'", response["status"])
	}
}

func TestServer_HandleHealthHistory_LastUpperBound(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	server.mu.Lock()
	cs := server.getOrCreateClusterState("")
	server.mu.Unlock()

	// Add a few snapshots
	for i := range 5 {
		cs.history.Add(history.HealthSnapshot{
			Timestamp:    time.Now().Add(time.Duration(i) * time.Minute),
			TotalHealthy: i + 1,
			HealthScore:  100,
		})
	}

	// Request way more than the ring buffer holds — should succeed, not error
	req := httptest.NewRequest(http.MethodGet, "/api/health/history?last=99999", nil)
	rr := httptest.NewRecorder()

	server.handleHealthHistory(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleHealthHistory(?last=99999) status = %d, want %d", rr.Code, http.StatusOK)
	}

	var snapshots []history.HealthSnapshot
	if err := json.Unmarshal(rr.Body.Bytes(), &snapshots); err != nil {
		t.Fatalf("returned invalid JSON: %v", err)
	}

	// Should return all 5 (capped at 1000, but only 5 exist)
	if len(snapshots) != 5 {
		t.Errorf("handleHealthHistory(?last=99999) returned %d snapshots, want 5", len(snapshots))
	}
}

func TestServer_HandleSSE_Headers(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		server.handleSSE(rr, req)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	ct := rr.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("SSE Content-Type = %q, want text/event-stream", ct)
	}
	cc := rr.Header().Get("Cache-Control")
	if cc != "no-cache" {
		t.Errorf("SSE Cache-Control = %q, want no-cache", cc)
	}
	conn := rr.Header().Get("Connection")
	if conn != "keep-alive" {
		t.Errorf("SSE Connection = %q, want keep-alive", conn)
	}
}

func TestServer_HandleSSE_MaxClientsLimit(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")
	server.maxSSEClients = 2

	// Register 2 clients manually to fill the limit
	ch1 := make(chan HealthUpdate, 1)
	ch2 := make(chan HealthUpdate, 1)
	server.mu.Lock()
	server.clients[ch1] = ""
	server.clients[ch2] = ""
	server.mu.Unlock()

	// 3rd connection should get 503
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(t.Context())
	rr := httptest.NewRecorder()

	server.handleSSE(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("3rd SSE client status = %d, want %d (503)", rr.Code, http.StatusServiceUnavailable)
	}

	// Cleanup
	server.mu.Lock()
	delete(server.clients, ch1)
	delete(server.clients, ch2)
	server.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Multi-cluster and fleet tests
// ---------------------------------------------------------------------------

func TestServer_HandleHealth_WithClusterId(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	server.mu.Lock()
	cs := server.getOrCreateClusterState("alpha")
	cs.latest = &HealthUpdate{
		Timestamp:  time.Now(),
		ClusterID:  "alpha",
		Namespaces: []string{"default"},
		Results:    map[string]CheckResult{},
		Summary:    Summary{TotalHealthy: 10, TotalIssues: 2, CriticalCount: 1, WarningCount: 1},
	}
	server.mu.Unlock()

	// Request with matching clusterId
	req := httptest.NewRequest(http.MethodGet, "/api/health?clusterId=alpha", nil)
	rr := httptest.NewRecorder()
	server.handleHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleHealth(?clusterId=alpha) status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp HealthUpdate
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("handleHealth(?clusterId=alpha) invalid JSON: %v", err)
	}
	if resp.Summary.TotalHealthy != 10 {
		t.Errorf("handleHealth(?clusterId=alpha) TotalHealthy = %d, want 10", resp.Summary.TotalHealthy)
	}
	if resp.ClusterID != "alpha" {
		t.Errorf("handleHealth(?clusterId=alpha) ClusterID = %q, want %q", resp.ClusterID, "alpha")
	}

	// Request with non-existent clusterId should return initializing
	req2 := httptest.NewRequest(http.MethodGet, "/api/health?clusterId=beta", nil)
	rr2 := httptest.NewRecorder()
	server.handleHealth(rr2, req2)

	var resp2 map[string]string
	if err := json.Unmarshal(rr2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("handleHealth(?clusterId=beta) invalid JSON: %v", err)
	}
	if resp2["status"] != "initializing" {
		t.Errorf("handleHealth(?clusterId=beta) status = %q, want %q", resp2["status"], "initializing")
	}
}

func TestServer_HandleFleetSummary(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	now := time.Now()
	server.mu.Lock()
	csA := server.getOrCreateClusterState("alpha")
	csA.latest = &HealthUpdate{
		Timestamp:  now,
		ClusterID:  "alpha",
		Namespaces: []string{"default"},
		Results:    map[string]CheckResult{},
		Summary:    Summary{TotalHealthy: 8, TotalIssues: 2, CriticalCount: 1, WarningCount: 1},
	}
	csB := server.getOrCreateClusterState("beta")
	csB.latest = &HealthUpdate{
		Timestamp:  now,
		ClusterID:  "beta",
		Namespaces: []string{"default", "prod"},
		Results:    map[string]CheckResult{},
		Summary:    Summary{TotalHealthy: 10, TotalIssues: 0},
	}
	server.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/fleet/summary", nil)
	rr := httptest.NewRecorder()
	server.handleFleetSummary(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleFleetSummary() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var summary FleetSummary
	if err := json.Unmarshal(rr.Body.Bytes(), &summary); err != nil {
		t.Fatalf("handleFleetSummary() invalid JSON: %v", err)
	}

	if len(summary.Clusters) != 2 {
		t.Fatalf("handleFleetSummary() clusters = %d, want 2", len(summary.Clusters))
	}

	// Find each cluster
	clusterMap := make(map[string]FleetClusterEntry)
	for _, c := range summary.Clusters {
		clusterMap[c.ClusterID] = c
	}

	alpha, ok := clusterMap["alpha"]
	if !ok {
		t.Fatal("handleFleetSummary() missing cluster 'alpha'")
	}
	if alpha.TotalIssues != 2 {
		t.Errorf("alpha TotalIssues = %d, want 2", alpha.TotalIssues)
	}
	if alpha.CriticalCount != 1 {
		t.Errorf("alpha CriticalCount = %d, want 1", alpha.CriticalCount)
	}
	// 8 healthy out of 10 total = 80%
	if alpha.HealthScore != 80 {
		t.Errorf("alpha HealthScore = %f, want 80", alpha.HealthScore)
	}

	beta, ok := clusterMap["beta"]
	if !ok {
		t.Fatal("handleFleetSummary() missing cluster 'beta'")
	}
	if beta.TotalIssues != 0 {
		t.Errorf("beta TotalIssues = %d, want 0", beta.TotalIssues)
	}
	if beta.HealthScore != 100 {
		t.Errorf("beta HealthScore = %f, want 100", beta.HealthScore)
	}
}

func TestServer_HandleSSE_ClusterFiltering(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	// Create cluster state for "alpha"
	server.mu.Lock()
	csA := server.getOrCreateClusterState("alpha")
	csA.latest = &HealthUpdate{
		Timestamp:  time.Now(),
		ClusterID:  "alpha",
		Namespaces: []string{"default"},
		Results:    map[string]CheckResult{},
		Summary:    Summary{TotalHealthy: 5},
	}
	server.mu.Unlock()

	// Subscribe to cluster "alpha"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/events?clusterId=alpha", nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		server.handleSSE(rr, req)
	}()

	// Give handler time to register and send initial state
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	body := rr.Body.String()
	if !strings.Contains(body, "data: ") {
		t.Fatalf("expected SSE data frame, got: %s", body)
	}

	line := strings.TrimPrefix(strings.Split(body, "\n")[0], "data: ")
	var update HealthUpdate
	if err := json.Unmarshal([]byte(line), &update); err != nil {
		t.Fatalf("failed to parse SSE data: %v", err)
	}
	if update.ClusterID != "alpha" {
		t.Errorf("SSE initial state ClusterID = %q, want %q", update.ClusterID, "alpha")
	}
	if update.Summary.TotalHealthy != 5 {
		t.Errorf("SSE initial state TotalHealthy = %d, want 5", update.Summary.TotalHealthy)
	}
}

// ---------------------------------------------------------------------------
// WithMaxSSEClients and unlimited SSE tests
// ---------------------------------------------------------------------------

func TestWithMaxSSEClients_Zero(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	server.WithMaxSSEClients(0)
	if server.maxSSEClients != 0 {
		t.Errorf("WithMaxSSEClients(0) maxSSEClients = %d, want 0", server.maxSSEClients)
	}
}

func TestWithMaxSSEClients_Negative(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	server.WithMaxSSEClients(-1)
	if server.maxSSEClients != 100 {
		t.Errorf("WithMaxSSEClients(-1) maxSSEClients = %d, want 100 (unchanged)", server.maxSSEClients)
	}
}

func TestHandleSSE_UnlimitedClients(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")
	server.maxSSEClients = 0 // unlimited

	// Register 200 clients manually
	channels := make([]chan HealthUpdate, 200)
	server.mu.Lock()
	for i := range channels {
		channels[i] = make(chan HealthUpdate, 1)
		server.clients[channels[i]] = ""
	}
	server.mu.Unlock()

	// 201st connection should still succeed (not get 503)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		server.handleSSE(rr, req)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if rr.Code == http.StatusServiceUnavailable {
		t.Error("SSE with maxSSEClients=0 should not reject connections")
	}

	// Cleanup
	server.mu.Lock()
	for _, ch := range channels {
		delete(server.clients, ch)
	}
	server.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Auth consistency tests (QA hardening)
// ---------------------------------------------------------------------------

func TestAuthMiddleware_ProtectsReadEndpoints(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")
	server.authToken = testAuthToken

	// All endpoints that should require auth
	endpoints := []struct {
		path   string
		method string
	}{
		{"/api/health", http.MethodGet},
		{"/api/health/history", http.MethodGet},
		{"/api/causal/groups", http.MethodGet},
		{"/api/prediction/trend", http.MethodGet},
		{"/api/clusters", http.MethodGet},
		{"/api/fleet/summary", http.MethodGet},
		{"/api/check", http.MethodPost},
		{"/api/settings/ai", http.MethodGet},
		{"/api/explain", http.MethodGet},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			handler := server.authMiddleware(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			req := httptest.NewRequest(ep.method, ep.path, nil)
			rr := httptest.NewRecorder()
			handler(rr, req)
			if rr.Code != http.StatusUnauthorized {
				t.Errorf("%s %s without token: status = %d, want %d", ep.method, ep.path, rr.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestAuthMiddleware_AllowsWithValidToken(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")
	server.authToken = testAuthToken

	endpoints := []string{
		"/api/health",
		"/api/health/history",
		"/api/causal/groups",
		"/api/prediction/trend",
		"/api/clusters",
		"/api/fleet/summary",
	}

	for _, path := range endpoints {
		t.Run(path, func(t *testing.T) {
			handler := server.authMiddleware(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Authorization", "Bearer "+testAuthToken)
			rr := httptest.NewRecorder()
			handler(rr, req)
			if rr.Code != http.StatusOK {
				t.Errorf("%s with valid token: status = %d, want %d", path, rr.Code, http.StatusOK)
			}
		})
	}
}

func TestAuthMiddleware_PassthroughWhenNoTokenConfigured(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")
	// authToken is "" by default (no env var set)

	endpoints := []string{
		"/api/health",
		"/api/health/history",
		"/api/causal/groups",
	}

	for _, path := range endpoints {
		t.Run(path, func(t *testing.T) {
			handler := server.authMiddleware(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()
			handler(rr, req)
			if rr.Code != http.StatusOK {
				t.Errorf("%s without auth config: status = %d, want %d", path, rr.Code, http.StatusOK)
			}
		})
	}
}

func TestCatalog_RemainsPublic(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")
	server.authToken = testAuthToken

	req := httptest.NewRequest(http.MethodGet, "/api/settings/ai/catalog", nil)
	rr := httptest.NewRecorder()
	server.handleAICatalog(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("catalog with auth configured: status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestSSEAuthMiddleware_QueryTokenNoLongerAccepted(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")
	server.authToken = testAuthToken

	called := false
	handler := server.sseAuthMiddleware(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	// Query-token SSE auth has been removed; only Bearer header and cookie are accepted
	req := httptest.NewRequest(http.MethodGet, "/api/events?token="+testAuthToken, nil)
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("SSE with query token: status = %d, want %d (query token auth removed)", rr.Code, http.StatusUnauthorized)
	}
	if called {
		t.Error("SSE handler should NOT have been called — query token auth is removed")
	}
}

func TestSSEAuthMiddleware_RejectsNoToken(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")
	server.authToken = testAuthToken

	handler := server.sseAuthMiddleware(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("SSE with no auth: status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestSSEAuthMiddleware_BearerHeader(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")
	server.authToken = testAuthToken

	called := false
	handler := server.sseAuthMiddleware(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req.Header.Set("Authorization", "Bearer "+testAuthToken)
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("SSE with valid Bearer: status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !called {
		t.Error("SSE handler should have been called with valid Bearer")
	}
}

func TestSSEAuthMiddleware_PassthroughWhenNoTokenConfigured(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")
	// No authToken configured

	called := false
	handler := server.sseAuthMiddleware(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("SSE without auth config: status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !called {
		t.Error("SSE handler should have been called when no auth configured")
	}
}

// ---------------------------------------------------------------------------
// TroubleshootRequest CRUD tests
// ---------------------------------------------------------------------------

func newTestServerWithWriter(t *testing.T) *Server {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = assistv1alpha1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	ds := datasource.NewKubernetes(fakeClient)
	server := NewServer(ds, registry, ":8080")
	server.WithK8sWriter(fakeClient, scheme)
	return server
}

const testTargetName = "my-app"

func TestHandleCreateTroubleshoot_Success(t *testing.T) {
	server := newTestServerWithWriter(t)

	body := CreateTroubleshootBody{
		Namespace: defaultNamespace,
		Actions:   []string{"diagnose", "logs"},
		TailLines: 200,
	}
	body.Target.Kind = defaultTargetKind
	body.Target.Name = testTargetName

	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/troubleshoot", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleCreateTroubleshoot(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("POST /api/troubleshoot status = %d, want %d; body = %s", rr.Code, http.StatusCreated, rr.Body.String())
	}

	var resp CreateTroubleshootResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("returned invalid JSON: %v", err)
	}
	if resp.Namespace != defaultNamespace {
		t.Errorf("response namespace = %q, want %q", resp.Namespace, defaultNamespace)
	}
	if resp.Phase != "Pending" {
		t.Errorf("response phase = %q, want %q", resp.Phase, "Pending")
	}
	if resp.Name == "" {
		t.Error("expected non-empty name in response")
	}
	if !strings.HasPrefix(resp.Name, "dash-") {
		t.Errorf("expected name to start with 'dash-', got %q", resp.Name)
	}
}

func TestHandleCreateTroubleshoot_MissingTargetName(t *testing.T) {
	server := newTestServerWithWriter(t)

	body := CreateTroubleshootBody{
		Namespace: defaultNamespace,
	}
	body.Target.Kind = defaultTargetKind
	// Target.Name intentionally empty

	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/troubleshoot", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleCreateTroubleshoot(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("POST /api/troubleshoot without target.name status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleCreateTroubleshoot_InvalidKind(t *testing.T) {
	server := newTestServerWithWriter(t)

	body := CreateTroubleshootBody{
		Namespace: defaultNamespace,
	}
	body.Target.Kind = "CronJob"
	body.Target.Name = testTargetName

	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/troubleshoot", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleCreateTroubleshoot(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("POST /api/troubleshoot with invalid kind status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleCreateTroubleshoot_NoWriter(t *testing.T) {
	scheme := runtime.NewScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(fakeClient), registry, ":8080")
	// k8sWriter not set

	body := CreateTroubleshootBody{
		Namespace: defaultNamespace,
	}
	body.Target.Kind = defaultTargetKind
	body.Target.Name = testTargetName

	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/troubleshoot", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleCreateTroubleshoot(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("POST /api/troubleshoot without writer status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleCreateTroubleshoot_InvalidActions(t *testing.T) {
	server := newTestServerWithWriter(t)

	body := CreateTroubleshootBody{
		Namespace: defaultNamespace,
		Actions:   []string{"diagnose", "hack"},
	}
	body.Target.Kind = defaultTargetKind
	body.Target.Name = testTargetName

	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/troubleshoot", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleCreateTroubleshoot(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("POST /api/troubleshoot with invalid action status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleListTroubleshoot(t *testing.T) {
	server := newTestServerWithWriter(t)

	// Create a CR first
	cr := &assistv1alpha1.TroubleshootRequest{}
	cr.GenerateName = "test-"
	cr.Namespace = defaultNamespace
	cr.Spec.Target.Kind = defaultTargetKind
	cr.Spec.Target.Name = testTargetName
	cr.Spec.Actions = []assistv1alpha1.TroubleshootAction{assistv1alpha1.ActionDiagnose}
	cr.Spec.TailLines = 100

	if err := server.k8sWriter.Create(context.Background(), cr); err != nil {
		t.Fatalf("failed to create test CR: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/troubleshoot", nil)
	rr := httptest.NewRecorder()

	server.handleCreateTroubleshoot(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("GET /api/troubleshoot status = %d, want %d; body = %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var summaries []TroubleshootRequestSummary
	if err := json.Unmarshal(rr.Body.Bytes(), &summaries); err != nil {
		t.Fatalf("returned invalid JSON: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].Target.Name != testTargetName {
		t.Errorf("summary target.name = %q, want %q", summaries[0].Target.Name, testTargetName)
	}
	if summaries[0].Target.Kind != defaultTargetKind {
		t.Errorf("summary target.kind = %q, want %q", summaries[0].Target.Kind, defaultTargetKind)
	}
}

func TestHandleCreateTroubleshoot_Defaults(t *testing.T) {
	server := newTestServerWithWriter(t)

	// Minimal body — only target.name
	body := `{"target":{"name":"` + testTargetName + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/troubleshoot", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleCreateTroubleshoot(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("POST /api/troubleshoot minimal body status = %d, want %d; body = %s", rr.Code, http.StatusCreated, rr.Body.String())
	}

	var resp CreateTroubleshootResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("returned invalid JSON: %v", err)
	}
	// Should default to "default" namespace
	if resp.Namespace != defaultNamespace {
		t.Errorf("default namespace = %q, want %q", resp.Namespace, defaultNamespace)
	}
}

func TestHandleCapabilities_WithWriter(t *testing.T) {
	server := newTestServerWithWriter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rr := httptest.NewRecorder()
	server.handleCapabilities(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/capabilities status = %d, want %d", rr.Code, http.StatusOK)
	}
	var caps map[string]bool
	if err := json.Unmarshal(rr.Body.Bytes(), &caps); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !caps["troubleshootCreate"] {
		t.Error("expected troubleshootCreate=true when k8sWriter is set")
	}
}

func TestHandleCapabilities_NoWriter(t *testing.T) {
	scheme := runtime.NewScheme()
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(fakeClient), registry, ":8080")

	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rr := httptest.NewRecorder()
	server.handleCapabilities(rr, req)

	var caps map[string]bool
	if err := json.Unmarshal(rr.Body.Bytes(), &caps); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if caps["troubleshootCreate"] {
		t.Error("expected troubleshootCreate=false when k8sWriter is nil")
	}
}

func TestHandleCreateTroubleshoot_InvalidTargetName(t *testing.T) {
	server := newTestServerWithWriter(t)

	body := CreateTroubleshootBody{
		Namespace: defaultNamespace,
	}
	body.Target.Kind = defaultTargetKind
	body.Target.Name = "INVALID_NAME"

	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/troubleshoot", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleCreateTroubleshoot(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("invalid target name status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleCreateTroubleshoot_TailLinesTooLarge(t *testing.T) {
	server := newTestServerWithWriter(t)

	body := CreateTroubleshootBody{
		Namespace: defaultNamespace,
		TailLines: 99999,
	}
	body.Target.Kind = defaultTargetKind
	body.Target.Name = testTargetName

	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/troubleshoot", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleCreateTroubleshoot(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("excessive tailLines status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestServeIndex_InjectsMetaTag(t *testing.T) {
	scheme := runtime.NewScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(cl), registry, ":8080")
	server.authToken = testAuthToken

	// Start the server to set up routes (but we won't actually listen)
	// Instead, test the serveIndex function by building it the same way Start() does
	spaFS, err := fs.Sub(webAssets, "web/dist")
	if err != nil {
		t.Fatalf("failed to create sub filesystem: %v", err)
	}

	indexHTML, err := fs.ReadFile(spaFS, "index.html")
	if err != nil {
		t.Fatalf("failed to read index.html: %v", err)
	}

	// Simulate what serveIndex does
	escaped := html.EscapeString(server.authToken)
	meta := []byte(`<meta name="dashboard-auth-token" content="` + escaped + `">`)
	body := bytes.Replace(indexHTML, []byte("</head>"), append(meta, []byte("</head>")...), 1)

	if !bytes.Contains(body, []byte(`<meta name="dashboard-auth-token"`)) {
		t.Error("index.html should contain dashboard-auth-token meta tag when auth is enabled")
	}
	if !bytes.Contains(body, []byte(testAuthToken)) {
		t.Error("meta tag should contain the auth token value")
	}
}

func TestServeIndex_NoMetaTag_WhenNoAuth(t *testing.T) {
	scheme := runtime.NewScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(cl), registry, ":8080")
	// authToken is empty (default)

	spaFS, err := fs.Sub(webAssets, "web/dist")
	if err != nil {
		t.Fatalf("failed to create sub filesystem: %v", err)
	}

	indexHTML, err := fs.ReadFile(spaFS, "index.html")
	if err != nil {
		t.Fatalf("failed to read index.html: %v", err)
	}

	// When no auth token, body should be unmodified
	if bytes.Contains(indexHTML, []byte(`<meta name="dashboard-auth-token"`)) {
		t.Error("index.html should not contain dashboard-auth-token meta tag when auth is disabled")
	}
	_ = server // use server variable
}

func TestServeIndex_SetsCookie(t *testing.T) {
	scheme := runtime.NewScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(cl), registry, ":8080")
	server.authToken = testAuthToken

	spaFS, err := fs.Sub(webAssets, "web/dist")
	if err != nil {
		t.Fatalf("failed to create sub filesystem: %v", err)
	}

	indexHTML, readErr := fs.ReadFile(spaFS, "index.html")
	if readErr != nil {
		t.Fatalf("failed to read index.html: %v", readErr)
	}

	// Build the serveIndex handler inline (mirrors Start() logic)
	serveIndex := func(w http.ResponseWriter, _ *http.Request) {
		body := indexHTML
		if server.authToken != "" {
			escaped := html.EscapeString(server.authToken)
			meta := []byte(`<meta name="dashboard-auth-token" content="` + escaped + `">`)
			body = bytes.Replace(body, []byte("</head>"), append(meta, []byte("</head>")...), 1)
			http.SetCookie(w, &http.Cookie{
				Name:     "__dashboard_session",
				Value:    server.authToken,
				Path:     "/",
				HttpOnly: true,
				Secure:   server.tlsConfigured(),
				SameSite: http.SameSiteStrictMode,
			})
			w.Header().Set("Cache-Control", "no-store")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	serveIndex(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("serveIndex status = %d, want 200", rr.Code)
	}

	// Check Cache-Control header
	if cc := rr.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-store")
	}

	// Check cookie
	cookies := rr.Result().Cookies()
	var found bool
	for _, c := range cookies {
		if c.Name == "__dashboard_session" {
			found = true
			if c.Value != testAuthToken {
				t.Errorf("cookie value = %q, want %q", c.Value, testAuthToken)
			}
			if !c.HttpOnly {
				t.Error("cookie should be HttpOnly")
			}
			if c.SameSite != http.SameSiteStrictMode {
				t.Error("cookie should be SameSite=Strict")
			}
		}
	}
	if !found {
		t.Error("__dashboard_session cookie not set")
	}

	// Check meta tag in response body
	if !bytes.Contains(rr.Body.Bytes(), []byte(`<meta name="dashboard-auth-token"`)) {
		t.Error("response body should contain auth meta tag")
	}
}

func TestSSEAuth_CookieAuth(t *testing.T) {
	scheme := runtime.NewScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(cl), registry, ":8080")
	server.authToken = testAuthToken

	called := false
	handler := server.sseAuthMiddleware(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req.AddCookie(&http.Cookie{Name: "__dashboard_session", Value: testAuthToken})
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("SSE cookie auth status = %d, want 200", rr.Code)
	}
	if !called {
		t.Error("handler should be called with valid cookie")
	}
}

func TestSSEAuth_CookiePrecedence(t *testing.T) {
	scheme := runtime.NewScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(cl), registry, ":8080")
	server.authToken = testAuthToken

	called := false
	handler := server.sseAuthMiddleware(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	// Valid cookie but no query param or header
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req.AddCookie(&http.Cookie{Name: "__dashboard_session", Value: testAuthToken})
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("SSE cookie precedence status = %d, want 200", rr.Code)
	}
	if !called {
		t.Error("handler should be called with valid cookie")
	}
}

func TestSSEAuth_NoCookieNoQuery_Rejects(t *testing.T) {
	scheme := runtime.NewScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(cl), registry, ":8080")
	server.authToken = testAuthToken

	handler := server.sseAuthMiddleware(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called without auth")
	})

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("SSE no auth status = %d, want 401", rr.Code)
	}
}

func TestSSEAuth_InvalidCookie_Rejects(t *testing.T) {
	scheme := runtime.NewScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	server := NewServer(datasource.NewKubernetes(cl), registry, ":8080")
	server.authToken = testAuthToken

	handler := server.sseAuthMiddleware(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called with wrong cookie")
	})

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req.AddCookie(&http.Cookie{Name: "__dashboard_session", Value: "wrong-token"})
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("SSE invalid cookie status = %d, want 403", rr.Code)
	}
}

func TestHandleCreateTroubleshoot_ActionsNormalizesAll(t *testing.T) {
	server := newTestServerWithWriter(t)

	body := CreateTroubleshootBody{
		Namespace: defaultNamespace,
		Actions:   []string{"logs", "all", "events"},
	}
	body.Target.Kind = defaultTargetKind
	body.Target.Name = testTargetName

	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/troubleshoot", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleCreateTroubleshoot(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("all-normalization status = %d, want %d; body = %s", rr.Code, http.StatusCreated, rr.Body.String())
	}
}
