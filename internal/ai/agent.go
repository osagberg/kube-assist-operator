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

const chatSystemPrompt = `You are KubeAssist, an AI assistant for Kubernetes cluster health monitoring.
You have access to tools that query live cluster data. Use them to answer
questions about cluster health, issues, namespaces, and trends.

Be concise and actionable. When issues are found, suggest specific kubectl
commands or remediation steps. If you need more context, call the appropriate tool.`

// ToolDef defines a tool available to the ChatAgent.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolExecutor executes a named tool with the given JSON arguments and returns
// the result as a string.
type ToolExecutor func(ctx context.Context, name string, args json.RawMessage) (string, error)

// ChatMessage represents a message in a multi-turn conversation.
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall represents a function call requested by the AI.
type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

// ChatEventType identifies the kind of SSE event emitted during a chat turn.
type ChatEventType string

const (
	ChatEventThinking   ChatEventType = "thinking"
	ChatEventToolCall   ChatEventType = "tool_call"
	ChatEventToolResult ChatEventType = "tool_result"
	ChatEventContent    ChatEventType = "content"
	ChatEventDone       ChatEventType = "done"
	ChatEventError      ChatEventType = "error"
)

// ChatEvent is emitted via the callback during RunTurn.
type ChatEvent struct {
	Type    ChatEventType   `json:"type"`
	Content string          `json:"content,omitempty"`
	Tool    string          `json:"tool,omitempty"`
	Args    json.RawMessage `json:"args,omitempty"`
	Tokens  int             `json:"tokens,omitempty"`
}

// ChatAgent orchestrates multi-turn conversations with function calling
// against an OpenAI- or Anthropic-compatible API.
type ChatAgent struct {
	providerName  string
	apiKey        string
	model         string
	endpoint      string
	timeout       time.Duration
	client        *http.Client
	tools         []ToolDef
	executor      ToolExecutor
	maxIterations int
}

// NewChatAgent creates a ChatAgent for the given provider.
func NewChatAgent(providerName, apiKey, model, endpoint string, executor ToolExecutor) *ChatAgent {
	if endpoint == "" {
		switch providerName {
		case ProviderNameOpenAI:
			endpoint = defaultOpenAIEndpoint
		case ProviderNameAnthropic:
			endpoint = defaultAnthropicEndpoint
		}
	}
	if model == "" {
		switch providerName {
		case ProviderNameOpenAI:
			model = defaultOpenAIModel
		case ProviderNameAnthropic:
			model = defaultAnthropicModel
		}
	}
	return &ChatAgent{
		providerName:  providerName,
		apiKey:        apiKey,
		model:         model,
		endpoint:      endpoint,
		timeout:       90 * time.Second,
		client:        &http.Client{Timeout: 90 * time.Second},
		tools:         ChatTools(),
		executor:      executor,
		maxIterations: 5,
	}
}

// SetMaxIterations overrides the default max tool-call iterations per turn.
func (a *ChatAgent) SetMaxIterations(n int) {
	if n > 0 {
		a.maxIterations = n
	}
}

// RunTurn executes one conversational turn: it sends messages to the AI
// provider and loops when the model returns tool calls (up to maxIterations).
// The emit callback receives ChatEvent values for each step. Returns the
// updated messages slice (including assistant and tool messages), total
// tokens used, and any error.
func (a *ChatAgent) RunTurn(ctx context.Context, messages []ChatMessage, emit func(ChatEvent)) ([]ChatMessage, int, error) {
	// Work on a copy to avoid aliasing the caller's backing array.
	msgs := make([]ChatMessage, len(messages))
	copy(msgs, messages)

	totalTokens := 0

	for i := 0; i < a.maxIterations; i++ {
		if err := ctx.Err(); err != nil {
			emit(ChatEvent{Type: ChatEventError, Content: "context cancelled"})
			return msgs, totalTokens, err
		}

		emit(ChatEvent{Type: ChatEventThinking})

		var (
			content string
			calls   []ToolCall
			tokens  int
			err     error
		)

		switch a.providerName {
		case ProviderNameOpenAI:
			content, calls, tokens, err = a.callOpenAI(ctx, msgs)
		case ProviderNameAnthropic:
			content, calls, tokens, err = a.callAnthropic(ctx, msgs)
		default:
			return msgs, totalTokens, fmt.Errorf("unsupported provider: %s", a.providerName)
		}
		if err != nil {
			emit(ChatEvent{Type: ChatEventError, Content: err.Error()})
			return msgs, totalTokens, err
		}
		totalTokens += tokens

		// No tool calls — return text content.
		if len(calls) == 0 {
			emit(ChatEvent{Type: ChatEventContent, Content: content, Tokens: tokens})
			emit(ChatEvent{Type: ChatEventDone, Tokens: totalTokens})
			// Append assistant message to history.
			msgs = append(msgs, ChatMessage{Role: "assistant", Content: content})
			return msgs, totalTokens, nil
		}

		// Append the assistant message with tool calls to history.
		msgs = append(msgs, ChatMessage{
			Role:      "assistant",
			Content:   content,
			ToolCalls: calls,
		})

		// Execute each tool call and feed results back.
		for _, tc := range calls {
			emit(ChatEvent{Type: ChatEventToolCall, Tool: tc.Name, Args: tc.Args})

			result, execErr := a.executor(ctx, tc.Name, tc.Args)
			if execErr != nil {
				result = fmt.Sprintf("error: %s", execErr.Error())
			}

			emit(ChatEvent{Type: ChatEventToolResult, Tool: tc.Name, Content: result})

			msgs = append(msgs, ChatMessage{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
		}
	}

	// Exhausted iterations — return what we have.
	emit(ChatEvent{Type: ChatEventError, Content: "max iterations reached"})
	emit(ChatEvent{Type: ChatEventDone, Tokens: totalTokens})
	return msgs, totalTokens, fmt.Errorf("max iterations (%d) reached", a.maxIterations)
}

// ---------------------------------------------------------------------------
// OpenAI-specific request/response handling
// ---------------------------------------------------------------------------

type openAIChatRequest struct {
	Model       string              `json:"model"`
	Messages    []openAIChatMessage `json:"messages"`
	Tools       []openAIChatTool    `json:"tools,omitempty"`
	MaxTokens   int                 `json:"max_tokens,omitempty"`
	Temperature float64             `json:"temperature"`
}

type openAIChatMessage struct {
	Role       string               `json:"role"`
	Content    string               `json:"content,omitempty"`
	ToolCallID string               `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIChatToolCall `json:"tool_calls,omitempty"`
}

type openAIChatTool struct {
	Type     string             `json:"type"`
	Function openAIChatFunction `json:"function"`
}

type openAIChatFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type openAIChatToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content   string               `json:"content"`
			ToolCalls []openAIChatToolCall `json:"tool_calls,omitempty"`
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

func (a *ChatAgent) callOpenAI(ctx context.Context, messages []ChatMessage) (string, []ToolCall, int, error) {
	oaiMsgs := make([]openAIChatMessage, 0, len(messages)+1)
	oaiMsgs = append(oaiMsgs, openAIChatMessage{Role: "system", Content: chatSystemPrompt})

	for _, m := range messages {
		msg := openAIChatMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		}
		for _, tc := range m.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, openAIChatToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      tc.Name,
					Arguments: string(tc.Args),
				},
			})
		}
		oaiMsgs = append(oaiMsgs, msg)
	}

	oaiTools := make([]openAIChatTool, 0, len(a.tools))
	for _, t := range a.tools {
		oaiTools = append(oaiTools, openAIChatTool{
			Type:     "function",
			Function: openAIChatFunction(t),
		})
	}

	reqBody := openAIChatRequest{
		Model:       a.model,
		Messages:    oaiMsgs,
		Tools:       oaiTools,
		MaxTokens:   4096,
		Temperature: 0.3,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", nil, 0, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", nil, 0, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return "", nil, 0, fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", nil, 0, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", nil, 0, fmt.Errorf("API error (status %d): %s", resp.StatusCode, truncate(string(respBody), 500))
	}

	var oaiResp openAIChatResponse
	if err := json.Unmarshal(respBody, &oaiResp); err != nil {
		return "", nil, 0, fmt.Errorf("parse response: %w", err)
	}
	if oaiResp.Error != nil {
		return "", nil, 0, fmt.Errorf("API error: %s", oaiResp.Error.Message)
	}
	if len(oaiResp.Choices) == 0 {
		return "", nil, 0, fmt.Errorf("no choices in response")
	}

	msg := oaiResp.Choices[0].Message
	tokens := oaiResp.Usage.TotalTokens

	calls := make([]ToolCall, 0, len(msg.ToolCalls))
	for _, tc := range msg.ToolCalls {
		calls = append(calls, ToolCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
			Args: json.RawMessage(tc.Function.Arguments), //nolint:unconvert // explicit conversion for clarity
		})
	}

	return msg.Content, calls, tokens, nil
}

// ---------------------------------------------------------------------------
// Anthropic-specific request/response handling
// ---------------------------------------------------------------------------

type anthropicChatRequest struct {
	Model     string                 `json:"model"`
	MaxTokens int                    `json:"max_tokens"`
	System    string                 `json:"system,omitempty"`
	Messages  []anthropicChatMessage `json:"messages"`
	Tools     []anthropicChatTool    `json:"tools,omitempty"`
}

type anthropicChatMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type anthropicChatTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicChatContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

type anthropicChatResponse struct {
	Content    []anthropicChatContentBlock `json:"content"`
	StopReason string                      `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (a *ChatAgent) callAnthropic(ctx context.Context, messages []ChatMessage) (string, []ToolCall, int, error) {
	anthMsgs := buildAnthropicMessages(messages)

	anthTools := make([]anthropicChatTool, 0, len(a.tools))
	for _, t := range a.tools {
		anthTools = append(anthTools, anthropicChatTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.Parameters,
		})
	}

	reqBody := anthropicChatRequest{
		Model:     a.model,
		MaxTokens: 4096,
		System:    chatSystemPrompt,
		Messages:  anthMsgs,
		Tools:     anthTools,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", nil, 0, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", nil, 0, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return "", nil, 0, fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", nil, 0, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", nil, 0, fmt.Errorf("API error (status %d): %s", resp.StatusCode, truncate(string(respBody), 500))
	}

	var anthResp anthropicChatResponse
	if err := json.Unmarshal(respBody, &anthResp); err != nil {
		return "", nil, 0, fmt.Errorf("parse response: %w", err)
	}
	if anthResp.Error != nil {
		return "", nil, 0, fmt.Errorf("API error: %s", anthResp.Error.Message)
	}

	tokens := anthResp.Usage.InputTokens + anthResp.Usage.OutputTokens

	var textContent string
	var calls []ToolCall
	for _, block := range anthResp.Content {
		switch block.Type {
		case "text":
			textContent = block.Text
		case "tool_use":
			calls = append(calls, ToolCall{
				ID:   block.ID,
				Name: block.Name,
				Args: block.Input,
			})
		}
	}

	return textContent, calls, tokens, nil
}

// buildAnthropicMessages converts ChatMessage history into Anthropic API
// messages. Anthropic requires tool results to be content blocks inside an
// "assistant" message followed by a "user" message with "tool_result" blocks.
func buildAnthropicMessages(messages []ChatMessage) []anthropicChatMessage {
	var result []anthropicChatMessage

	for _, m := range messages {
		switch m.Role {
		case "user":
			content, _ := json.Marshal(m.Content)
			result = append(result, anthropicChatMessage{
				Role:    "user",
				Content: content,
			})

		case "assistant":
			var blocks []anthropicChatContentBlock
			if m.Content != "" {
				blocks = append(blocks, anthropicChatContentBlock{
					Type: "text",
					Text: m.Content,
				})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, anthropicChatContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: tc.Args,
				})
			}
			blockJSON, _ := json.Marshal(blocks)
			result = append(result, anthropicChatMessage{
				Role:    "assistant",
				Content: blockJSON,
			})

		case "tool": //nolint:goconst // API role value
			// Anthropic expects tool results as "user" messages with tool_result
			// content blocks. Consecutive tool results must be merged into a
			// single "user" message to satisfy Anthropic's alternating-role
			// requirement.
			newBlock := anthropicChatContentBlock{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   m.Content,
			}
			if len(result) > 0 && result[len(result)-1].Role == "user" {
				// Merge into the preceding user message.
				var existing []anthropicChatContentBlock
				_ = json.Unmarshal(result[len(result)-1].Content, &existing)
				existing = append(existing, newBlock)
				blockJSON, _ := json.Marshal(existing)
				result[len(result)-1].Content = blockJSON
			} else {
				blockJSON, _ := json.Marshal([]anthropicChatContentBlock{newBlock})
				result = append(result, anthropicChatMessage{
					Role:    "user",
					Content: blockJSON,
				})
			}
		}
	}

	return result
}

// truncate shortens s to at most maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...(truncated)"
}

// ---------------------------------------------------------------------------
// ChatTools returns the tool definitions available to the chat agent.
// ---------------------------------------------------------------------------

// ChatTools returns the 5 built-in tool definitions for the NLQ chat agent.
func ChatTools() []ToolDef {
	return []ToolDef{
		{
			Name:        "get_issues",
			Description: "Get current health check issues. Optionally filter by namespace, checker name, or severity.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"namespace": map[string]any{
						"type":        "string",
						"description": "Filter issues by namespace",
					},
					"checker": map[string]any{
						"type":        "string",
						"description": "Filter issues by checker name",
					},
					"severity": map[string]any{
						"type":        "string",
						"description": "Filter issues by severity level",
						"enum":        []string{"Critical", "Warning", "Info"},
					},
				},
				"additionalProperties": false,
			},
		},
		{
			Name:        "get_namespaces",
			Description: "List all monitored namespaces.",
			Parameters: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
		{
			Name:        "get_health_score",
			Description: "Get the current cluster health score and summary information.",
			Parameters: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
		{
			Name:        "explain_issue",
			Description: "Get AI-enhanced details and remediation steps for a specific issue.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"issueKey": map[string]any{
						"type":        "string",
						"description": "The issue key to explain (e.g. 'CrashLoopBackOff:deployment/my-app')",
					},
				},
				"required":             []string{"issueKey"},
				"additionalProperties": false,
			},
		},
		{
			Name:        "get_checkers",
			Description: "List all health checkers and their current status.",
			Parameters: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
	}
}
