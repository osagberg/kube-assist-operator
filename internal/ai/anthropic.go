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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	defaultAnthropicEndpoint = "https://api.anthropic.com/v1/messages"
	defaultAnthropicModel    = "claude-haiku-4-5-20251001"
	anthropicAPIVersion      = "2023-06-01"
)

// AnthropicProvider implements the Provider interface for Anthropic
type AnthropicProvider struct {
	apiKey    string
	endpoint  string
	model     string
	maxTokens int
	timeout   time.Duration
	client    *http.Client
}

// NewAnthropicProvider creates a new Anthropic provider
func NewAnthropicProvider(config Config) *AnthropicProvider {
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = defaultAnthropicEndpoint
	}

	model := config.Model
	if model == "" {
		model = defaultAnthropicModel
	}

	maxTokens := config.MaxTokens
	if maxTokens == 0 {
		maxTokens = 16384
	}

	timeout := time.Duration(config.Timeout) * time.Second
	if timeout == 0 {
		timeout = 90 * time.Second
	}

	return &AnthropicProvider{
		apiKey:    config.APIKey,
		endpoint:  endpoint,
		model:     model,
		maxTokens: maxTokens,
		timeout:   timeout,
		client:    &http.Client{Timeout: timeout},
	}
}

// ProviderNameAnthropic is the constant for the Anthropic provider name
const ProviderNameAnthropic = "anthropic"

// Name returns the provider identifier
func (p *AnthropicProvider) Name() string {
	return ProviderNameAnthropic
}

// Available returns true if API key is configured
func (p *AnthropicProvider) Available() bool {
	return p.apiKey != ""
}

type systemBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type cacheControl struct {
	Type string `json:"type"`
}

// anthropicRequest represents an Anthropic API request
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    []systemBlock      `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Analyze sends issues to Anthropic for enhanced analysis
func (p *AnthropicProvider) Analyze(ctx context.Context, request AnalysisRequest) (*AnalysisResponse, error) {
	if !p.Available() {
		return nil, ErrNotConfigured
	}

	prompt := BuildPrompt(request)

	anthropicReq := anthropicRequest{
		Model:     p.model,
		MaxTokens: p.maxTokens,
		System: []systemBlock{
			{
				Type:         "text",
				Text:         systemPrompt,
				CacheControl: &cacheControl{Type: "ephemeral"},
			},
		},
		Messages: []anthropicMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
	}

	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)
	req.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body := string(respBody)
		if len(body) > 500 {
			body = body[:500] + "...(truncated)"
		}
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, body)
	}

	var anthropicResp anthropicResponse
	if err := json.Unmarshal(respBody, &anthropicResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if anthropicResp.Error != nil {
		return nil, fmt.Errorf("API error: %s", anthropicResp.Error.Message)
	}

	if len(anthropicResp.Content) == 0 {
		return nil, fmt.Errorf("no response from API")
	}

	// Get text content
	var textContent string
	for _, content := range anthropicResp.Content {
		if content.Type == "text" {
			textContent = content.Text
			break
		}
	}

	tokensUsed := anthropicResp.Usage.InputTokens + anthropicResp.Usage.OutputTokens
	return ParseResponse(textContent, tokensUsed, ProviderNameAnthropic), nil
}
