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

const explainRiskUnknown = "unknown"

func TestServer_HandleExplain_AINotConfigured(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")
	// AI not enabled by default

	req := httptest.NewRequest(http.MethodGet, "/api/explain", nil)
	rr := httptest.NewRecorder()

	server.handleExplain(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleExplain() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp ai.ExplainResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("handleExplain() returned invalid JSON: %v", err)
	}

	if resp.RiskLevel != explainRiskUnknown {
		t.Errorf("RiskLevel = %q, want %s", resp.RiskLevel, explainRiskUnknown)
	}
	if resp.TrendDirection != explainRiskUnknown {
		t.Errorf("TrendDirection = %q, want %s", resp.TrendDirection, explainRiskUnknown)
	}
	if resp.Narrative == "" {
		t.Error("expected non-empty narrative about AI not configured")
	}
}

func TestServer_HandleExplain_NoHealthData(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")
	server.WithAI(ai.NewNoOpProvider(), true)

	req := httptest.NewRequest(http.MethodGet, "/api/explain", nil)
	rr := httptest.NewRecorder()

	server.handleExplain(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleExplain() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp ai.ExplainResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("handleExplain() returned invalid JSON: %v", err)
	}

	if resp.Narrative == "" {
		t.Error("expected non-empty narrative about no health data")
	}
	if resp.RiskLevel != explainRiskUnknown {
		t.Errorf("RiskLevel = %q, want %s", resp.RiskLevel, explainRiskUnknown)
	}
}

func TestServer_HandleExplain_MethodNotAllowed(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")

	req := httptest.NewRequest(http.MethodPost, "/api/explain", nil)
	rr := httptest.NewRecorder()

	server.handleExplain(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("handleExplain() POST status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestServer_HandleExplain_CachedResponse(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")
	server.WithAI(ai.NewNoOpProvider(), true)

	// Set up a cached explain response
	cachedResp := &ai.ExplainResponse{
		Narrative:      "Cached: cluster is healthy.",
		RiskLevel:      "low",
		TrendDirection: "stable",
		Confidence:     0.95,
		TokensUsed:     100,
	}
	server.mu.Lock()
	cs := server.getOrCreateClusterState("")
	cs.lastAICacheHash = "test-hash-123"
	cs.lastExplain = &ExplainCacheEntry{
		Response:  cachedResp,
		IssueHash: "test-hash-123",
		CachedAt:  time.Now(), // fresh cache
	}
	server.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/explain", nil)
	rr := httptest.NewRecorder()

	server.handleExplain(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleExplain() cached status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp ai.ExplainResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("handleExplain() cached returned invalid JSON: %v", err)
	}

	if resp.Narrative != "Cached: cluster is healthy." {
		t.Errorf("Narrative = %q, want cached value", resp.Narrative)
	}
	if resp.RiskLevel != "low" {
		t.Errorf("RiskLevel = %q, want low", resp.RiskLevel)
	}
}

func TestServer_HandleExplain_ExpiredCache(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")
	server.WithAI(ai.NewNoOpProvider(), true)

	// Set latest health data via cluster state
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
		Summary: Summary{TotalHealthy: 5},
	}
	cs.lastAICacheHash = "test-hash-123"
	cs.lastExplain = &ExplainCacheEntry{
		Response: &ai.ExplainResponse{
			Narrative:      "Old cached response.",
			RiskLevel:      "low",
			TrendDirection: "stable",
		},
		IssueHash: "test-hash-123",
		CachedAt:  time.Now().Add(-10 * time.Minute), // expired
	}
	server.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/explain", nil)
	rr := httptest.NewRecorder()

	server.handleExplain(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleExplain() expired cache status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp ai.ExplainResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("handleExplain() expired cache returned invalid JSON: %v", err)
	}

	// With noop provider + explain mode, the response goes through ParseExplainResponse
	// which may or may not parse the NoOp response as valid JSON.
	// The key check is that we get a fresh response, not the old cached one.
	if resp.Narrative == "Old cached response." {
		t.Error("expected fresh response, got stale cached response")
	}
}

func TestServer_HandleExplain_HashMismatchInvalidatesCache(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	server := NewServer(datasource.NewKubernetes(client), registry, ":8080")
	server.WithAI(ai.NewNoOpProvider(), true)

	// Set latest health data via cluster state
	server.mu.Lock()
	cs := server.getOrCreateClusterState("")
	cs.latest = &HealthUpdate{
		Timestamp:  time.Now(),
		Namespaces: []string{"default"},
		Results:    map[string]CheckResult{},
		Summary:    Summary{},
	}
	cs.lastAICacheHash = "new-hash-456"
	cs.lastExplain = &ExplainCacheEntry{
		Response: &ai.ExplainResponse{
			Narrative: "Cached with old hash.",
		},
		IssueHash: "old-hash-123", // different from current
		CachedAt:  time.Now(),
	}
	server.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/explain", nil)
	rr := httptest.NewRecorder()

	server.handleExplain(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleExplain() hash mismatch status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp ai.ExplainResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("handleExplain() hash mismatch returned invalid JSON: %v", err)
	}

	if resp.Narrative == "Cached with old hash." {
		t.Error("expected fresh response when hash changed, got stale cached response")
	}
}

func TestExplainCacheEntry_JSON(t *testing.T) {
	entry := ExplainCacheEntry{
		Response: &ai.ExplainResponse{
			Narrative:      "Test narrative",
			RiskLevel:      "medium",
			TrendDirection: "stable",
			Confidence:     0.8,
			TokensUsed:     250,
		},
		IssueHash: "should-not-appear",
		CachedAt:  time.Now(),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal error = %v", err)
	}

	// IssueHash and CachedAt should not be in JSON (tagged with json:"-")
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}

	if _, exists := raw["IssueHash"]; exists {
		t.Error("IssueHash should not be serialized")
	}
	if _, exists := raw["CachedAt"]; exists {
		t.Error("CachedAt should not be serialized")
	}
	if _, exists := raw["response"]; !exists {
		t.Error("response should be serialized")
	}
}
