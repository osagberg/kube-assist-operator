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

// Package dashboard provides a web-based health monitoring dashboard.
package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	assistv1alpha1 "github.com/osagberg/kube-assist-operator/api/v1alpha1"
	"github.com/osagberg/kube-assist-operator/internal/ai"
	"github.com/osagberg/kube-assist-operator/internal/causal"
	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/datasource"
	"github.com/osagberg/kube-assist-operator/internal/history"
	"github.com/osagberg/kube-assist-operator/internal/scope"
)

var log = logf.Log.WithName("dashboard")

// HealthUpdate represents a health check update sent via SSE
type HealthUpdate struct {
	Timestamp  time.Time              `json:"timestamp"`
	Namespaces []string               `json:"namespaces"`
	Results    map[string]CheckResult `json:"results"`
	Summary    Summary                `json:"summary"`
}

// CheckResult is a simplified result for the dashboard
type CheckResult struct {
	Name    string  `json:"name"`
	Healthy int     `json:"healthy"`
	Issues  []Issue `json:"issues"`
	Error   string  `json:"error,omitempty"`
}

// Issue is a simplified issue for the dashboard
type Issue struct {
	Type       string `json:"type"`
	Severity   string `json:"severity"`
	Resource   string `json:"resource"`
	Namespace  string `json:"namespace"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion"`
}

// Summary provides aggregate health information
type Summary struct {
	TotalHealthy  int `json:"totalHealthy"`
	TotalIssues   int `json:"totalIssues"`
	CriticalCount int `json:"criticalCount"`
	WarningCount  int `json:"warningCount"`
	InfoCount     int `json:"infoCount"`
}

// AISettingsRequest is the JSON body for POST /api/settings/ai
type AISettingsRequest struct {
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider"`
	APIKey   string `json:"apiKey,omitempty"`
	Model    string `json:"model,omitempty"`
}

// AISettingsResponse is the JSON response for GET /api/settings/ai
type AISettingsResponse struct {
	Enabled       bool   `json:"enabled"`
	Provider      string `json:"provider"`
	Model         string `json:"model,omitempty"`
	HasAPIKey     bool   `json:"hasApiKey"`
	ProviderReady bool   `json:"providerReady"`
}

// Server is the dashboard web server
type Server struct {
	client        datasource.DataSource
	registry      *checker.Registry
	addr          string
	aiProvider    ai.Provider
	aiEnabled     bool
	aiConfig      ai.Config
	checkTimeout  time.Duration
	history       *history.RingBuffer
	correlator    *causal.Correlator
	mu            sync.RWMutex
	clients       map[chan HealthUpdate]bool
	latest        *HealthUpdate
	latestCausal  *causal.CausalContext
	running       bool
	checkInFlight atomic.Bool
	stopCh        chan struct{}
}

// NewServer creates a new dashboard server
func NewServer(ds datasource.DataSource, registry *checker.Registry, addr string) *Server {
	return &Server{
		client:       ds,
		registry:     registry,
		addr:         addr,
		aiConfig:     ai.DefaultConfig(),
		checkTimeout: 2 * time.Minute,
		history:      history.New(100),
		correlator:   causal.NewCorrelator(),
		clients:      make(map[chan HealthUpdate]bool),
		stopCh:       make(chan struct{}),
	}
}

// WithAI configures AI provider for enhanced suggestions
func (s *Server) WithAI(provider ai.Provider, enabled bool) *Server {
	s.aiProvider = provider
	s.aiEnabled = enabled
	if provider != nil {
		s.aiConfig.Provider = provider.Name()
	}
	return s
}

// Start starts the dashboard server
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/health/history", s.handleHealthHistory)
	mux.HandleFunc("/api/events", s.handleSSE)
	mux.HandleFunc("/api/check", s.handleTriggerCheck)
	mux.HandleFunc("/api/settings/ai", s.handleAISettings)
	mux.HandleFunc("/api/causal/groups", s.handleCausalGroups)

	// React SPA dashboard
	spaFS, err := fs.Sub(webAssets, "web/dist")
	if err != nil {
		log.Error(err, "Failed to create sub filesystem for SPA assets")
	} else {
		fileServer := http.FileServer(http.FS(spaFS))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// Serve static assets directly, fall back to index.html for SPA routing
			path := r.URL.Path
			if path == "/" || path == "" {
				r.URL.Path = "/index.html"
			}
			fileServer.ServeHTTP(w, r)
		})
	}

	server := &http.Server{
		Addr:         s.addr,
		Handler:      securityHeaders(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	s.running = true

	// Run initial health check synchronously so the first HTTP request
	// never sees {"status": "initializing"}.
	s.runCheck(ctx)

	// Start background health checker (skips initial check since we just ran it)
	go s.runHealthChecker(ctx)

	log.Info("Starting dashboard server", "addr", s.addr)

	// Run server in goroutine
	errCh := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Info("Shutting down dashboard server")
		s.running = false
		close(s.stopCh)
		return server.Shutdown(context.Background())
	case err := <-errCh:
		return err
	}
}

// runHealthChecker periodically runs health checks.
// The first check is already performed synchronously in Start().
func (s *Server) runHealthChecker(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.runCheck(ctx)
		}
	}
}

// runCheck performs a health check and broadcasts results
func (s *Server) runCheck(ctx context.Context) {
	// Resolve namespaces via scope resolver
	resolver := scope.NewResolver(s.client, "default")
	namespaces, err := resolver.ResolveNamespaces(ctx, assistv1alpha1.ScopeConfig{})
	if err != nil {
		log.Error(err, "Failed to resolve namespaces")
		namespaces = []string{"default"}
	}
	namespaces = scope.FilterSystemNamespaces(namespaces)
	if len(namespaces) == 0 {
		namespaces = []string{"default"}
	}

	s.mu.RLock()
	provider := s.aiProvider
	enabled := s.aiEnabled
	s.mu.RUnlock()

	checkCtx2, cancel := context.WithTimeout(ctx, s.checkTimeout)
	defer cancel()

	checkCtx := &checker.CheckContext{
		DataSource: s.client,
		Namespaces: namespaces,
		AIProvider: provider,
		AIEnabled:  enabled,
	}

	results := s.registry.RunAll(checkCtx2, checkCtx, nil)

	// Convert to dashboard format
	update := &HealthUpdate{
		Timestamp:  time.Now(),
		Namespaces: namespaces,
		Results:    make(map[string]CheckResult),
	}

	var summary Summary
	for name, result := range results {
		cr := CheckResult{
			Name:    name,
			Healthy: result.Healthy,
		}

		if result.Error != nil {
			cr.Error = result.Error.Error()
		} else {
			summary.TotalHealthy += result.Healthy
			summary.TotalIssues += len(result.Issues)

			for _, issue := range result.Issues {
				cr.Issues = append(cr.Issues, Issue{
					Type:       issue.Type,
					Severity:   issue.Severity,
					Resource:   issue.Resource,
					Namespace:  issue.Namespace,
					Message:    issue.Message,
					Suggestion: issue.Suggestion,
				})

				switch issue.Severity {
				case checker.SeverityCritical:
					summary.CriticalCount++
				case checker.SeverityWarning:
					summary.WarningCount++
				case checker.SeverityInfo:
					summary.InfoCount++
				}
			}
		}

		update.Results[name] = cr
	}
	update.Summary = summary

	// Run causal correlation
	causalCtx := s.correlator.Analyze(causal.CorrelationInput{
		Results:   results,
		Timestamp: update.Timestamp,
	})

	// Store latest and broadcast
	s.mu.Lock()
	s.latest = update
	s.latestCausal = causalCtx
	for clientCh := range s.clients {
		select {
		case clientCh <- *update:
		default:
			// Client slow, skip
		}
	}
	s.mu.Unlock()

	// Record history snapshot
	snapshot := history.HealthSnapshot{
		Timestamp:    update.Timestamp,
		TotalHealthy: update.Summary.TotalHealthy,
		TotalIssues:  update.Summary.TotalIssues,
		BySeverity: map[string]int{
			"Critical": update.Summary.CriticalCount,
			"Warning":  update.Summary.WarningCount,
			"Info":     update.Summary.InfoCount,
		},
		ByChecker:   make(map[string]int),
		HealthScore: history.ComputeScore(update.Summary.TotalHealthy, update.Summary.TotalIssues),
	}
	for name, result := range update.Results {
		snapshot.ByChecker[name] = len(result.Issues)
	}
	s.history.Add(snapshot)
}

// handleHealth returns current health status
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	s.mu.RLock()
	latest := s.latest
	s.mu.RUnlock()

	if latest == nil {
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "initializing"}); err != nil {
			log.Error(err, "Failed to encode response", "handler", "handleHealth")
		}
		return
	}

	if err := json.NewEncoder(w).Encode(latest); err != nil {
		log.Error(err, "Failed to encode response", "handler", "handleHealth")
	}
}

// handleSSE handles Server-Sent Events connections
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Same-origin only; no CORS header needed for internal dashboard

	// Create client channel
	clientCh := make(chan HealthUpdate, 10)

	// Register client
	s.mu.Lock()
	s.clients[clientCh] = true
	s.mu.Unlock()

	// Cleanup on disconnect
	defer func() {
		s.mu.Lock()
		delete(s.clients, clientCh)
		s.mu.Unlock()
		close(clientCh)
	}()

	// Send initial state
	s.mu.RLock()
	if s.latest != nil {
		data, err := json.Marshal(s.latest)
		if err != nil {
			log.Error(err, "Failed to marshal SSE initial state")
			s.mu.RUnlock()
			return
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	s.mu.RUnlock()

	// Stream updates
	for {
		select {
		case <-r.Context().Done():
			return
		case update := <-clientCh:
			data, err := json.Marshal(update)
			if err != nil {
				log.Error(err, "Failed to marshal SSE update")
				continue
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}
}

// handleTriggerCheck triggers an immediate health check
func (s *Server) handleTriggerCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !s.checkInFlight.CompareAndSwap(false, true) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "check already in progress"}); err != nil {
			log.Error(err, "Failed to encode response", "handler", "handleTriggerCheck")
		}
		return
	}

	go func() {
		defer s.checkInFlight.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), s.checkTimeout)
		defer cancel()
		s.runCheck(ctx)
	}()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "check triggered"}); err != nil {
		log.Error(err, "Failed to encode response", "handler", "handleTriggerCheck")
	}
}

// handleAISettings handles GET and POST for /api/settings/ai
func (s *Server) handleAISettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetAISettings(w, r)
	case http.MethodPost:
		s.handlePostAISettings(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetAISettings returns current AI configuration (API key masked)
func (s *Server) handleGetAISettings(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	resp := AISettingsResponse{
		Enabled:       s.aiEnabled,
		Provider:      s.aiConfig.Provider,
		Model:         s.aiConfig.Model,
		HasAPIKey:     s.aiConfig.APIKey != "",
		ProviderReady: s.aiProvider != nil && s.aiProvider.Available(),
	}
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Error(err, "Failed to encode response", "handler", "handleGetAISettings")
	}
}

// handlePostAISettings updates AI configuration at runtime
func (s *Server) handlePostAISettings(w http.ResponseWriter, r *http.Request) {
	var req AISettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	// Validate provider name
	providerName := strings.ToLower(req.Provider)
	if providerName != "" && providerName != "noop" && providerName != "openai" && providerName != "anthropic" {
		http.Error(w, "Invalid provider: must be one of noop, openai, anthropic", http.StatusBadRequest)
		return
	}

	s.mu.Lock()

	s.aiEnabled = req.Enabled

	// Update config - only update fields that are provided
	if providerName != "" {
		s.aiConfig.Provider = providerName
	}
	if req.APIKey != "" {
		s.aiConfig.APIKey = req.APIKey
	}
	if req.Model != "" {
		s.aiConfig.Model = req.Model
	}

	// If the provider is an ai.Manager, use Reconfigure() so the controller's
	// AI provider is also updated (Manager is shared between dashboard and controllers).
	if mgr, ok := s.aiProvider.(*ai.Manager); ok {
		provider := s.aiConfig.Provider
		apiKey := s.aiConfig.APIKey
		model := s.aiConfig.Model
		s.mu.Unlock()
		mgr.SetEnabled(req.Enabled)
		if providerName != "" {
			if err := mgr.Reconfigure(provider, apiKey, model); err != nil {
				log.Error(err, "Failed to reconfigure AI provider", "provider", provider)
				http.Error(w, "Failed to reconfigure AI provider", http.StatusInternalServerError)
				return
			}
		}
		resp := AISettingsResponse{
			Enabled:       req.Enabled,
			Provider:      provider,
			Model:         model,
			HasAPIKey:     apiKey != "",
			ProviderReady: mgr.Available(),
		}
		log.Info("AI settings updated via dashboard (Manager)", "provider", resp.Provider, "enabled", resp.Enabled)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Error(err, "Failed to encode AI settings response")
		}
		return
	}

	// Fallback: recreate provider directly (no Manager available)
	provider, err := ai.NewProvider(s.aiConfig)
	if err != nil {
		s.mu.Unlock()
		log.Error(err, "Failed to create AI provider", "provider", s.aiConfig.Provider)
		http.Error(w, "Failed to create AI provider", http.StatusInternalServerError)
		return
	}
	s.aiProvider = provider

	resp := AISettingsResponse{
		Enabled:       s.aiEnabled,
		Provider:      s.aiConfig.Provider,
		Model:         s.aiConfig.Model,
		HasAPIKey:     s.aiConfig.APIKey != "",
		ProviderReady: provider.Available(),
	}
	s.mu.Unlock()

	log.Info("AI settings updated via dashboard", "provider", resp.Provider, "enabled", resp.Enabled)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Error(err, "Failed to encode response", "handler", "handlePostAISettings")
	}
}

// handleHealthHistory returns historical health snapshots
func (s *Server) handleHealthHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if lastParam := r.URL.Query().Get("last"); lastParam != "" {
		n, err := strconv.Atoi(lastParam)
		if err != nil || n < 1 {
			http.Error(w, "invalid 'last' parameter", http.StatusBadRequest)
			return
		}
		if n > 1000 {
			n = 1000
		}
		if err := json.NewEncoder(w).Encode(s.history.Last(n)); err != nil {
			log.Error(err, "Failed to encode response", "handler", "handleHealthHistory")
		}
		return
	}

	if sinceParam := r.URL.Query().Get("since"); sinceParam != "" {
		t, err := time.Parse(time.RFC3339, sinceParam)
		if err != nil {
			http.Error(w, "invalid 'since' parameter, use RFC3339", http.StatusBadRequest)
			return
		}
		if err := json.NewEncoder(w).Encode(s.history.Since(t)); err != nil {
			log.Error(err, "Failed to encode response", "handler", "handleHealthHistory")
		}
		return
	}

	// Default: return last 50
	if err := json.NewEncoder(w).Encode(s.history.Last(50)); err != nil {
		log.Error(err, "Failed to encode response", "handler", "handleHealthHistory")
	}
}

// handleCausalGroups returns the latest causal correlation analysis
func (s *Server) handleCausalGroups(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	s.mu.RLock()
	cc := s.latestCausal
	s.mu.RUnlock()

	if cc == nil {
		if err := json.NewEncoder(w).Encode(&causal.CausalContext{}); err != nil {
			log.Error(err, "Failed to encode response", "handler", "handleCausalGroups")
		}
		return
	}

	if err := json.NewEncoder(w).Encode(cc); err != nil {
		log.Error(err, "Failed to encode response", "handler", "handleCausalGroups")
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}
