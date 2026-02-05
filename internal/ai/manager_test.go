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
	"sync"
	"testing"
)

func TestNewManager_NilProvider(t *testing.T) {
	m := NewManager(nil, false)
	if m.Name() != ProviderNameNoop {
		t.Errorf("NewManager(nil) Name() = %s, want %s", m.Name(), ProviderNameNoop)
	}
	if m.Enabled() {
		t.Error("NewManager(nil, false) should not be enabled")
	}
}

func TestNewManager_WithProvider(t *testing.T) {
	provider := NewNoOpProvider()
	m := NewManager(provider, true)

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
	m := NewManager(NewNoOpProvider(), false)

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
	m := NewManager(NewNoOpProvider(), true)

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
	m := NewManager(provider, true)

	got := m.Provider()
	if got != provider {
		t.Error("Provider() should return the underlying provider")
	}
}

func TestManager_Reconfigure_Noop(t *testing.T) {
	m := NewManager(NewNoOpProvider(), true)

	err := m.Reconfigure(ProviderNameNoop, "", "")
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
	m := NewManager(NewNoOpProvider(), false)

	err := m.Reconfigure(ProviderNameOpenAI, "test-key", "gpt-4")
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
	m := NewManager(NewNoOpProvider(), false)

	err := m.Reconfigure(ProviderNameAnthropic, "test-key", "claude-sonnet-4-5-20250929")
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
	m := NewManager(NewNoOpProvider(), true)

	err := m.Reconfigure("unknown-provider", "", "")
	if err == nil {
		t.Error("Reconfigure(unknown) should return error")
	}

	// Original provider should be unchanged
	if m.Name() != ProviderNameNoop {
		t.Errorf("Name() = %s, want %s (unchanged after error)", m.Name(), ProviderNameNoop)
	}
}

func TestManager_Reconfigure_EmptyProvider(t *testing.T) {
	m := NewManager(NewNoOpProvider(), true)

	err := m.Reconfigure("", "", "")
	if err != nil {
		t.Fatalf("Reconfigure('') error = %v", err)
	}

	// Empty defaults to noop
	if m.Name() != ProviderNameNoop {
		t.Errorf("Name() = %s, want %s", m.Name(), ProviderNameNoop)
	}
}

func TestManager_ConcurrentAccess(t *testing.T) {
	m := NewManager(NewNoOpProvider(), true)

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
			_ = m.Reconfigure(ProviderNameNoop, "", "")
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
	m := NewManager(NewNoOpProvider(), false)

	// Reconfigure to openai
	err := m.Reconfigure(ProviderNameOpenAI, "key1", "gpt-4")
	if err != nil {
		t.Fatalf("first reconfigure error = %v", err)
	}
	if m.Name() != ProviderNameOpenAI {
		t.Errorf("after first reconfigure, Name() = %s, want %s", m.Name(), ProviderNameOpenAI)
	}

	// Reconfigure back to noop
	err = m.Reconfigure(ProviderNameNoop, "", "")
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
