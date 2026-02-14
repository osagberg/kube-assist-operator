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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// collectEvents runs a turn and collects all emitted events.
func collectEvents(agent *ChatAgent, ctx context.Context, msgs []ChatMessage) ([]ChatEvent, int, error) {
	var events []ChatEvent
	_, tokens, err := agent.RunTurn(ctx, msgs, func(e ChatEvent) {
		events = append(events, e)
	})
	return events, tokens, err
}

// openAITextResponse builds a JSON response body for a plain text reply.
func openAITextResponse(content string, tokens int) []byte {
	resp := map[string]any{
		"choices": []map[string]any{
			{
				"message": map[string]any{
					"content": content,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"total_tokens": tokens,
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// openAIToolCallResponse builds a JSON response body with tool calls.
func openAIToolCallResponse(calls []openAIChatToolCall, tokens int) []byte {
	resp := map[string]any{
		"choices": []map[string]any{
			{
				"message": map[string]any{
					"content":    "",
					"tool_calls": calls,
				},
				"finish_reason": "tool_calls",
			},
		},
		"usage": map[string]any{
			"total_tokens": tokens,
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// anthropicTextResponse builds a JSON response body for a plain text reply.
func anthropicTextResponse(content string, inputTokens, outputTokens int) []byte {
	resp := map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": content},
		},
		"stop_reason": "end_turn",
		"usage": map[string]any{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// anthropicToolCallResponse builds a JSON response body with tool_use blocks.
func anthropicToolCallResponse(id, name string, input json.RawMessage, inputTokens, outputTokens int) []byte {
	resp := map[string]any{
		"content": []map[string]any{
			{"type": "tool_use", "id": id, "name": name, "input": input},
		},
		"stop_reason": "tool_use",
		"usage": map[string]any{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// noopExecutor always returns a fixed result.
func noopExecutor(_ context.Context, name string, _ json.RawMessage) (string, error) {
	return `{"result":"ok from ` + name + `"}`, nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestChatAgent_SimpleResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(openAITextResponse("Cluster is healthy.", 150))
	}))
	defer server.Close()

	agent := NewChatAgent(ProviderNameOpenAI, "test-key", "gpt-4o-mini", server.URL, noopExecutor)

	events, tokens, err := collectEvents(agent, context.Background(), []ChatMessage{
		{Role: "user", Content: "How is the cluster?"},
	})
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}
	if tokens != 150 {
		t.Errorf("tokens = %d, want 150", tokens)
	}

	// Expect: thinking, content, done
	types := make([]ChatEventType, 0, len(events))
	for _, e := range events {
		types = append(types, e.Type)
	}
	if len(types) < 3 {
		t.Fatalf("expected at least 3 events, got %d: %v", len(types), types)
	}
	if types[0] != ChatEventThinking {
		t.Errorf("event[0] = %s, want thinking", types[0])
	}
	if types[1] != ChatEventContent {
		t.Errorf("event[1] = %s, want content", types[1])
	}
	if types[2] != ChatEventDone {
		t.Errorf("event[2] = %s, want done", types[2])
	}

	// Verify content
	for _, e := range events {
		if e.Type == ChatEventContent && e.Content != "Cluster is healthy." {
			t.Errorf("content = %q, want %q", e.Content, "Cluster is healthy.")
		}
	}
}

func TestChatAgent_ToolCall(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")

		if n == 1 {
			// First call: AI requests tool call
			calls := []openAIChatToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      "get_issues",
						Arguments: `{"severity":"Critical"}`,
					},
				},
			}
			_, _ = w.Write(openAIToolCallResponse(calls, 100))
			return
		}

		// Second call: AI returns text after receiving tool result
		_, _ = w.Write(openAITextResponse("Found 2 critical issues in default namespace.", 200))
	}))
	defer server.Close()

	executorCalled := false
	executor := func(_ context.Context, name string, args json.RawMessage) (string, error) {
		executorCalled = true
		if name != "get_issues" { //nolint:goconst // test value
			t.Errorf("executor called with name = %q, want get_issues", name)
		}
		return `[{"type":"CrashLoopBackOff","severity":"Critical"}]`, nil
	}

	agent := NewChatAgent(ProviderNameOpenAI, "test-key", "gpt-4o-mini", server.URL, executor)
	events, tokens, err := collectEvents(agent, context.Background(), []ChatMessage{
		{Role: "user", Content: "Show me critical issues"},
	})
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}
	if !executorCalled {
		t.Error("executor was not called")
	}
	if tokens != 300 {
		t.Errorf("tokens = %d, want 300", tokens)
	}

	// Check events contain tool_call and tool_result
	var hasToolCall, hasToolResult, hasContent bool
	for _, e := range events {
		switch e.Type {
		case ChatEventToolCall:
			hasToolCall = true
			if e.Tool != "get_issues" {
				t.Errorf("tool_call tool = %q, want get_issues", e.Tool)
			}
		case ChatEventToolResult:
			hasToolResult = true
		case ChatEventContent:
			hasContent = true
		}
	}
	if !hasToolCall {
		t.Error("missing tool_call event")
	}
	if !hasToolResult {
		t.Error("missing tool_result event")
	}
	if !hasContent {
		t.Error("missing content event")
	}
}

func TestChatAgent_MultipleToolCalls(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")

		switch n {
		case 1:
			// First: call get_health_score
			calls := []openAIChatToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "get_health_score", Arguments: `{}`},
				},
			}
			_, _ = w.Write(openAIToolCallResponse(calls, 80))
		case 2:
			// Second: call get_issues
			calls := []openAIChatToolCall{
				{
					ID:   "call_2",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "get_issues", Arguments: `{}`},
				},
			}
			_, _ = w.Write(openAIToolCallResponse(calls, 90))
		default:
			// Final: text response
			_, _ = w.Write(openAITextResponse("Score is 85. No critical issues.", 100))
		}
	}))
	defer server.Close()

	var toolNames []string
	executor := func(_ context.Context, name string, _ json.RawMessage) (string, error) {
		toolNames = append(toolNames, name)
		return `{"ok":true}`, nil
	}

	agent := NewChatAgent(ProviderNameOpenAI, "test-key", "gpt-4o-mini", server.URL, executor)
	_, tokens, err := collectEvents(agent, context.Background(), []ChatMessage{
		{Role: "user", Content: "Health score and issues?"},
	})
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}

	if len(toolNames) != 2 {
		t.Fatalf("expected 2 tool calls, got %d: %v", len(toolNames), toolNames)
	}
	if toolNames[0] != "get_health_score" {
		t.Errorf("tool[0] = %q, want get_health_score", toolNames[0])
	}
	if toolNames[1] != "get_issues" {
		t.Errorf("tool[1] = %q, want get_issues", toolNames[1])
	}

	// 80 + 90 + 100
	if tokens != 270 {
		t.Errorf("tokens = %d, want 270", tokens)
	}
}

func TestChatAgent_MaxIterations(t *testing.T) {
	// Server always returns a tool call — agent should stop after maxIterations.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		calls := []openAIChatToolCall{
			{
				ID:   "call_loop",
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "get_health_score", Arguments: `{}`},
			},
		}
		_, _ = w.Write(openAIToolCallResponse(calls, 50))
	}))
	defer server.Close()

	agent := NewChatAgent(ProviderNameOpenAI, "test-key", "gpt-4o-mini", server.URL, noopExecutor)
	agent.maxIterations = 3

	events, _, err := collectEvents(agent, context.Background(), []ChatMessage{
		{Role: "user", Content: "Loop forever"},
	})

	if err == nil {
		t.Fatal("expected error for max iterations, got nil")
	}
	if !strings.Contains(err.Error(), "max iterations") {
		t.Errorf("error = %q, should contain 'max iterations'", err.Error())
	}

	// Should have an error event
	var hasError bool
	for _, e := range events {
		if e.Type == ChatEventError && strings.Contains(e.Content, "max iterations") {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected error event with 'max iterations'")
	}
}

func TestChatAgent_ToolError(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")

		if n == 1 {
			calls := []openAIChatToolCall{
				{
					ID:   "call_err",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "get_issues", Arguments: `{}`},
				},
			}
			_, _ = w.Write(openAIToolCallResponse(calls, 80))
			return
		}
		_, _ = w.Write(openAITextResponse("The tool returned an error, but the cluster seems fine.", 120))
	}))
	defer server.Close()

	executor := func(_ context.Context, name string, _ json.RawMessage) (string, error) {
		return "", fmt.Errorf("connection refused")
	}

	agent := NewChatAgent(ProviderNameOpenAI, "test-key", "gpt-4o-mini", server.URL, executor)
	events, _, err := collectEvents(agent, context.Background(), []ChatMessage{
		{Role: "user", Content: "Check issues"},
	})
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}

	// The tool result should contain the error message
	var toolResultContent string
	for _, e := range events {
		if e.Type == ChatEventToolResult {
			toolResultContent = e.Content
		}
	}
	if !strings.Contains(toolResultContent, "error: connection refused") {
		t.Errorf("tool result = %q, want to contain 'error: connection refused'", toolResultContent)
	}
}

func TestChatAgent_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should never be reached
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(openAITextResponse("should not reach", 0))
	}))
	defer server.Close()

	agent := NewChatAgent(ProviderNameOpenAI, "test-key", "gpt-4o-mini", server.URL, noopExecutor)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	events, _, err := collectEvents(agent, ctx, []ChatMessage{
		{Role: "user", Content: "Hello"},
	})
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}

	var hasError, hasDone bool
	for _, e := range events {
		if e.Type == ChatEventError {
			hasError = true
		}
		if e.Type == ChatEventDone {
			hasDone = true
		}
	}
	if !hasError {
		t.Error("expected error event for context cancellation")
	}
	if !hasDone {
		t.Error("expected done event after context cancellation error")
	}
}

func TestChatAgent_OpenAIFormat(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the request
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		// Verify headers
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key-oai" {
			t.Errorf("Authorization = %q, want 'Bearer test-key-oai'", auth)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want 'application/json'", ct)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(openAITextResponse("ok", 10))
	}))
	defer server.Close()

	agent := NewChatAgent(ProviderNameOpenAI, "test-key-oai", "gpt-4o-mini", server.URL, noopExecutor)
	_, _, err := collectEvents(agent, context.Background(), []ChatMessage{
		{Role: "user", Content: "test"},
	})
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}

	// Verify request structure
	if model, _ := receivedBody["model"].(string); model != "gpt-4o-mini" {
		t.Errorf("model = %q, want gpt-4o-mini", model)
	}

	msgs, ok := receivedBody["messages"].([]any)
	if !ok {
		t.Fatal("messages not found or wrong type")
	}
	// System + user message
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(msgs))
	}
	firstMsg := msgs[0].(map[string]any)
	if firstMsg["role"] != "system" {
		t.Errorf("first message role = %v, want system", firstMsg["role"])
	}

	// Verify tools are present
	tools, ok := receivedBody["tools"].([]any)
	if !ok {
		t.Fatal("tools not found or wrong type")
	}
	if len(tools) != 5 {
		t.Errorf("tools count = %d, want 5", len(tools))
	}

	// Verify tool format
	firstTool := tools[0].(map[string]any)
	if firstTool["type"] != "function" {
		t.Errorf("tool type = %v, want function", firstTool["type"])
	}
	fn, ok := firstTool["function"].(map[string]any)
	if !ok {
		t.Fatal("tool function field not found")
	}
	if fn["name"] == nil || fn["name"] == "" {
		t.Error("tool function name is empty")
	}
}

func TestChatAgent_AnthropicFormat(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		// Verify Anthropic-specific headers
		if apiKey := r.Header.Get("x-api-key"); apiKey != "test-key-anth" {
			t.Errorf("x-api-key = %q, want 'test-key-anth'", apiKey)
		}
		if ver := r.Header.Get("anthropic-version"); ver != anthropicAPIVersion {
			t.Errorf("anthropic-version = %q, want %q", ver, anthropicAPIVersion)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want 'application/json'", ct)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(anthropicTextResponse("ok", 5, 5))
	}))
	defer server.Close()

	agent := NewChatAgent(ProviderNameAnthropic, "test-key-anth", defaultAnthropicModel, server.URL, noopExecutor)
	_, _, err := collectEvents(agent, context.Background(), []ChatMessage{
		{Role: "user", Content: "test"},
	})
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}

	// Verify Anthropic request structure
	if model, _ := receivedBody["model"].(string); model != defaultAnthropicModel {
		t.Errorf("model = %q, want %s", model, defaultAnthropicModel)
	}

	// system is a string for chat agent (not array)
	if sys, _ := receivedBody["system"].(string); sys == "" {
		t.Error("system prompt is empty")
	}

	// Verify tools use input_schema (not parameters)
	tools, ok := receivedBody["tools"].([]any)
	if !ok {
		t.Fatal("tools not found or wrong type")
	}
	if len(tools) != 5 {
		t.Errorf("tools count = %d, want 5", len(tools))
	}

	firstTool := tools[0].(map[string]any)
	if _, ok := firstTool["input_schema"]; !ok {
		t.Error("tool should have input_schema (Anthropic format)")
	}
	if _, ok := firstTool["parameters"]; ok {
		t.Error("tool should NOT have parameters (that's OpenAI format)")
	}
}

func TestChatAgent_AnthropicToolCall(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")

		if n == 1 {
			_, _ = w.Write(anthropicToolCallResponse("toolu_1", "get_health_score", json.RawMessage(`{}`), 50, 30)) //nolint:unconvert // RawMessage for clarity
			return
		}
		_, _ = w.Write(anthropicTextResponse("Health score is 92.", 40, 20))
	}))
	defer server.Close()

	agent := NewChatAgent(ProviderNameAnthropic, "test-key", defaultAnthropicModel, server.URL, noopExecutor)
	events, tokens, err := collectEvents(agent, context.Background(), []ChatMessage{
		{Role: "user", Content: "What's the health score?"},
	})
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}

	// 50+30 + 40+20
	if tokens != 140 {
		t.Errorf("tokens = %d, want 140", tokens)
	}

	var hasToolCall, hasContent bool
	for _, e := range events {
		if e.Type == ChatEventToolCall && e.Tool == "get_health_score" {
			hasToolCall = true
		}
		if e.Type == ChatEventContent && e.Content == "Health score is 92." {
			hasContent = true
		}
	}
	if !hasToolCall {
		t.Error("missing tool_call event for get_health_score")
	}
	if !hasContent {
		t.Error("missing content event")
	}
}

func TestChatTools_Definitions(t *testing.T) {
	tools := ChatTools()
	if len(tools) != 5 {
		t.Fatalf("ChatTools() returned %d tools, want 5", len(tools))
	}

	expected := map[string]bool{
		"get_issues":       false,
		"get_namespaces":   false,
		"get_health_score": false,
		"explain_issue":    false,
		"get_checkers":     false,
	}

	for _, tool := range tools {
		if _, ok := expected[tool.Name]; !ok {
			t.Errorf("unexpected tool: %s", tool.Name)
			continue
		}
		expected[tool.Name] = true

		if tool.Description == "" {
			t.Errorf("tool %s has empty description", tool.Name)
		}
		if tool.Parameters == nil {
			t.Errorf("tool %s has nil parameters", tool.Name)
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing tool: %s", name)
		}
	}
}

func TestBuildAnthropicMessages(t *testing.T) {
	t.Run("user message", func(t *testing.T) {
		msgs, err := buildAnthropicMessages([]ChatMessage{
			{Role: "user", Content: "hello"},
		})
		if err != nil {
			t.Fatalf("buildAnthropicMessages error: %v", err)
		}
		if len(msgs) != 1 {
			t.Fatalf("expected 1 message, got %d", len(msgs))
		}
		if msgs[0].Role != "user" { //nolint:goconst // test value
			t.Errorf("role = %q, want user", msgs[0].Role)
		}
		// Content should be a JSON string (quoted)
		var s string
		if err := json.Unmarshal(msgs[0].Content, &s); err != nil {
			t.Fatalf("failed to unmarshal user content as string: %v", err)
		}
		if s != "hello" {
			t.Errorf("content = %q, want hello", s)
		}
	})

	t.Run("assistant with tool calls", func(t *testing.T) {
		msgs, err := buildAnthropicMessages([]ChatMessage{
			{Role: "assistant", Content: "Let me check.", ToolCalls: []ToolCall{
				{ID: "tc_1", Name: "get_issues", Args: json.RawMessage(`{"severity":"Critical"}`)},
			}},
		})
		if err != nil {
			t.Fatalf("buildAnthropicMessages error: %v", err)
		}
		if len(msgs) != 1 {
			t.Fatalf("expected 1 message, got %d", len(msgs))
		}
		if msgs[0].Role != "assistant" { //nolint:goconst // test value
			t.Errorf("role = %q, want assistant", msgs[0].Role)
		}
		var blocks []anthropicChatContentBlock
		if err := json.Unmarshal(msgs[0].Content, &blocks); err != nil {
			t.Fatalf("failed to unmarshal assistant content: %v", err)
		}
		if len(blocks) != 2 {
			t.Fatalf("expected 2 blocks, got %d", len(blocks))
		}
		if blocks[0].Type != "text" || blocks[0].Text != "Let me check." { //nolint:goconst // test value
			t.Errorf("block[0] = %+v, want text block with 'Let me check.'", blocks[0])
		}
		if blocks[1].Type != "tool_use" || blocks[1].Name != "get_issues" { //nolint:goconst // test value
			t.Errorf("block[1] = %+v, want tool_use block for get_issues", blocks[1])
		}
	})

	t.Run("consecutive tool results merged", func(t *testing.T) {
		msgs, err := buildAnthropicMessages([]ChatMessage{
			{Role: "user", Content: "check"},
			{Role: "assistant", Content: "", ToolCalls: []ToolCall{
				{ID: "tc_1", Name: "get_issues", Args: json.RawMessage(`{}`)},
				{ID: "tc_2", Name: "get_health_score", Args: json.RawMessage(`{}`)},
			}},
			{Role: "tool", Content: "issues result", ToolCallID: "tc_1"},
			{Role: "tool", Content: "health result", ToolCallID: "tc_2"},
		})
		if err != nil {
			t.Fatalf("buildAnthropicMessages error: %v", err)
		}
		// user + assistant + single merged user message for both tool results
		if len(msgs) != 3 {
			t.Fatalf("expected 3 messages, got %d", len(msgs))
		}
		// The last message should be "user" with 2 tool_result blocks
		if msgs[2].Role != "user" {
			t.Errorf("msgs[2].Role = %q, want user", msgs[2].Role)
		}
		var blocks []anthropicChatContentBlock
		if err := json.Unmarshal(msgs[2].Content, &blocks); err != nil {
			t.Fatalf("failed to unmarshal tool result content: %v", err)
		}
		if len(blocks) != 2 {
			t.Fatalf("expected 2 tool_result blocks, got %d", len(blocks))
		}
		if blocks[0].ToolUseID != "tc_1" || blocks[1].ToolUseID != "tc_2" {
			t.Errorf("tool_use_ids = [%s, %s], want [tc_1, tc_2]", blocks[0].ToolUseID, blocks[1].ToolUseID)
		}
	})

	t.Run("empty tool result content not omitted", func(t *testing.T) {
		msgs, err := buildAnthropicMessages([]ChatMessage{
			{Role: "assistant", Content: "", ToolCalls: []ToolCall{
				{ID: "tc_1", Name: "get_issues", Args: json.RawMessage(`{}`)},
			}},
			{Role: "tool", Content: "", ToolCallID: "tc_1"},
		})
		if err != nil {
			t.Fatalf("buildAnthropicMessages error: %v", err)
		}
		if len(msgs) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(msgs))
		}
		// Marshal the tool result message and verify "content" key is present
		raw := msgs[1].Content
		var blocks []json.RawMessage
		if err := json.Unmarshal(raw, &blocks); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if len(blocks) != 1 {
			t.Fatalf("expected 1 block, got %d", len(blocks))
		}
		blockStr := string(blocks[0])
		// The "content" key must be present even when empty (Fix 3 validation)
		if !strings.Contains(blockStr, `"content"`) {
			t.Errorf("tool_result block missing 'content' key: %s", blockStr)
		}
	})
}

func TestChatAgent_RunTurnReturnsMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(openAITextResponse("All good.", 50))
	}))
	defer server.Close()

	agent := NewChatAgent(ProviderNameOpenAI, "test-key", "gpt-4o-mini", server.URL, noopExecutor)

	initial := []ChatMessage{
		{Role: "user", Content: "How are things?"},
	}
	result, _, err := agent.RunTurn(context.Background(), initial, func(ChatEvent) {})
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}

	// Should contain the original user message + the assistant response
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[0].Role != "user" || result[0].Content != "How are things?" {
		t.Errorf("result[0] = %+v, want user message", result[0])
	}
	if result[1].Role != "assistant" || result[1].Content != "All good." {
		t.Errorf("result[1] = %+v, want assistant message with 'All good.'", result[1])
	}

	// Verify original slice was not mutated
	if len(initial) != 1 {
		t.Errorf("original messages slice was mutated: len = %d, want 1", len(initial))
	}
}

func TestChatAgent_UnsupportedProvider(t *testing.T) {
	agent := NewChatAgent("unsupported", "key", "model", "http://localhost", noopExecutor)

	var events []ChatEvent
	_, _, err := agent.RunTurn(context.Background(), []ChatMessage{
		{Role: "user", Content: "test"},
	}, func(e ChatEvent) {
		events = append(events, e)
	})

	if err == nil {
		t.Fatal("expected error for unsupported provider, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported provider") {
		t.Errorf("error = %q, want to contain 'unsupported provider'", err.Error())
	}
}

func TestChatAgent_ParallelToolCalls(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")

		if n == 1 {
			// First call: return 2 tool calls in a single response
			calls := []openAIChatToolCall{
				{
					ID:   "call_a",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "get_issues", Arguments: `{}`},
				},
				{
					ID:   "call_b",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "get_health_score", Arguments: `{}`},
				},
			}
			_, _ = w.Write(openAIToolCallResponse(calls, 100))
			return
		}

		// Second call: final text response
		_, _ = w.Write(openAITextResponse("Both tools executed.", 120))
	}))
	defer server.Close()

	var executedTools []string
	executor := func(_ context.Context, name string, _ json.RawMessage) (string, error) {
		executedTools = append(executedTools, name)
		return `{"ok":true}`, nil
	}

	agent := NewChatAgent(ProviderNameOpenAI, "test-key", "gpt-4o-mini", server.URL, executor)

	var events []ChatEvent
	_, _, err := agent.RunTurn(context.Background(), []ChatMessage{
		{Role: "user", Content: "Run both tools"},
	}, func(e ChatEvent) {
		events = append(events, e)
	})
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}

	// Verify both tools were executed
	if len(executedTools) != 2 {
		t.Fatalf("expected 2 tool executions, got %d: %v", len(executedTools), executedTools)
	}

	// Count tool_call and tool_result events
	var toolCallCount, toolResultCount int
	for _, e := range events {
		switch e.Type {
		case ChatEventToolCall:
			toolCallCount++
		case ChatEventToolResult:
			toolResultCount++
		}
	}
	if toolCallCount != 2 {
		t.Errorf("tool_call events = %d, want 2", toolCallCount)
	}
	if toolResultCount != 2 {
		t.Errorf("tool_result events = %d, want 2", toolResultCount)
	}
}
