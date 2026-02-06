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
	"strings"
	"time"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("ai")

const (
	defaultOpenAIEndpoint = "https://api.openai.com/v1/chat/completions"
	defaultOpenAIModel    = "gpt-4o"
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

// ProviderNameOpenAI is the constant for the OpenAI provider name
const ProviderNameOpenAI = "openai"

// Name returns the provider identifier
func (p *OpenAIProvider) Name() string {
	return ProviderNameOpenAI
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

	return p.parseResponse(openAIResp.Choices[0].Message.Content, openAIResp.Usage.TotalTokens), nil
}

func (p *OpenAIProvider) buildPrompt(request AnalysisRequest) string {
	contextJSON, _ := json.MarshalIndent(request.ClusterContext, "", "  ")

	var issueLines strings.Builder
	for i, issue := range request.Issues {
		issueJSON, _ := json.Marshal(issue)
		fmt.Fprintf(&issueLines, "issue_%d: %s\n", i, issueJSON)
	}

	prompt := fmt.Sprintf(`Analyze these Kubernetes health check issues and provide enhanced suggestions.
Use the exact issue key (issue_0, issue_1, ...) in your response.

Cluster Context:
%s

Issues:
%s`, contextJSON, issueLines.String())

	if request.CausalContext != nil && len(request.CausalContext.Groups) > 0 {
		causalJSON, _ := json.MarshalIndent(request.CausalContext, "", "  ")
		prompt += fmt.Sprintf(`

Causal Correlation Analysis (issues have been automatically grouped by likely root cause):
%s

Use the correlation data above to provide deeper root cause analysis. Correlated issues should be analyzed together.`, causalJSON)
	}

	prompt += "\n\nProvide a JSON response with enhanced suggestions for each issue."
	return prompt
}

func (p *OpenAIProvider) parseResponse(content string, tokensUsed int) *AnalysisResponse {
	// Try to parse as JSON
	var result struct {
		Suggestions map[string]EnhancedSuggestion `json:"suggestions"`
		Summary     string                        `json:"summary"`
	}

	cleaned := extractJSON(content)
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		// Log the parse failure for debugging
		preview := content
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		log.Error(err, "Failed to parse AI response as JSON", "provider", "openai", "preview", preview)
		return &AnalysisResponse{
			EnhancedSuggestions: make(map[string]EnhancedSuggestion),
			Summary:             content,
			TokensUsed:          tokensUsed,
		}
	}

	return &AnalysisResponse{
		EnhancedSuggestions: result.Suggestions,
		Summary:             result.Summary,
		TokensUsed:          tokensUsed,
	}
}

// extractJSON strips markdown code fences from AI responses.
// AI models often wrap JSON in ```json ... ``` blocks.
func extractJSON(content string) string {
	// Try ```json ... ```
	if idx := strings.Index(content, "```json"); idx >= 0 {
		start := idx + 7
		if end := strings.Index(content[start:], "```"); end >= 0 {
			return strings.TrimSpace(content[start : start+end])
		}
	}
	// Try bare ``` ... ```
	if idx := strings.Index(content, "```"); idx >= 0 {
		start := idx + 3
		// Skip optional newline after opening fence
		if start < len(content) && content[start] == '\n' {
			start++
		}
		if end := strings.Index(content[start:], "```"); end >= 0 {
			return strings.TrimSpace(content[start : start+end])
		}
	}
	return content
}

const systemPrompt = `You are a Kubernetes troubleshooting expert. You analyze health check issues from Kubernetes clusters and provide actionable suggestions.

When analyzing issues:
1. Identify the root cause based on the error type and message
2. Provide specific, actionable remediation steps
3. Consider common patterns and best practices
4. Reference relevant Kubernetes documentation when helpful

Each issue is labeled with an index key (issue_0, issue_1, ...). Use the EXACT same key in your response.

Respond in JSON format with this structure:
{
  "suggestions": {
    "issue_0": {
      "suggestion": "Main suggestion text",
      "rootCause": "Likely root cause",
      "steps": ["Step 1", "Step 2"],
      "references": ["https://kubernetes.io/..."],
      "confidence": 0.8
    },
    "issue_1": { ... }
  },
  "summary": "Overall analysis summary"
}

Keep suggestions concise but actionable. Focus on the most impactful fixes.`
