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
	"fmt"
	"os"
	"strings"
	"time"
)

// ProviderNameNoop is the constant for the NoOp provider name
const ProviderNameNoop = "noop"

// createSingleProvider creates a single provider based on the configuration.
func createSingleProvider(config Config) (Provider, error) {
	// Resolve API key from environment if it looks like an env var reference
	apiKey := config.APIKey
	if envVar, found := strings.CutPrefix(apiKey, "$"); found {
		apiKey = os.Getenv(envVar)
	}

	// Create a copy of config with resolved API key
	resolvedConfig := config
	resolvedConfig.APIKey = apiKey

	providerName := strings.ToLower(config.Provider)

	// Validate model against catalog if a model is specified
	if resolvedConfig.Model != "" && providerName != ProviderNameNoop && providerName != "" {
		catalog := DefaultCatalog()
		models := catalog.ForProvider(providerName)
		found := false
		for _, m := range models {
			if m.ID == resolvedConfig.Model {
				found = true
				break
			}
		}
		if !found {
			log.Info("Model not found in catalog, proceeding anyway", "provider", providerName, "model", resolvedConfig.Model)
		}
	}

	switch providerName {
	case ProviderNameOpenAI:
		return NewOpenAIProvider(resolvedConfig), nil
	case ProviderNameAnthropic:
		return NewAnthropicProvider(resolvedConfig), nil
	case ProviderNameNoop, "":
		return NewNoOpProvider(), nil
	default:
		return nil, fmt.Errorf("unknown AI provider: %s", config.Provider)
	}
}

// NewProvider creates providers based on the configuration.
// Returns (primaryProvider, explainProvider, budget, error).
// explainProvider is the same as primaryProvider when ExplainModel is empty or identical.
func NewProvider(config Config) (Provider, Provider, *Budget, error) {
	primary, err := createSingleProvider(config)
	if err != nil {
		return nil, nil, nil, err
	}

	// Determine the resolved primary model for comparison
	resolvedModel := config.Model
	if resolvedModel == "" {
		switch strings.ToLower(config.Provider) {
		case ProviderNameOpenAI:
			resolvedModel = defaultOpenAIModel
		case ProviderNameAnthropic:
			resolvedModel = defaultAnthropicModel
		}
	}

	// Create explain provider if a different model is configured
	explain := primary
	if config.ExplainModel != "" && config.ExplainModel != resolvedModel {
		explainConfig := config
		explainConfig.Model = config.ExplainModel
		explain, err = createSingleProvider(explainConfig)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("creating explain provider: %w", err)
		}
	}

	// Build budget from config
	var windows []BudgetWindow
	if config.DailyTokenLimit > 0 {
		windows = append(windows, BudgetWindow{
			Name:     "daily",
			Duration: 24 * time.Hour,
			Limit:    config.DailyTokenLimit,
		})
	}
	if config.MonthlyTokenLimit > 0 {
		windows = append(windows, BudgetWindow{
			Name:     "monthly",
			Duration: 30 * 24 * time.Hour,
			Limit:    config.MonthlyTokenLimit,
		})
	}
	budget := NewBudget(windows)

	return primary, explain, budget, nil
}

// MustNewProvider creates a primary provider or panics.
func MustNewProvider(config Config) Provider {
	provider, _, _, err := NewProvider(config)
	if err != nil {
		panic(err)
	}
	return provider
}

// ConfigFromEnv creates a config from environment variables
// Environment variables:
//   - KUBE_ASSIST_AI_PROVIDER: Provider name (openai, anthropic, noop)
//   - KUBE_ASSIST_AI_API_KEY: API key
//   - KUBE_ASSIST_AI_ENDPOINT: Custom endpoint (optional)
//   - KUBE_ASSIST_AI_MODEL: Model name (optional)
func ConfigFromEnv() Config {
	config := DefaultConfig()

	if provider := os.Getenv("KUBE_ASSIST_AI_PROVIDER"); provider != "" {
		config.Provider = provider
	}

	if apiKey := os.Getenv("KUBE_ASSIST_AI_API_KEY"); apiKey != "" {
		config.APIKey = apiKey
	}

	if endpoint := os.Getenv("KUBE_ASSIST_AI_ENDPOINT"); endpoint != "" {
		config.Endpoint = endpoint
	}

	if model := os.Getenv("KUBE_ASSIST_AI_MODEL"); model != "" {
		config.Model = model
	}

	return config
}
