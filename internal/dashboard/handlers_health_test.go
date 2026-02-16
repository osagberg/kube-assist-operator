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

// ---------------------------------------------------------------------------
// handleClusters tests
// ---------------------------------------------------------------------------

func TestHandleClusters_NonConsoleDataSource(t *testing.T) {
	// When the data source is NOT a ConsoleDataSource, handleClusters should
	// return an empty clusters array.
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/clusters", nil)
	rr := httptest.NewRecorder()
	s.handleClusters(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleClusters() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp map[string][]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("handleClusters() invalid JSON: %v", err)
	}

	clusters, ok := resp["clusters"]
	if !ok {
		t.Fatal("handleClusters() response missing 'clusters' key")
	}
	if len(clusters) != 0 {
		t.Errorf("handleClusters() clusters = %v, want empty array", clusters)
	}
}

func TestHandleClusters_ConsoleDataSource_Success(t *testing.T) {
	// Start a fake console backend that returns a list of clusters.
	fakeClusters := []string{"cluster-alpha", "cluster-beta", "cluster-gamma"}
	fakeBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/clusters" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string][]string{"clusters": fakeClusters})
			return
		}
		http.NotFound(w, r)
	}))
	defer fakeBackend.Close()

	cds, err := datasource.NewConsole(fakeBackend.URL, "test-cluster", datasource.WithHTTPClient(fakeBackend.Client()))
	if err != nil {
		t.Fatalf("NewConsole() error = %v", err)
	}

	registry := checker.NewRegistry()
	s := NewServer(cds, registry, ":0")

	req := httptest.NewRequest(http.MethodGet, "/api/clusters", nil)
	rr := httptest.NewRecorder()
	s.handleClusters(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleClusters() with ConsoleDataSource status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp map[string][]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("handleClusters() invalid JSON: %v", err)
	}

	clusters, ok := resp["clusters"]
	if !ok {
		t.Fatal("handleClusters() response missing 'clusters' key")
	}
	if len(clusters) != 3 {
		t.Fatalf("handleClusters() clusters count = %d, want 3", len(clusters))
	}
	expected := map[string]bool{"cluster-alpha": true, "cluster-beta": true, "cluster-gamma": true}
	for _, c := range clusters {
		if !expected[c] {
			t.Errorf("unexpected cluster %q in response", c)
		}
	}
}

func TestHandleClusters_ConsoleDataSource_Error(t *testing.T) {
	// Start a fake console backend that always returns an error.
	fakeBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer fakeBackend.Close()

	cds, err := datasource.NewConsole(fakeBackend.URL, "test-cluster", datasource.WithHTTPClient(fakeBackend.Client()))
	if err != nil {
		t.Fatalf("NewConsole() error = %v", err)
	}

	registry := checker.NewRegistry()
	s := NewServer(cds, registry, ":0")

	req := httptest.NewRequest(http.MethodGet, "/api/clusters", nil)
	rr := httptest.NewRecorder()
	s.handleClusters(rr, req)

	// When GetClusters fails, handler should still return 200 with empty array.
	if rr.Code != http.StatusOK {
		t.Fatalf("handleClusters() error path status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp map[string][]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("handleClusters() invalid JSON: %v", err)
	}

	clusters, ok := resp["clusters"]
	if !ok {
		t.Fatal("handleClusters() response missing 'clusters' key")
	}
	if len(clusters) != 0 {
		t.Errorf("handleClusters() error path clusters = %v, want empty", clusters)
	}
}

// ---------------------------------------------------------------------------
// handleFleetSummary tests
// ---------------------------------------------------------------------------

func TestHandleFleetSummary_Empty(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/fleet/summary", nil)
	rr := httptest.NewRecorder()
	s.handleFleetSummary(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleFleetSummary() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var summary FleetSummary
	if err := json.Unmarshal(rr.Body.Bytes(), &summary); err != nil {
		t.Fatalf("handleFleetSummary() invalid JSON: %v", err)
	}
	if len(summary.Clusters) != 0 {
		t.Errorf("handleFleetSummary() clusters = %d, want 0", len(summary.Clusters))
	}
}

func TestHandleFleetSummary_MultipleClusters(t *testing.T) {
	s := newTestServer(t)

	now := time.Now()

	s.mu.Lock()
	csA := s.getOrCreateClusterState("cluster-a")
	csA.latest = &HealthUpdate{
		Timestamp:  now,
		Namespaces: []string{"default"},
		Results:    map[string]CheckResult{},
		Summary: Summary{
			HealthScore:              85.0,
			TotalIssues:              3,
			CriticalCount:            1,
			WarningCount:             1,
			InfoCount:                1,
			DeploymentReady:          5,
			DeploymentDesired:        6,
			DeploymentReadinessScore: 83.3,
		},
	}
	csB := s.getOrCreateClusterState("cluster-b")
	csB.latest = &HealthUpdate{
		Timestamp:  now.Add(-1 * time.Minute),
		Namespaces: []string{"prod"},
		Results:    map[string]CheckResult{},
		Summary: Summary{
			HealthScore:              100.0,
			TotalIssues:              0,
			DeploymentReady:          10,
			DeploymentDesired:        10,
			DeploymentReadinessScore: 100.0,
		},
	}
	s.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/fleet/summary", nil)
	rr := httptest.NewRecorder()
	s.handleFleetSummary(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleFleetSummary() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var summary FleetSummary
	if err := json.Unmarshal(rr.Body.Bytes(), &summary); err != nil {
		t.Fatalf("handleFleetSummary() invalid JSON: %v", err)
	}
	if len(summary.Clusters) != 2 {
		t.Fatalf("handleFleetSummary() clusters = %d, want 2", len(summary.Clusters))
	}

	var foundA bool
	for _, c := range summary.Clusters {
		if c.ClusterID == "cluster-a" {
			foundA = true
			if c.HealthScore != 85.0 {
				t.Errorf("cluster-a HealthScore = %f, want 85.0", c.HealthScore)
			}
			if c.TotalIssues != 3 {
				t.Errorf("cluster-a TotalIssues = %d, want 3", c.TotalIssues)
			}
			if c.CriticalCount != 1 {
				t.Errorf("cluster-a CriticalCount = %d, want 1", c.CriticalCount)
			}
			if c.DeploymentReady != 5 {
				t.Errorf("cluster-a DeploymentReady = %d, want 5", c.DeploymentReady)
			}
			if c.LastUpdated == "" {
				t.Error("cluster-a LastUpdated should not be empty")
			}
		}
	}
	if !foundA {
		t.Error("cluster-a not found in fleet summary")
	}
}

func TestHandleFleetSummary_SkipsNilLatest(t *testing.T) {
	s := newTestServer(t)

	s.mu.Lock()
	csA := s.getOrCreateClusterState("active")
	csA.latest = &HealthUpdate{
		Timestamp:  time.Now(),
		Namespaces: []string{"default"},
		Results:    map[string]CheckResult{},
		Summary:    Summary{HealthScore: 90.0},
	}
	_ = s.getOrCreateClusterState("pending")
	s.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/fleet/summary", nil)
	rr := httptest.NewRecorder()
	s.handleFleetSummary(rr, req)

	var summary FleetSummary
	if err := json.Unmarshal(rr.Body.Bytes(), &summary); err != nil {
		t.Fatalf("handleFleetSummary() invalid JSON: %v", err)
	}
	if len(summary.Clusters) != 1 {
		t.Errorf("handleFleetSummary() clusters = %d, want 1 (nil latest should be skipped)", len(summary.Clusters))
	}
	if len(summary.Clusters) == 1 && summary.Clusters[0].ClusterID != "active" {
		t.Errorf("expected cluster 'active', got %q", summary.Clusters[0].ClusterID)
	}
}

// ---------------------------------------------------------------------------
// WithLogContext tests
// ---------------------------------------------------------------------------

func TestWithLogContext(t *testing.T) {
	s := newTestServer(t)

	config := &checker.LogContextConfig{
		Enabled:           true,
		MaxLogLines:       50,
		MaxEventsPerIssue: 20,
	}

	result := s.WithLogContext(config, nil)

	if result != s {
		t.Error("WithLogContext should return *Server for chaining")
	}
	if s.logContextConfig != config {
		t.Error("WithLogContext should set logContextConfig")
	}
	if s.logContextClient != nil {
		t.Error("WithLogContext should set logContextClient to nil when nil passed")
	}
}

// ---------------------------------------------------------------------------
// cleanupExpiredIssueStates tests (one-pass logic)
// ---------------------------------------------------------------------------

func TestCleanupExpiredIssueStates_RemovesExpired(t *testing.T) {
	s := newTestServer(t)

	past := time.Now().Add(-1 * time.Hour)
	future := time.Now().Add(1 * time.Hour)

	s.mu.Lock()
	cs := s.getOrCreateClusterState("")
	cs.issueStates["expired-snooze"] = &IssueState{
		Key:          "expired-snooze",
		Action:       ActionSnoozed,
		SnoozedUntil: &past,
		CreatedAt:    time.Now().Add(-2 * time.Hour),
	}
	cs.issueStates["active-snooze"] = &IssueState{
		Key:          "active-snooze",
		Action:       ActionSnoozed,
		SnoozedUntil: &future,
		CreatedAt:    time.Now(),
	}
	cs.issueStates["acknowledged"] = &IssueState{
		Key:       "acknowledged",
		Action:    ActionAcknowledged,
		CreatedAt: time.Now(),
	}
	s.mu.Unlock()

	// Simulate the cleanup logic that runs inside the ticker handler.
	s.mu.Lock()
	for _, cs := range s.clusters {
		for k, st := range cs.issueStates {
			if st.IsExpired() {
				delete(cs.issueStates, k)
			}
		}
	}
	s.mu.Unlock()

	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := cs.issueStates["expired-snooze"]; ok {
		t.Error("expired snooze should have been cleaned up")
	}
	if _, ok := cs.issueStates["active-snooze"]; !ok {
		t.Error("active snooze should NOT have been cleaned up")
	}
	if _, ok := cs.issueStates["acknowledged"]; !ok {
		t.Error("acknowledged state should NOT have been cleaned up")
	}
}

func TestCleanupExpiredIssueStates_MultipleClusters(t *testing.T) {
	s := newTestServer(t)

	past := time.Now().Add(-1 * time.Hour)

	s.mu.Lock()
	csA := s.getOrCreateClusterState("cluster-a")
	csA.issueStates["expired-a"] = &IssueState{
		Key:          "expired-a",
		Action:       ActionSnoozed,
		SnoozedUntil: &past,
		CreatedAt:    time.Now().Add(-2 * time.Hour),
	}
	csB := s.getOrCreateClusterState("cluster-b")
	csB.issueStates["expired-b"] = &IssueState{
		Key:          "expired-b",
		Action:       ActionSnoozed,
		SnoozedUntil: &past,
		CreatedAt:    time.Now().Add(-2 * time.Hour),
	}
	s.mu.Unlock()

	s.mu.Lock()
	for _, cs := range s.clusters {
		for k, st := range cs.issueStates {
			if st.IsExpired() {
				delete(cs.issueStates, k)
			}
		}
	}
	s.mu.Unlock()

	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(csA.issueStates) != 0 {
		t.Errorf("cluster-a should have 0 issue states, got %d", len(csA.issueStates))
	}
	if len(csB.issueStates) != 0 {
		t.Errorf("cluster-b should have 0 issue states, got %d", len(csB.issueStates))
	}
}

func TestCleanupExpiredIssueStates_NoClusters(t *testing.T) {
	s := newTestServer(t)

	// Should not panic when there are no clusters
	s.mu.Lock()
	for _, cs := range s.clusters {
		for k, st := range cs.issueStates {
			if st.IsExpired() {
				delete(cs.issueStates, k)
			}
		}
	}
	s.mu.Unlock()
}

// ---------------------------------------------------------------------------
// cleanupChatSessions (two-pass cleanup logic) tests
// ---------------------------------------------------------------------------

func TestCleanupChatSessions_TwoPassRemovesExpired(t *testing.T) {
	s := newChatTestServer(t, func(s *Server) {
		s.chatSessionTTL = 10 * time.Millisecond
	})

	expiredSession := &ChatSession{
		ID:          "expired",
		TokenBudget: 50000,
		CreatedAt:   time.Now().Add(-1 * time.Hour),
	}
	expiredSession.setLastAccessed(time.Now().Add(-1 * time.Hour))

	freshSession := &ChatSession{
		ID:          "fresh",
		TokenBudget: 50000,
		CreatedAt:   time.Now(),
	}
	freshSession.setLastAccessed(time.Now())

	s.chatMu.Lock()
	s.chatSessions["expired"] = expiredSession
	s.chatSessions["fresh"] = freshSession
	s.chatMu.Unlock()

	// Simulate the two-pass cleanup logic from cleanupChatSessions
	type snapshot struct {
		id      string
		session *ChatSession
	}
	var expired []snapshot
	now := time.Now()
	s.chatMu.RLock()
	for id, session := range s.chatSessions {
		if now.Sub(session.lastAccessed()) > s.chatSessionTTL {
			expired = append(expired, snapshot{id: id, session: session})
		}
	}
	s.chatMu.RUnlock()

	if len(expired) > 0 {
		now = time.Now()
		s.chatMu.Lock()
		for _, candidate := range expired {
			session, ok := s.chatSessions[candidate.id]
			if !ok || session != candidate.session {
				continue
			}
			if now.Sub(session.lastAccessed()) > s.chatSessionTTL {
				delete(s.chatSessions, candidate.id)
			}
		}
		s.chatMu.Unlock()
	}

	s.chatMu.RLock()
	defer s.chatMu.RUnlock()

	if _, ok := s.chatSessions["expired"]; ok {
		t.Error("expired session should have been cleaned up")
	}
	if _, ok := s.chatSessions["fresh"]; !ok {
		t.Error("fresh session should NOT have been cleaned up")
	}
}

// ---------------------------------------------------------------------------
// deepCopyCausalContext tests
// ---------------------------------------------------------------------------

func TestDeepCopyCausalContext_Nil(t *testing.T) {
	result := deepCopyCausalContext(nil)
	if result != nil {
		t.Errorf("deepCopyCausalContext(nil) = %v, want nil", result)
	}
}

func TestDeepCopyCausalContext_EmptyGroups(t *testing.T) {
	cc := &causal.CausalContext{
		UncorrelatedCount: 3,
		TotalIssues:       5,
	}
	result := deepCopyCausalContext(cc)
	if result == nil {
		t.Fatal("deepCopyCausalContext() returned nil")
	}
	if result == cc {
		t.Error("deepCopyCausalContext should return a new pointer")
	}
	if result.UncorrelatedCount != 3 {
		t.Errorf("UncorrelatedCount = %d, want 3", result.UncorrelatedCount)
	}
	if result.TotalIssues != 5 {
		t.Errorf("TotalIssues = %d, want 5", result.TotalIssues)
	}
}

func TestDeepCopyCausalContext_WithGroups(t *testing.T) {
	cc := &causal.CausalContext{
		UncorrelatedCount: 1,
		TotalIssues:       4,
		Groups: []causal.CausalGroup{
			{
				ID:        "g1",
				Title:     "OOM in default",
				RootCause: "Memory leak",
				Severity:  "critical",
				Events: []causal.TimelineEvent{
					{
						Checker: "workloads",
						Issue: checker.Issue{
							Namespace: "default",
							Resource:  "deploy/app",
						},
					},
				},
				AISteps:    []string{"Step 1", "Step 2"},
				AIEnhanced: true,
			},
			{
				ID:       "g2",
				Title:    "Network issue",
				Severity: "warning",
				Events: []causal.TimelineEvent{
					{
						Checker: "network",
						Issue: checker.Issue{
							Namespace: "prod",
							Resource:  "svc/api",
						},
					},
				},
			},
		},
	}

	result := deepCopyCausalContext(cc)
	if result == nil {
		t.Fatal("deepCopyCausalContext() returned nil")
	}

	if len(result.Groups) != 2 {
		t.Fatalf("Groups = %d, want 2", len(result.Groups))
	}

	g0 := result.Groups[0]
	if g0.ID != "g1" {
		t.Errorf("Groups[0].ID = %q, want g1", g0.ID)
	}
	if g0.Title != "OOM in default" {
		t.Errorf("Groups[0].Title = %q", g0.Title)
	}
	if len(g0.Events) != 1 {
		t.Fatalf("Groups[0].Events = %d, want 1", len(g0.Events))
	}
	if len(g0.AISteps) != 2 {
		t.Fatalf("Groups[0].AISteps = %d, want 2", len(g0.AISteps))
	}

	// Verify deep copy: mutating original Events should not affect copy
	cc.Groups[0].Events[0].Checker = "mutated"
	if result.Groups[0].Events[0].Checker == "mutated" {
		t.Error("Events slice was not deep-copied; mutation propagated to copy")
	}

	// Verify deep copy: mutating original AISteps should not affect copy
	cc.Groups[0].AISteps[0] = "mutated step"
	if result.Groups[0].AISteps[0] == "mutated step" {
		t.Error("AISteps slice was not deep-copied; mutation propagated to copy")
	}
}

func TestDeepCopyCausalContext_EmptySlicesInGroup(t *testing.T) {
	cc := &causal.CausalContext{
		TotalIssues: 1,
		Groups: []causal.CausalGroup{
			{
				ID:       "g1",
				Title:    "Test",
				Events:   nil,
				AISteps:  nil,
				Severity: "info",
			},
		},
	}

	result := deepCopyCausalContext(cc)
	if result == nil {
		t.Fatal("deepCopyCausalContext() returned nil")
	}
	if len(result.Groups) != 1 {
		t.Fatalf("Groups = %d, want 1", len(result.Groups))
	}
	if result.Groups[0].Events != nil {
		t.Error("nil Events should remain nil in copy")
	}
	if result.Groups[0].AISteps != nil {
		t.Error("nil AISteps should remain nil in copy")
	}
}

// ---------------------------------------------------------------------------
// IssueState.IsExpired tests
// ---------------------------------------------------------------------------

func TestIssueState_IsExpired(t *testing.T) {
	past := time.Now().Add(-1 * time.Hour)
	future := time.Now().Add(1 * time.Hour)

	tests := []struct {
		name    string
		state   *IssueState
		expired bool
	}{
		{
			name: "acknowledged is never expired",
			state: &IssueState{
				Action:    ActionAcknowledged,
				CreatedAt: time.Now(),
			},
			expired: false,
		},
		{
			name: "snoozed with nil SnoozedUntil is not expired",
			state: &IssueState{
				Action:    ActionSnoozed,
				CreatedAt: time.Now(),
			},
			expired: false,
		},
		{
			name: "snoozed with future expiry is not expired",
			state: &IssueState{
				Action:       ActionSnoozed,
				SnoozedUntil: &future,
				CreatedAt:    time.Now(),
			},
			expired: false,
		},
		{
			name: "snoozed with past expiry is expired",
			state: &IssueState{
				Action:       ActionSnoozed,
				SnoozedUntil: &past,
				CreatedAt:    time.Now(),
			},
			expired: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.state.IsExpired()
			if got != tt.expired {
				t.Errorf("IsExpired() = %v, want %v", got, tt.expired)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// WithHistorySize tests
// ---------------------------------------------------------------------------

func TestWithHistorySize(t *testing.T) {
	s := newTestServer(t)

	result := s.WithHistorySize(200)
	if result != s {
		t.Error("WithHistorySize should return *Server for chaining")
	}
	if s.historySize != 200 {
		t.Errorf("historySize = %d, want 200", s.historySize)
	}
}

func TestWithHistorySize_ZeroIgnored(t *testing.T) {
	s := newTestServer(t)
	original := s.historySize

	s.WithHistorySize(0)
	if s.historySize != original {
		t.Errorf("historySize = %d, want %d (should not change for zero)", s.historySize, original)
	}
}

func TestWithHistorySize_NegativeIgnored(t *testing.T) {
	s := newTestServer(t)
	original := s.historySize

	s.WithHistorySize(-5)
	if s.historySize != original {
		t.Errorf("historySize = %d, want %d (should not change for negative)", s.historySize, original)
	}
}

// ---------------------------------------------------------------------------
// HandleHealth with issue states tests
// ---------------------------------------------------------------------------

func TestHandleHealth_WithIssueStates(t *testing.T) {
	s := newTestServer(t)

	future := time.Now().Add(1 * time.Hour)
	past := time.Now().Add(-1 * time.Hour)

	s.mu.Lock()
	cs := s.getOrCreateClusterState("")
	cs.latest = &HealthUpdate{
		Timestamp:  time.Now(),
		Namespaces: []string{"default"},
		Results:    map[string]CheckResult{},
		Summary:    Summary{TotalHealthy: 5},
	}
	cs.issueStates["active-key"] = &IssueState{
		Key:          "active-key",
		Action:       ActionSnoozed,
		SnoozedUntil: &future,
		CreatedAt:    time.Now(),
	}
	cs.issueStates["expired-key"] = &IssueState{
		Key:          "expired-key",
		Action:       ActionSnoozed,
		SnoozedUntil: &past,
		CreatedAt:    time.Now(),
	}
	s.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()
	s.handleHealth(rr, req)

	var update HealthUpdate
	if err := json.Unmarshal(rr.Body.Bytes(), &update); err != nil {
		t.Fatalf("handleHealth() invalid JSON: %v", err)
	}

	if update.IssueStates == nil {
		t.Fatal("expected IssueStates to be non-nil")
	}
	if _, ok := update.IssueStates["active-key"]; !ok {
		t.Error("active-key should be included in IssueStates")
	}
	if _, ok := update.IssueStates["expired-key"]; ok {
		t.Error("expired-key should NOT be included in IssueStates")
	}
}

// ---------------------------------------------------------------------------
// HandleHealthHistory edge cases
// ---------------------------------------------------------------------------

func TestHandleHealthHistory_InvalidLast(t *testing.T) {
	s := newTestServer(t)

	s.mu.Lock()
	_ = s.getOrCreateClusterState("")
	s.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/health/history?last=abc", nil)
	rr := httptest.NewRecorder()
	s.handleHealthHistory(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("handleHealthHistory(?last=abc) status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleHealthHistory_InvalidSince(t *testing.T) {
	s := newTestServer(t)

	s.mu.Lock()
	_ = s.getOrCreateClusterState("")
	s.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/health/history?since=not-a-date", nil)
	rr := httptest.NewRecorder()
	s.handleHealthHistory(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("handleHealthHistory(?since=invalid) status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleHealthHistory_NoCluster(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/health/history?clusterId=nonexistent", nil)
	rr := httptest.NewRecorder()
	s.handleHealthHistory(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handleHealthHistory(nonexistent cluster) status = %d, want %d", rr.Code, http.StatusOK)
	}

	var snapshots []history.HealthSnapshot
	if err := json.Unmarshal(rr.Body.Bytes(), &snapshots); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(snapshots) != 0 {
		t.Errorf("expected empty array for nonexistent cluster, got %d", len(snapshots))
	}
}

// ---------------------------------------------------------------------------
// WithSSEBufferSize tests
// ---------------------------------------------------------------------------

func TestWithSSEBufferSize(t *testing.T) {
	s := newTestServer(t)

	result := s.WithSSEBufferSize(50)
	if result != s {
		t.Error("WithSSEBufferSize should return *Server for chaining")
	}
	if s.sseBufferSize != 50 {
		t.Errorf("sseBufferSize = %d, want 50", s.sseBufferSize)
	}
}

func TestWithSSEBufferSize_ZeroIgnored(t *testing.T) {
	s := newTestServer(t)
	original := s.sseBufferSize

	s.WithSSEBufferSize(0)
	if s.sseBufferSize != original {
		t.Errorf("sseBufferSize changed to %d for zero input", s.sseBufferSize)
	}
}

// ---------------------------------------------------------------------------
// WithCheckInterval / WithAIAnalysisTimeout / WithMaxIssuesPerBatch tests
// ---------------------------------------------------------------------------

func TestWithCheckInterval(t *testing.T) {
	s := newTestServer(t)

	result := s.WithCheckInterval(5 * time.Minute)
	if result != s {
		t.Error("WithCheckInterval should return *Server for chaining")
	}
	if s.checkInterval != 5*time.Minute {
		t.Errorf("checkInterval = %v, want 5m", s.checkInterval)
	}
}

func TestWithCheckInterval_ZeroIgnored(t *testing.T) {
	s := newTestServer(t)
	original := s.checkInterval

	s.WithCheckInterval(0)
	if s.checkInterval != original {
		t.Errorf("checkInterval changed for zero input: %v", s.checkInterval)
	}
}

func TestWithAIAnalysisTimeout(t *testing.T) {
	s := newTestServer(t)

	result := s.WithAIAnalysisTimeout(2 * time.Minute)
	if result != s {
		t.Error("WithAIAnalysisTimeout should return *Server for chaining")
	}
	if s.aiAnalysisTimeout != 2*time.Minute {
		t.Errorf("aiAnalysisTimeout = %v, want 2m", s.aiAnalysisTimeout)
	}
}

func TestWithMaxIssuesPerBatch(t *testing.T) {
	s := newTestServer(t)

	result := s.WithMaxIssuesPerBatch(25)
	if result != s {
		t.Error("WithMaxIssuesPerBatch should return *Server for chaining")
	}
	if s.maxIssuesPerBatch != 25 {
		t.Errorf("maxIssuesPerBatch = %d, want 25", s.maxIssuesPerBatch)
	}
}

func TestWithMaxIssuesPerBatch_ZeroIgnored(t *testing.T) {
	s := newTestServer(t)
	original := s.maxIssuesPerBatch

	s.WithMaxIssuesPerBatch(0)
	if s.maxIssuesPerBatch != original {
		t.Errorf("maxIssuesPerBatch changed for zero input: %d", s.maxIssuesPerBatch)
	}
}

// ---------------------------------------------------------------------------
// WithMaxSSEClients tests
// ---------------------------------------------------------------------------

func TestWithMaxSSEClients(t *testing.T) {
	s := newTestServer(t)

	result := s.WithMaxSSEClients(50)
	if result != s {
		t.Error("WithMaxSSEClients should return *Server for chaining")
	}
	if s.maxSSEClients != 50 {
		t.Errorf("maxSSEClients = %d, want 50", s.maxSSEClients)
	}
}

func TestWithMaxSSEClients_ZeroForUnlimited(t *testing.T) {
	s := newTestServer(t)

	s.WithMaxSSEClients(0)
	if s.maxSSEClients != 0 {
		t.Errorf("maxSSEClients = %d, want 0 (unlimited)", s.maxSSEClients)
	}
}

// ---------------------------------------------------------------------------
// cleanupAuthSessionsOnce tests
// ---------------------------------------------------------------------------

func TestCleanupAuthSessionsOnce_RemovesExpired(t *testing.T) {
	s := newTestServer(t)

	// Store an expired session and a valid session.
	s.sessions.Store("expired-session", authSession{
		expiresAt: time.Now().Add(-1 * time.Hour),
	})
	s.sessions.Store("valid-session", authSession{
		expiresAt: time.Now().Add(1 * time.Hour),
	})

	s.cleanupAuthSessionsOnce(time.Now())

	if _, ok := s.sessions.Load("expired-session"); ok {
		t.Error("expired session should have been removed")
	}
	if _, ok := s.sessions.Load("valid-session"); !ok {
		t.Error("valid session should still be present")
	}
}

func TestCleanupAuthSessionsOnce_NoSessions(t *testing.T) {
	s := newTestServer(t)
	// Should not panic when there are no sessions.
	s.cleanupAuthSessionsOnce(time.Now())
}

func TestCleanupAuthSessionsOnce_AllExpired(t *testing.T) {
	s := newTestServer(t)

	s.sessions.Store("s1", authSession{
		expiresAt: time.Now().Add(-10 * time.Minute),
	})
	s.sessions.Store("s2", authSession{
		expiresAt: time.Now().Add(-5 * time.Minute),
	})
	s.sessions.Store("s3", authSession{
		expiresAt: time.Now().Add(-1 * time.Second),
	})

	s.cleanupAuthSessionsOnce(time.Now())

	count := 0
	s.sessions.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count != 0 {
		t.Errorf("expected 0 sessions after cleanup, got %d", count)
	}
}

func TestCleanupAuthSessionsOnce_InvalidValueType(t *testing.T) {
	s := newTestServer(t)

	// Store a value that is not an authSession. The cleanup should delete it.
	s.sessions.Store("bad-type", "not-an-authSession")
	s.sessions.Store("valid", authSession{
		expiresAt: time.Now().Add(1 * time.Hour),
	})

	s.cleanupAuthSessionsOnce(time.Now())

	if _, ok := s.sessions.Load("bad-type"); ok {
		t.Error("non-authSession value should have been deleted")
	}
	if _, ok := s.sessions.Load("valid"); !ok {
		t.Error("valid session should still be present")
	}
}

// ---------------------------------------------------------------------------
// handlePostAISettings with ai.Manager path
// ---------------------------------------------------------------------------

func TestHandlePostAISettings_WithManager_NoopProvider(t *testing.T) {
	scheme := runtime.NewScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	s := NewServer(datasource.NewKubernetes(cl), registry, ":0")

	// Wire up an ai.Manager as the provider (this is the Manager path in handlePostAISettings).
	noop := ai.NewNoOpProvider()
	mgr := ai.NewManager(noop, nil, false, nil, nil, nil)
	s.aiProvider = mgr
	s.aiEnabled = false

	body := AISettingsRequest{
		Enabled:  true,
		Provider: "noop",
	}
	data, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/settings/ai", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	s.handlePostAISettings(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handlePostAISettings (Manager) status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp AISettingsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if !resp.Enabled {
		t.Error("expected AI enabled after POST")
	}
	if resp.Provider != "noop" {
		t.Errorf("provider = %q, want noop", resp.Provider)
	}

	// Verify server state was updated via the Manager path.
	if !s.aiEnabled {
		t.Error("server.aiEnabled should be true after Manager POST")
	}
}

func TestHandlePostAISettings_WithManager_ClearAPIKey(t *testing.T) {
	scheme := runtime.NewScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	s := NewServer(datasource.NewKubernetes(cl), registry, ":0")

	noop := ai.NewNoOpProvider()
	mgr := ai.NewManager(noop, nil, true, nil, nil, nil)
	s.aiProvider = mgr
	s.aiEnabled = true
	s.aiConfig.APIKey = "sk-secret"
	s.aiConfig.Provider = "noop"

	body := AISettingsRequest{
		Enabled:     true,
		Provider:    "noop",
		ClearAPIKey: true,
	}
	data, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/settings/ai", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	s.handlePostAISettings(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handlePostAISettings (Manager, clearKey) status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	if s.aiConfig.APIKey != "" {
		t.Errorf("apiKey should be empty after clearApiKey, got %q", s.aiConfig.APIKey)
	}
}

func TestHandlePostAISettings_WithManager_ResetsClusterAIState(t *testing.T) {
	scheme := runtime.NewScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	s := NewServer(datasource.NewKubernetes(cl), registry, ":0")

	noop := ai.NewNoOpProvider()
	mgr := ai.NewManager(noop, nil, false, nil, nil, nil)
	s.aiProvider = mgr
	s.aiEnabled = false

	// Create a cluster state with cached AI data.
	s.mu.Lock()
	cs := s.getOrCreateClusterState("")
	cs.lastIssueHash = "oldhash"
	cs.lastAIResult = &AIStatus{Provider: "old", IssuesEnhanced: 5}
	cs.lastAIEnhancements = map[string]map[string]aiEnhancement{
		"checker": {"key": {Suggestion: "old suggestion"}},
	}
	cs.latest = &HealthUpdate{
		Timestamp: time.Now(),
		Results:   map[string]CheckResult{},
		Summary:   Summary{HealthScore: 90},
	}
	s.mu.Unlock()

	body := AISettingsRequest{
		Enabled:  true,
		Provider: "noop",
	}
	data, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/settings/ai", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	s.handlePostAISettings(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if cs.lastIssueHash != "" {
		t.Errorf("lastIssueHash should be cleared, got %q", cs.lastIssueHash)
	}
	if cs.lastAIResult != nil {
		t.Error("lastAIResult should be nil after reconfigure")
	}
	if cs.lastAIEnhancements != nil {
		t.Error("lastAIEnhancements should be nil after reconfigure")
	}
	if cs.latest.AIStatus == nil {
		t.Fatal("latest.AIStatus should be set")
	}
	if !cs.latest.AIStatus.Pending {
		t.Error("AIStatus.Pending should be true after reconfigure")
	}
}

func TestHandlePostAISettings_DirectAPIKey_Rejected(t *testing.T) {
	s := newTestServer(t)

	body := AISettingsRequest{
		Enabled:  true,
		Provider: "noop",
		APIKey:   "sk-should-be-rejected",
	}
	data, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/settings/ai", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	s.handlePostAISettings(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for direct apiKey, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "secretRef") {
		t.Errorf("error message should mention secretRef, got: %s", rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// writeSSEEvent tests
// ---------------------------------------------------------------------------

func TestWriteSSEEvent_Success(t *testing.T) {
	rr := httptest.NewRecorder()

	evt := map[string]string{"type": "message", "content": "hello"}
	err := writeSSEEvent(rr, evt)
	if err != nil {
		t.Fatalf("writeSSEEvent() error = %v", err)
	}

	body := rr.Body.String()
	if !strings.HasPrefix(body, "data: ") {
		t.Errorf("SSE event should start with 'data: ', got: %q", body)
	}
	if !strings.HasSuffix(body, "\n\n") {
		t.Errorf("SSE event should end with double newline, got: %q", body)
	}

	// Extract JSON payload and validate it.
	jsonPart := strings.TrimPrefix(body, "data: ")
	jsonPart = strings.TrimSuffix(jsonPart, "\n\n")
	var decoded map[string]string
	if err := json.Unmarshal([]byte(jsonPart), &decoded); err != nil {
		t.Fatalf("SSE event payload is not valid JSON: %v", err)
	}
	if decoded["type"] != "message" {
		t.Errorf("decoded type = %q, want message", decoded["type"])
	}
}

func TestWriteSSEEvent_MarshalError(t *testing.T) {
	rr := httptest.NewRecorder()

	// json.Marshal cannot handle channels.
	unmarshalable := make(chan int)
	err := writeSSEEvent(rr, unmarshalable)
	if err == nil {
		t.Error("writeSSEEvent should return error for unmarshalable value")
	}
}

// ---------------------------------------------------------------------------
// chatToolExplainIssue additional paths
// ---------------------------------------------------------------------------

func TestChatToolExplainIssue_WithSpecificChecker(t *testing.T) {
	s := newChatTestServer(t)

	// Seed AI enhancement data under a specific checker.
	s.mu.Lock()
	cs := s.clusters[""]
	cs.lastAIEnhancements = map[string]map[string]aiEnhancement{
		"workloads": {
			"crash-key": {
				Suggestion: "Increase memory limits",
				RootCause:  "OOM killed",
			},
		},
		"network": {
			"dns-key": {
				Suggestion: "Check CoreDNS pods",
				RootCause:  "DNS resolution timeout",
			},
		},
	}
	s.mu.Unlock()

	executor := s.buildChatToolExecutor("")

	// Test with checker specified -- should find the issue in that checker.
	args, _ := json.Marshal(map[string]string{"checker": "network", "issueKey": "dns-key"})
	result, err := executor(nil, "explain_issue", args)
	if err != nil {
		t.Fatalf("explain_issue error: %v", err)
	}
	if !strings.Contains(result, "Check CoreDNS pods") {
		t.Errorf("expected CoreDNS suggestion, got: %s", result)
	}

	// Test with checker specified but wrong checker -- should report not found.
	args2, _ := json.Marshal(map[string]string{"checker": "workloads", "issueKey": "dns-key"})
	result2, err := executor(nil, "explain_issue", args2)
	if err != nil {
		t.Fatalf("explain_issue error: %v", err)
	}
	if !strings.Contains(result2, "No AI enhancement found") {
		t.Errorf("expected 'No AI enhancement found', got: %s", result2)
	}
}

func TestChatToolExplainIssue_InvalidArgs(t *testing.T) {
	s := newChatTestServer(t)
	executor := s.buildChatToolExecutor("")

	// Pass invalid JSON.
	_, err := executor(nil, "explain_issue", []byte("not-json"))
	if err == nil {
		t.Error("expected error for invalid JSON args")
	}
}

func TestChatToolExplainIssue_MissingIssueKey(t *testing.T) {
	s := newChatTestServer(t)
	executor := s.buildChatToolExecutor("")

	args, _ := json.Marshal(map[string]string{"checker": "workloads"})
	_, err := executor(nil, "explain_issue", args)
	if err == nil {
		t.Error("expected error for missing issueKey")
	}
	if err != nil && !strings.Contains(err.Error(), "issueKey is required") {
		t.Errorf("expected 'issueKey is required' error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// handleHealthHistory additional edge cases
// ---------------------------------------------------------------------------

func TestHandleHealthHistory_NoCluster_InvalidLast(t *testing.T) {
	s := newTestServer(t)

	// When cluster does not exist, it still validates the 'last' parameter.
	req := httptest.NewRequest(http.MethodGet, "/api/health/history?clusterId=nonexistent&last=abc", nil)
	rr := httptest.NewRecorder()
	s.handleHealthHistory(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid last with nonexistent cluster, got %d", rr.Code)
	}
}

func TestHandleHealthHistory_NoCluster_InvalidSince(t *testing.T) {
	s := newTestServer(t)

	// When cluster does not exist, it still validates the 'since' parameter.
	req := httptest.NewRequest(http.MethodGet, "/api/health/history?clusterId=nonexistent&since=bad", nil)
	rr := httptest.NewRecorder()
	s.handleHealthHistory(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid since with nonexistent cluster, got %d", rr.Code)
	}
}

func TestHandleHealthHistory_LastCapped(t *testing.T) {
	s := newTestServer(t)

	s.mu.Lock()
	_ = s.getOrCreateClusterState("")
	s.mu.Unlock()

	// Request last=2000 -- should be capped to 1000.
	req := httptest.NewRequest(http.MethodGet, "/api/health/history?last=2000", nil)
	rr := httptest.NewRecorder()
	s.handleHealthHistory(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleHealthHistory(?last=2000) status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestHandleHealthHistory_SinceQuery(t *testing.T) {
	s := newTestServer(t)

	s.mu.Lock()
	_ = s.getOrCreateClusterState("")
	s.mu.Unlock()

	since := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, "/api/health/history?since="+since, nil)
	rr := httptest.NewRecorder()
	s.handleHealthHistory(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleHealthHistory(?since=...) status = %d, want %d", rr.Code, http.StatusOK)
	}
}
