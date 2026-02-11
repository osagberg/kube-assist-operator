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
	"testing"
)

func TestDefaultCatalog_HasProviders(t *testing.T) {
	catalog := DefaultCatalog()

	if len(catalog) == 0 {
		t.Fatal("DefaultCatalog() returned empty catalog")
	}

	for _, provider := range []string{ProviderNameAnthropic, ProviderNameOpenAI} {
		models := catalog[provider]
		if len(models) == 0 {
			t.Errorf("DefaultCatalog()[%q] has no models", provider)
		}
	}
}

func TestDefaultCatalog_AllModelsHaveRequiredFields(t *testing.T) {
	catalog := DefaultCatalog()

	for provider, models := range catalog {
		for _, m := range models {
			if m.ID == "" {
				t.Errorf("catalog[%s]: model has empty ID", provider)
			}
			if m.Label == "" {
				t.Errorf("catalog[%s][%s]: model has empty Label", provider, m.ID)
			}
			if m.Status != ModelStatusActive && m.Status != ModelStatusDeprecated {
				t.Errorf("catalog[%s][%s]: invalid status %q", provider, m.ID, m.Status)
			}
			if m.VerifiedAt == "" {
				t.Errorf("catalog[%s][%s]: missing verifiedAt", provider, m.ID)
			}
			if m.Tier != ModelTierPrimary && m.Tier != ModelTierExplain && m.Tier != ModelTierBoth {
				t.Errorf("catalog[%s][%s]: invalid tier %q", provider, m.ID, m.Tier)
			}
		}
	}
}

func TestDefaultCatalog_NoDuplicateIDs(t *testing.T) {
	catalog := DefaultCatalog()

	for provider, models := range catalog {
		seen := make(map[string]bool)
		for _, m := range models {
			if seen[m.ID] {
				t.Errorf("catalog[%s]: duplicate model ID %q", provider, m.ID)
			}
			seen[m.ID] = true
		}
	}
}

func TestCatalog_ForProvider(t *testing.T) {
	catalog := DefaultCatalog()

	models := catalog.ForProvider(ProviderNameAnthropic)
	if models == nil {
		t.Fatal("ForProvider(anthropic) returned nil")
		return
	}
	if len(models) == 0 {
		t.Error("ForProvider(anthropic) returned empty")
	}

	// Unknown provider should return nil
	if catalog.ForProvider("unknown") != nil {
		t.Error("ForProvider(unknown) should return nil")
	}
}

func TestCostPer1KTokens_KnownModels(t *testing.T) {
	tests := []struct {
		provider string
		model    string
		wantMin  float64
		wantMax  float64
	}{
		{"anthropic", "claude-haiku-4-5-20251001", 0.001, 0.002},
		{"anthropic", "claude-sonnet-4-5-20250929", 0.004, 0.006},
		{"openai", "gpt-4o-mini", 0.0005, 0.001},
		{"openai", "gpt-4o", 0.003, 0.005},
	}

	for _, tt := range tests {
		cost := CostPer1KTokens(tt.provider, tt.model)
		if cost < tt.wantMin || cost > tt.wantMax {
			t.Errorf("CostPer1KTokens(%s, %s) = %f, want in [%f, %f]",
				tt.provider, tt.model, cost, tt.wantMin, tt.wantMax)
		}
	}
}

func TestCostPer1KTokens_UnknownModel(t *testing.T) {
	// Should fall back to provider default
	cost := CostPer1KTokens("anthropic", "unknown-model")
	if cost <= 0 {
		t.Error("CostPer1KTokens with unknown model should return provider default > 0")
	}
}

func TestCostPer1KTokens_UnknownProvider(t *testing.T) {
	cost := CostPer1KTokens("unknown", "unknown-model")
	if cost != 0 {
		t.Errorf("CostPer1KTokens with unknown provider = %f, want 0", cost)
	}
}

func TestDefaultCatalog_HasExplainTierModels(t *testing.T) {
	catalog := DefaultCatalog()

	for provider, models := range catalog {
		hasExplainCapable := false
		for _, m := range models {
			if m.Tier == ModelTierExplain || m.Tier == ModelTierBoth {
				hasExplainCapable = true
				break
			}
		}
		if !hasExplainCapable {
			t.Errorf("catalog[%s] has no explain-capable models", provider)
		}
	}
}
