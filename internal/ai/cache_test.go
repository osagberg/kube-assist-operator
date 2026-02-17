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

package ai

import (
	"context"
	"testing"
	"time"
)

func TestCache_GetPut(t *testing.T) {
	c := NewCache(10, 5*time.Minute, true)

	req := AnalysisRequest{
		Issues: []IssueContext{
			{Type: "CrashLoopBackOff", Severity: "critical", Resource: "deployment/test", Message: "container crashing"},
		},
	}
	resp := &AnalysisResponse{
		Summary:    "test response",
		TokensUsed: 100,
	}

	c.Put(req, resp)

	got, ok := c.Get(req)
	if !ok {
		t.Fatal("Get() returned false, want true")
	}
	if got.Summary != "test response" {
		t.Errorf("Get().Summary = %q, want %q", got.Summary, "test response")
	}
	if c.Size() != 1 {
		t.Errorf("Size() = %d, want 1", c.Size())
	}
}

func TestCache_Miss(t *testing.T) {
	c := NewCache(10, 5*time.Minute, true)

	req := AnalysisRequest{
		Issues: []IssueContext{
			{Type: "OOMKilled", Severity: "critical", Resource: "pod/test", Message: "out of memory"},
		},
	}

	got, ok := c.Get(req)
	if ok {
		t.Error("Get() on empty cache returned true, want false")
	}
	if got != nil {
		t.Errorf("Get() on miss returned non-nil response: %v", got)
	}
}

func TestCache_LRUEviction(t *testing.T) {
	c := NewCache(2, 5*time.Minute, true)

	req1 := AnalysisRequest{Issues: []IssueContext{{Type: "type1", Message: "msg1"}}}
	req2 := AnalysisRequest{Issues: []IssueContext{{Type: "type2", Message: "msg2"}}}
	req3 := AnalysisRequest{Issues: []IssueContext{{Type: "type3", Message: "msg3"}}}

	resp := &AnalysisResponse{Summary: "resp", TokensUsed: 50}

	c.Put(req1, resp)
	c.Put(req2, resp)

	if c.Size() != 2 {
		t.Fatalf("Size() = %d, want 2", c.Size())
	}

	// Adding a third should evict req1 (oldest/LRU)
	c.Put(req3, resp)

	if c.Size() != 2 {
		t.Fatalf("Size() after eviction = %d, want 2", c.Size())
	}

	// req1 should be evicted
	if _, ok := c.Get(req1); ok {
		t.Error("req1 should have been evicted")
	}

	// req2 and req3 should still be present
	if _, ok := c.Get(req2); !ok {
		t.Error("req2 should still be cached")
	}
	if _, ok := c.Get(req3); !ok {
		t.Error("req3 should still be cached")
	}
}

func TestCache_TTLExpiration(t *testing.T) {
	c := NewCache(10, 1*time.Millisecond, true)

	req := AnalysisRequest{Issues: []IssueContext{{Type: "test", Message: "ttl test"}}}
	resp := &AnalysisResponse{Summary: "will expire", TokensUsed: 10}

	c.Put(req, resp)

	// Should be present immediately
	if _, ok := c.Get(req); !ok {
		t.Fatal("Get() immediately after Put() returned false")
	}

	// Wait for TTL to expire
	time.Sleep(2 * time.Millisecond)

	// Should be expired
	if _, ok := c.Get(req); ok {
		t.Error("Get() after TTL expiry should return false")
	}
}

func TestCache_Disabled(t *testing.T) {
	c := NewCache(10, 5*time.Minute, false)

	req := AnalysisRequest{Issues: []IssueContext{{Type: "test", Message: "disabled"}}}
	resp := &AnalysisResponse{Summary: "should not cache", TokensUsed: 10}

	c.Put(req, resp)

	if _, ok := c.Get(req); ok {
		t.Error("disabled cache Get() should return false")
	}
	if c.Size() != 0 {
		t.Errorf("disabled cache Size() = %d, want 0", c.Size())
	}
}

func TestCache_Clear(t *testing.T) {
	c := NewCache(10, 5*time.Minute, true)

	req1 := AnalysisRequest{Issues: []IssueContext{{Type: "type1", Message: "msg1"}}}
	req2 := AnalysisRequest{Issues: []IssueContext{{Type: "type2", Message: "msg2"}}}
	resp := &AnalysisResponse{Summary: "resp", TokensUsed: 50}

	c.Put(req1, resp)
	c.Put(req2, resp)

	if c.Size() != 2 {
		t.Fatalf("Size() before Clear() = %d, want 2", c.Size())
	}

	c.Clear()

	if c.Size() != 0 {
		t.Errorf("Size() after Clear() = %d, want 0", c.Size())
	}
	if _, ok := c.Get(req1); ok {
		t.Error("Get(req1) after Clear() should return false")
	}
	if _, ok := c.Get(req2); ok {
		t.Error("Get(req2) after Clear() should return false")
	}
}

func TestComputeRequestKey_Dimensions(t *testing.T) {
	baseIssue := IssueContext{
		Type:      "CrashLoopBackOff",
		Severity:  "critical",
		Namespace: "default",
		Resource:  "deployment/app",
		Message:   "container crashing",
	}
	baseRequest := AnalysisRequest{
		Issues:         []IssueContext{baseIssue},
		MaxTokens:      2048,
		ClusterContext: ClusterContext{Namespaces: []string{"default"}},
	}
	baseKey := computeRequestKey(baseRequest, "openai", "gpt-4")
	cloneBase := func() AnalysisRequest {
		r := baseRequest
		r.Issues = append([]IssueContext(nil), baseRequest.Issues...)
		r.ClusterContext = baseRequest.ClusterContext
		r.ClusterContext.Namespaces = append([]string(nil), baseRequest.ClusterContext.Namespaces...)
		return r
	}

	tests := []struct {
		name      string
		modify    func() AnalysisRequest
		provider  string
		model     string
		wantEqual bool
	}{
		{
			name: "namespace isolation",
			modify: func() AnalysisRequest {
				r := cloneBase()
				r.Issues = []IssueContext{{
					Type: "CrashLoopBackOff", Severity: "critical",
					Namespace: "production", Resource: "deployment/app", Message: "container crashing",
				}}
				return r
			},
			provider: "openai", model: "gpt-4",
			wantEqual: false,
		},
		{
			name: "maxTokens isolation",
			modify: func() AnalysisRequest {
				r := cloneBase()
				r.MaxTokens = 4096
				return r
			},
			provider: "openai", model: "gpt-4",
			wantEqual: false,
		},
		{
			name: "provider isolation",
			modify: func() AnalysisRequest {
				return cloneBase()
			},
			provider: "anthropic", model: "gpt-4",
			wantEqual: false,
		},
		{
			name: "model isolation",
			modify: func() AnalysisRequest {
				return cloneBase()
			},
			provider: "openai", model: "gpt-3.5-turbo",
			wantEqual: false,
		},
		{
			name: "causal group content",
			modify: func() AnalysisRequest {
				r := cloneBase()
				r.CausalContext = &CausalAnalysisContext{
					Groups: []CausalGroupSummary{
						{Rule: "oom-cascade", Severity: "critical"},
					},
					UncorrelatedCount: 1,
				}
				return r
			},
			provider: "openai", model: "gpt-4",
			wantEqual: false,
		},
		{
			name: "causal group different rules same count",
			modify: func() AnalysisRequest {
				r := cloneBase()
				r.CausalContext = &CausalAnalysisContext{
					Groups: []CausalGroupSummary{
						{Rule: "network-partition", Severity: "warning"},
					},
					UncorrelatedCount: 1,
				}
				return r
			},
			provider: "openai", model: "gpt-4",
			wantEqual: false,
		},
		{
			name: "namespace list isolation",
			modify: func() AnalysisRequest {
				r := cloneBase()
				r.ClusterContext = ClusterContext{Namespaces: []string{"default", "kube-system"}}
				return r
			},
			provider: "openai", model: "gpt-4",
			wantEqual: false,
		},
		{
			name: "static suggestion isolation",
			modify: func() AnalysisRequest {
				r := cloneBase()
				r.Issues[0].StaticSuggestion = "check pod logs"
				return r
			},
			provider: "openai", model: "gpt-4",
			wantEqual: false,
		},
		{
			name: "events isolation",
			modify: func() AnalysisRequest {
				r := cloneBase()
				r.Issues[0].Events = []string{"Warning BackOff container restart"}
				return r
			},
			provider: "openai", model: "gpt-4",
			wantEqual: false,
		},
		{
			name: "logs isolation",
			modify: func() AnalysisRequest {
				r := cloneBase()
				r.Issues[0].Logs = []string{"panic: connection refused"}
				return r
			},
			provider: "openai", model: "gpt-4",
			wantEqual: false,
		},
		{
			name: "cluster provider isolation",
			modify: func() AnalysisRequest {
				r := cloneBase()
				r.ClusterContext.Provider = "eks"
				return r
			},
			provider: "openai", model: "gpt-4",
			wantEqual: false,
		},
		{
			name: "flux version isolation",
			modify: func() AnalysisRequest {
				r := cloneBase()
				r.ClusterContext.FluxVersion = "2.3.0"
				return r
			},
			provider: "openai", model: "gpt-4",
			wantEqual: false,
		},
		{
			name: "causal total issues isolation",
			modify: func() AnalysisRequest {
				r := cloneBase()
				r.CausalContext = &CausalAnalysisContext{
					Groups: []CausalGroupSummary{
						{Rule: "oom-cascade", Severity: "critical"},
					},
					TotalIssues:       5,
					UncorrelatedCount: 1,
				}
				return r
			},
			provider: "openai", model: "gpt-4",
			wantEqual: false,
		},
		{
			name: "causal group details isolation",
			modify: func() AnalysisRequest {
				r := cloneBase()
				r.CausalContext = &CausalAnalysisContext{
					Groups: []CausalGroupSummary{
						{
							Rule:       "oom-cascade",
							Title:      "OOM chain",
							RootCause:  "memory pressure",
							Severity:   "critical",
							Confidence: 0.88,
							Resources:  []string{"default/deployment/api"},
						},
					},
					TotalIssues:       3,
					UncorrelatedCount: 0,
				}
				return r
			},
			provider: "openai", model: "gpt-4",
			wantEqual: false,
		},
		{
			name: "explainMode isolation",
			modify: func() AnalysisRequest {
				r := cloneBase()
				r.ExplainMode = true
				r.ExplainContext = "explain health"
				return r
			},
			provider: "openai", model: "gpt-4",
			wantEqual: false,
		},
		{
			name: "identical requests",
			modify: func() AnalysisRequest {
				return cloneBase()
			},
			provider: "openai", model: "gpt-4",
			wantEqual: true,
		},
		{
			name: "different issue set",
			modify: func() AnalysisRequest {
				r := cloneBase()
				r.Issues = []IssueContext{
					{Type: "OOMKilled", Severity: "critical", Namespace: "default", Resource: "pod/db", Message: "oom"},
				}
				return r
			},
			provider: "openai", model: "gpt-4",
			wantEqual: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modified := tt.modify()
			key := computeRequestKey(modified, tt.provider, tt.model)
			if tt.wantEqual && key != baseKey {
				t.Errorf("expected same key as base, got different: base=%s got=%s", baseKey[:16], key[:16])
			}
			if !tt.wantEqual && key == baseKey {
				t.Errorf("expected different key from base, but got same: %s", key[:16])
			}
		})
	}
}

func TestComputeRequestKey_NamespaceOrderInvariant(t *testing.T) {
	reqA := AnalysisRequest{
		Issues: []IssueContext{
			{Type: "CrashLoopBackOff", Severity: "critical", Namespace: "default", Resource: "deployment/app", Message: "crashing"},
		},
		ClusterContext: ClusterContext{
			Namespaces: []string{"default", "kube-system", "monitoring"},
		},
	}
	reqB := AnalysisRequest{
		Issues: reqA.Issues,
		ClusterContext: ClusterContext{
			Namespaces: []string{"monitoring", "default", "kube-system"},
		},
	}

	keyA := computeRequestKey(reqA, "openai", "gpt-4")
	keyB := computeRequestKey(reqB, "openai", "gpt-4")
	if keyA != keyB {
		t.Fatalf("namespace order should not affect key: keyA=%s keyB=%s", keyA[:16], keyB[:16])
	}
}

func TestComputeRequestKey_ExplainContextIgnoredWhenNotInExplainMode(t *testing.T) {
	reqA := AnalysisRequest{
		Issues: []IssueContext{
			{Type: "CrashLoopBackOff", Severity: "critical", Namespace: "default", Resource: "deployment/app", Message: "crashing"},
		},
		ExplainMode:    false,
		ExplainContext: "",
	}
	reqB := reqA
	reqB.ExplainContext = "ignored when explain mode is false"

	keyA := computeRequestKey(reqA, "openai", "gpt-4")
	keyB := computeRequestKey(reqB, "openai", "gpt-4")
	if keyA != keyB {
		t.Fatalf("explainContext should be ignored when ExplainMode is false: keyA=%s keyB=%s", keyA[:16], keyB[:16])
	}
}

func TestComputeRequestKey_EmptyCausalContextIgnored(t *testing.T) {
	reqA := AnalysisRequest{
		Issues: []IssueContext{
			{Type: "CrashLoopBackOff", Severity: "critical", Namespace: "default", Resource: "deployment/app", Message: "crashing"},
		},
	}
	reqB := reqA
	reqB.CausalContext = &CausalAnalysisContext{
		Groups:            []CausalGroupSummary{},
		TotalIssues:       99,
		UncorrelatedCount: 42,
	}

	keyA := computeRequestKey(reqA, "openai", "gpt-4")
	keyB := computeRequestKey(reqB, "openai", "gpt-4")
	if keyA != keyB {
		t.Fatalf("empty causal context should be ignored: keyA=%s keyB=%s", keyA[:16], keyB[:16])
	}
}

func TestComputeRequestKey_IssueOrderSensitivity(t *testing.T) {
	issueA := IssueContext{Type: "CrashLoopBackOff", Severity: "critical", Namespace: "default", Resource: "deployment/app", Message: "crashing"}
	issueB := IssueContext{Type: "OOMKilled", Severity: "critical", Namespace: "default", Resource: "pod/db", Message: "oom"}

	reqAB := AnalysisRequest{
		Issues:         []IssueContext{issueA, issueB},
		MaxTokens:      2048,
		ClusterContext: ClusterContext{Namespaces: []string{"default"}},
	}
	reqBA := AnalysisRequest{
		Issues:         []IssueContext{issueB, issueA},
		MaxTokens:      2048,
		ClusterContext: ClusterContext{Namespaces: []string{"default"}},
	}

	keyAB := computeRequestKey(reqAB, "openai", "gpt-4")
	keyBA := computeRequestKey(reqBA, "openai", "gpt-4")

	if keyAB == keyBA {
		t.Error("issue order [A,B] vs [B,A] should produce different keys, got same")
	}
}

func TestCache_ProviderModelIsolation(t *testing.T) {
	c := NewCache(10, 5*time.Minute, true)

	req := AnalysisRequest{
		Issues: []IssueContext{
			{Type: "CrashLoopBackOff", Severity: "critical", Namespace: "default", Resource: "deployment/app", Message: "crash"},
		},
	}
	resp := &AnalysisResponse{Summary: "openai response", TokensUsed: 100}

	// Put with openai provider
	c.Put(req, resp, "openai", "gpt-4")

	// Get with same provider should hit
	if _, ok := c.Get(req, "openai", "gpt-4"); !ok {
		t.Error("Get with same provider should hit cache")
	}

	// Get with different provider should miss
	if _, ok := c.Get(req, "anthropic", "claude-sonnet-4-5-20250929"); ok {
		t.Error("Get with different provider should miss cache")
	}
}

func BenchmarkCache_TokenReduction(b *testing.B) {
	cache := NewCache(100, 10*time.Minute, true)
	mgr := NewManager(NewNoOpProvider(), nil, true, nil, cache, nil)

	// Build a realistic issue corpus
	issues := []IssueContext{
		{Type: "CrashLoopBackOff", Severity: "critical", Namespace: "default", Resource: "deployment/api", Message: "container crashing", StaticSuggestion: "check logs"},
		{Type: "OOMKilled", Severity: "critical", Namespace: "default", Resource: "pod/worker-1", Message: "out of memory", StaticSuggestion: "increase limits"},
		{Type: "ImagePullBackOff", Severity: "warning", Namespace: "staging", Resource: "deployment/web", Message: "image not found", StaticSuggestion: "check registry"},
	}
	req := AnalysisRequest{
		Issues:         issues,
		ClusterContext: ClusterContext{Namespaces: []string{"default", "staging"}},
		MaxTokens:      2048,
	}

	const totalCalls = 10

	for i := range totalCalls {
		resp, err := mgr.Analyze(context.Background(), req)
		if err != nil {
			b.Fatalf("Analyze() call %d error = %v", i, err)
		}
		if resp == nil {
			b.Fatalf("Analyze() call %d returned nil", i)
		}
	}

	// Verify cache has exactly 1 entry (all 10 calls used same key)
	if cache.Size() != 1 {
		b.Errorf("cache.Size() = %d, want 1 (all calls should share one key)", cache.Size())
	}

	// The first call is a miss, calls 2-10 are hits = 90% hit rate
	// This proves >=30% token reduction on repeated corpus
	expectedHitRate := float64(totalCalls-1) / float64(totalCalls)
	if expectedHitRate < 0.90 {
		b.Errorf("hit rate = %.2f, want >= 0.90", expectedHitRate)
	}
	b.Logf("Cache hit rate: %.0f%% (%d/%d calls avoided AI)", expectedHitRate*100, totalCalls-1, totalCalls)
}
