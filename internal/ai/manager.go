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
)

// Manager wraps an AI Provider with thread-safe runtime reconfiguration.
// It implements the Provider interface so it can be used as a drop-in replacement.
type Manager struct {
	mu       sync.RWMutex
	provider Provider
	enabled  bool
}

// NewManager creates a new AI Manager with the given provider and enabled state.
func NewManager(provider Provider, enabled bool) *Manager {
	if provider == nil {
		provider = NewNoOpProvider()
	}
	return &Manager{
		provider: provider,
		enabled:  enabled,
	}
}

// Name returns the current provider's name.
func (m *Manager) Name() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.provider.Name()
}

// Analyze delegates to the current provider.
func (m *Manager) Analyze(ctx context.Context, request AnalysisRequest) (*AnalysisResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.provider.Analyze(ctx, request)
}

// Available returns true if the current provider is available.
func (m *Manager) Available() bool {
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
func (m *Manager) Reconfigure(providerName, apiKey, model string) error {
	cfg := Config{
		Provider: providerName,
		APIKey:   apiKey,
		Model:    model,
	}

	newProvider, err := NewProvider(cfg)
	if err != nil {
		return fmt.Errorf("creating provider: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.provider = newProvider
	m.enabled = providerName != "" && providerName != ProviderNameNoop
	return nil
}

// Provider returns the current underlying provider. Use this only when you
// need to inspect the provider directly; prefer calling Manager methods instead.
func (m *Manager) Provider() Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.provider
}
