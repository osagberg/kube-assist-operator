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

package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/osagberg/kube-assist-operator/internal/ai"
	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/datasource"
	"github.com/osagberg/kube-assist-operator/internal/history"
)

// newChatTestServer creates a minimal Server with chat enabled for testing.
func newChatTestServer(t *testing.T, opts ...func(*Server)) *Server {
	t.Helper()
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	noop := ai.NewNoOpProvider()
	s := NewServer(datasource.NewKubernetes(client), registry, ":0")
	s.WithAI(noop, true)
	s.WithChat(true, 10, 50000, 100, 30*time.Minute)
	s.WithChatAIConfig("noop", "", "", "")

	// Seed cluster state
	s.mu.Lock()
	cs := s.getOrCreateClusterState("")
	cs.latest = &HealthUpdate{
		Timestamp:  time.Now(),
		Namespaces: []string{"default", "kube-system"},
		Results: map[string]CheckResult{
			"workloads": {
				Name:    "workloads",
				Healthy: 5,
				Issues: []Issue{
					{
						Type:      "CrashLoopBackOff",
						Severity:  "Critical",
						Resource:  "deployment/my-app",
						Namespace: "default",
						Message:   "Pod is crash looping",
					},
					{
						Type:      "HighMemory",
						Severity:  "Warning",
						Resource:  "deployment/cache",
						Namespace: "kube-system",
						Message:   "Memory usage above 80%",
					},
				},
			},
		},
		Summary: Summary{
			TotalHealthy: 5,
			TotalIssues:  2,
			HealthScore:  75.0,
		},
	}
	cs.history = history.New(100)
	s.mu.Unlock()

	for _, opt := range opts {
		opt(s)
	}
	return s
}

func chatBody(sessionID, message string) *bytes.Buffer {
	body, _ := json.Marshal(chatRequest{
		SessionID: sessionID,
		Message:   message,
	})
	return bytes.NewBuffer(body)
}

func TestHandleChat_POSTOnly(t *testing.T) {
	s := newChatTestServer(t)
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/api/chat", chatBody("", "hello"))
		rr := httptest.NewRecorder()
		s.handleChat(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("handleChat(%s) status = %d, want %d", method, rr.Code, http.StatusMethodNotAllowed)
		}
	}
}

func TestHandleChat_EmptyMessage(t *testing.T) {
	s := newChatTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/chat", chatBody("", ""))
	rr := httptest.NewRecorder()
	s.handleChat(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("handleChat(empty) status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleChat_WhitespaceOnlyMessage(t *testing.T) {
	s := newChatTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/chat", chatBody("", "   "))
	rr := httptest.NewRecorder()
	s.handleChat(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("handleChat(whitespace) status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleChat_MessageTooLong(t *testing.T) {
	s := newChatTestServer(t)
	longMsg := strings.Repeat("x", maxChatMessageLen+1)
	req := httptest.NewRequest(http.MethodPost, "/api/chat", chatBody("", longMsg))
	rr := httptest.NewRecorder()
	s.handleChat(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("handleChat(long) status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleChat_AIDisabled(t *testing.T) {
	s := newChatTestServer(t, func(s *Server) {
		s.aiEnabled = false
	})
	req := httptest.NewRequest(http.MethodPost, "/api/chat", chatBody("", "hello"))
	rr := httptest.NewRecorder()
	s.handleChat(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("handleChat(ai disabled) status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleChat_ChatDisabled(t *testing.T) {
	s := newChatTestServer(t, func(s *Server) {
		s.chatEnabled = false
	})
	req := httptest.NewRequest(http.MethodPost, "/api/chat", chatBody("", "hello"))
	rr := httptest.NewRecorder()
	s.handleChat(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("handleChat(chat disabled) status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleChat_MaxSessionsReached(t *testing.T) {
	existingID := "dddddddd-dddd-dddd-dddd-dddddddddddd"
	s := newChatTestServer(t, func(s *Server) {
		s.chatMaxSessions = 1
		// Pre-fill with one session
		s.chatSessions[existingID] = &ChatSession{
			ID:             existingID,
			Messages:       []ai.ChatMessage{},
			TokenBudget:    50000,
			CreatedAt:      time.Now(),
			LastAccessedAt: time.Now(),
		}
	})
	req := httptest.NewRequest(http.MethodPost, "/api/chat", chatBody("", "hello"))
	rr := httptest.NewRecorder()
	s.handleChat(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("handleChat(max sessions) status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleChat_NewSession_SSEFormat(t *testing.T) {
	// The noop provider will return an error since it doesn't implement
	// the chat API. But we can verify the SSE headers and session_id event.
	s := newChatTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/chat", chatBody("", "hello"))
	rr := httptest.NewRecorder()
	s.handleChat(rr, req)

	// SSE headers should be set (status 200 implied by header write before body)
	ct := rr.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	cc := rr.Header().Get("Cache-Control")
	if cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}

	// Body should contain SSE data lines
	body := rr.Body.String()
	if !strings.Contains(body, "data: ") {
		t.Error("response body should contain SSE data lines")
	}

	// First event should be session_id
	lines := strings.Split(strings.TrimSpace(body), "\n\n")
	if len(lines) == 0 {
		t.Fatal("expected at least one SSE event")
	}
	firstLine := strings.TrimPrefix(lines[0], "data: ")
	var firstEvt map[string]any
	if err := json.Unmarshal([]byte(firstLine), &firstEvt); err != nil {
		t.Fatalf("failed to parse first SSE event: %v", err)
	}
	if firstEvt["type"] != "session_id" {
		t.Errorf("first event type = %v, want session_id", firstEvt["type"])
	}
	if _, ok := firstEvt["sessionId"]; !ok {
		t.Error("session_id event missing sessionId field")
	}
}

func TestHandleChat_ExistingSession(t *testing.T) {
	s := newChatTestServer(t)

	sessionID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	// Pre-create a session
	session := &ChatSession{
		ID:             sessionID,
		Messages:       []ai.ChatMessage{{Role: "user", Content: "first message"}},
		TokenBudget:    50000,
		CreatedAt:      time.Now(),
		LastAccessedAt: time.Now(),
	}
	s.chatMu.Lock()
	s.chatSessions[sessionID] = session
	s.chatMu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/chat", chatBody(sessionID, "follow up"))
	rr := httptest.NewRecorder()
	s.handleChat(rr, req)

	// Should NOT have a session_id event (session already exists)
	body := rr.Body.String()
	for line := range strings.SplitSeq(strings.TrimSpace(body), "\n\n") {
		data := strings.TrimPrefix(line, "data: ")
		var evt map[string]any
		if err := json.Unmarshal([]byte(data), &evt); err != nil {
			continue
		}
		if evt["type"] == "session_id" {
			t.Error("existing session should not receive session_id event")
		}
	}

	// Session should now have the new user message appended
	session.mu.Lock()
	found := false
	for _, m := range session.Messages {
		if m.Content == "follow up" {
			found = true
			break
		}
	}
	session.mu.Unlock()
	if !found {
		t.Error("new message not appended to existing session")
	}
}

func TestHandleChat_SessionBudgetExceeded(t *testing.T) {
	s := newChatTestServer(t)

	budgetSessionID := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"

	// Pre-create a session that has exceeded its budget
	session := &ChatSession{
		ID:             budgetSessionID,
		Messages:       []ai.ChatMessage{},
		TokensUsed:     50000,
		TokenBudget:    50000,
		CreatedAt:      time.Now(),
		LastAccessedAt: time.Now(),
	}
	s.chatMu.Lock()
	s.chatSessions[budgetSessionID] = session
	s.chatMu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/chat", chatBody(budgetSessionID, "hello"))
	rr := httptest.NewRecorder()
	s.handleChat(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "budget exceeded") {
		t.Error("expected budget exceeded error in SSE events")
	}
}

func TestHandleChat_InvalidJSON(t *testing.T) {
	s := newChatTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewBufferString("not json"))
	rr := httptest.NewRecorder()
	s.handleChat(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("handleChat(invalid json) status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleChat_SessionTTLCleanup(t *testing.T) {
	s := newChatTestServer(t, func(s *Server) {
		s.chatSessionTTL = 1 * time.Millisecond
	})

	// Add an expired session
	s.chatMu.Lock()
	s.chatSessions["expired-session"] = &ChatSession{
		ID:             "expired-session",
		Messages:       []ai.ChatMessage{},
		TokenBudget:    50000,
		CreatedAt:      time.Now().Add(-1 * time.Hour),
		LastAccessedAt: time.Now().Add(-1 * time.Hour),
	}
	s.chatSessions["fresh-session"] = &ChatSession{
		ID:             "fresh-session",
		Messages:       []ai.ChatMessage{},
		TokenBudget:    50000,
		CreatedAt:      time.Now(),
		LastAccessedAt: time.Now(),
	}
	s.chatMu.Unlock()

	// Manually call cleanup logic (simulating what the goroutine does)
	s.chatMu.Lock()
	now := time.Now()
	for id, session := range s.chatSessions {
		session.mu.Lock()
		if now.Sub(session.LastAccessedAt) > s.chatSessionTTL {
			delete(s.chatSessions, id)
		}
		session.mu.Unlock()
	}
	s.chatMu.Unlock()

	s.chatMu.RLock()
	_, expiredExists := s.chatSessions["expired-session"]
	_, freshExists := s.chatSessions["fresh-session"]
	s.chatMu.RUnlock()

	if expiredExists {
		t.Error("expired session should have been cleaned up")
	}
	if !freshExists {
		t.Error("fresh session should not have been cleaned up")
	}
}

func TestHandleCapabilities_ChatEnabled(t *testing.T) {
	s := newChatTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rr := httptest.NewRecorder()
	s.handleCapabilities(rr, req)

	var resp map[string]bool
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse capabilities: %v", err)
	}
	if !resp["chat"] {
		t.Error("capabilities.chat should be true when chat and AI are enabled")
	}
}

func TestHandleCapabilities_ChatDisabled(t *testing.T) {
	s := newChatTestServer(t, func(s *Server) {
		s.chatEnabled = false
	})
	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rr := httptest.NewRecorder()
	s.handleCapabilities(rr, req)

	var resp map[string]bool
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse capabilities: %v", err)
	}
	if resp["chat"] {
		t.Error("capabilities.chat should be false when chat is disabled")
	}
}

// --- Tool executor tests ---

func TestChatToolGetIssues_All(t *testing.T) {
	s := newChatTestServer(t)
	executor := s.buildChatToolExecutor("")
	result, err := executor(nil, "get_issues", nil)
	if err != nil {
		t.Fatalf("get_issues error: %v", err)
	}
	var issues []Issue
	if err := json.Unmarshal([]byte(result), &issues); err != nil {
		t.Fatalf("get_issues parse error: %v", err)
	}
	if len(issues) != 2 {
		t.Errorf("get_issues returned %d issues, want 2", len(issues))
	}
}

func TestChatToolGetIssues_FilterNamespace(t *testing.T) {
	s := newChatTestServer(t)
	executor := s.buildChatToolExecutor("")
	args, _ := json.Marshal(map[string]string{"namespace": "default"})
	result, err := executor(nil, "get_issues", args)
	if err != nil {
		t.Fatalf("get_issues error: %v", err)
	}
	var issues []Issue
	if err := json.Unmarshal([]byte(result), &issues); err != nil {
		t.Fatalf("get_issues parse error: %v", err)
	}
	if len(issues) != 1 {
		t.Errorf("get_issues(namespace=default) returned %d issues, want 1", len(issues))
	}
}

func TestChatToolGetIssues_FilterSeverity(t *testing.T) {
	s := newChatTestServer(t)
	executor := s.buildChatToolExecutor("")
	args, _ := json.Marshal(map[string]string{"severity": "Warning"})
	result, err := executor(nil, "get_issues", args)
	if err != nil {
		t.Fatalf("get_issues error: %v", err)
	}
	var issues []Issue
	if err := json.Unmarshal([]byte(result), &issues); err != nil {
		t.Fatalf("get_issues parse error: %v", err)
	}
	if len(issues) != 1 {
		t.Errorf("get_issues(severity=Warning) returned %d issues, want 1", len(issues))
	}
}

func TestChatToolGetNamespaces(t *testing.T) {
	s := newChatTestServer(t)
	executor := s.buildChatToolExecutor("")
	result, err := executor(nil, "get_namespaces", nil)
	if err != nil {
		t.Fatalf("get_namespaces error: %v", err)
	}
	var namespaces []string
	if err := json.Unmarshal([]byte(result), &namespaces); err != nil {
		t.Fatalf("get_namespaces parse error: %v", err)
	}
	if len(namespaces) != 2 {
		t.Errorf("get_namespaces returned %d, want 2", len(namespaces))
	}
}

func TestChatToolGetHealthScore(t *testing.T) {
	s := newChatTestServer(t)
	executor := s.buildChatToolExecutor("")
	result, err := executor(nil, "get_health_score", nil)
	if err != nil {
		t.Fatalf("get_health_score error: %v", err)
	}
	var summary Summary
	if err := json.Unmarshal([]byte(result), &summary); err != nil {
		t.Fatalf("get_health_score parse error: %v", err)
	}
	if summary.HealthScore != 75.0 {
		t.Errorf("healthScore = %f, want 75.0", summary.HealthScore)
	}
}

func TestChatToolGetCheckers(t *testing.T) {
	s := newChatTestServer(t)
	executor := s.buildChatToolExecutor("")
	result, err := executor(nil, "get_checkers", nil)
	if err != nil {
		t.Fatalf("get_checkers error: %v", err)
	}
	var checkers []string
	if err := json.Unmarshal([]byte(result), &checkers); err != nil {
		t.Fatalf("get_checkers parse error: %v", err)
	}
	if len(checkers) != 1 || checkers[0] != "workloads" {
		t.Errorf("get_checkers = %v, want [workloads]", checkers)
	}
}

func TestChatToolExplainIssue_NoEnhancements(t *testing.T) {
	s := newChatTestServer(t)
	executor := s.buildChatToolExecutor("")
	args, _ := json.Marshal(map[string]string{"issueKey": "test-key"})
	result, err := executor(nil, "explain_issue", args)
	if err != nil {
		t.Fatalf("explain_issue error: %v", err)
	}
	if !strings.Contains(result, "No AI enhancements") {
		t.Errorf("explain_issue should report no enhancements, got: %s", result)
	}
}

func TestChatToolExplainIssue_WithEnhancements(t *testing.T) {
	s := newChatTestServer(t)

	// Seed AI enhancement data
	s.mu.Lock()
	cs := s.clusters[""]
	cs.lastAIEnhancements = map[string]map[string]aiEnhancement{
		"workloads": {
			"crash-key": {
				Suggestion: "Increase memory limits",
				RootCause:  "OOM killed",
			},
		},
	}
	s.mu.Unlock()

	executor := s.buildChatToolExecutor("")
	args, _ := json.Marshal(map[string]string{"issueKey": "crash-key"})
	result, err := executor(nil, "explain_issue", args)
	if err != nil {
		t.Fatalf("explain_issue error: %v", err)
	}
	if !strings.Contains(result, "Increase memory limits") {
		t.Errorf("explain_issue should return enhancement, got: %s", result)
	}
}

func TestChatToolUnknown(t *testing.T) {
	s := newChatTestServer(t)
	executor := s.buildChatToolExecutor("")
	_, err := executor(nil, "unknown_tool", nil)
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

func TestChatToolGetIssues_NoCluster(t *testing.T) {
	s := newChatTestServer(t)
	executor := s.buildChatToolExecutor("nonexistent-cluster")
	result, err := executor(nil, "get_issues", nil)
	if err != nil {
		t.Fatalf("get_issues error: %v", err)
	}
	if result != "[]" {
		t.Errorf("get_issues for nonexistent cluster = %q, want []", result)
	}
}

func TestGetOrCreateChatSession_New(t *testing.T) {
	s := newChatTestServer(t)
	session, created, err := s.getOrCreateChatSession("")
	if err != nil {
		t.Fatalf("getOrCreateChatSession error: %v", err)
	}
	if !created {
		t.Error("expected created=true for new session")
	}
	if session.ID == "" {
		t.Error("session ID should not be empty")
	}
	if session.TokenBudget != 50000 {
		t.Errorf("token budget = %d, want 50000", session.TokenBudget)
	}
}

func TestGetOrCreateChatSession_Existing(t *testing.T) {
	s := newChatTestServer(t)
	existingID := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	s.chatMu.Lock()
	s.chatSessions[existingID] = &ChatSession{ID: existingID, TokenBudget: 50000, CreatedAt: time.Now(), LastAccessedAt: time.Now()}
	s.chatMu.Unlock()

	session, created, err := s.getOrCreateChatSession(existingID)
	if err != nil {
		t.Fatalf("getOrCreateChatSession error: %v", err)
	}
	if created {
		t.Error("expected created=false for existing session")
	}
	if session.ID != existingID {
		t.Errorf("session ID = %q, want %s", session.ID, existingID)
	}
}

func TestGetOrCreateChatSession_MaxReached(t *testing.T) {
	s := newChatTestServer(t, func(s *Server) {
		s.chatMaxSessions = 0
	})
	_, _, err := s.getOrCreateChatSession("")
	if err == nil {
		t.Error("expected error when max sessions reached")
	}
}

func TestWithChat(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	s := NewServer(datasource.NewKubernetes(client), registry, ":0")
	s.WithChat(true, 5, 10000, 50, 15*time.Minute)

	if !s.chatEnabled {
		t.Error("chatEnabled should be true")
	}
	if s.chatMaxTurns != 5 {
		t.Errorf("chatMaxTurns = %d, want 5", s.chatMaxTurns)
	}
	if s.chatTokenBudget != 10000 {
		t.Errorf("chatTokenBudget = %d, want 10000", s.chatTokenBudget)
	}
	if s.chatMaxSessions != 50 {
		t.Errorf("chatMaxSessions = %d, want 50", s.chatMaxSessions)
	}
	if s.chatSessionTTL != 15*time.Minute {
		t.Errorf("chatSessionTTL = %v, want 15m", s.chatSessionTTL)
	}
	if s.chatSessions == nil {
		t.Error("chatSessions map should be initialized")
	}
}

func TestWithChatAIConfig(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	registry := checker.NewRegistry()

	s := NewServer(datasource.NewKubernetes(client), registry, ":0")
	s.WithChatAIConfig("openai", "test-key", "gpt-4", "https://custom.endpoint/v1")

	if s.chatProviderName != "openai" {
		t.Errorf("chatProviderName = %q, want openai", s.chatProviderName)
	}
	if s.chatAIKey != "test-key" {
		t.Errorf("chatAIKey = %q, want test-key", s.chatAIKey)
	}
	if s.chatAIModel != "gpt-4" {
		t.Errorf("chatAIModel = %q, want gpt-4", s.chatAIModel)
	}
	if s.chatEndpoint != "https://custom.endpoint/v1" {
		t.Errorf("chatEndpoint = %q, want https://custom.endpoint/v1", s.chatEndpoint)
	}
}

func TestHandleChat_ConcurrentSameSession(t *testing.T) {
	s := newChatTestServer(t)

	sessionID := "11111111-2222-3333-4444-555555555555"

	// Pre-create a session with inFlight already set
	session := &ChatSession{
		ID:             sessionID,
		Messages:       []ai.ChatMessage{},
		TokenBudget:    50000,
		CreatedAt:      time.Now(),
		LastAccessedAt: time.Now(),
		inFlight:       true,
	}
	s.chatMu.Lock()
	s.chatSessions[sessionID] = session
	s.chatMu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/chat", chatBody(sessionID, "hello"))
	rr := httptest.NewRecorder()
	s.handleChat(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("handleChat(concurrent same session) status = %d, want %d", rr.Code, http.StatusConflict)
	}
	if !strings.Contains(rr.Body.String(), "already in progress") {
		t.Error("expected 'already in progress' message in response")
	}
}

func TestHandleChat_SessionIdReuse(t *testing.T) {
	s := newChatTestServer(t)

	clientID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	req := httptest.NewRequest(http.MethodPost, "/api/chat", chatBody(clientID, "hello"))
	rr := httptest.NewRecorder()
	s.handleChat(rr, req)

	// The session should be stored under the client-supplied ID
	s.chatMu.RLock()
	_, exists := s.chatSessions[clientID]
	s.chatMu.RUnlock()

	if !exists {
		t.Errorf("session should be stored under client-supplied ID %q", clientID)
	}
}

func TestHandleChat_InvalidSessionId(t *testing.T) {
	s := newChatTestServer(t)

	// Non-UUID session ID should be rejected
	req := httptest.NewRequest(http.MethodPost, "/api/chat", chatBody("not-a-uuid", "hello"))
	rr := httptest.NewRecorder()
	s.handleChat(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("handleChat(invalid session id) status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rr.Body.String(), "invalid session ID") {
		t.Error("expected 'invalid session ID' error message")
	}
}

func TestCleanupChatSessions_TOCTOU(t *testing.T) {
	s := newChatTestServer(t, func(s *Server) {
		s.chatSessionTTL = 1 * time.Millisecond
	})

	// Create an expired session
	session := &ChatSession{
		ID:             "toctou-session",
		Messages:       []ai.ChatMessage{},
		TokenBudget:    50000,
		CreatedAt:      time.Now().Add(-1 * time.Hour),
		LastAccessedAt: time.Now().Add(-1 * time.Hour),
	}
	s.chatMu.Lock()
	s.chatSessions["toctou-session"] = session
	s.chatMu.Unlock()

	// Pass 1: identify expired sessions under RLock (simulating what cleanup does)
	var expired []string
	s.chatMu.RLock()
	now := time.Now()
	for id, sess := range s.chatSessions {
		sess.mu.Lock()
		if now.Sub(sess.LastAccessedAt) > s.chatSessionTTL {
			expired = append(expired, id)
		}
		sess.mu.Unlock()
	}
	s.chatMu.RUnlock()

	if len(expired) == 0 {
		t.Fatal("expected session to be identified as expired in pass 1")
	}

	// Between pass 1 and pass 2, the session gets accessed (simulating TOCTOU race)
	session.mu.Lock()
	session.LastAccessedAt = time.Now()
	session.mu.Unlock()

	// Pass 2: with the TOCTOU fix, it should re-check and NOT delete
	if len(expired) > 0 {
		now = time.Now()
		s.chatMu.Lock()
		for _, id := range expired {
			if sess, ok := s.chatSessions[id]; ok {
				sess.mu.Lock()
				if now.Sub(sess.LastAccessedAt) > s.chatSessionTTL {
					delete(s.chatSessions, id)
				}
				sess.mu.Unlock()
			}
		}
		s.chatMu.Unlock()
	}

	// Session should NOT have been deleted because it was refreshed
	s.chatMu.RLock()
	_, exists := s.chatSessions["toctou-session"]
	s.chatMu.RUnlock()

	if !exists {
		t.Error("session should not have been deleted after being refreshed between pass 1 and pass 2")
	}
}

func TestHandleChat_RequestBodyTooLarge(t *testing.T) {
	s := newChatTestServer(t)

	// Create a valid JSON body that exceeds maxChatBodySize (64 KB).
	// The message field needs to be large enough to push the total over 64KB.
	largeMsg := strings.Repeat("x", maxChatBodySize)
	body, _ := json.Marshal(chatRequest{
		SessionID: "",
		Message:   largeMsg,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	s.handleChat(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("handleChat(large body) status = %d, want %d", rr.Code, http.StatusRequestEntityTooLarge)
	}
}
