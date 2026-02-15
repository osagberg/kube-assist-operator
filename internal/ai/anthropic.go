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
	// anthropicAPIVersion is the Anthropic API version header value.
	// Update this when Anthropic releases a new API version.
	anthropicAPIVersion = "2023-06-01"
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
	Model      string               `json:"model"`
	MaxTokens  int                  `json:"max_tokens"`
	System     []systemBlock        `json:"system,omitempty"`
	Messages   []anthropicMessage   `json:"messages"`
	Tools      []anthropicTool      `json:"tools,omitempty"`
	ToolChoice *anthropicToolChoice `json:"tool_choice,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type anthropicContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type anthropicResponse struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	StopReason string                  `json:"stop_reason"`
	Content    []anthropicContentBlock `json:"content"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// anthropicResult holds the parsed result from doAnthropicRequest.
type anthropicResult struct {
	Content    string
	TokensUsed int
	Truncated  bool
}

// doAnthropicRequest performs the HTTP call and extracts content from the Anthropic response.
// It returns the text content (or tool_use input) from the first matching block.
func (p *AnthropicProvider) doAnthropicRequest(ctx context.Context, req anthropicRequest) (*anthropicResult, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)
	httpReq.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		delay := parseRetryAfter(resp.Header.Get("Retry-After"))
		return nil, &rateLimitError{retryAfter: delay, statusCode: resp.StatusCode}
	}

	if resp.StatusCode != http.StatusOK {
		errBody := NewSanitizer().SanitizeString(string(respBody))
		if len(errBody) > 500 {
			errBody = string([]rune(errBody)[:500]) + "...(truncated)"
		}
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, errBody)
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

	tokensUsed := anthropicResp.Usage.InputTokens + anthropicResp.Usage.OutputTokens
	truncated := anthropicResp.StopReason == "max_tokens"

	// Look for tool_use content block first (structured output mode)
	for _, block := range anthropicResp.Content {
		if block.Type == "tool_use" && len(block.Input) > 0 {
			return &anthropicResult{
				Content:    string(block.Input),
				TokensUsed: tokensUsed,
				Truncated:  truncated,
			}, nil
		}
	}

	// Fall back to text content
	for _, block := range anthropicResp.Content {
		if block.Type == "text" {
			return &anthropicResult{
				Content:    block.Text,
				TokensUsed: tokensUsed,
				Truncated:  truncated,
			}, nil
		}
	}

	return nil, fmt.Errorf("no text or tool_use content in response")
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

	// Set tool use for schema-enforced structured output
	schema := AnalysisResponseSchema()
	toolName := "analyze_issues"
	toolDesc := "Provide structured analysis of Kubernetes health check issues"
	if request.ExplainMode {
		schema = ExplainResponseSchema()
		toolName = "explain_cluster"
		toolDesc = "Provide structured explanation of cluster health"
	}
	anthropicReq.Tools = []anthropicTool{
		{
			Name:        toolName,
			Description: toolDesc,
			InputSchema: schema,
		},
	}
	anthropicReq.ToolChoice = &anthropicToolChoice{
		Type: "tool",
		Name: toolName,
	}

	result, err := p.doAnthropicRequest(ctx, anthropicReq)
	if err != nil {
		if !isRetryableForFallback(err) {
			return nil, err
		}
		// Retry without tools on failure (model may not support tool use)
		log.Info("Tool use mode failed, retrying with prompt-based JSON", "error", err)
		anthropicReq.Tools = nil
		anthropicReq.ToolChoice = nil
		result, err = p.doAnthropicRequest(ctx, anthropicReq)
		if err != nil {
			return nil, err
		}
	}

	if result.Truncated {
		log.Info("Anthropic response truncated by max_tokens", "tokensUsed", result.TokensUsed)
	}

	resp := ParseResponse(result.Content, result.TokensUsed, ProviderNameAnthropic)
	resp.Truncated = result.Truncated
	return resp, nil
}
