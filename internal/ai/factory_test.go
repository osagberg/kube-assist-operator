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

func TestCreateSingleProvider_EnvVarAPIKey(t *testing.T) {
	tests := []struct {
		name      string
		apiKey    string
		envName   string
		envValue  string
		wantAvail bool
	}{
		{
			name:      "env var reference resolves to existing var",
			apiKey:    "$TEST_AI_KEY_EXISTS",
			envName:   "TEST_AI_KEY_EXISTS",
			envValue:  "resolved-key",
			wantAvail: true,
		},
		{
			name:      "env var reference resolves to missing var",
			apiKey:    "$TEST_AI_KEY_MISSING",
			wantAvail: false,
		},
		{
			name:      "literal API key stays literal",
			apiKey:    "literal-key-value",
			wantAvail: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envName != "" {
				t.Setenv(tt.envName, tt.envValue)
			}

			provider, err := createSingleProvider(Config{
				Provider: ProviderNameOpenAI,
				APIKey:   tt.apiKey,
			})
			if err != nil {
				t.Fatalf("createSingleProvider() error = %v", err)
			}

			if provider.Available() != tt.wantAvail {
				t.Errorf("Available() = %v, want %v", provider.Available(), tt.wantAvail)
			}
		})
	}
}

func TestNewProvider_ExplainModelRouting(t *testing.T) {
	t.Run("empty explain model returns same provider", func(t *testing.T) {
		primary, explain, _, err := NewProvider(Config{
			Provider: ProviderNameNoop,
		})
		if err != nil {
			t.Fatalf("NewProvider() error = %v", err)
		}
		if primary != explain {
			t.Error("with empty ExplainModel, explain should be same as primary")
		}
	})

	t.Run("different explain model returns separate provider", func(t *testing.T) {
		primary, explain, _, err := NewProvider(Config{
			Provider:     ProviderNameOpenAI,
			APIKey:       "test-key",
			Model:        "gpt-4o",
			ExplainModel: "gpt-4o-mini",
		})
		if err != nil {
			t.Fatalf("NewProvider() error = %v", err)
		}
		// Both are openai providers, but they should be different instances
		if primary == explain {
			t.Error("with different ExplainModel, explain should be a separate provider instance")
		}
	})

	t.Run("same explain model as primary returns same provider", func(t *testing.T) {
		primary, explain, _, err := NewProvider(Config{
			Provider:     ProviderNameOpenAI,
			APIKey:       "test-key",
			Model:        "gpt-4o-mini",
			ExplainModel: "gpt-4o-mini",
		})
		if err != nil {
			t.Fatalf("NewProvider() error = %v", err)
		}
		if primary != explain {
			t.Error("when ExplainModel equals Model, explain should be same as primary")
		}
	})
}

func TestNewProvider_BudgetWindows(t *testing.T) {
	t.Run("daily only", func(t *testing.T) {
		_, _, budget, err := NewProvider(Config{
			Provider:        ProviderNameNoop,
			DailyTokenLimit: 1000,
		})
		if err != nil {
			t.Fatalf("NewProvider() error = %v", err)
		}
		if budget == nil {
			t.Fatal("budget should not be nil with daily limit")
			return
		}
		usage := budget.GetUsage()
		if len(usage) != 1 {
			t.Fatalf("expected 1 window, got %d", len(usage))
		}
		if usage[0].Name != "daily" {
			t.Errorf("window name = %q, want %q", usage[0].Name, "daily")
		}
	})

	t.Run("monthly only", func(t *testing.T) {
		_, _, budget, err := NewProvider(Config{
			Provider:          ProviderNameNoop,
			MonthlyTokenLimit: 50000,
		})
		if err != nil {
			t.Fatalf("NewProvider() error = %v", err)
		}
		if budget == nil {
			t.Fatal("budget should not be nil with monthly limit")
			return
		}
		usage := budget.GetUsage()
		if len(usage) != 1 {
			t.Fatalf("expected 1 window, got %d", len(usage))
		}
		if usage[0].Name != "monthly" {
			t.Errorf("window name = %q, want %q", usage[0].Name, "monthly")
		}
	})

	t.Run("both daily and monthly", func(t *testing.T) {
		_, _, budget, err := NewProvider(Config{
			Provider:          ProviderNameNoop,
			DailyTokenLimit:   1000,
			MonthlyTokenLimit: 30000,
		})
		if err != nil {
			t.Fatalf("NewProvider() error = %v", err)
		}
		if budget == nil {
			t.Fatal("budget should not be nil")
			return
		}
		usage := budget.GetUsage()
		if len(usage) != 2 {
			t.Fatalf("expected 2 windows, got %d", len(usage))
		}
	})

	t.Run("neither limit", func(t *testing.T) {
		_, _, budget, err := NewProvider(Config{
			Provider: ProviderNameNoop,
		})
		if err != nil {
			t.Fatalf("NewProvider() error = %v", err)
		}
		if budget != nil {
			t.Error("budget should be nil when no limits are set")
		}
	})
}

func TestMustNewProvider_Success(t *testing.T) {
	provider := MustNewProvider(Config{Provider: ProviderNameNoop})
	if provider == nil {
		t.Fatal("MustNewProvider(noop) returned nil")
		return
	}
	if provider.Name() != ProviderNameNoop {
		t.Errorf("Name() = %q, want %q", provider.Name(), ProviderNameNoop)
	}
}

func TestMustNewProvider_Panic(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("MustNewProvider(unknown) should panic")
		}
	}()

	MustNewProvider(Config{Provider: "unknown-provider"})
}

func TestConfigFromEnv(t *testing.T) {
	t.Run("all env vars set", func(t *testing.T) {
		t.Setenv("KUBE_ASSIST_AI_PROVIDER", "openai")
		t.Setenv("KUBE_ASSIST_AI_API_KEY", "test-key-123")
		t.Setenv("KUBE_ASSIST_AI_ENDPOINT", "https://custom.endpoint.com")
		t.Setenv("KUBE_ASSIST_AI_MODEL", "gpt-4o")

		config := ConfigFromEnv()

		if config.Provider != "openai" {
			t.Errorf("Provider = %q, want %q", config.Provider, "openai")
		}
		if config.APIKey != "test-key-123" {
			t.Errorf("APIKey = %q, want %q", config.APIKey, "test-key-123")
		}
		if config.Endpoint != "https://custom.endpoint.com" {
			t.Errorf("Endpoint = %q, want %q", config.Endpoint, "https://custom.endpoint.com")
		}
		if config.Model != "gpt-4o" {
			t.Errorf("Model = %q, want %q", config.Model, "gpt-4o")
		}
	})

	t.Run("no env vars set uses defaults", func(t *testing.T) {
		// t.Setenv ensures these are cleared for this subtest
		t.Setenv("KUBE_ASSIST_AI_PROVIDER", "")
		t.Setenv("KUBE_ASSIST_AI_API_KEY", "")
		t.Setenv("KUBE_ASSIST_AI_ENDPOINT", "")
		t.Setenv("KUBE_ASSIST_AI_MODEL", "")

		config := ConfigFromEnv()

		defaults := DefaultConfig()
		if config.Provider != defaults.Provider {
			t.Errorf("Provider = %q, want default %q", config.Provider, defaults.Provider)
		}
		if config.MaxTokens != defaults.MaxTokens {
			t.Errorf("MaxTokens = %d, want default %d", config.MaxTokens, defaults.MaxTokens)
		}
	})

	t.Run("partial env vars set", func(t *testing.T) {
		t.Setenv("KUBE_ASSIST_AI_PROVIDER", "anthropic")
		t.Setenv("KUBE_ASSIST_AI_API_KEY", "")
		t.Setenv("KUBE_ASSIST_AI_ENDPOINT", "")
		t.Setenv("KUBE_ASSIST_AI_MODEL", "")

		config := ConfigFromEnv()

		if config.Provider != "anthropic" {
			t.Errorf("Provider = %q, want %q", config.Provider, "anthropic")
		}
		// APIKey should remain empty (not overridden from default)
		if config.APIKey != "" {
			t.Errorf("APIKey = %q, want empty", config.APIKey)
		}
	})
}
