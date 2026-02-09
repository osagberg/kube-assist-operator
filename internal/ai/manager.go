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
	"fmt"
	"sync"
	"time"
)

// Manager wraps an AI Provider with thread-safe runtime reconfiguration.
// It implements the Provider interface so it can be used as a drop-in replacement.
type Manager struct {
	mu              sync.RWMutex
	provider        Provider
	explainProvider Provider
	enabled         bool
	budget          *Budget
	cache           *Cache
}

// NewManager creates a new AI Manager with the given providers, enabled state, budget, and cache.
func NewManager(provider, explainProvider Provider, enabled bool, budget *Budget, cache *Cache) *Manager {
	if provider == nil {
		provider = NewNoOpProvider()
	}
	if explainProvider == nil {
		explainProvider = provider
	}
	return &Manager{
		provider:        provider,
		explainProvider: explainProvider,
		enabled:         enabled,
		budget:          budget,
		cache:           cache,
	}
}

// Name returns the current provider's name.
func (m *Manager) Name() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.provider.Name()
}

// Analyze delegates to the current provider with caching, budget, and tiered routing.
func (m *Manager) Analyze(ctx context.Context, request AnalysisRequest) (*AnalysisResponse, error) {
	m.mu.RLock()
	provider := m.provider
	if request.ExplainMode && m.explainProvider != nil {
		provider = m.explainProvider
	}
	budget := m.budget
	cache := m.cache
	m.mu.RUnlock()

	// Check cache
	if cache != nil {
		if cached, ok := cache.Get(request); ok {
			RecordCacheHit()
			return cached, nil
		}
		RecordCacheMiss()
	}

	// Check budget (estimate ~2000 tokens per issue, minimum 500)
	estimatedTokens := max(len(request.Issues)*2000, 500)
	if err := budget.CheckAllowance(estimatedTokens); err != nil {
		RecordBudgetExceeded(err.Error())
		return nil, fmt.Errorf("AI budget exceeded: %w", err)
	}

	// Call provider
	start := time.Now()
	resp, err := provider.Analyze(ctx, request)
	duration := time.Since(start)

	if err != nil {
		mode := "analyze"
		if request.ExplainMode {
			mode = "explain"
		}
		RecordAICall(provider.Name(), mode, "error", 0, duration)
		return nil, fmt.Errorf("AI analysis failed (%s): %w", provider.Name(), err)
	}

	// Record usage
	if budget != nil && resp != nil {
		budget.RecordUsage(resp.TokensUsed)
	}

	// Cache result
	if cache != nil && resp != nil {
		cache.Put(request, resp)
	}

	// Record metrics
	mode := "analyze"
	if request.ExplainMode {
		mode = "explain"
	}
	RecordAICall(provider.Name(), mode, "success", resp.TokensUsed, duration)

	return resp, nil
}

// Available returns true if the manager is enabled and the current provider is available.
func (m *Manager) Available() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled && m.provider.Available()
}

// ProviderAvailable returns true if the underlying provider is available,
// regardless of the enabled flag. Used by the settings UI to show provider
// readiness independently from the enabled toggle.
func (m *Manager) ProviderAvailable() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.provider.Available()
}

// Enabled returns whether AI is enabled.
func (m *Manager) Enabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled
}

// SetEnabled enables or disables AI analysis without changing the provider.
func (m *Manager) SetEnabled(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = enabled
}

// Reconfigure swaps the active AI provider at runtime. This is thread-safe
// and intended to be called from the dashboard settings API.
// Pass empty strings to keep current values for provider/model.
// explainModel configures a separate model for explain/narrative mode;
// when empty, the explain provider mirrors the primary provider.
func (m *Manager) Reconfigure(providerName, apiKey, model, explainModel string) error {
	cfg := Config{
		Provider:     providerName,
		APIKey:       apiKey,
		Model:        model,
		ExplainModel: explainModel,
	}

	newProvider, newExplain, _, err := NewProvider(cfg)
	if err != nil {
		return fmt.Errorf("creating provider: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.provider = newProvider
	m.explainProvider = newExplain
	m.enabled = providerName != "" && providerName != ProviderNameNoop
	// Clear cache on reconfigure
	if m.cache != nil {
		m.cache.Clear()
	}
	return nil
}

// Provider returns the current underlying provider. Use this only when you
// need to inspect the provider directly; prefer calling Manager methods instead.
func (m *Manager) Provider() Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.provider
}

// Budget returns the current budget (may be nil).
func (m *Manager) Budget() *Budget {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.budget
}

// Cache returns the current cache (may be nil).
func (m *Manager) Cache() *Cache {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cache
}
