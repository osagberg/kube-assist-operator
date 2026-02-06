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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/osagberg/kube-assist-operator/internal/ai"
	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/datasource"
)

const (
	providerNoop      = "noop"
	providerAnthropic = "anthropic"
)

func TestServer_HandleDashboard(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	server.handleDashboard(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleDashboard() status = %d, want %d", rr.Code, http.StatusOK)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "text/html" {
		t.Errorf("handleDashboard() Content-Type = %s, want text/html", contentType)
	}

	// Check that HTML contains expected content
	body := rr.Body.String()
	if len(body) < 100 {
		t.Error("handleDashboard() returned too short response")
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

	// Set some data
	server.latest = &HealthUpdate{
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

	if server.latest != nil {
		t.Error("expected latest to be nil before runCheck")
	}

	// Simulate what Start() does: run initial check synchronously
	server.runCheck(t.Context())

	if server.latest == nil {
		t.Error("expected latest to be non-nil after runCheck (no more 'initializing' flash)")
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
