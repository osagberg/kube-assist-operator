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
)

// NewProvider creates a provider based on the configuration
func NewProvider(config Config) (Provider, error) {
	// Resolve API key from environment if it looks like an env var reference
	apiKey := config.APIKey
	if strings.HasPrefix(apiKey, "$") {
		envVar := strings.TrimPrefix(apiKey, "$")
		apiKey = os.Getenv(envVar)
	}

	// Create a copy of config with resolved API key
	resolvedConfig := config
	resolvedConfig.APIKey = apiKey

	switch strings.ToLower(config.Provider) {
	case "openai":
		return NewOpenAIProvider(resolvedConfig), nil
	case "anthropic":
		return NewAnthropicProvider(resolvedConfig), nil
	case "noop", "":
		return NewNoOpProvider(), nil
	default:
		return nil, fmt.Errorf("unknown AI provider: %s", config.Provider)
	}
}

// MustNewProvider creates a provider or panics
func MustNewProvider(config Config) Provider {
	provider, err := NewProvider(config)
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
