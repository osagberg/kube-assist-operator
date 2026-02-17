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
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
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

const phaseDone = "done"

// ---------------------------------------------------------------------------
// Helper: create a minimal server for AI analysis tests
// ---------------------------------------------------------------------------

func newTestServer(t *testing.T) *Server {
	t.Helper()
	scheme := runtime.NewScheme()
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()
	return NewServer(datasource.NewKubernetes(cl), registry, ":8080")
}

// ---------------------------------------------------------------------------
// computeIssueHash tests
// ---------------------------------------------------------------------------

func TestComputeIssueHash(t *testing.T) {
	tests := []struct {
		name    string
		results map[string]*checker.CheckResult
	}{
		{
			name:    "nil map",
			results: nil,
		},
		{
			name:    "empty map",
			results: map[string]*checker.CheckResult{},
		},
		{
			name: "single checker no issues",
			results: map[string]*checker.CheckResult{
				"workloads": {Healthy: 3},
			},
		},
		{
			name: "single checker with issues",
			results: map[string]*checker.CheckResult{
				"workloads": {
					Healthy: 2,
					Issues: []checker.Issue{
						{Type: "CrashLoop", Severity: "critical", Namespace: "default", Resource: "deploy/app"},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := computeIssueHash(tt.results)
			if hash == "" {
				t.Error("computeIssueHash() returned empty string")
			}
			// Same input should produce same hash (deterministic)
			hash2 := computeIssueHash(tt.results)
			if hash != hash2 {
				t.Errorf("computeIssueHash() not deterministic: %q != %q", hash, hash2)
			}
		})
	}
}

func TestComputeIssueHash_SameIssuesSameHash(t *testing.T) {
	results := map[string]*checker.CheckResult{
		"workloads": {
			Healthy: 2,
			Issues: []checker.Issue{
				{Type: "CrashLoop", Severity: "critical", Namespace: "default", Resource: "deploy/app"},
				{Type: "OOMKilled", Severity: "warning", Namespace: "prod", Resource: "deploy/web"},
			},
		},
	}
	hash1 := computeIssueHash(results)
	hash2 := computeIssueHash(results)
	if hash1 != hash2 {
		t.Errorf("same issues produced different hashes: %q vs %q", hash1, hash2)
	}
}

func TestComputeIssueHash_DifferentIssuesDifferentHash(t *testing.T) {
	results1 := map[string]*checker.CheckResult{
		"workloads": {
			Issues: []checker.Issue{
				{Type: "CrashLoop", Severity: "critical", Namespace: "default", Resource: "deploy/app"},
			},
		},
	}
	results2 := map[string]*checker.CheckResult{
		"workloads": {
			Issues: []checker.Issue{
				{Type: "OOMKilled", Severity: "critical", Namespace: "default", Resource: "deploy/app"},
			},
		},
	}
	hash1 := computeIssueHash(results1)
	hash2 := computeIssueHash(results2)
	if hash1 == hash2 {
		t.Error("different issues should produce different hashes")
	}
}

func TestComputeIssueHash_OrderIndependent(t *testing.T) {
	// Issue keys within a checker are sorted, so order should not matter
	results1 := map[string]*checker.CheckResult{
		"workloads": {
			Issues: []checker.Issue{
				{Type: "CrashLoop", Severity: "critical", Namespace: "default", Resource: "deploy/a"},
				{Type: "OOMKilled", Severity: "warning", Namespace: "default", Resource: "deploy/b"},
			},
		},
	}
	results2 := map[string]*checker.CheckResult{
		"workloads": {
			Issues: []checker.Issue{
				{Type: "OOMKilled", Severity: "warning", Namespace: "default", Resource: "deploy/b"},
				{Type: "CrashLoop", Severity: "critical", Namespace: "default", Resource: "deploy/a"},
			},
		},
	}
	hash1 := computeIssueHash(results1)
	hash2 := computeIssueHash(results2)
	if hash1 != hash2 {
		t.Errorf("issue order should not affect hash: %q vs %q", hash1, hash2)
	}
}

func TestComputeIssueHash_SkipsErrorResults(t *testing.T) {
	results := map[string]*checker.CheckResult{
		"workloads": {
			Error: errors.New("timeout"),
			Issues: []checker.Issue{
				{Type: "CrashLoop", Severity: "critical", Namespace: "default", Resource: "deploy/app"},
			},
		},
	}
	// Result with error should be skipped, so hash should equal empty
	hashWithError := computeIssueHash(results)
	hashEmpty := computeIssueHash(map[string]*checker.CheckResult{})
	if hashWithError != hashEmpty {
		t.Errorf("error result should be skipped; hashWithError=%q, hashEmpty=%q", hashWithError, hashEmpty)
	}
}

func TestComputeIssueHash_MultipleCheckersSorted(t *testing.T) {
	// Checker names are sorted to ensure determinism across Go map iterations
	results := map[string]*checker.CheckResult{
		"zebra": {
			Issues: []checker.Issue{
				{Type: "Issue", Severity: "info", Namespace: "ns", Resource: "r"},
			},
		},
		"alpha": {
			Issues: []checker.Issue{
				{Type: "Issue", Severity: "info", Namespace: "ns", Resource: "r"},
			},
		},
	}
	hash1 := computeIssueHash(results)
	hash2 := computeIssueHash(results)
	if hash1 != hash2 {
		t.Errorf("multiple checkers should produce deterministic hash: %q vs %q", hash1, hash2)
	}
}

// ---------------------------------------------------------------------------
// snapshotAIEnhancements tests
// ---------------------------------------------------------------------------

func TestSnapshotAIEnhancements(t *testing.T) {
	tests := []struct {
		name       string
		results    map[string]*checker.CheckResult
		wantCount  int // total number of cached enhancements
		wantChecks func(t *testing.T, snap map[string]map[string]aiEnhancement)
	}{
		{
			name:      "nil results",
			results:   nil,
			wantCount: 0,
		},
		{
			name:      "empty results",
			results:   map[string]*checker.CheckResult{},
			wantCount: 0,
		},
		{
			name: "issues without AI suggestions are not cached",
			results: map[string]*checker.CheckResult{
				"workloads": {
					Issues: []checker.Issue{
						{
							Type:       "CrashLoop",
							Severity:   "critical",
							Namespace:  "default",
							Resource:   "deploy/app",
							Suggestion: "Check container logs",
						},
					},
				},
			},
			wantCount: 0,
		},
		{
			name: "issues with AI suggestions are cached",
			results: map[string]*checker.CheckResult{
				"workloads": {
					Issues: []checker.Issue{
						{
							Type:       "CrashLoop",
							Severity:   "critical",
							Namespace:  "default",
							Resource:   "deploy/app",
							Suggestion: "AI Analysis: The container is crashing due to memory limits",
							Metadata:   map[string]string{"aiRootCause": "OOM kill"},
						},
					},
				},
			},
			wantCount: 1,
			wantChecks: func(t *testing.T, snap map[string]map[string]aiEnhancement) {
				checkerSnap, ok := snap["workloads"]
				if !ok {
					t.Fatal("expected 'workloads' key in snapshot")
				}
				key := "CrashLoop|deploy/app|default"
				enh, ok := checkerSnap[key]
				if !ok {
					t.Fatalf("expected key %q in snapshot", key)
				}
				if enh.Suggestion != "AI Analysis: The container is crashing due to memory limits" {
					t.Errorf("suggestion = %q", enh.Suggestion)
				}
				if enh.RootCause != "OOM kill" {
					t.Errorf("rootCause = %q, want 'OOM kill'", enh.RootCause)
				}
			},
		},
		{
			name: "mixed issues: only AI-enhanced ones are cached",
			results: map[string]*checker.CheckResult{
				"workloads": {
					Issues: []checker.Issue{
						{
							Type:       "CrashLoop",
							Severity:   "critical",
							Namespace:  "default",
							Resource:   "deploy/app",
							Suggestion: "AI Analysis: OOM detected",
						},
						{
							Type:       "HighRestarts",
							Severity:   "warning",
							Namespace:  "default",
							Resource:   "deploy/web",
							Suggestion: "Check pod restarts",
						},
					},
				},
			},
			wantCount: 1,
		},
		{
			name: "error results are skipped",
			results: map[string]*checker.CheckResult{
				"workloads": {
					Error: errors.New("timeout"),
					Issues: []checker.Issue{
						{
							Type:       "CrashLoop",
							Namespace:  "default",
							Resource:   "deploy/app",
							Suggestion: "AI Analysis: some insight",
						},
					},
				},
			},
			wantCount: 0,
		},
		{
			name: "multiple checkers with AI suggestions",
			results: map[string]*checker.CheckResult{
				"workloads": {
					Issues: []checker.Issue{
						{
							Type:       "CrashLoop",
							Namespace:  "default",
							Resource:   "deploy/app",
							Suggestion: "AI Analysis: insight 1",
						},
					},
				},
				"resources": {
					Issues: []checker.Issue{
						{
							Type:       "PVCPending",
							Namespace:  "prod",
							Resource:   "pvc/data",
							Suggestion: "AI Analysis: insight 2",
						},
					},
				},
			},
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := snapshotAIEnhancements(tt.results)
			// Count total enhancements across all checkers
			total := 0
			for _, checkerSnap := range snap {
				total += len(checkerSnap)
			}
			if total != tt.wantCount {
				t.Errorf("total enhancements = %d, want %d", total, tt.wantCount)
			}
			if tt.wantChecks != nil {
				tt.wantChecks(t, snap)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// reapplyAIEnhancements tests
// ---------------------------------------------------------------------------

func TestReapplyAIEnhancements(t *testing.T) {
	tests := []struct {
		name         string
		results      map[string]*checker.CheckResult
		enhancements map[string]map[string]aiEnhancement
		wantChecks   func(t *testing.T, results map[string]*checker.CheckResult)
	}{
		{
			name: "nil enhancements does not panic",
			results: map[string]*checker.CheckResult{
				"workloads": {
					Issues: []checker.Issue{
						{Type: "CrashLoop", Namespace: "default", Resource: "deploy/app"},
					},
				},
			},
			enhancements: nil,
			wantChecks: func(t *testing.T, results map[string]*checker.CheckResult) {
				// Issues should remain unchanged
				if results["workloads"].Issues[0].Suggestion != "" {
					t.Errorf("suggestion should remain empty, got %q", results["workloads"].Issues[0].Suggestion)
				}
			},
		},
		{
			name: "matching issues get suggestions restored",
			results: map[string]*checker.CheckResult{
				"workloads": {
					Issues: []checker.Issue{
						{Type: "CrashLoop", Namespace: "default", Resource: "deploy/app", Suggestion: "basic"},
					},
				},
			},
			enhancements: map[string]map[string]aiEnhancement{
				"workloads": {
					"CrashLoop|deploy/app|default": {
						Suggestion: "AI Analysis: OOM detected",
						RootCause:  "Memory pressure",
					},
				},
			},
			wantChecks: func(t *testing.T, results map[string]*checker.CheckResult) {
				issue := results["workloads"].Issues[0]
				if issue.Suggestion != "AI Analysis: OOM detected" {
					t.Errorf("suggestion = %q, want 'AI Analysis: OOM detected'", issue.Suggestion)
				}
				if issue.Metadata["aiRootCause"] != "Memory pressure" {
					t.Errorf("aiRootCause = %q, want 'Memory pressure'", issue.Metadata["aiRootCause"])
				}
			},
		},
		{
			name: "non-matching issues remain unchanged",
			results: map[string]*checker.CheckResult{
				"workloads": {
					Issues: []checker.Issue{
						{Type: "HighRestarts", Namespace: "default", Resource: "deploy/web", Suggestion: "original"},
					},
				},
			},
			enhancements: map[string]map[string]aiEnhancement{
				"workloads": {
					"CrashLoop|deploy/app|default": {
						Suggestion: "AI Analysis: OOM detected",
					},
				},
			},
			wantChecks: func(t *testing.T, results map[string]*checker.CheckResult) {
				if results["workloads"].Issues[0].Suggestion != "original" {
					t.Errorf("non-matching issue suggestion should be unchanged, got %q", results["workloads"].Issues[0].Suggestion)
				}
			},
		},
		{
			name: "root cause creates metadata map if nil",
			results: map[string]*checker.CheckResult{
				"workloads": {
					Issues: []checker.Issue{
						{Type: "CrashLoop", Namespace: "default", Resource: "deploy/app"},
					},
				},
			},
			enhancements: map[string]map[string]aiEnhancement{
				"workloads": {
					"CrashLoop|deploy/app|default": {
						Suggestion: "AI Analysis: test",
						RootCause:  "root cause text",
					},
				},
			},
			wantChecks: func(t *testing.T, results map[string]*checker.CheckResult) {
				issue := results["workloads"].Issues[0]
				if issue.Metadata == nil {
					t.Fatal("expected Metadata map to be created")
				}
				if issue.Metadata["aiRootCause"] != "root cause text" {
					t.Errorf("aiRootCause = %q", issue.Metadata["aiRootCause"])
				}
			},
		},
		{
			name: "empty root cause does not set metadata",
			results: map[string]*checker.CheckResult{
				"workloads": {
					Issues: []checker.Issue{
						{Type: "CrashLoop", Namespace: "default", Resource: "deploy/app"},
					},
				},
			},
			enhancements: map[string]map[string]aiEnhancement{
				"workloads": {
					"CrashLoop|deploy/app|default": {
						Suggestion: "AI Analysis: test",
						RootCause:  "",
					},
				},
			},
			wantChecks: func(t *testing.T, results map[string]*checker.CheckResult) {
				issue := results["workloads"].Issues[0]
				if issue.Metadata != nil {
					t.Error("expected Metadata to remain nil when RootCause is empty")
				}
			},
		},
		{
			name: "checker with error is skipped",
			results: map[string]*checker.CheckResult{
				"workloads": {
					Error: errors.New("timeout"),
					Issues: []checker.Issue{
						{Type: "CrashLoop", Namespace: "default", Resource: "deploy/app"},
					},
				},
			},
			enhancements: map[string]map[string]aiEnhancement{
				"workloads": {
					"CrashLoop|deploy/app|default": {
						Suggestion: "AI Analysis: should not be applied",
					},
				},
			},
			wantChecks: func(t *testing.T, results map[string]*checker.CheckResult) {
				if results["workloads"].Issues[0].Suggestion != "" {
					t.Error("should not apply enhancements to checker with error")
				}
			},
		},
		{
			name: "enhancement for nonexistent checker is ignored",
			results: map[string]*checker.CheckResult{
				"workloads": {
					Issues: []checker.Issue{
						{Type: "CrashLoop", Namespace: "default", Resource: "deploy/app"},
					},
				},
			},
			enhancements: map[string]map[string]aiEnhancement{
				"nonexistent": {
					"CrashLoop|deploy/app|default": {
						Suggestion: "AI Analysis: should be ignored",
					},
				},
			},
			wantChecks: func(t *testing.T, results map[string]*checker.CheckResult) {
				if results["workloads"].Issues[0].Suggestion != "" {
					t.Error("should not apply enhancements from nonexistent checker")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reapplyAIEnhancements(tt.results, tt.enhancements)
			if tt.wantChecks != nil {
				tt.wantChecks(t, tt.results)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// applyCausalInsights tests
// ---------------------------------------------------------------------------

func TestApplyCausalInsights(t *testing.T) {
	tests := []struct {
		name       string
		cc         *causal.CausalContext
		insights   []ai.CausalGroupInsight
		wantChecks func(t *testing.T, cc *causal.CausalContext)
	}{
		{
			name:     "nil context does not panic",
			cc:       nil,
			insights: []ai.CausalGroupInsight{{GroupID: "group_0"}},
		},
		{
			name: "empty groups does not panic",
			cc: &causal.CausalContext{
				Groups: []causal.CausalGroup{},
			},
			insights: []ai.CausalGroupInsight{{GroupID: "group_0"}},
		},
		{
			name: "nil insights is a no-op",
			cc: &causal.CausalContext{
				Groups: []causal.CausalGroup{
					{ID: "test", Title: "Test"},
				},
			},
			insights: nil,
			wantChecks: func(t *testing.T, cc *causal.CausalContext) {
				if cc.Groups[0].AIEnhanced {
					t.Error("should not be AI enhanced with nil insights")
				}
			},
		},
		{
			name: "empty insights is a no-op",
			cc: &causal.CausalContext{
				Groups: []causal.CausalGroup{
					{ID: "test", Title: "Test"},
				},
			},
			insights: []ai.CausalGroupInsight{},
			wantChecks: func(t *testing.T, cc *causal.CausalContext) {
				if cc.Groups[0].AIEnhanced {
					t.Error("should not be AI enhanced with empty insights")
				}
			},
		},
		{
			name: "matching group index is enriched",
			cc: &causal.CausalContext{
				Groups: []causal.CausalGroup{
					{ID: "group-a", Title: "OOM in default"},
					{ID: "group-b", Title: "Network issue"},
				},
			},
			insights: []ai.CausalGroupInsight{
				{
					GroupID:      "group_1",
					AIRootCause:  "DNS resolution failure",
					AISuggestion: "Check CoreDNS pods",
					AISteps:      []string{"Step 1", "Step 2"},
				},
			},
			wantChecks: func(t *testing.T, cc *causal.CausalContext) {
				// group_0 should not be enriched
				if cc.Groups[0].AIEnhanced {
					t.Error("group_0 should not be enriched")
				}
				// group_1 should be enriched
				g := cc.Groups[1]
				if !g.AIEnhanced {
					t.Error("group_1 should be AI enhanced")
				}
				if g.AIRootCause != "DNS resolution failure" {
					t.Errorf("AIRootCause = %q", g.AIRootCause)
				}
				if g.AISuggestion != "Check CoreDNS pods" {
					t.Errorf("AISuggestion = %q", g.AISuggestion)
				}
				if len(g.AISteps) != 2 {
					t.Errorf("AISteps length = %d, want 2", len(g.AISteps))
				}
			},
		},
		{
			name: "out-of-bounds index does not panic",
			cc: &causal.CausalContext{
				Groups: []causal.CausalGroup{
					{ID: "only-group", Title: "Single"},
				},
			},
			insights: []ai.CausalGroupInsight{
				{GroupID: "group_5", AIRootCause: "should be ignored"},
			},
			wantChecks: func(t *testing.T, cc *causal.CausalContext) {
				if cc.Groups[0].AIEnhanced {
					t.Error("out-of-bounds insight should not enrich any group")
				}
			},
		},
		{
			name: "negative index does not panic",
			cc: &causal.CausalContext{
				Groups: []causal.CausalGroup{
					{ID: "test", Title: "Test"},
				},
			},
			insights: []ai.CausalGroupInsight{
				{GroupID: "group_-1", AIRootCause: "should be ignored"},
			},
			wantChecks: func(t *testing.T, cc *causal.CausalContext) {
				if cc.Groups[0].AIEnhanced {
					t.Error("negative index should not enrich any group")
				}
			},
		},
		{
			name: "invalid group ID format does not panic",
			cc: &causal.CausalContext{
				Groups: []causal.CausalGroup{
					{ID: "test", Title: "Test"},
				},
			},
			insights: []ai.CausalGroupInsight{
				{GroupID: "not_a_valid_id", AIRootCause: "should be ignored"},
			},
			wantChecks: func(t *testing.T, cc *causal.CausalContext) {
				if cc.Groups[0].AIEnhanced {
					t.Error("invalid group ID should not enrich any group")
				}
			},
		},
		{
			name: "multiple insights enrich multiple groups",
			cc: &causal.CausalContext{
				Groups: []causal.CausalGroup{
					{ID: "g0", Title: "Group 0"},
					{ID: "g1", Title: "Group 1"},
					{ID: "g2", Title: "Group 2"},
				},
			},
			insights: []ai.CausalGroupInsight{
				{GroupID: "group_0", AIRootCause: "root0", AISuggestion: "fix0"},
				{GroupID: "group_2", AIRootCause: "root2", AISuggestion: "fix2"},
			},
			wantChecks: func(t *testing.T, cc *causal.CausalContext) {
				if !cc.Groups[0].AIEnhanced {
					t.Error("group_0 should be enhanced")
				}
				if cc.Groups[1].AIEnhanced {
					t.Error("group_1 should NOT be enhanced")
				}
				if !cc.Groups[2].AIEnhanced {
					t.Error("group_2 should be enhanced")
				}
				if cc.Groups[0].AIRootCause != "root0" {
					t.Errorf("group_0 AIRootCause = %q", cc.Groups[0].AIRootCause)
				}
				if cc.Groups[2].AISuggestion != "fix2" {
					t.Errorf("group_2 AISuggestion = %q", cc.Groups[2].AISuggestion)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyCausalInsights(tt.cc, tt.insights)
			if tt.wantChecks != nil {
				tt.wantChecks(t, tt.cc)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// estimateCost tests
// ---------------------------------------------------------------------------

func TestEstimateCost(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		tokens   int
		wantZero bool
		wantMin  float64 // minimum expected cost (0 means just check > 0)
	}{
		{
			name:     "zero tokens returns zero",
			provider: "openai",
			tokens:   0,
			wantZero: true,
		},
		{
			name:     "negative tokens returns zero",
			provider: "openai",
			tokens:   -100,
			wantZero: true,
		},
		{
			name:     "openai positive tokens returns positive cost",
			provider: "openai",
			tokens:   1000,
			wantMin:  0.0001,
		},
		{
			name:     "anthropic positive tokens returns positive cost",
			provider: "anthropic",
			tokens:   1000,
			wantMin:  0.0001,
		},
		{
			name:     "unknown provider returns zero cost",
			provider: "unknown-provider",
			tokens:   1000,
			wantZero: true,
		},
		{
			name:     "noop provider returns zero cost",
			provider: "noop",
			tokens:   1000,
			wantZero: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := estimateCost(tt.provider, "", tt.tokens)
			if tt.wantZero {
				if cost != 0 {
					t.Errorf("estimateCost(%q, %d) = %f, want 0", tt.provider, tt.tokens, cost)
				}
			} else {
				if cost <= 0 {
					t.Errorf("estimateCost(%q, %d) = %f, want > 0", tt.provider, tt.tokens, cost)
				}
				if cost < tt.wantMin {
					t.Errorf("estimateCost(%q, %d) = %f, want >= %f", tt.provider, tt.tokens, cost, tt.wantMin)
				}
			}
		})
	}
}

func TestEstimateCost_Proportional(t *testing.T) {
	// Cost should be proportional to tokens
	cost1k := estimateCost("anthropic", "", 1000)
	cost2k := estimateCost("anthropic", "", 2000)
	if cost1k <= 0 {
		t.Skip("anthropic cost is zero, cannot test proportionality")
	}
	ratio := cost2k / cost1k
	if ratio < 1.99 || ratio > 2.01 {
		t.Errorf("cost should be proportional to tokens: cost1k=%f, cost2k=%f, ratio=%f", cost1k, cost2k, ratio)
	}
}

// ---------------------------------------------------------------------------
// broadcastPhase tests
// ---------------------------------------------------------------------------

func TestBroadcastPhase(t *testing.T) {
	tests := []struct {
		name      string
		clusterID string
		phase     string
		setup     func(s *Server)
		wantRecv  bool   // whether the client should receive an update
		wantPhase string // expected phase in received update
	}{
		{
			name:      "no cluster state does not panic",
			clusterID: "nonexistent",
			phase:     "checkers",
			setup:     func(s *Server) {},
			wantRecv:  false,
		},
		{
			name:      "cluster with nil latest does not panic",
			clusterID: "test",
			phase:     "ai",
			setup: func(s *Server) {
				s.mu.Lock()
				s.clusters["test"] = &clusterState{
					history:     history.New(10),
					issueStates: make(map[string]*IssueState),
				}
				s.mu.Unlock()
			},
			wantRecv: false,
		},
		{
			name:      "broadcasts to subscribed client",
			clusterID: "alpha",
			phase:     "causal",
			setup: func(s *Server) {
				s.mu.Lock()
				cs := s.getOrCreateClusterState("alpha")
				cs.latest = &HealthUpdate{
					Timestamp:  time.Now(),
					Namespaces: []string{"default"},
					Results:    map[string]CheckResult{},
				}
				s.mu.Unlock()
			},
			wantRecv:  true,
			wantPhase: "causal",
		},
		{
			name:      "creates AIStatus if nil",
			clusterID: "beta",
			phase:     phaseDone,
			setup: func(s *Server) {
				s.mu.Lock()
				cs := s.getOrCreateClusterState("beta")
				cs.latest = &HealthUpdate{
					Timestamp:  time.Now(),
					Namespaces: []string{"default"},
					Results:    map[string]CheckResult{},
					AIStatus:   nil, // explicitly nil
				}
				s.mu.Unlock()
			},
			wantRecv:  true,
			wantPhase: phaseDone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t)
			tt.setup(s)

			// Register a client subscribed to the target cluster
			clientCh := make(chan HealthUpdate, 5)
			s.mu.Lock()
			s.clients[clientCh] = tt.clusterID
			s.mu.Unlock()

			s.broadcastPhase(tt.clusterID, tt.phase)

			if tt.wantRecv {
				select {
				case update := <-clientCh:
					if update.AIStatus == nil {
						t.Fatal("expected AIStatus in broadcasted update")
					}
					if update.AIStatus.CheckPhase != tt.wantPhase {
						t.Errorf("CheckPhase = %q, want %q", update.AIStatus.CheckPhase, tt.wantPhase)
					}
				default:
					t.Error("expected client to receive phase update")
				}
			} else {
				select {
				case <-clientCh:
					t.Error("client should NOT have received an update")
				default:
					// Expected: no update
				}
			}

			// Cleanup
			s.mu.Lock()
			delete(s.clients, clientCh)
			s.mu.Unlock()
		})
	}
}

func TestBroadcastPhase_NoClients(t *testing.T) {
	s := newTestServer(t)
	s.mu.Lock()
	cs := s.getOrCreateClusterState("test")
	cs.latest = &HealthUpdate{
		Timestamp:  time.Now(),
		Namespaces: []string{"default"},
		Results:    map[string]CheckResult{},
	}
	s.mu.Unlock()

	// Should not panic even with no clients
	s.broadcastPhase("test", "ai")
}

func TestBroadcastPhase_FleetClient(t *testing.T) {
	// Client subscribed to "" (all clusters) should receive updates from any cluster
	s := newTestServer(t)
	s.mu.Lock()
	cs := s.getOrCreateClusterState("alpha")
	cs.latest = &HealthUpdate{
		Timestamp:  time.Now(),
		Namespaces: []string{"default"},
		Results:    map[string]CheckResult{},
	}
	s.mu.Unlock()

	clientCh := make(chan HealthUpdate, 5)
	s.mu.Lock()
	s.clients[clientCh] = "" // fleet subscription
	s.mu.Unlock()

	s.broadcastPhase("alpha", phaseDone)

	select {
	case update := <-clientCh:
		if update.AIStatus == nil || update.AIStatus.CheckPhase != phaseDone {
			t.Error("fleet client should receive phase update from any cluster")
		}
	default:
		t.Error("fleet client should have received update")
	}

	s.mu.Lock()
	delete(s.clients, clientCh)
	s.mu.Unlock()
}

func TestBroadcastPhase_DifferentClusterFilteredOut(t *testing.T) {
	// Client subscribed to "beta" should NOT receive updates from "alpha"
	s := newTestServer(t)
	s.mu.Lock()
	cs := s.getOrCreateClusterState("alpha")
	cs.latest = &HealthUpdate{
		Timestamp:  time.Now(),
		Namespaces: []string{"default"},
		Results:    map[string]CheckResult{},
	}
	s.mu.Unlock()

	clientCh := make(chan HealthUpdate, 5)
	s.mu.Lock()
	s.clients[clientCh] = "beta" // subscribed to different cluster
	s.mu.Unlock()

	s.broadcastPhase("alpha", phaseDone)

	select {
	case <-clientCh:
		t.Error("client subscribed to 'beta' should NOT receive 'alpha' updates")
	default:
		// Expected: no update
	}

	s.mu.Lock()
	delete(s.clients, clientCh)
	s.mu.Unlock()
}

// ---------------------------------------------------------------------------
// handleAIErrorForCluster tests
// ---------------------------------------------------------------------------

func TestHandleAIErrorForCluster(t *testing.T) {
	tests := []struct {
		name       string
		setupCS    func(cs *clusterState)
		aiErr      error
		totalCount int
		wantChecks func(t *testing.T, status *AIStatus, results map[string]*checker.CheckResult)
	}{
		{
			name: "error with existing cache reuses cached result",
			setupCS: func(cs *clusterState) {
				cs.lastAIResult = &AIStatus{
					IssuesEnhanced:  3,
					TotalIssueCount: 5,
				}
				cs.lastAIEnhancements = map[string]map[string]aiEnhancement{
					"workloads": {
						"CrashLoop|deploy/app|default": {
							Suggestion: "AI Analysis: cached suggestion",
							RootCause:  "cached root cause",
						},
					},
				}
				cs.lastCausalInsights = []ai.CausalGroupInsight{
					{GroupID: "group_0", AIRootCause: "cached insight"},
				}
			},
			aiErr:      errors.New("AI provider timeout"),
			totalCount: 5,
			wantChecks: func(t *testing.T, status *AIStatus, results map[string]*checker.CheckResult) {
				if !status.CacheHit {
					t.Error("expected CacheHit=true when falling back to cache")
				}
				if status.IssuesEnhanced != 3 {
					t.Errorf("IssuesEnhanced = %d, want 3", status.IssuesEnhanced)
				}
				if !containsStr(status.LastError, "retrying:") {
					t.Errorf("LastError = %q, want prefix 'retrying:'", status.LastError)
				}
				// Verify cached enhancements were reapplied
				issue := results["workloads"].Issues[0]
				if issue.Suggestion != "AI Analysis: cached suggestion" {
					t.Errorf("cached suggestion not reapplied: %q", issue.Suggestion)
				}
			},
		},
		{
			name: "error with no cache returns plain error status",
			setupCS: func(cs *clusterState) {
				// No cached result
			},
			aiErr:      errors.New("API key invalid"),
			totalCount: 7,
			wantChecks: func(t *testing.T, status *AIStatus, results map[string]*checker.CheckResult) {
				if status.CacheHit {
					t.Error("expected CacheHit=false when no cache exists")
				}
				if status.IssuesEnhanced != 0 {
					t.Errorf("IssuesEnhanced = %d, want 0", status.IssuesEnhanced)
				}
				if status.LastError != "API key invalid" {
					t.Errorf("LastError = %q, want 'API key invalid'", status.LastError)
				}
				if status.TotalIssueCount != 7 {
					t.Errorf("TotalIssueCount = %d, want 7", status.TotalIssueCount)
				}
			},
		},
		{
			name: "error with cached result that already has error does not reuse",
			setupCS: func(cs *clusterState) {
				cs.lastAIResult = &AIStatus{
					IssuesEnhanced: 2,
					LastError:      "previous error", // already has an error
				}
			},
			aiErr:      errors.New("another error"),
			totalCount: 4,
			wantChecks: func(t *testing.T, status *AIStatus, results map[string]*checker.CheckResult) {
				// Should NOT reuse cache because cachedResult.LastError is not empty
				if status.CacheHit {
					t.Error("expected CacheHit=false when cached result has error")
				}
				if status.LastError != "another error" {
					t.Errorf("LastError = %q, want 'another error'", status.LastError)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t)
			provider := ai.NewNoOpProvider()
			s.WithAI(provider, true)

			cs := &clusterState{
				history:     history.New(10),
				issueStates: make(map[string]*IssueState),
			}
			if tt.setupCS != nil {
				tt.setupCS(cs)
			}

			s.mu.Lock()
			s.clusters["test"] = cs
			s.mu.Unlock()

			results := map[string]*checker.CheckResult{
				"workloads": {
					Issues: []checker.Issue{
						{Type: "CrashLoop", Namespace: "default", Resource: "deploy/app"},
					},
				},
			}
			causalCtx := &causal.CausalContext{
				Groups: []causal.CausalGroup{
					{ID: "g0", Title: "Test group"},
				},
			}

			status := s.handleAIErrorForCluster(tt.aiErr, results, causalCtx, true, provider, tt.totalCount, cs)
			if status == nil {
				t.Fatal("expected non-nil AIStatus")
			}
			if !status.Enabled {
				t.Error("expected Enabled=true")
			}
			if status.Provider != providerNoop {
				t.Errorf("Provider = %q, want %q", status.Provider, providerNoop)
			}
			if status.CheckPhase != phaseDone {
				t.Errorf("CheckPhase = %q, want 'done'", status.CheckPhase)
			}

			tt.wantChecks(t, status, results)
		})
	}
}

// ---------------------------------------------------------------------------
// handleAITruncatedForCluster tests
// ---------------------------------------------------------------------------

func TestHandleAITruncatedForCluster(t *testing.T) {
	tests := []struct {
		name       string
		setupCS    func(cs *clusterState)
		aiResp     *ai.AnalysisResponse
		totalCount int
		tokens     int
		wantChecks func(t *testing.T, status *AIStatus, results map[string]*checker.CheckResult)
	}{
		{
			name: "truncated with existing cache reuses cached result",
			setupCS: func(cs *clusterState) {
				cs.lastAIResult = &AIStatus{
					IssuesEnhanced:  4,
					TotalIssueCount: 6,
				}
				cs.lastAIEnhancements = map[string]map[string]aiEnhancement{
					"workloads": {
						"CrashLoop|deploy/app|default": {
							Suggestion: "AI Analysis: cached from previous run",
						},
					},
				}
			},
			aiResp:     &ai.AnalysisResponse{Truncated: true},
			totalCount: 6,
			tokens:     500,
			wantChecks: func(t *testing.T, status *AIStatus, results map[string]*checker.CheckResult) {
				if !status.CacheHit {
					t.Error("expected CacheHit=true")
				}
				if status.IssuesEnhanced != 4 {
					t.Errorf("IssuesEnhanced = %d, want 4", status.IssuesEnhanced)
				}
				if status.LastError != "" {
					t.Errorf("LastError should be empty on cache hit, got %q", status.LastError)
				}
				// Verify cached enhancements reapplied
				issue := results["workloads"].Issues[0]
				if issue.Suggestion != "AI Analysis: cached from previous run" {
					t.Errorf("cached suggestion not reapplied: %q", issue.Suggestion)
				}
			},
		},
		{
			name: "truncated without cache returns truncation reason",
			setupCS: func(cs *clusterState) {
				// No cached result
			},
			aiResp:     &ai.AnalysisResponse{Truncated: true},
			totalCount: 8,
			tokens:     200,
			wantChecks: func(t *testing.T, status *AIStatus, results map[string]*checker.CheckResult) {
				if status.CacheHit {
					t.Error("expected CacheHit=false")
				}
				if status.IssuesEnhanced != 0 {
					t.Errorf("IssuesEnhanced = %d, want 0", status.IssuesEnhanced)
				}
				if status.TotalIssueCount != 8 {
					t.Errorf("TotalIssueCount = %d, want 8", status.TotalIssueCount)
				}
				if !containsStr(status.LastError, "truncated") {
					t.Errorf("LastError = %q, want to contain 'truncated'", status.LastError)
				}
			},
		},
		{
			name: "parse failed without cache returns parse error reason",
			setupCS: func(cs *clusterState) {
				// No cached result
			},
			aiResp:     &ai.AnalysisResponse{ParseFailed: true},
			totalCount: 3,
			tokens:     100,
			wantChecks: func(t *testing.T, status *AIStatus, results map[string]*checker.CheckResult) {
				if !containsStr(status.LastError, "parsed") {
					t.Errorf("LastError = %q, want to contain 'parsed'", status.LastError)
				}
			},
		},
		{
			name: "nil aiResp without cache returns generic reason",
			setupCS: func(cs *clusterState) {
				// No cached result
			},
			aiResp:     nil,
			totalCount: 2,
			tokens:     50,
			wantChecks: func(t *testing.T, status *AIStatus, results map[string]*checker.CheckResult) {
				if !containsStr(status.LastError, "no suggestions") {
					t.Errorf("LastError = %q, want to contain 'no suggestions'", status.LastError)
				}
			},
		},
		{
			name: "cached result with zero enhanced does not reuse",
			setupCS: func(cs *clusterState) {
				cs.lastAIResult = &AIStatus{
					IssuesEnhanced:  0, // previous run also had zero
					TotalIssueCount: 5,
				}
			},
			aiResp:     &ai.AnalysisResponse{Truncated: true},
			totalCount: 5,
			tokens:     300,
			wantChecks: func(t *testing.T, status *AIStatus, results map[string]*checker.CheckResult) {
				// Should NOT reuse cache because IssuesEnhanced is 0
				if status.CacheHit {
					t.Error("expected CacheHit=false when cached result has 0 enhanced")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t)
			provider := ai.NewNoOpProvider()
			s.WithAI(provider, true)

			cs := &clusterState{
				history:     history.New(10),
				issueStates: make(map[string]*IssueState),
			}
			if tt.setupCS != nil {
				tt.setupCS(cs)
			}

			s.mu.Lock()
			s.clusters["test"] = cs
			s.mu.Unlock()

			results := map[string]*checker.CheckResult{
				"workloads": {
					Issues: []checker.Issue{
						{Type: "CrashLoop", Namespace: "default", Resource: "deploy/app"},
					},
				},
			}
			causalCtx := &causal.CausalContext{
				Groups: []causal.CausalGroup{
					{ID: "g0", Title: "Test group"},
				},
			}

			status := s.handleAITruncatedForCluster(results, causalCtx, true, provider, tt.totalCount, tt.tokens, cs, tt.aiResp)
			if status == nil {
				t.Fatal("expected non-nil AIStatus")
			}
			if !status.Enabled {
				t.Error("expected Enabled=true")
			}
			if status.Provider != providerNoop {
				t.Errorf("Provider = %q, want %q", status.Provider, providerNoop)
			}
			if status.CheckPhase != phaseDone {
				t.Errorf("CheckPhase = %q, want 'done'", status.CheckPhase)
			}

			tt.wantChecks(t, status, results)
		})
	}
}

// ---------------------------------------------------------------------------
// aiTruncationReason tests
// ---------------------------------------------------------------------------

func TestAITruncationReason(t *testing.T) {
	tests := []struct {
		name    string
		resp    *ai.AnalysisResponse
		wantSub string // substring expected in the result
	}{
		{
			name:    "truncated response",
			resp:    &ai.AnalysisResponse{Truncated: true},
			wantSub: "truncated",
		},
		{
			name:    "parse failed response",
			resp:    &ai.AnalysisResponse{ParseFailed: true},
			wantSub: "parsed",
		},
		{
			name:    "nil response",
			resp:    nil,
			wantSub: "no suggestions",
		},
		{
			name:    "normal response with neither flag",
			resp:    &ai.AnalysisResponse{},
			wantSub: "no suggestions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := aiTruncationReason(tt.resp)
			if !containsStr(reason, tt.wantSub) {
				t.Errorf("aiTruncationReason() = %q, want substring %q", reason, tt.wantSub)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// toCausalAnalysisContext tests
// ---------------------------------------------------------------------------

func TestToCausalAnalysisContext(t *testing.T) {
	tests := []struct {
		name       string
		cc         *causal.CausalContext
		wantNil    bool
		wantGroups int
	}{
		{
			name:    "nil context returns nil",
			cc:      nil,
			wantNil: true,
		},
		{
			name: "empty groups returns nil",
			cc: &causal.CausalContext{
				Groups: []causal.CausalGroup{},
			},
			wantNil: true,
		},
		{
			name: "single group is converted",
			cc: &causal.CausalContext{
				Groups: []causal.CausalGroup{
					{
						ID:         "g1",
						Title:      "OOM in default",
						Rule:       "oom-correlation",
						RootCause:  "Memory leak",
						Severity:   "critical",
						Confidence: 0.95,
						Events: []causal.TimelineEvent{
							{
								Checker: "workloads",
								Issue: checker.Issue{
									Namespace: "default",
									Resource:  "deploy/app",
								},
							},
						},
					},
				},
				UncorrelatedCount: 2,
				TotalIssues:       5,
			},
			wantGroups: 1,
		},
		{
			name: "multiple groups with multiple events",
			cc: &causal.CausalContext{
				Groups: []causal.CausalGroup{
					{
						Rule:       "oom",
						Title:      "OOM group",
						Severity:   "critical",
						Confidence: 0.9,
						Events: []causal.TimelineEvent{
							{Issue: checker.Issue{Namespace: "ns1", Resource: "deploy/a"}},
							{Issue: checker.Issue{Namespace: "ns1", Resource: "deploy/b"}},
						},
					},
					{
						Rule:       "net",
						Title:      "Network group",
						Severity:   "warning",
						Confidence: 0.7,
						Events: []causal.TimelineEvent{
							{Issue: checker.Issue{Namespace: "ns2", Resource: "svc/x"}},
						},
					},
				},
				UncorrelatedCount: 1,
				TotalIssues:       10,
			},
			wantGroups: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toCausalAnalysisContext(tt.cc)
			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil, got %+v", result)
				}
				return
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}
			if len(result.Groups) != tt.wantGroups {
				t.Errorf("groups = %d, want %d", len(result.Groups), tt.wantGroups)
			}
			if result.UncorrelatedCount != tt.cc.UncorrelatedCount {
				t.Errorf("UncorrelatedCount = %d, want %d", result.UncorrelatedCount, tt.cc.UncorrelatedCount)
			}
			if result.TotalIssues != tt.cc.TotalIssues {
				t.Errorf("TotalIssues = %d, want %d", result.TotalIssues, tt.cc.TotalIssues)
			}
		})
	}
}

func TestToCausalAnalysisContext_GroupFieldMapping(t *testing.T) {
	cc := &causal.CausalContext{
		Groups: []causal.CausalGroup{
			{
				Rule:       "oom-correlation",
				Title:      "OOM in default",
				RootCause:  "Memory leak in container",
				Severity:   "critical",
				Confidence: 0.95,
				Events: []causal.TimelineEvent{
					{Issue: checker.Issue{Namespace: "default", Resource: "deploy/app"}},
					{Issue: checker.Issue{Namespace: "default", Resource: "deploy/web"}},
				},
			},
		},
	}

	result := toCausalAnalysisContext(cc)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	g := result.Groups[0]
	if g.Rule != "oom-correlation" {
		t.Errorf("Rule = %q", g.Rule)
	}
	if g.Title != "OOM in default" {
		t.Errorf("Title = %q", g.Title)
	}
	if g.RootCause != "Memory leak in container" {
		t.Errorf("RootCause = %q", g.RootCause)
	}
	if g.Severity != "critical" {
		t.Errorf("Severity = %q", g.Severity)
	}
	if g.Confidence != 0.95 {
		t.Errorf("Confidence = %f", g.Confidence)
	}
	if len(g.Resources) != 2 {
		t.Fatalf("Resources = %d, want 2", len(g.Resources))
	}
	sort.Strings(g.Resources)
	if g.Resources[0] != "default/deploy/app" {
		t.Errorf("Resources[0] = %q", g.Resources[0])
	}
	if g.Resources[1] != "default/deploy/web" {
		t.Errorf("Resources[1] = %q", g.Resources[1])
	}
}

// ---------------------------------------------------------------------------
// snapshotAIEnhancements + reapplyAIEnhancements round-trip test
// ---------------------------------------------------------------------------

func TestSnapshotAndReapply_RoundTrip(t *testing.T) {
	// Build results with AI-enhanced issues
	originalSuggestion := "AI Analysis: The container is OOM-killed due to memory limits"
	originalRootCause := "Memory pressure from upstream traffic spike"

	results := map[string]*checker.CheckResult{
		"workloads": {
			Healthy: 3,
			Issues: []checker.Issue{
				{
					Type:       "CrashLoop",
					Severity:   "critical",
					Namespace:  "default",
					Resource:   "deploy/app",
					Suggestion: originalSuggestion,
					Metadata:   map[string]string{"aiRootCause": originalRootCause},
				},
				{
					Type:       "HighRestarts",
					Severity:   "warning",
					Namespace:  "default",
					Resource:   "deploy/web",
					Suggestion: "Check restart count", // not AI-enhanced
				},
			},
		},
	}

	// Snapshot
	snap := snapshotAIEnhancements(results)

	// Simulate fresh results (without AI enhancements)
	freshResults := map[string]*checker.CheckResult{
		"workloads": {
			Healthy: 3,
			Issues: []checker.Issue{
				{
					Type:       "CrashLoop",
					Severity:   "critical",
					Namespace:  "default",
					Resource:   "deploy/app",
					Suggestion: "basic suggestion", // reset
				},
				{
					Type:       "HighRestarts",
					Severity:   "warning",
					Namespace:  "default",
					Resource:   "deploy/web",
					Suggestion: "Check restart count",
				},
			},
		},
	}

	// Reapply
	reapplyAIEnhancements(freshResults, snap)

	// Verify CrashLoop issue got AI suggestion restored
	issue0 := freshResults["workloads"].Issues[0]
	if issue0.Suggestion != originalSuggestion {
		t.Errorf("after reapply: suggestion = %q, want %q", issue0.Suggestion, originalSuggestion)
	}
	if issue0.Metadata["aiRootCause"] != originalRootCause {
		t.Errorf("after reapply: aiRootCause = %q, want %q", issue0.Metadata["aiRootCause"], originalRootCause)
	}

	// Verify HighRestarts issue was NOT modified (it wasn't AI-enhanced)
	issue1 := freshResults["workloads"].Issues[1]
	if issue1.Suggestion != "Check restart count" {
		t.Errorf("non-AI issue should be unchanged, got %q", issue1.Suggestion)
	}
}

// ---------------------------------------------------------------------------
// buildHandler tests
// ---------------------------------------------------------------------------

func TestBuildHandler_RoutesExist(t *testing.T) {
	s := newTestServer(t)
	handler := s.buildHandler()

	if handler == nil {
		t.Fatal("buildHandler() returned nil")
	}

	// Verify that API routes are registered by making requests
	// (without auth, we expect either 200 or actual handler response, not 404)
	routes := []string{
		"/api/health",
		"/api/health/history",
		"/api/check",
		"/api/settings/ai",
		"/api/causal/groups",
		"/api/explain",
		"/api/prediction/trend",
		"/api/clusters",
		"/api/fleet/summary",
		"/api/settings/ai/catalog",
		"/api/troubleshoot",
		"/api/capabilities",
		"/api/issues/acknowledge",
		"/api/issues/snooze",
		"/api/issue-states",
	}

	for _, route := range routes {
		t.Run(route, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, route, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			// Should NOT be 404 (routes must be registered)
			if rr.Code == http.StatusNotFound {
				t.Errorf("route %s returned 404 -- not registered", route)
			}
		})
	}
}

func TestBuildHandler_SecurityHeaders(t *testing.T) {
	s := newTestServer(t)
	handler := s.buildHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Verify security headers are applied to API routes
	if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("expected X-Content-Type-Options: nosniff")
	}
	if rr.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("expected X-Frame-Options: DENY")
	}
}

func TestBuildHandler_SPAFallback(t *testing.T) {
	s := newTestServer(t)
	handler := s.buildHandler()

	// Request for a non-existent path should get SPA fallback (index.html)
	req := httptest.NewRequest(http.MethodGet, "/some/spa/route", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("SPA fallback status = %d, want 200", rr.Code)
	}
	ct := rr.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("SPA fallback Content-Type = %q, want text/html", ct)
	}
}

// ---------------------------------------------------------------------------
// runAIAnalysisForCluster tests
// ---------------------------------------------------------------------------

func TestRunAIAnalysisForCluster_DisabledProvider(t *testing.T) {
	s := newTestServer(t)

	cs := &clusterState{
		history:     history.New(10),
		issueStates: make(map[string]*IssueState),
	}
	s.mu.Lock()
	s.clusters["test"] = cs
	// Counter is now incremented by the caller (runCheckForCluster), simulate it
	cs.checkCounter++
	s.mu.Unlock()

	results := map[string]*checker.CheckResult{
		"workloads": {Issues: []checker.Issue{{Type: "CrashLoop"}}},
	}
	causalCtx := &causal.CausalContext{}
	checkCtx := &checker.CheckContext{}

	// Call with enabled=false
	status := s.runAIAnalysisForCluster(context.Background(), results, causalCtx, checkCtx, false, nil, cs)
	if status != nil {
		t.Errorf("expected nil status when AI is disabled, got %+v", status)
	}

	// checkCounter should remain at 1 (not incremented by runAIAnalysisForCluster)
	s.mu.RLock()
	counter := cs.checkCounter
	s.mu.RUnlock()
	if counter != 1 {
		t.Errorf("checkCounter = %d, want 1", counter)
	}
}

func TestRunAIAnalysisForCluster_NilProvider(t *testing.T) {
	s := newTestServer(t)

	cs := &clusterState{
		history:     history.New(10),
		issueStates: make(map[string]*IssueState),
	}
	s.mu.Lock()
	s.clusters["test"] = cs
	s.mu.Unlock()

	results := map[string]*checker.CheckResult{}
	causalCtx := &causal.CausalContext{}
	checkCtx := &checker.CheckContext{}

	status := s.runAIAnalysisForCluster(context.Background(), results, causalCtx, checkCtx, true, nil, cs)
	if status != nil {
		t.Errorf("expected nil status when provider is nil, got %+v", status)
	}
}

func TestRunAIAnalysisForCluster_CacheHit(t *testing.T) {
	s := newTestServer(t)
	provider := ai.NewNoOpProvider()
	s.WithAI(provider, true)

	results := map[string]*checker.CheckResult{
		"workloads": {
			Healthy: 2,
			Issues: []checker.Issue{
				{Type: "CrashLoop", Severity: "critical", Namespace: "default", Resource: "deploy/app"},
			},
		},
	}
	issueHash := computeIssueHash(results)

	cs := &clusterState{
		history:         history.New(10),
		issueStates:     make(map[string]*IssueState),
		lastAICacheHash: issueHash,
		lastAIResult: &AIStatus{
			Enabled:         true,
			Provider:        providerNoop,
			IssuesEnhanced:  1,
			TotalIssueCount: 1,
		},
		lastAIEnhancements: map[string]map[string]aiEnhancement{
			"workloads": {
				"CrashLoop|deploy/app|default": {
					Suggestion: "AI Analysis: cached",
				},
			},
		},
	}
	s.mu.Lock()
	s.clusters["test"] = cs
	s.mu.Unlock()

	causalCtx := &causal.CausalContext{}
	checkCtx := &checker.CheckContext{
		AIEnabled:  true,
		AIProvider: provider,
	}

	status := s.runAIAnalysisForCluster(context.Background(), results, causalCtx, checkCtx, true, provider, cs)
	if status == nil {
		t.Fatal("expected non-nil status on cache hit")
	}
	if !status.CacheHit {
		t.Error("expected CacheHit=true")
	}
	if status.IssuesEnhanced != 1 {
		t.Errorf("IssuesEnhanced = %d, want 1", status.IssuesEnhanced)
	}

	// Verify cached enhancement was reapplied
	issue := results["workloads"].Issues[0]
	if issue.Suggestion != "AI Analysis: cached" {
		t.Errorf("cached suggestion not reapplied: %q", issue.Suggestion)
	}
}

// ---------------------------------------------------------------------------
// severityRank tests (already partially tested via computeCheckerOperationalCounts,
// but adding explicit unit tests for completeness)
// ---------------------------------------------------------------------------

func TestSeverityRank(t *testing.T) {
	tests := []struct {
		severity string
		want     int
	}{
		{checker.SeverityCritical, 3},
		{checker.SeverityWarning, 2},
		{checker.SeverityInfo, 1},
		{"", 0},
		{"unknown", 0},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("severity=%q", tt.severity), func(t *testing.T) {
			got := severityRank(tt.severity)
			if got != tt.want {
				t.Errorf("severityRank(%q) = %d, want %d", tt.severity, got, tt.want)
			}
		})
	}

	// Verify ordering
	if severityRank(checker.SeverityCritical) <= severityRank(checker.SeverityWarning) {
		t.Error("critical should rank higher than warning")
	}
	if severityRank(checker.SeverityWarning) <= severityRank(checker.SeverityInfo) {
		t.Error("warning should rank higher than info")
	}
}

// ---------------------------------------------------------------------------
// computeOperationalHealthScore edge cases
// ---------------------------------------------------------------------------

func TestComputeOperationalHealthScore_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		checked  int
		critical int
		want     float64
	}{
		{"zero checked returns 100", 0, 0, 100},
		{"negative checked returns 100", -1, 0, 100},
		{"all critical returns 0", 5, 5, 0},
		{"no critical returns 100", 5, 0, 100},
		{"negative critical clamped to 0", 5, -1, 100},
		{"critical exceeds checked clamped", 3, 10, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeOperationalHealthScore(tt.checked, tt.critical)
			if got != tt.want {
				t.Errorf("computeOperationalHealthScore(%d, %d) = %f, want %f", tt.checked, tt.critical, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Async AI enrichment test providers
// ---------------------------------------------------------------------------

// errorProvider always returns an error.
type errorProvider struct {
	err  error
	name string
}

func (p *errorProvider) Name() string    { return p.name }
func (p *errorProvider) Available() bool { return true }
func (p *errorProvider) Analyze(_ context.Context, _ ai.AnalysisRequest) (*ai.AnalysisResponse, error) {
	return nil, p.err
}

// insightProvider returns one enhanced issue plus one causal insight.
type insightProvider struct {
	name string
}

func (p *insightProvider) Name() string {
	if p.name == "" {
		return "insight"
	}
	return p.name
}
func (p *insightProvider) Available() bool { return true }
func (p *insightProvider) Analyze(_ context.Context, _ ai.AnalysisRequest) (*ai.AnalysisResponse, error) {
	return &ai.AnalysisResponse{
		EnhancedSuggestions: map[string]ai.EnhancedSuggestion{
			"issue_0": {
				Suggestion: "Increase memory limits",
				RootCause:  "Pod memory pressure",
				Confidence: 0.95,
			},
		},
		CausalInsights: []ai.CausalGroupInsight{
			{
				GroupID:      "group_0",
				AIRootCause:  "Node memory contention",
				AISuggestion: "Reschedule workload to a larger node pool",
				AISteps:      []string{"Increase requests", "Roll deployment"},
				Confidence:   0.9,
			},
		},
		TokensUsed: 42,
	}, nil
}

// ---------------------------------------------------------------------------
// Async AI enrichment integration tests
// ---------------------------------------------------------------------------

func newTestServerWithQueue(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t)
	s.enrichQueue = newEnrichmentQueue(s.enrichQueueSize)
	return s
}

func TestAsyncAI_CacheHitSynchronous(t *testing.T) {
	// AC-7: second check with same hash should get AI from cache with no Pending
	s := newTestServerWithQueue(t)
	provider := ai.NewNoOpProvider()
	s.WithAI(provider, true)

	results := map[string]*checker.CheckResult{
		"workloads": {
			Healthy: 2,
			Issues: []checker.Issue{
				{Type: "CrashLoop", Severity: "critical", Namespace: "default", Resource: "deploy/app"},
			},
		},
	}
	issueHash := computeIssueHash(results)

	cs := &clusterState{
		history:         history.New(10),
		issueStates:     make(map[string]*IssueState),
		lastAICacheHash: issueHash,
		lastAIResult: &AIStatus{
			Enabled:         true,
			Provider:        providerNoop,
			IssuesEnhanced:  1,
			TotalIssueCount: 1,
		},
		lastAIEnhancements: map[string]map[string]aiEnhancement{
			"workloads": {
				"CrashLoop|deploy/app|default": {
					Suggestion: "AI Analysis: cached from previous run",
				},
			},
		},
	}

	s.mu.Lock()
	s.clusters["test"] = cs
	cs.checkCounter++
	s.mu.Unlock()

	causalCtx := &causal.CausalContext{}
	checkCtx := &checker.CheckContext{AIEnabled: true, AIProvider: provider}

	status := s.runAIAnalysisForCluster(context.Background(), results, causalCtx, checkCtx, true, provider, cs)
	if status == nil {
		t.Fatal("expected non-nil status on cache hit")
	}
	if !status.CacheHit {
		t.Error("expected CacheHit=true")
	}
	if status.Pending {
		t.Error("cache hit should NOT have Pending=true")
	}
	if status.IssuesEnhanced != 1 {
		t.Errorf("IssuesEnhanced = %d, want 1", status.IssuesEnhanced)
	}
}

func TestAsyncAI_FailureNonBlocking(t *testing.T) {
	// AC-5: error provider should result in degraded status, not blocking
	s := newTestServerWithQueue(t)
	ctx := t.Context()

	errProv := &errorProvider{err: errors.New("API key invalid"), name: "test-err"}
	s.WithAI(errProv, true)

	s.enrichQueue.Run(ctx, s.handleEnrichmentResult)

	cs := &clusterState{
		history:     history.New(10),
		issueStates: make(map[string]*IssueState),
	}
	s.mu.Lock()
	cs.checkCounter = 1
	cs.aiConfigVersion = configVersionHash("test-err", "")
	cs.currentIssueHash = "test-hash"
	cs.latest = &HealthUpdate{
		Timestamp: time.Now(),
		Results:   map[string]CheckResult{},
		AIStatus: &AIStatus{
			Enabled:  true,
			Provider: "test-err",
			Pending:  true,
		},
	}
	s.clusters["test"] = cs
	s.mu.Unlock()

	// Subscribe a client to receive broadcasts
	clientCh := make(chan HealthUpdate, 5)
	s.mu.Lock()
	s.clients[clientCh] = "test"
	s.mu.Unlock()

	// Enqueue an enrichment that will fail
	req := enrichmentRequest{
		clusterID:     "test",
		generation:    1,
		issueHash:     "test-hash",
		configVersion: configVersionHash("test-err", ""),
		results:       map[string]*checker.CheckResult{},
		checkCtx:      &checker.CheckContext{AIEnabled: true, AIProvider: errProv},
		provider:      errProv,
		aiModel:       "",
		enqueuedAt:    time.Now(),
	}
	s.enrichQueue.Enqueue(req)

	// Wait for the error to be processed and broadcast
	select {
	case update := <-clientCh:
		if update.AIStatus == nil {
			t.Fatal("expected AIStatus in broadcast")
		}
		if update.AIStatus.Pending {
			t.Error("Pending should be false after error")
		}
		if update.AIStatus.LastError == "" {
			t.Error("expected LastError to be set")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for error broadcast")
	}

	s.mu.Lock()
	delete(s.clients, clientCh)
	s.mu.Unlock()
}

func TestAsyncAI_QueueFullSetsError(t *testing.T) {
	// AC-1/5: fill queue, verify LastError on drop
	s := newTestServer(t)
	s.enrichQueueSize = 1
	s.enrichQueue = newEnrichmentQueue(1)
	// Don't start worker — channel stays full

	provider := ai.NewNoOpProvider()
	s.WithAI(provider, true)

	cs := &clusterState{
		history:     history.New(10),
		issueStates: make(map[string]*IssueState),
	}
	s.mu.Lock()
	s.clusters["test"] = cs
	s.mu.Unlock()

	// Fill the queue
	s.enrichQueue.Enqueue(enrichmentRequest{clusterID: "other", generation: 1})

	// This second enqueue (different cluster) should fail
	cs.checkCounter = 1
	cs.latest = &HealthUpdate{
		Timestamp: time.Now(),
		Results:   map[string]CheckResult{},
		AIStatus: &AIStatus{
			Enabled: true,
			Pending: true,
		},
	}

	ok := s.enrichQueue.Enqueue(enrichmentRequest{clusterID: "test", generation: 1})
	if ok {
		t.Error("enqueue should fail when channel is full with different clusters")
	}
}

func TestAsyncAI_StaleEnrichmentDiscarded(t *testing.T) {
	// Trigger enrichment, advance generation before worker finishes, verify discard
	s := newTestServerWithQueue(t)
	ctx := t.Context()

	provider := ai.NewNoOpProvider()
	s.WithAI(provider, true)

	s.enrichQueue.Run(ctx, s.handleEnrichmentResult)

	cs := &clusterState{
		history:     history.New(10),
		issueStates: make(map[string]*IssueState),
	}
	s.mu.Lock()
	// Set generation to 5, but enqueue with generation 3 — should be stale
	cs.checkCounter = 5
	cs.currentIssueHash = "current-hash"
	cs.aiConfigVersion = configVersionHash("noop", "")
	cs.latest = &HealthUpdate{
		Timestamp: time.Now(),
		Results:   map[string]CheckResult{},
		AIStatus:  &AIStatus{Enabled: true, Provider: "noop"},
	}
	s.clusters["test"] = cs
	s.mu.Unlock()

	req := enrichmentRequest{
		clusterID:     "test",
		generation:    3, // stale — current is 5
		issueHash:     "current-hash",
		configVersion: configVersionHash("noop", ""),
		results:       map[string]*checker.CheckResult{},
		checkCtx:      &checker.CheckContext{AIEnabled: true, AIProvider: provider},
		provider:      provider,
		enqueuedAt:    time.Now(),
	}
	s.enrichQueue.Enqueue(req)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// AI cache should NOT have been updated (stale enrichment discarded)
	s.mu.RLock()
	if cs.lastAICacheHash != "" {
		t.Errorf("stale enrichment should not update cache, got hash %q", cs.lastAICacheHash)
	}
	s.mu.RUnlock()
}

func TestAsyncAI_ConfigChangeDiscards(t *testing.T) {
	// Trigger enrichment, reconfigure provider before worker finishes, verify discard
	s := newTestServerWithQueue(t)
	ctx := t.Context()

	provider := ai.NewNoOpProvider()
	s.WithAI(provider, true)

	s.enrichQueue.Run(ctx, s.handleEnrichmentResult)

	oldConfigVersion := configVersionHash("noop", "old-model")
	newConfigVersion := configVersionHash("noop", "new-model")

	cs := &clusterState{
		history:     history.New(10),
		issueStates: make(map[string]*IssueState),
	}
	s.mu.Lock()
	cs.checkCounter = 1
	cs.currentIssueHash = "hash1"
	cs.aiConfigVersion = newConfigVersion // config was updated after enqueue
	cs.latest = &HealthUpdate{
		Timestamp: time.Now(),
		Results:   map[string]CheckResult{},
		AIStatus:  &AIStatus{Enabled: true},
	}
	s.clusters["test"] = cs
	s.mu.Unlock()

	req := enrichmentRequest{
		clusterID:     "test",
		generation:    1,
		issueHash:     "hash1",
		configVersion: oldConfigVersion, // outdated config version
		results:       map[string]*checker.CheckResult{},
		checkCtx:      &checker.CheckContext{AIEnabled: true, AIProvider: provider},
		provider:      provider,
		enqueuedAt:    time.Now(),
	}
	s.enrichQueue.Enqueue(req)

	time.Sleep(100 * time.Millisecond)

	s.mu.RLock()
	if cs.lastAICacheHash != "" {
		t.Error("config-changed enrichment should not update cache")
	}
	s.mu.RUnlock()
}

func TestAsyncAI_NilLatestSafe(t *testing.T) {
	// Enrichment arrives for cluster with no baseline yet — no panic
	s := newTestServerWithQueue(t)
	ctx := t.Context()

	provider := ai.NewNoOpProvider()
	s.WithAI(provider, true)

	s.enrichQueue.Run(ctx, s.handleEnrichmentResult)

	cs := &clusterState{
		history:          history.New(10),
		issueStates:      make(map[string]*IssueState),
		checkCounter:     1,
		currentIssueHash: "hash",
		aiConfigVersion:  configVersionHash("noop", ""),
		latest:           nil, // no baseline yet
	}
	s.mu.Lock()
	s.clusters["test"] = cs
	s.mu.Unlock()

	req := enrichmentRequest{
		clusterID:     "test",
		generation:    1,
		issueHash:     "hash",
		configVersion: configVersionHash("noop", ""),
		results:       map[string]*checker.CheckResult{},
		checkCtx:      &checker.CheckContext{AIEnabled: true, AIProvider: provider},
		provider:      provider,
		enqueuedAt:    time.Now(),
	}

	// Should not panic
	s.enrichQueue.Enqueue(req)
	time.Sleep(100 * time.Millisecond)
}

func TestHandleEnrichmentResult_UpdatesLatestCausal(t *testing.T) {
	s := newTestServerWithQueue(t)
	provider := &insightProvider{name: "insight"}
	s.WithAI(provider, true)

	cs := &clusterState{
		history:          history.New(10),
		issueStates:      make(map[string]*IssueState),
		checkCounter:     1,
		currentIssueHash: "hash-1",
		aiConfigVersion:  configVersionHash(provider.Name(), ""),
		latest: &HealthUpdate{
			Timestamp: time.Now(),
			ClusterID: "test",
			Results: map[string]CheckResult{
				"workloads": {
					Name: "workloads",
					Issues: []Issue{
						{
							Type:      "CrashLoop",
							Severity:  checker.SeverityCritical,
							Namespace: "default",
							Resource:  "deploy/app",
							Message:   "crashing",
						},
					},
				},
			},
			AIStatus: &AIStatus{
				Enabled:  true,
				Provider: provider.Name(),
				Pending:  true,
			},
		},
		latestCausal: &causal.CausalContext{
			Groups: []causal.CausalGroup{
				{
					ID:       "g1",
					Rule:     "test_rule",
					Severity: checker.SeverityCritical,
				},
			},
			TotalIssues: 1,
		},
	}
	s.mu.Lock()
	s.clusters["test"] = cs
	s.mu.Unlock()

	req := enrichmentRequest{
		clusterID:     "test",
		generation:    1,
		issueHash:     "hash-1",
		configVersion: configVersionHash(provider.Name(), ""),
		results: map[string]*checker.CheckResult{
			"workloads": {
				Healthy: 0,
				Issues: []checker.Issue{
					{
						Type:      "CrashLoop",
						Severity:  checker.SeverityCritical,
						Namespace: "default",
						Resource:  "deploy/app",
						Message:   "crashing",
					},
				},
			},
		},
		causalCtx:  deepCopyCausalContext(cs.latestCausal),
		checkCtx:   &checker.CheckContext{AIEnabled: true, AIProvider: provider},
		provider:   provider,
		aiModel:    "",
		enqueuedAt: time.Now(),
	}

	s.handleEnrichmentResult(t.Context(), req)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if cs.latestCausal == nil || len(cs.latestCausal.Groups) == 0 {
		t.Fatal("expected latestCausal to be present")
	}
	group := cs.latestCausal.Groups[0]
	if !group.AIEnhanced {
		t.Fatal("expected async enrichment to update latestCausal AIEnhanced")
	}
	if group.AIRootCause == "" {
		t.Fatal("expected AIRootCause in latestCausal")
	}
	if cs.latest == nil || cs.latest.AIStatus == nil {
		t.Fatal("expected latest AIStatus to be updated")
	}
	if cs.latest.AIStatus.Pending {
		t.Fatal("expected Pending=false after enrichment")
	}
}

func TestAsyncAI_RaceSafety(t *testing.T) {
	// Run with -race: concurrent checks + enrichments
	s := newTestServerWithQueue(t)
	ctx := t.Context()

	provider := ai.NewNoOpProvider()
	s.WithAI(provider, true)

	s.enrichQueue.Run(ctx, s.handleEnrichmentResult)

	cs := &clusterState{
		history:     history.New(10),
		issueStates: make(map[string]*IssueState),
	}
	s.mu.Lock()
	s.clusters["test"] = cs
	s.mu.Unlock()

	// Run concurrent enqueues
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.mu.Lock()
			cs.checkCounter++
			gen := cs.checkCounter
			cs.currentIssueHash = fmt.Sprintf("hash-%d", i)
			cs.aiConfigVersion = configVersionHash("noop", "")
			cs.latest = &HealthUpdate{
				Timestamp: time.Now(),
				Results:   map[string]CheckResult{},
				AIStatus:  &AIStatus{Enabled: true, Provider: "noop", Pending: true},
			}
			s.mu.Unlock()

			s.enrichQueue.Enqueue(enrichmentRequest{
				clusterID:     "test",
				generation:    gen,
				issueHash:     fmt.Sprintf("hash-%d", i),
				configVersion: configVersionHash("noop", ""),
				results:       deepCopyResults(map[string]*checker.CheckResult{}),
				checkCtx:      &checker.CheckContext{AIEnabled: true, AIProvider: provider},
				provider:      provider,
				enqueuedAt:    time.Now(),
			})
		}(i)
	}
	wg.Wait()

	// Wait for all to process
	time.Sleep(200 * time.Millisecond)
}

func TestAsyncAI_WorkerShutdownNoLeak(t *testing.T) {
	q := newEnrichmentQueue(4)
	ctx, cancel := context.WithCancel(context.Background())

	q.Run(ctx, func(_ context.Context, req enrichmentRequest) {
		time.Sleep(10 * time.Millisecond)
		q.markDone(req.clusterID)
	})

	q.Enqueue(enrichmentRequest{clusterID: "a", generation: 1})

	cancel()

	done := make(chan struct{})
	go func() {
		q.Shutdown()
		close(done)
	}()

	select {
	case <-done:
		// No leak
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown timed out — possible goroutine leak")
	}
}

// ---------------------------------------------------------------------------
// broadcastUpdate tests
// ---------------------------------------------------------------------------

func TestBroadcastUpdate(t *testing.T) {
	s := newTestServer(t)

	clientCh := make(chan HealthUpdate, 5)
	s.mu.Lock()
	s.clients[clientCh] = "test"
	s.mu.Unlock()

	update := HealthUpdate{
		Timestamp: time.Now(),
		ClusterID: "test",
		Results:   map[string]CheckResult{},
		AIStatus:  &AIStatus{Enabled: true, Provider: "noop"},
	}

	s.broadcastUpdate("test", update)

	select {
	case received := <-clientCh:
		if received.AIStatus == nil || received.AIStatus.Provider != "noop" {
			t.Error("broadcast should deliver update to subscribed client")
		}
	default:
		t.Error("client should have received update")
	}

	s.mu.Lock()
	delete(s.clients, clientCh)
	s.mu.Unlock()
}

func TestBroadcastUpdate_FiltersByCluster(t *testing.T) {
	s := newTestServer(t)

	clientCh := make(chan HealthUpdate, 5)
	s.mu.Lock()
	s.clients[clientCh] = "beta" // subscribed to different cluster
	s.mu.Unlock()

	update := HealthUpdate{ClusterID: "alpha"}
	s.broadcastUpdate("alpha", update)

	select {
	case <-clientCh:
		t.Error("client subscribed to 'beta' should NOT receive 'alpha' updates")
	default:
		// Expected
	}

	s.mu.Lock()
	delete(s.clients, clientCh)
	s.mu.Unlock()
}

// ---------------------------------------------------------------------------
// patchDashboardResults tests
// ---------------------------------------------------------------------------

func TestPatchDashboardResults(t *testing.T) {
	dashResults := map[string]CheckResult{
		"workloads": {
			Name: "workloads",
			Issues: []Issue{
				{Type: "CrashLoop", Severity: "critical", Namespace: "default", Resource: "deploy/app", Suggestion: "original"},
				{Type: "HighRestarts", Severity: "warning", Namespace: "default", Resource: "deploy/web", Suggestion: "original2"},
			},
		},
	}

	enrichedResults := map[string]*checker.CheckResult{
		"workloads": {
			Issues: []checker.Issue{
				{
					Type: "CrashLoop", Severity: "critical", Namespace: "default", Resource: "deploy/app",
					Suggestion: "AI Analysis: OOM killed due to memory limits",
					Metadata:   map[string]string{"aiRootCause": "Memory pressure"},
				},
				{
					Type: "HighRestarts", Severity: "warning", Namespace: "default", Resource: "deploy/web",
					Suggestion: "Check restart count", // not AI-enhanced
				},
			},
		},
	}

	patchDashboardResults(dashResults, enrichedResults)

	cr := dashResults["workloads"]
	if !cr.Issues[0].AIEnhanced {
		t.Error("first issue should be AI-enhanced")
	}
	if cr.Issues[0].Suggestion != "OOM killed due to memory limits" {
		t.Errorf("first issue suggestion = %q, want stripped AI prefix", cr.Issues[0].Suggestion)
	}
	if cr.Issues[0].RootCause != "Memory pressure" {
		t.Errorf("first issue RootCause = %q", cr.Issues[0].RootCause)
	}
	// Second issue should be unchanged
	if cr.Issues[1].AIEnhanced {
		t.Error("second issue should NOT be AI-enhanced")
	}
	if cr.Issues[1].Suggestion != "original2" {
		t.Errorf("second issue suggestion = %q, should be unchanged", cr.Issues[1].Suggestion)
	}
}

func TestPatchDashboardResults_MissingChecker(t *testing.T) {
	dashResults := map[string]CheckResult{
		"workloads": {
			Issues: []Issue{{Suggestion: "original"}},
		},
	}
	enrichedResults := map[string]*checker.CheckResult{
		"nonexistent": {
			Issues: []checker.Issue{
				{Suggestion: "AI Analysis: should not crash"},
			},
		},
	}
	// Should not panic
	patchDashboardResults(dashResults, enrichedResults)
	if dashResults["workloads"].Issues[0].Suggestion != "original" {
		t.Error("existing issues should be unchanged")
	}
}

// ---------------------------------------------------------------------------
// runAIEnrichment tests
// ---------------------------------------------------------------------------

func TestRunAIEnrichment_ReturnsStatus(t *testing.T) {
	provider := ai.NewNoOpProvider()
	results := map[string]*checker.CheckResult{
		"workloads": {
			Issues: []checker.Issue{
				{Type: "CrashLoop", Severity: "critical", Namespace: "default", Resource: "deploy/app"},
			},
		},
	}
	causalCtx := &causal.CausalContext{}
	checkCtx := &checker.CheckContext{
		AIEnabled:  true,
		AIProvider: provider,
		MaxIssues:  15,
	}

	status, _, _, err := runAIEnrichment(
		context.Background(), results, causalCtx, checkCtx,
		provider, "", 30*time.Second,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.CheckPhase != phaseDone {
		t.Errorf("CheckPhase = %q, want %q", status.CheckPhase, phaseDone)
	}
}

func TestRunAIEnrichment_ErrorReturnsErr(t *testing.T) {
	errProv := &errorProvider{err: errors.New("test error"), name: "err-prov"}
	results := map[string]*checker.CheckResult{
		"workloads": {
			Issues: []checker.Issue{
				{Type: "CrashLoop", Severity: "critical", Namespace: "default", Resource: "deploy/app"},
			},
		},
	}
	checkCtx := &checker.CheckContext{
		AIEnabled:  true,
		AIProvider: errProv,
		MaxIssues:  15,
	}

	_, _, _, err := runAIEnrichment(
		context.Background(), results, nil, checkCtx,
		errProv, "", 30*time.Second,
	)

	if err == nil {
		t.Error("expected error from error provider")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (substr == "" || findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
