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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/osagberg/kube-assist-operator/internal/ai"
)

// maxChatMessageLen is the maximum length of a single chat message.
const maxChatMessageLen = 2000

// maxChatBodySize is the maximum body size for chat requests.
const maxChatBodySize = 1 << 16 // 64 KB

// ChatSession holds the state for an interactive chat conversation.
type ChatSession struct {
	ID             string
	Messages       []ai.ChatMessage
	TokensUsed     int
	TokenBudget    int
	CreatedAt      time.Time
	LastAccessedAt time.Time
	mu             sync.Mutex
}

// chatRequest is the JSON body for POST /api/chat.
type chatRequest struct {
	SessionID string `json:"sessionId"`
	Message   string `json:"message"`
	ClusterID string `json:"clusterId"`
}

// handleChat handles POST /api/chat. It manages chat sessions and streams
// responses as Server-Sent Events.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req chatRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxChatBodySize)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		if isMaxBytesError(err) {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	// Validate message
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}
	if len(msg) > maxChatMessageLen {
		http.Error(w, fmt.Sprintf("message exceeds maximum length of %d characters", maxChatMessageLen), http.StatusBadRequest)
		return
	}

	// Check that AI and chat are enabled
	if !s.chatEnabled || !s.aiEnabled {
		http.Error(w, "Chat is not available", http.StatusServiceUnavailable)
		return
	}

	// Check AI provider is available
	s.mu.RLock()
	provider := s.aiProvider
	s.mu.RUnlock()
	if provider == nil || !provider.Available() {
		http.Error(w, "AI provider is not available", http.StatusServiceUnavailable)
		return
	}

	// Get or create session
	session, created, err := s.getOrCreateChatSession(req.SessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	// Set SSE headers before any event writes.
	setSSEHeaders(w)
	flusher, _ := w.(http.Flusher)

	// Lock session briefly to check budget, append user message, and snapshot.
	session.mu.Lock()
	session.LastAccessedAt = time.Now()

	// Send session_id event if newly created
	if created {
		writeSSEEvent(w, map[string]any{
			"type":      "session_id",
			"sessionId": session.ID,
		})
		if flusher != nil {
			flusher.Flush()
		}
	}

	// Check session token budget
	if session.TokensUsed >= session.TokenBudget {
		session.mu.Unlock()
		writeSSEEvent(w, map[string]any{
			"type":    string(ai.ChatEventError),
			"content": "Session token budget exceeded. Start a new session.",
		})
		writeSSEEvent(w, map[string]any{
			"type":      string(ai.ChatEventDone),
			"sessionId": session.ID,
		})
		if flusher != nil {
			flusher.Flush()
		}
		return
	}

	// Append user message and snapshot history for the agent.
	session.Messages = append(session.Messages, ai.ChatMessage{
		Role:    "user",
		Content: msg,
	})
	msgsCopy := make([]ai.ChatMessage, len(session.Messages))
	copy(msgsCopy, session.Messages)
	session.mu.Unlock()

	// Check global budget (no session lock needed)
	if mgr, ok := provider.(*ai.Manager); ok {
		if budget := mgr.Budget(); budget != nil {
			if err := budget.CheckAllowance(500); err != nil {
				writeSSEEvent(w, map[string]any{
					"type":    string(ai.ChatEventError),
					"content": "Global AI token budget exceeded.",
				})
				writeSSEEvent(w, map[string]any{
					"type":      string(ai.ChatEventDone),
					"sessionId": session.ID,
				})
				if flusher != nil {
					flusher.Flush()
				}
				return
			}
		}
	}

	// Build tool executor from cluster state
	clusterID := req.ClusterID
	executor := s.buildChatToolExecutor(clusterID)

	// Determine provider name from the manager
	providerName := s.chatProviderName
	if providerName == "" {
		if mgr, ok := provider.(*ai.Manager); ok {
			providerName = mgr.Name()
		}
	}

	// Create chat agent
	agent := ai.NewChatAgent(providerName, s.chatAIKey, s.chatAIModel, s.chatEndpoint, executor)
	if s.chatMaxTurns > 0 {
		agent.SetMaxIterations(s.chatMaxTurns)
	}

	// Run the agent turn with SSE streaming (no session lock held).
	emit := func(evt ai.ChatEvent) {
		writeSSEEvent(w, evt)
		if flusher != nil {
			flusher.Flush()
		}
	}

	updatedMsgs, tokensUsed, runErr := agent.RunTurn(r.Context(), msgsCopy, emit)

	// Re-acquire lock to update session state.
	session.mu.Lock()
	session.Messages = updatedMsgs
	session.TokensUsed += tokensUsed
	session.mu.Unlock()

	// Record global budget usage
	if mgr, ok := provider.(*ai.Manager); ok {
		if budget := mgr.Budget(); budget != nil {
			budget.RecordUsage(tokensUsed)
		}
	}

	if runErr != nil {
		log.Error(runErr, "Chat turn failed", "session", session.ID)
	}
}

// setSSEHeaders sets the standard Server-Sent Events response headers.
func setSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
}

// writeSSEEvent writes a single SSE event to the response writer.
func writeSSEEvent(w http.ResponseWriter, evt any) {
	data, err := json.Marshal(evt)
	if err != nil {
		log.Error(err, "Failed to marshal SSE chat event")
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
}

// getOrCreateChatSession returns an existing session or creates a new one.
// Returns (session, created, error).
func (s *Server) getOrCreateChatSession(sessionID string) (*ChatSession, bool, error) {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()

	if sessionID != "" {
		if session, ok := s.chatSessions[sessionID]; ok {
			return session, false, nil
		}
	}

	// Check max sessions
	if len(s.chatSessions) >= s.chatMaxSessions {
		return nil, false, fmt.Errorf("maximum concurrent chat sessions reached")
	}

	// Create new session
	id := uuid.New().String()
	session := &ChatSession{
		ID:             id,
		Messages:       make([]ai.ChatMessage, 0),
		TokenBudget:    s.chatTokenBudget,
		CreatedAt:      time.Now(),
		LastAccessedAt: time.Now(),
	}
	s.chatSessions[id] = session
	return session, true, nil
}

// buildChatToolExecutor creates a ToolExecutor that reads from cluster state.
func (s *Server) buildChatToolExecutor(clusterID string) ai.ToolExecutor {
	return func(ctx context.Context, name string, args json.RawMessage) (string, error) {
		switch name {
		case "get_issues":
			return s.chatToolGetIssues(clusterID, args)
		case "get_namespaces":
			return s.chatToolGetNamespaces(clusterID)
		case "get_health_score":
			return s.chatToolGetHealthScore(clusterID)
		case "explain_issue":
			return s.chatToolExplainIssue(clusterID, args)
		case "get_checkers":
			return s.chatToolGetCheckers(clusterID)
		default:
			return "", fmt.Errorf("unknown tool: %s", name)
		}
	}
}

// chatToolGetIssues returns issues filtered by optional namespace/checker/severity.
func (s *Server) chatToolGetIssues(clusterID string, args json.RawMessage) (string, error) {
	var filters struct {
		Namespace string `json:"namespace"`
		Checker   string `json:"checker"`
		Severity  string `json:"severity"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &filters)
	}

	s.mu.RLock()
	cs, ok := s.clusters[clusterID]
	if !ok || cs.latest == nil {
		s.mu.RUnlock()
		return "[]", nil
	}

	var issues []Issue
	for checkerName, result := range cs.latest.Results {
		if filters.Checker != "" && !strings.EqualFold(checkerName, filters.Checker) {
			continue
		}
		for _, issue := range result.Issues {
			if filters.Namespace != "" && !strings.EqualFold(issue.Namespace, filters.Namespace) {
				continue
			}
			if filters.Severity != "" && !strings.EqualFold(issue.Severity, filters.Severity) {
				continue
			}
			issues = append(issues, issue)
		}
	}
	s.mu.RUnlock()

	data, err := json.Marshal(issues)
	if err != nil {
		return "[]", err
	}
	return string(data), nil
}

// chatToolGetNamespaces returns the list of namespaces from the latest health check.
func (s *Server) chatToolGetNamespaces(clusterID string) (string, error) {
	s.mu.RLock()
	cs, ok := s.clusters[clusterID]
	if !ok || cs.latest == nil {
		s.mu.RUnlock()
		return "[]", nil
	}
	namespaces := cs.latest.Namespaces
	s.mu.RUnlock()

	data, err := json.Marshal(namespaces)
	if err != nil {
		return "[]", err
	}
	return string(data), nil
}

// chatToolGetHealthScore returns the health summary from the latest check.
func (s *Server) chatToolGetHealthScore(clusterID string) (string, error) {
	s.mu.RLock()
	cs, ok := s.clusters[clusterID]
	if !ok || cs.latest == nil {
		s.mu.RUnlock()
		return "{}", nil
	}
	summary := cs.latest.Summary
	s.mu.RUnlock()

	data, err := json.Marshal(summary)
	if err != nil {
		return "{}", err
	}
	return string(data), nil
}

// chatToolExplainIssue looks up an AI enhancement for an issue.
func (s *Server) chatToolExplainIssue(clusterID string, args json.RawMessage) (string, error) {
	var params struct {
		Checker  string `json:"checker"`
		IssueKey string `json:"issueKey"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid args for explain_issue: %w", err)
	}
	if params.IssueKey == "" {
		return "", fmt.Errorf("issueKey is required")
	}

	s.mu.RLock()
	cs, ok := s.clusters[clusterID]
	if !ok || cs.lastAIEnhancements == nil {
		s.mu.RUnlock()
		return `{"message":"No AI enhancements available for this issue."}`, nil
	}

	// Search across all checkers if checker not specified
	if params.Checker != "" {
		if checkerMap, ok := cs.lastAIEnhancements[params.Checker]; ok {
			if enh, ok := checkerMap[params.IssueKey]; ok {
				s.mu.RUnlock()
				data, _ := json.Marshal(enh)
				return string(data), nil
			}
		}
		s.mu.RUnlock()
		return `{"message":"No AI enhancement found for this issue."}`, nil
	}

	for _, checkerMap := range cs.lastAIEnhancements {
		if enh, ok := checkerMap[params.IssueKey]; ok {
			s.mu.RUnlock()
			data, _ := json.Marshal(enh)
			return string(data), nil
		}
	}
	s.mu.RUnlock()
	return `{"message":"No AI enhancement found for this issue."}`, nil
}

// chatToolGetCheckers returns the list of checker names from the latest results.
func (s *Server) chatToolGetCheckers(clusterID string) (string, error) {
	s.mu.RLock()
	cs, ok := s.clusters[clusterID]
	if !ok || cs.latest == nil {
		s.mu.RUnlock()
		return "[]", nil
	}

	checkers := make([]string, 0, len(cs.latest.Results))
	for name := range cs.latest.Results {
		checkers = append(checkers, name)
	}
	s.mu.RUnlock()

	data, err := json.Marshal(checkers)
	if err != nil {
		return "[]", err
	}
	return string(data), nil
}

// cleanupChatSessions periodically evicts expired chat sessions.
// Uses a two-pass approach to avoid holding chatMu while locking individual
// sessions, which could block new session creation.
func (s *Server) cleanupChatSessions(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			// Pass 1: identify expired sessions under RLock.
			var expired []string
			now := time.Now()
			s.chatMu.RLock()
			for id, session := range s.chatSessions {
				session.mu.Lock()
				if now.Sub(session.LastAccessedAt) > s.chatSessionTTL {
					expired = append(expired, id)
				}
				session.mu.Unlock()
			}
			s.chatMu.RUnlock()

			// Pass 2: delete expired sessions under write Lock.
			if len(expired) > 0 {
				s.chatMu.Lock()
				for _, id := range expired {
					delete(s.chatSessions, id)
				}
				s.chatMu.Unlock()
			}
		}
	}
}
