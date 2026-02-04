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
	defaultOpenAIEndpoint = "https://api.openai.com/v1/chat/completions"
	defaultOpenAIModel    = "gpt-4"
)

// OpenAIProvider implements the Provider interface for OpenAI
type OpenAIProvider struct {
	apiKey    string
	endpoint  string
	model     string
	maxTokens int
	timeout   time.Duration
	client    *http.Client
}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider(config Config) *OpenAIProvider {
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = defaultOpenAIEndpoint
	}

	model := config.Model
	if model == "" {
		model = defaultOpenAIModel
	}

	maxTokens := config.MaxTokens
	if maxTokens == 0 {
		maxTokens = 2000
	}

	timeout := time.Duration(config.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &OpenAIProvider{
		apiKey:    config.APIKey,
		endpoint:  endpoint,
		model:     model,
		maxTokens: maxTokens,
		timeout:   timeout,
		client:    &http.Client{Timeout: timeout},
	}
}

// Name returns the provider identifier
func (p *OpenAIProvider) Name() string {
	return "openai"
}

// Available returns true if API key is configured
func (p *OpenAIProvider) Available() bool {
	return p.apiKey != ""
}

// openAIRequest represents an OpenAI API request
type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Analyze sends issues to OpenAI for enhanced analysis
func (p *OpenAIProvider) Analyze(ctx context.Context, request AnalysisRequest) (*AnalysisResponse, error) {
	if !p.Available() {
		return nil, ErrNotConfigured
	}

	prompt := p.buildPrompt(request)

	openAIReq := openAIRequest{
		Model: p.model,
		Messages: []openAIMessage{
			{
				Role:    "system",
				Content: systemPrompt,
			},
			{
				Role:    "user",
				Content: prompt,
			},
		},
		MaxTokens:   p.maxTokens,
		Temperature: 0.3, // Lower temperature for more consistent outputs
	}

	body, err := json.Marshal(openAIReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var openAIResp openAIResponse
	if err := json.Unmarshal(respBody, &openAIResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if openAIResp.Error != nil {
		return nil, fmt.Errorf("API error: %s", openAIResp.Error.Message)
	}

	if len(openAIResp.Choices) == 0 {
		return nil, fmt.Errorf("no response from API")
	}

	return p.parseResponse(openAIResp.Choices[0].Message.Content, openAIResp.Usage.TotalTokens)
}

func (p *OpenAIProvider) buildPrompt(request AnalysisRequest) string {
	issuesJSON, _ := json.MarshalIndent(request.Issues, "", "  ")
	contextJSON, _ := json.MarshalIndent(request.ClusterContext, "", "  ")

	return fmt.Sprintf(`Analyze these Kubernetes health check issues and provide enhanced suggestions:

Cluster Context:
%s

Issues:
%s

Provide a JSON response with enhanced suggestions for each issue.`, contextJSON, issuesJSON)
}

func (p *OpenAIProvider) parseResponse(content string, tokensUsed int) (*AnalysisResponse, error) {
	// Try to parse as JSON
	var result struct {
		Suggestions map[string]EnhancedSuggestion `json:"suggestions"`
		Summary     string                        `json:"summary"`
	}

	if err := json.Unmarshal([]byte(content), &result); err != nil {
		// If parsing fails, return the raw content as summary
		return &AnalysisResponse{
			EnhancedSuggestions: make(map[string]EnhancedSuggestion),
			Summary:             content,
			TokensUsed:          tokensUsed,
		}, nil
	}

	return &AnalysisResponse{
		EnhancedSuggestions: result.Suggestions,
		Summary:             result.Summary,
		TokensUsed:          tokensUsed,
	}, nil
}

const systemPrompt = `You are a Kubernetes troubleshooting expert. You analyze health check issues from Kubernetes clusters and provide actionable suggestions.

When analyzing issues:
1. Identify the root cause based on the error type and message
2. Provide specific, actionable remediation steps
3. Consider common patterns and best practices
4. Reference relevant Kubernetes documentation when helpful

Respond in JSON format with this structure:
{
  "suggestions": {
    "namespace/resource": {
      "suggestion": "Main suggestion text",
      "rootCause": "Likely root cause",
      "steps": ["Step 1", "Step 2"],
      "references": ["https://kubernetes.io/..."],
      "confidence": 0.8
    }
  },
  "summary": "Overall analysis summary"
}

Keep suggestions concise but actionable. Focus on the most impactful fixes.`
