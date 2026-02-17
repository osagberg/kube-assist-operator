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
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// failingProvider always returns an error from Analyze.
type failingProvider struct {
	callCount atomic.Int32
}

func (f *failingProvider) Name() string { return "failing" }
func (f *failingProvider) Analyze(_ context.Context, _ AnalysisRequest) (*AnalysisResponse, error) {
	f.callCount.Add(1)
	return nil, errors.New("provider unavailable")
}
func (f *failingProvider) Available() bool { return true }

func TestNewManager_NilProvider(t *testing.T) {
	m := NewManager(nil, nil, false, nil, nil, nil)
	if m.Name() != ProviderNameNoop {
		t.Errorf("NewManager(nil) Name() = %s, want %s", m.Name(), ProviderNameNoop)
	}
	if m.Enabled() {
		t.Error("NewManager(nil, false) should not be enabled")
	}
}

func TestNewManager_WithProvider(t *testing.T) {
	provider := NewNoOpProvider()
	m := NewManager(provider, nil, true, nil, nil, nil)

	if m.Name() != ProviderNameNoop {
		t.Errorf("Name() = %s, want %s", m.Name(), ProviderNameNoop)
	}
	if !m.Enabled() {
		t.Error("expected enabled")
	}
	if !m.Available() {
		t.Error("expected available")
	}
}

func TestManager_SetEnabled(t *testing.T) {
	m := NewManager(NewNoOpProvider(), nil, false, nil, nil, nil)

	if m.Enabled() {
		t.Error("should start disabled")
	}

	m.SetEnabled(true)
	if !m.Enabled() {
		t.Error("should be enabled after SetEnabled(true)")
	}

	m.SetEnabled(false)
	if m.Enabled() {
		t.Error("should be disabled after SetEnabled(false)")
	}
}

func TestManager_Analyze(t *testing.T) {
	m := NewManager(NewNoOpProvider(), nil, true, nil, nil, nil)

	request := AnalysisRequest{
		Issues: []IssueContext{
			{
				Type:             "CrashLoopBackOff",
				Severity:         "critical",
				Resource:         "deployment/test",
				Namespace:        "default",
				Message:          "Container crashing",
				StaticSuggestion: "Check logs",
			},
		},
	}

	resp, err := m.Analyze(context.Background(), request)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if len(resp.EnhancedSuggestions) != 1 {
		t.Errorf("EnhancedSuggestions length = %d, want 1", len(resp.EnhancedSuggestions))
	}
}

func TestManager_Provider(t *testing.T) {
	provider := NewNoOpProvider()
	m := NewManager(provider, nil, true, nil, nil, nil)

	got := m.Provider()
	if got != provider {
		t.Error("Provider() should return the underlying provider")
	}
}

func TestManager_Reconfigure_Noop(t *testing.T) {
	m := NewManager(NewNoOpProvider(), nil, true, nil, nil, nil)

	err := m.Reconfigure(ProviderNameNoop, "", "", "")
	if err != nil {
		t.Fatalf("Reconfigure(noop) error = %v", err)
	}

	if m.Name() != ProviderNameNoop {
		t.Errorf("Name() after reconfigure = %s, want %s", m.Name(), ProviderNameNoop)
	}
	// noop provider sets enabled = false because providerName is "noop"
	if m.Enabled() {
		t.Error("should not be enabled after reconfigure to noop")
	}
}

func TestManager_Reconfigure_OpenAI(t *testing.T) {
	m := NewManager(NewNoOpProvider(), nil, false, nil, nil, nil)

	err := m.Reconfigure(ProviderNameOpenAI, "test-key", "gpt-4", "")
	if err != nil {
		t.Fatalf("Reconfigure(openai) error = %v", err)
	}

	if m.Name() != ProviderNameOpenAI {
		t.Errorf("Name() = %s, want %s", m.Name(), ProviderNameOpenAI)
	}
	if !m.Enabled() {
		t.Error("should be enabled after reconfigure to openai")
	}
	if !m.Available() {
		t.Error("should be available with API key")
	}
}

func TestManager_Reconfigure_Anthropic(t *testing.T) {
	m := NewManager(NewNoOpProvider(), nil, false, nil, nil, nil)

	err := m.Reconfigure(ProviderNameAnthropic, "test-key", "claude-sonnet-4-5-20250929", "")
	if err != nil {
		t.Fatalf("Reconfigure(anthropic) error = %v", err)
	}

	if m.Name() != ProviderNameAnthropic {
		t.Errorf("Name() = %s, want %s", m.Name(), ProviderNameAnthropic)
	}
	if !m.Enabled() {
		t.Error("should be enabled after reconfigure to anthropic")
	}
}

func TestManager_Reconfigure_InvalidProvider(t *testing.T) {
	m := NewManager(NewNoOpProvider(), nil, true, nil, nil, nil)

	err := m.Reconfigure("unknown-provider", "", "", "")
	if err == nil {
		t.Error("Reconfigure(unknown) should return error")
	}

	// Original provider should be unchanged
	if m.Name() != ProviderNameNoop {
		t.Errorf("Name() = %s, want %s (unchanged after error)", m.Name(), ProviderNameNoop)
	}
}

func TestManager_Reconfigure_EmptyProvider(t *testing.T) {
	m := NewManager(NewNoOpProvider(), nil, true, nil, nil, nil)

	err := m.Reconfigure("", "", "", "")
	if err != nil {
		t.Fatalf("Reconfigure('') error = %v", err)
	}

	// Empty defaults to noop
	if m.Name() != ProviderNameNoop {
		t.Errorf("Name() = %s, want %s", m.Name(), ProviderNameNoop)
	}
}

func TestManager_ConcurrentAccess(t *testing.T) {
	m := NewManager(NewNoOpProvider(), nil, true, nil, nil, nil)

	var wg sync.WaitGroup
	const goroutines = 50

	// Concurrent reads
	for range goroutines {
		wg.Go(func() {
			_ = m.Name()
			_ = m.Available()
			_ = m.Enabled()
			_ = m.Provider()
		})
	}

	// Concurrent writes
	for i := range goroutines {
		wg.Go(func() {
			if i%2 == 0 {
				m.SetEnabled(true)
			} else {
				m.SetEnabled(false)
			}
		})
	}

	// Concurrent reconfigures
	for range 10 {
		wg.Go(func() {
			_ = m.Reconfigure(ProviderNameNoop, "", "", "")
		})
	}

	// Concurrent analyze calls
	for range goroutines {
		wg.Go(func() {
			_, _ = m.Analyze(context.Background(), AnalysisRequest{
				Issues: []IssueContext{
					{
						Type:      "test",
						Resource:  "deployment/test",
						Namespace: "default",
					},
				},
			})
		})
	}

	wg.Wait()
	// If we get here without a race condition, the test passes
}

func TestManager_ReconfigurePreservesThread(t *testing.T) {
	m := NewManager(NewNoOpProvider(), nil, false, nil, nil, nil)

	// Reconfigure to openai
	err := m.Reconfigure(ProviderNameOpenAI, "key1", "gpt-4", "")
	if err != nil {
		t.Fatalf("first reconfigure error = %v", err)
	}
	if m.Name() != ProviderNameOpenAI {
		t.Errorf("after first reconfigure, Name() = %s, want %s", m.Name(), ProviderNameOpenAI)
	}

	// Reconfigure back to noop
	err = m.Reconfigure(ProviderNameNoop, "", "", "")
	if err != nil {
		t.Fatalf("second reconfigure error = %v", err)
	}
	if m.Name() != ProviderNameNoop {
		t.Errorf("after second reconfigure, Name() = %s, want %s", m.Name(), ProviderNameNoop)
	}
	if m.Enabled() {
		t.Error("should be disabled after switching to noop")
	}
}

func TestManager_BudgetBlocking(t *testing.T) {
	budget := NewBudget([]BudgetWindow{
		{Name: "daily", Duration: 24 * time.Hour, Limit: 10000},
	})

	m := NewManager(NewNoOpProvider(), nil, true, budget, nil, nil)

	// First call should succeed
	_, err := m.Analyze(context.Background(), AnalysisRequest{
		Issues: []IssueContext{
			{Type: "test", Resource: "deployment/test", Namespace: "default", Message: "msg"},
		},
	})
	if err != nil {
		t.Fatalf("first Analyze() error = %v", err)
	}

	// Exhaust the budget
	budget.RecordUsage(9000)

	// Next call should be blocked (estimated ~2000 tokens > remaining ~1000)
	_, err = m.Analyze(context.Background(), AnalysisRequest{
		Issues: []IssueContext{
			{Type: "test2", Resource: "deployment/test2", Namespace: "default", Message: "msg2"},
		},
	})
	if err == nil {
		t.Fatal("Analyze() should fail when budget is exceeded")
	}
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Errorf("error should wrap ErrBudgetExceeded, got: %v", err)
	}
}

func TestManager_CacheHitMiss(t *testing.T) {
	cache := NewCache(10, 5*time.Minute, true)
	m := NewManager(NewNoOpProvider(), nil, true, nil, cache, nil)

	request := AnalysisRequest{
		Issues: []IssueContext{
			{
				Type:             "CrashLoopBackOff",
				Severity:         "critical",
				Resource:         "deployment/cached-test",
				Namespace:        "default",
				Message:          "Container crashing",
				StaticSuggestion: "Check logs",
			},
		},
	}

	// First call should be a cache miss
	resp1, err := m.Analyze(context.Background(), request)
	if err != nil {
		t.Fatalf("first Analyze() error = %v", err)
	}

	// Second call with same request should hit cache
	resp2, err := m.Analyze(context.Background(), request)
	if err != nil {
		t.Fatalf("second Analyze() error = %v", err)
	}

	// Both should return valid responses
	if resp1 == nil || resp2 == nil {
		t.Fatal("responses should not be nil")
	}

	// Cache should have exactly 1 entry
	if cache.Size() != 1 {
		t.Errorf("cache Size() = %d, want 1", cache.Size())
	}
}

func TestManager_TieredRouting(t *testing.T) {
	primary := NewNoOpProvider()
	explain := NewNoOpProvider()

	m := NewManager(primary, explain, true, nil, nil, nil)

	// Normal analyze should use primary
	normalReq := AnalysisRequest{
		Issues: []IssueContext{
			{Type: "test", Resource: "deployment/test", Namespace: "default", Message: "msg", StaticSuggestion: "fix it"},
		},
	}
	_, err := m.Analyze(context.Background(), normalReq)
	if err != nil {
		t.Fatalf("normal Analyze() error = %v", err)
	}

	// Explain mode should use explain provider
	explainReq := AnalysisRequest{
		ExplainMode:    true,
		ExplainContext: "explain the cluster health",
	}
	_, err = m.Analyze(context.Background(), explainReq)
	if err != nil {
		t.Fatalf("explain Analyze() error = %v", err)
	}

	// Verify the manager has different providers stored
	if m.Provider() != primary {
		t.Error("Provider() should return primary provider")
	}
}

func TestManager_Reconfigure_WithExplainModel(t *testing.T) {
	m := NewManager(NewNoOpProvider(), nil, false, nil, nil, nil)

	// Reconfigure with different explain model
	err := m.Reconfigure(ProviderNameAnthropic, "test-key", "claude-sonnet-4-5-20250929", "claude-haiku-4-5-20251001")
	if err != nil {
		t.Fatalf("Reconfigure with explainModel error = %v", err)
	}

	if m.Name() != ProviderNameAnthropic {
		t.Errorf("Name() = %s, want %s", m.Name(), ProviderNameAnthropic)
	}
	if !m.Enabled() {
		t.Error("should be enabled")
	}
}

func TestManager_Reconfigure_SameExplainModel(t *testing.T) {
	m := NewManager(NewNoOpProvider(), nil, false, nil, nil, nil)

	// When explainModel equals model, explain should be same as primary
	err := m.Reconfigure(ProviderNameAnthropic, "test-key", "claude-haiku-4-5-20251001", "claude-haiku-4-5-20251001")
	if err != nil {
		t.Fatalf("Reconfigure with same explainModel error = %v", err)
	}

	if m.Name() != ProviderNameAnthropic {
		t.Errorf("Name() = %s, want %s", m.Name(), ProviderNameAnthropic)
	}
}

func TestManager_Available_IncorporatesEnabled(t *testing.T) {
	m := NewManager(NewNoOpProvider(), nil, false, nil, nil, nil)

	// Disabled + available provider = not available
	if m.Available() {
		t.Error("Available() should be false when disabled")
	}

	m.SetEnabled(true)
	if !m.Available() {
		t.Error("Available() should be true when enabled with available provider")
	}

	m.SetEnabled(false)
	if m.Available() {
		t.Error("Available() should be false after disabling")
	}
}

func TestManager_CircuitBreaker_Integration(t *testing.T) {
	fp := &failingProvider{}
	cb := NewCircuitBreaker(3, 5*time.Minute)
	m := NewManager(fp, nil, true, nil, nil, cb)

	req := AnalysisRequest{
		Issues: []IssueContext{
			{Type: "test", Resource: "deployment/test", Namespace: "default", Message: "msg"},
		},
	}

	// First 3 calls should hit the provider and fail
	for i := range 3 {
		_, err := m.Analyze(context.Background(), req)
		if err == nil {
			t.Fatalf("call %d should have failed", i+1)
		}
	}

	// Circuit should be open now
	if cb.State() != CircuitOpen {
		t.Errorf("circuit state = %s, want open", cb.State())
	}

	// 4th call should be rejected by circuit breaker without hitting provider
	_, err := m.Analyze(context.Background(), req)
	if err == nil {
		t.Fatal("4th call should have failed")
	}
	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("4th call error should wrap ErrCircuitOpen, got: %v", err)
	}

	// Provider should only have been called 3 times
	if got := fp.callCount.Load(); got != 3 {
		t.Errorf("provider call count = %d, want 3", got)
	}
}

func TestManager_CircuitBreaker_Recovery(t *testing.T) {
	now := time.Now()
	fp := &failingProvider{}
	cb := NewCircuitBreaker(3, 5*time.Minute, WithNowFunc(func() time.Time {
		return now
	}))
	m := NewManager(fp, nil, true, nil, nil, cb)

	req := AnalysisRequest{
		Issues: []IssueContext{
			{Type: "test", Resource: "deployment/test", Namespace: "default", Message: "msg"},
		},
	}

	// Trip the circuit with 3 failures
	for range 3 {
		_, _ = m.Analyze(context.Background(), req)
	}
	if cb.State() != CircuitOpen {
		t.Fatalf("circuit state = %s, want open", cb.State())
	}

	// Advance past timeout — switch to a succeeding provider
	now = now.Add(6 * time.Minute)
	m.mu.Lock()
	m.provider = NewNoOpProvider()
	m.mu.Unlock()

	// Half-open probe should succeed → circuit closes
	resp, err := m.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("half-open probe should succeed, got: %v", err)
	}
	if resp == nil {
		t.Fatal("response should not be nil")
	}
	if cb.State() != CircuitClosed {
		t.Errorf("circuit state = %s, want closed after successful probe", cb.State())
	}

	// Subsequent calls should work normally
	resp, err = m.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("post-recovery call failed: %v", err)
	}
	if resp == nil {
		t.Fatal("response should not be nil")
	}
}

func TestManager_CircuitBreaker_CacheBypassesCB(t *testing.T) {
	cache := NewCache(10, 5*time.Minute, true)
	cb := NewCircuitBreaker(3, 5*time.Minute)
	m := NewManager(NewNoOpProvider(), nil, true, nil, cache, cb)

	req := AnalysisRequest{
		Issues: []IssueContext{
			{Type: "CrashLoopBackOff", Severity: "critical", Resource: "deployment/test", Namespace: "default", Message: "crash", StaticSuggestion: "fix"},
		},
	}

	// First call populates cache
	_, err := m.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}

	// Trip the circuit
	for range 3 {
		cb.RecordFailure()
	}
	if cb.State() != CircuitOpen {
		t.Fatalf("circuit state = %s, want open", cb.State())
	}

	// Cached response should still be served even though circuit is open
	resp, err := m.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("cached call should succeed despite open circuit, got: %v", err)
	}
	if resp == nil {
		t.Fatal("cached response should not be nil")
	}
}

func TestManager_ProviderAvailable_IgnoresEnabled(t *testing.T) {
	m := NewManager(NewNoOpProvider(), nil, false, nil, nil, nil)

	// Disabled but provider is available
	if !m.ProviderAvailable() {
		t.Error("ProviderAvailable() should be true regardless of enabled flag")
	}

	m.SetEnabled(true)
	if !m.ProviderAvailable() {
		t.Error("ProviderAvailable() should be true when enabled")
	}

	m.SetEnabled(false)
	if !m.ProviderAvailable() {
		t.Error("ProviderAvailable() should still be true when disabled")
	}
}

func TestManager_Reconfigure_ClearsCache(t *testing.T) {
	cache := NewCache(10, 5*time.Minute, true)
	m := NewManager(NewNoOpProvider(), nil, true, nil, cache, nil)

	// Populate cache via Analyze
	req := AnalysisRequest{
		Issues: []IssueContext{
			{
				Type:             "CrashLoopBackOff",
				Severity:         "critical",
				Resource:         "deployment/test",
				Namespace:        "default",
				Message:          "Container crashing",
				StaticSuggestion: "Check logs",
			},
		},
	}

	resp1, err := m.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("first Analyze() error = %v", err)
	}
	if resp1 == nil {
		t.Fatal("first Analyze() returned nil")
	}
	if cache.Size() != 1 {
		t.Fatalf("cache.Size() = %d after first Analyze(), want 1", cache.Size())
	}

	// Reconfigure to a different provider (noop with different name triggers cache clear)
	if err := m.Reconfigure(ProviderNameNoop, "", "", ""); err != nil {
		t.Fatalf("Reconfigure() error = %v", err)
	}

	// Cache should be cleared
	if cache.Size() != 0 {
		t.Errorf("cache.Size() = %d after Reconfigure(), want 0", cache.Size())
	}

	// Re-enable so Analyze works with the new noop provider
	m.SetEnabled(true)

	// Same request should return a fresh response (not cached)
	resp2, err := m.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("second Analyze() error = %v", err)
	}
	if resp2 == nil {
		t.Fatal("second Analyze() returned nil")
	}

	// Cache should have exactly 1 entry again (the fresh response)
	if cache.Size() != 1 {
		t.Errorf("cache.Size() = %d after second Analyze(), want 1", cache.Size())
	}
}
