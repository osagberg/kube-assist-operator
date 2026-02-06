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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/osagberg/kube-assist-operator/internal/ai"
	"github.com/osagberg/kube-assist-operator/internal/causal"
	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/datasource"
	"github.com/osagberg/kube-assist-operator/internal/history"
)

const (
	providerNoop      = "noop"
	providerAnthropic = "anthropic"
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

func TestServer_HandleHealthHistory_Last(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	// Add 3 snapshots
	for i := range 3 {
		server.history.Add(history.HealthSnapshot{
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

	now := time.Now()
	early := now.Add(-10 * time.Minute)
	mid := now.Add(-5 * time.Minute)
	late := now

	server.history.Add(history.HealthSnapshot{Timestamp: early, TotalHealthy: 1, HealthScore: 100})
	server.history.Add(history.HealthSnapshot{Timestamp: mid, TotalHealthy: 2, HealthScore: 100})
	server.history.Add(history.HealthSnapshot{Timestamp: late, TotalHealthy: 3, HealthScore: 100})

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

	server.history.Add(history.HealthSnapshot{
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

	server.latestCausal = &causal.CausalContext{
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

	server.latest = &HealthUpdate{
		Timestamp:  time.Now(),
		Namespaces: []string{"default"},
		Results:    map[string]CheckResult{},
		Summary:    Summary{TotalHealthy: 42},
	}

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

	// Add a few snapshots
	for i := range 5 {
		server.history.Add(history.HealthSnapshot{
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
