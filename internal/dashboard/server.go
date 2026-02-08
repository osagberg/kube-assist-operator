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
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	assistv1alpha1 "github.com/osagberg/kube-assist-operator/api/v1alpha1"
	"github.com/osagberg/kube-assist-operator/internal/ai"
	"github.com/osagberg/kube-assist-operator/internal/causal"
	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/datasource"
	"github.com/osagberg/kube-assist-operator/internal/history"
	"github.com/osagberg/kube-assist-operator/internal/prediction"
	"github.com/osagberg/kube-assist-operator/internal/scope"
)

var log = logf.Log.WithName("dashboard")

// AIStatus tracks AI analysis state for the frontend
type AIStatus struct {
	Enabled          bool    `json:"enabled"`
	Provider         string  `json:"provider"`
	LastError        string  `json:"lastError,omitempty"`
	IssuesEnhanced   int     `json:"issuesEnhanced"`
	TokensUsed       int     `json:"tokensUsed"`
	EstimatedCostUSD float64 `json:"estimatedCostUsd,omitempty"`
	CacheHit         bool    `json:"cacheHit,omitempty"`
	IssuesCapped     bool    `json:"issuesCapped,omitempty"`
	TotalIssueCount  int     `json:"totalIssueCount,omitempty"`
	Pending          bool    `json:"pending,omitempty"`
	CheckPhase       string  `json:"checkPhase,omitempty"` // checkers, causal, ai, done
}

// HealthUpdate represents a health check update sent via SSE
type HealthUpdate struct {
	Timestamp  time.Time              `json:"timestamp"`
	Namespaces []string               `json:"namespaces"`
	Results    map[string]CheckResult `json:"results"`
	Summary    Summary                `json:"summary"`
	AIStatus   *AIStatus              `json:"aiStatus,omitempty"`
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
	Type       string            `json:"type"`
	Severity   string            `json:"severity"`
	Resource   string            `json:"resource"`
	Namespace  string            `json:"namespace"`
	Message    string            `json:"message"`
	Suggestion string            `json:"suggestion"`
	AIEnhanced bool              `json:"aiEnhanced,omitempty"`
	RootCause  string            `json:"rootCause,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// Summary provides aggregate health information
type Summary struct {
	TotalHealthy  int `json:"totalHealthy"`
	TotalIssues   int `json:"totalIssues"`
	CriticalCount int `json:"criticalCount"`
	WarningCount  int `json:"warningCount"`
	InfoCount     int `json:"infoCount"`
}

// aiEnhancement stores a cached AI suggestion for reapplication on cache hits.
type aiEnhancement struct {
	Suggestion string
	RootCause  string
}

// ExplainCacheEntry stores a cached explain response with its issue hash.
type ExplainCacheEntry struct {
	Response  *ai.ExplainResponse `json:"response"`
	IssueHash string              `json:"-"`
	CachedAt  time.Time           `json:"-"`
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
	client             datasource.DataSource
	registry           *checker.Registry
	addr               string
	authToken          string
	allowInsecureHTTP  bool
	tlsCertFile        string
	tlsKeyFile         string
	aiProvider         ai.Provider
	aiEnabled          bool
	aiConfig           ai.Config
	checkTimeout       time.Duration
	history            *history.RingBuffer
	correlator         *causal.Correlator
	mu                 sync.RWMutex
	clients            map[chan HealthUpdate]bool
	maxSSEClients      int
	latest             *HealthUpdate
	latestCausal       *causal.CausalContext
	running            bool
	checkInFlight      atomic.Bool
	lastIssueHash      string
	lastAIResult       *AIStatus
	lastAIEnhancements map[string]map[string]aiEnhancement // checker -> "type|resource|namespace" -> enhancement
	lastCausalInsights []ai.CausalGroupInsight
	lastExplain        *ExplainCacheEntry
	checkCounter       uint64
	stopCh             chan struct{}
}

// NewServer creates a new dashboard server
func NewServer(ds datasource.DataSource, registry *checker.Registry, addr string) *Server {
	return &Server{
		client:    ds,
		registry:  registry,
		addr:      addr,
		authToken: os.Getenv("DASHBOARD_AUTH_TOKEN"),
		// Opt-out guard for local/dev use only. By default, auth requires TLS.
		allowInsecureHTTP: parseBoolEnv("DASHBOARD_ALLOW_INSECURE_HTTP"),
		aiConfig:          ai.DefaultConfig(),
		checkTimeout:      2 * time.Minute,
		history:           history.New(100),
		correlator:        causal.NewCorrelator(),
		clients:           make(map[chan HealthUpdate]bool),
		maxSSEClients:     100,
		stopCh:            make(chan struct{}),
	}
}

func parseBoolEnv(name string) bool {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return false
	}
	parsed, err := strconv.ParseBool(v)
	return err == nil && parsed
}

func (s *Server) tlsConfigured() bool {
	return s.tlsCertFile != "" && s.tlsKeyFile != ""
}

func (s *Server) validateSecurityConfig() error {
	if s.authToken == "" || s.tlsConfigured() || s.allowInsecureHTTP {
		return nil
	}
	return fmt.Errorf("dashboard auth is configured but TLS is not enabled; configure dashboard TLS cert/key or set DASHBOARD_ALLOW_INSECURE_HTTP=true for local development only")
}

// WithTLS configures TLS for the dashboard server
func (s *Server) WithTLS(certFile, keyFile string) *Server {
	s.tlsCertFile = certFile
	s.tlsKeyFile = keyFile
	return s
}

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.authToken == "" {
			// No token configured — allow but warn (logged on startup)
			next(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		gotHash := sha256.Sum256([]byte(token))
		wantHash := sha256.Sum256([]byte(s.authToken))
		if subtle.ConstantTimeCompare(gotHash[:], wantHash[:]) != 1 {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
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
	mux.HandleFunc("/api/check", s.authMiddleware(s.handleTriggerCheck))
	mux.HandleFunc("/api/settings/ai", s.authMiddleware(s.handleAISettings))
	mux.HandleFunc("/api/causal/groups", s.handleCausalGroups)
	mux.HandleFunc("/api/explain", s.authMiddleware(s.handleExplain))
	mux.HandleFunc("/api/prediction/trend", s.handlePrediction)

	// React SPA dashboard
	spaFS, err := fs.Sub(webAssets, "web/dist")
	if err != nil {
		log.Error(err, "Failed to create sub filesystem for SPA assets")
	} else {
		fileServer := http.FileServer(http.FS(spaFS))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			// Serve index.html directly for root and SPA routes to avoid
			// FileServer's redirect loop (it redirects /index.html → ./ → /)
			if path == "/" || path == "" {
				http.ServeFileFS(w, r, spaFS, "index.html")
				return
			}
			// Try static asset first; fall back to index.html for SPA routing
			f, err := spaFS.Open(path[1:]) // strip leading /
			if err != nil {
				http.ServeFileFS(w, r, spaFS, "index.html")
				return
			}
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
		})
	}

	server := &http.Server{
		Addr:         s.addr,
		Handler:      securityHeaders(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	if s.authToken == "" {
		log.Info("WARNING: Dashboard authentication not configured. Set DASHBOARD_AUTH_TOKEN to secure mutating endpoints.")
	} else {
		log.Info("Dashboard authentication enabled for mutating endpoints")
	}
	if err := s.validateSecurityConfig(); err != nil {
		return err
	}
	if s.authToken != "" && !s.tlsConfigured() && s.allowInsecureHTTP {
		log.Info("WARNING: Dashboard auth enabled without TLS due to explicit override. Tokens will be sent in plaintext.",
			"env", "DASHBOARD_ALLOW_INSECURE_HTTP")
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
		if s.tlsConfigured() {
			log.Info("Dashboard TLS enabled", "cert", s.tlsCertFile)
			if err := server.ListenAndServeTLS(s.tlsCertFile, s.tlsKeyFile); err != nil && err != http.ErrServerClosed {
				errCh <- err
			}
		} else {
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				errCh <- err
			}
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
			if s.checkInFlight.CompareAndSwap(false, true) {
				s.runCheck(ctx)
				s.checkInFlight.Store(false)
			}
		}
	}
}

// broadcastPhase sends a lightweight SSE update with just the current check phase,
// so the frontend can show a pipeline progress indicator during long checks.
func (s *Server) broadcastPhase(phase string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latest == nil {
		return
	}
	if s.latest.AIStatus == nil {
		s.latest.AIStatus = &AIStatus{}
	}
	s.latest.AIStatus.CheckPhase = phase
	for clientCh := range s.clients {
		select {
		case clientCh <- *s.latest:
		default:
		}
	}
}

// runAIAnalysis handles AI caching, throttling, and enhancement of check results.
// Extracted from runCheck to reduce cyclomatic complexity.
func (s *Server) runAIAnalysis(
	ctx context.Context,
	results map[string]*checker.CheckResult,
	causalCtx *causal.CausalContext,
	checkCtx *checker.CheckContext,
	enabled bool,
	provider ai.Provider,
) *AIStatus {
	s.checkCounter++
	issueHash := computeIssueHash(results)

	if !enabled || provider == nil || !provider.Available() {
		return nil
	}

	hashChanged := issueHash != s.lastIssueHash
	if !hashChanged && s.lastAIResult != nil {
		reapplyAIEnhancements(results, s.lastAIEnhancements)
		if len(s.lastCausalInsights) > 0 {
			applyCausalInsights(causalCtx, s.lastCausalInsights)
		}
		log.Info("AI analysis skipped (issues unchanged)", "hash", issueHash[:12])
		return &AIStatus{
			Enabled:         enabled,
			Provider:        provider.Name(),
			IssuesEnhanced:  s.lastAIResult.IssuesEnhanced,
			CacheHit:        true,
			TotalIssueCount: s.lastAIResult.TotalIssueCount,
			CheckPhase:      "done",
		}
	}

	aiCtx, aiCancel := context.WithTimeout(ctx, 90*time.Second)
	enhanced, tokens, totalCount, aiResp, aiErr := checker.EnhanceAllWithAI(aiCtx, results, checkCtx)
	aiCancel()

	if aiErr != nil {
		return s.handleAIError(aiErr, results, causalCtx, enabled, provider, totalCount)
	}

	if enhanced > 0 {
		status := &AIStatus{
			Enabled:          enabled,
			Provider:         provider.Name(),
			IssuesEnhanced:   enhanced,
			TokensUsed:       tokens,
			EstimatedCostUSD: estimateCost(provider.Name(), tokens),
			IssuesCapped:     totalCount > enhanced && totalCount > 0,
			TotalIssueCount:  totalCount,
			CheckPhase:       "done",
		}
		s.lastIssueHash = issueHash
		s.lastAIResult = status
		s.lastAIEnhancements = snapshotAIEnhancements(results)
		if aiResp != nil && len(aiResp.CausalInsights) > 0 {
			applyCausalInsights(causalCtx, aiResp.CausalInsights)
			s.lastCausalInsights = aiResp.CausalInsights
		}
		return status
	}

	// AI returned 0 enhanced (likely truncated JSON)
	return s.handleAITruncated(results, causalCtx, enabled, provider, totalCount, tokens)
}

// handleAIError handles AI call failure, reusing cached results when available.
func (s *Server) handleAIError(
	aiErr error,
	results map[string]*checker.CheckResult,
	causalCtx *causal.CausalContext,
	enabled bool,
	provider ai.Provider,
	totalCount int,
) *AIStatus {
	if s.lastAIResult != nil && s.lastAIResult.LastError == "" {
		reapplyAIEnhancements(results, s.lastAIEnhancements)
		if len(s.lastCausalInsights) > 0 {
			applyCausalInsights(causalCtx, s.lastCausalInsights)
		}
		log.Info("AI call failed, reusing last good result", "error", aiErr.Error())
		return &AIStatus{
			Enabled:         enabled,
			Provider:        provider.Name(),
			IssuesEnhanced:  s.lastAIResult.IssuesEnhanced,
			CacheHit:        true,
			TotalIssueCount: s.lastAIResult.TotalIssueCount,
			LastError:       "retrying: " + aiErr.Error(),
			CheckPhase:      "done",
		}
	}
	return &AIStatus{
		Enabled:         enabled,
		Provider:        provider.Name(),
		TotalIssueCount: totalCount,
		LastError:       aiErr.Error(),
		CheckPhase:      "done",
	}
}

// handleAITruncated handles AI responses with 0 enhanced issues, reusing cached results when available.
func (s *Server) handleAITruncated(
	results map[string]*checker.CheckResult,
	causalCtx *causal.CausalContext,
	enabled bool,
	provider ai.Provider,
	totalCount int,
	tokens int,
) *AIStatus {
	if s.lastAIResult != nil && s.lastAIResult.IssuesEnhanced > 0 {
		reapplyAIEnhancements(results, s.lastAIEnhancements)
		if len(s.lastCausalInsights) > 0 {
			applyCausalInsights(causalCtx, s.lastCausalInsights)
		}
		log.Info("AI response truncated (0 enhanced), reusing last good result", "tokens", tokens)
		return &AIStatus{
			Enabled:         enabled,
			Provider:        provider.Name(),
			IssuesEnhanced:  s.lastAIResult.IssuesEnhanced,
			CacheHit:        true,
			TotalIssueCount: s.lastAIResult.TotalIssueCount,
			CheckPhase:      "done",
		}
	}
	log.Info("AI response truncated, no cached result to fall back on", "tokens", tokens)
	return &AIStatus{
		Enabled:         enabled,
		Provider:        provider.Name(),
		TotalIssueCount: totalCount,
		LastError:       "AI response truncated — retrying next cycle",
		CheckPhase:      "done",
	}
}

// runCheck performs a health check and broadcasts results
func (s *Server) runCheck(ctx context.Context) {
	// 1. Resolve namespaces via scope resolver — scan all non-system namespaces
	resolver := scope.NewResolver(s.client, "default")
	namespaces, err := resolver.ResolveNamespaces(ctx, assistv1alpha1.ScopeConfig{
		NamespaceSelector: &metav1.LabelSelector{},
	})
	if err != nil {
		log.Error(err, "Failed to resolve namespaces")
		namespaces = []string{"default"}
	}
	namespaces = scope.FilterSystemNamespaces(namespaces)
	if len(namespaces) == 0 {
		namespaces = []string{"default"}
	}

	checkCtx2, cancel := context.WithTimeout(ctx, s.checkTimeout)
	defer cancel()

	s.broadcastPhase("checkers")

	// 2. Build CheckContext with AIEnabled: false — pure health checks first
	checkCtx := &checker.CheckContext{
		DataSource: s.client,
		Namespaces: namespaces,
		AIEnabled:  false,
	}

	// 3. RunAll — pure health checks, no AI
	results := s.registry.RunAll(checkCtx2, checkCtx, nil)

	s.broadcastPhase("causal")

	// 4. Run causal correlator BEFORE AI so causal context is available
	now := time.Now()
	causalCtx := s.correlator.Analyze(causal.CorrelationInput{
		Results:   results,
		Timestamp: now,
	})

	// 5. Populate ClusterContext with namespaces
	checkCtx.ClusterContext = ai.ClusterContext{
		Namespaces: namespaces,
	}

	// 6. Convert causal.CausalContext to ai.CausalAnalysisContext
	checkCtx.CausalContext = toCausalAnalysisContext(causalCtx)

	// 7. Enable AI and set provider for the batched call
	s.mu.RLock()
	provider := s.aiProvider
	enabled := s.aiEnabled
	s.mu.RUnlock()

	checkCtx.AIEnabled = enabled
	checkCtx.AIProvider = provider

	s.broadcastPhase("ai")

	aiStatus := s.runAIAnalysis(checkCtx2, results, causalCtx, checkCtx, enabled, provider)

	// 9. Convert to dashboard format
	update := &HealthUpdate{
		Timestamp:  now,
		Namespaces: namespaces,
		Results:    make(map[string]CheckResult),
		AIStatus:   aiStatus,
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
				suggestion := issue.Suggestion
				aiEnhanced := strings.Contains(suggestion, "AI Analysis:")
				// When AI enhanced, show only the AI part
				if aiEnhanced {
					if idx := strings.Index(suggestion, "AI Analysis: "); idx >= 0 {
						suggestion = suggestion[idx+len("AI Analysis: "):]
					}
				}
				di := Issue{
					Type:       issue.Type,
					Severity:   issue.Severity,
					Resource:   issue.Resource,
					Namespace:  issue.Namespace,
					Message:    issue.Message,
					Suggestion: suggestion,
					AIEnhanced: aiEnhanced,
					Metadata:   issue.Metadata,
				}
				if issue.Metadata != nil {
					if rc, ok := issue.Metadata["aiRootCause"]; ok {
						di.RootCause = rc
					}
				}
				cr.Issues = append(cr.Issues, di)

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

	// 10. Store latest and broadcast
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

// toCausalAnalysisContext converts causal.CausalContext to ai.CausalAnalysisContext
func toCausalAnalysisContext(cc *causal.CausalContext) *ai.CausalAnalysisContext {
	if cc == nil || len(cc.Groups) == 0 {
		return nil
	}
	groups := make([]ai.CausalGroupSummary, len(cc.Groups))
	for i, g := range cc.Groups {
		resources := make([]string, len(g.Events))
		for j, e := range g.Events {
			resources[j] = e.Issue.Namespace + "/" + e.Issue.Resource
		}
		groups[i] = ai.CausalGroupSummary{
			Rule:       g.Rule,
			Title:      g.Title,
			RootCause:  g.RootCause,
			Severity:   g.Severity,
			Confidence: g.Confidence,
			Resources:  resources,
		}
	}
	return &ai.CausalAnalysisContext{
		Groups:            groups,
		UncorrelatedCount: cc.UncorrelatedCount,
		TotalIssues:       cc.TotalIssues,
	}
}

// computeIssueHash computes a SHA-256 hash of all issue signatures to detect changes.
// Keys are sorted to ensure deterministic output across Go map iterations.
func computeIssueHash(results map[string]*checker.CheckResult) string {
	h := sha256.New()
	keys := make([]string, 0, len(results))
	for name := range results {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		result := results[name]
		if result.Error != nil {
			continue
		}
		// Sort issues by stable key to ensure deterministic hash
		issueKeys := make([]string, len(result.Issues))
		for i, issue := range result.Issues {
			issueKeys[i] = fmt.Sprintf("%s|%s|%s|%s|%s", name, issue.Type, issue.Severity, issue.Namespace, issue.Resource)
		}
		sort.Strings(issueKeys)
		for _, k := range issueKeys {
			_, _ = fmt.Fprintln(h, k)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// snapshotAIEnhancements captures AI-enhanced suggestions from results for later reapplication.
func snapshotAIEnhancements(results map[string]*checker.CheckResult) map[string]map[string]aiEnhancement {
	snapshot := make(map[string]map[string]aiEnhancement)
	for name, result := range results {
		if result.Error != nil {
			continue
		}
		for _, issue := range result.Issues {
			if !strings.Contains(issue.Suggestion, "AI Analysis:") {
				continue
			}
			if snapshot[name] == nil {
				snapshot[name] = make(map[string]aiEnhancement)
			}
			key := issue.Type + "|" + issue.Resource + "|" + issue.Namespace
			snapshot[name][key] = aiEnhancement{
				Suggestion: issue.Suggestion,
				RootCause:  issue.Metadata["aiRootCause"],
			}
		}
	}
	return snapshot
}

// reapplyAIEnhancements restores cached AI suggestions onto fresh health check results.
func reapplyAIEnhancements(results map[string]*checker.CheckResult, enhancements map[string]map[string]aiEnhancement) {
	if enhancements == nil {
		return
	}
	for name, issueEnhancements := range enhancements {
		result, ok := results[name]
		if !ok || result.Error != nil {
			continue
		}
		for i := range result.Issues {
			key := result.Issues[i].Type + "|" + result.Issues[i].Resource + "|" + result.Issues[i].Namespace
			enh, ok := issueEnhancements[key]
			if !ok {
				continue
			}
			result.Issues[i].Suggestion = enh.Suggestion
			if enh.RootCause != "" {
				if result.Issues[i].Metadata == nil {
					result.Issues[i].Metadata = make(map[string]string)
				}
				result.Issues[i].Metadata["aiRootCause"] = enh.RootCause
			}
		}
	}
}

// applyCausalInsights enriches causal groups with AI-generated analysis.
// It matches insights to groups by GroupID index (e.g., "group_0", "group_1").
func applyCausalInsights(cc *causal.CausalContext, insights []ai.CausalGroupInsight) {
	if cc == nil || len(cc.Groups) == 0 || len(insights) == 0 {
		return
	}
	for _, insight := range insights {
		idx, err := strconv.Atoi(strings.TrimPrefix(insight.GroupID, "group_"))
		if err != nil || idx < 0 || idx >= len(cc.Groups) {
			continue
		}
		cc.Groups[idx].AIRootCause = insight.AIRootCause
		cc.Groups[idx].AISuggestion = insight.AISuggestion
		cc.Groups[idx].AISteps = insight.AISteps
		cc.Groups[idx].AIEnhanced = true
	}
}

// estimateCost estimates the USD cost of an AI call based on provider and tokens.
func estimateCost(provider string, tokens int) float64 {
	if tokens <= 0 {
		return 0
	}
	// Approximate cost per 1K tokens (input+output blended)
	var costPer1K float64
	switch provider {
	case "anthropic":
		costPer1K = 0.00125 // Haiku 3.5 blended
	case "openai":
		costPer1K = 0.00075 // GPT-4o-mini blended
	default:
		return 0
	}
	return float64(tokens) / 1000.0 * costPer1K
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

	// Register client (enforce limit)
	s.mu.Lock()
	if len(s.clients) >= s.maxSSEClients {
		s.mu.Unlock()
		http.Error(w, "Too many SSE clients", http.StatusServiceUnavailable)
		return
	}
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

	// Invalidate AI cache so the next check uses the new provider immediately
	s.lastIssueHash = ""
	s.lastAIResult = nil
	s.lastAIEnhancements = nil
	s.lastCausalInsights = nil

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

		// Immediately update live AIStatus so the frontend knows the provider changed
		s.mu.Lock()
		if s.latest != nil {
			s.latest.AIStatus = &AIStatus{
				Enabled:  req.Enabled,
				Provider: provider,
				Pending:  true,
			}
		}
		s.mu.Unlock()

		// Trigger immediate check so user doesn't wait for next 30s cycle
		go func() {
			if s.checkInFlight.CompareAndSwap(false, true) {
				defer s.checkInFlight.Store(false)
				checkCtx, checkCancel := context.WithTimeout(context.Background(), s.checkTimeout)
				defer checkCancel()
				s.runCheck(checkCtx)
			}
		}()

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Error(err, "Failed to encode AI settings response")
		}
		return
	}

	// Fallback: recreate provider directly (no Manager available)
	provider, _, _, err := ai.NewProvider(s.aiConfig)
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

	// Immediately update live AIStatus so the frontend knows the provider changed
	if s.latest != nil {
		s.latest.AIStatus = &AIStatus{
			Enabled:  s.aiEnabled,
			Provider: s.aiConfig.Provider,
			Pending:  true,
		}
	}
	s.mu.Unlock()

	log.Info("AI settings updated via dashboard", "provider", resp.Provider, "enabled", resp.Enabled)

	// Trigger immediate check so user doesn't wait for next 30s cycle
	go func() {
		if s.checkInFlight.CompareAndSwap(false, true) {
			defer s.checkInFlight.Store(false)
			checkCtx, checkCancel := context.WithTimeout(context.Background(), s.checkTimeout)
			defer checkCancel()
			s.runCheck(checkCtx)
		}
	}()

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

// handleExplain returns an AI-generated narrative explanation of cluster health.
// GET /api/explain
func (s *Server) handleExplain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Check AI availability
	s.mu.RLock()
	provider := s.aiProvider
	enabled := s.aiEnabled
	s.mu.RUnlock()

	if !enabled || provider == nil || !provider.Available() {
		if err := json.NewEncoder(w).Encode(ai.ExplainResponse{
			Narrative:      "AI is not configured. Enable an AI provider in settings to get cluster explanations.",
			RiskLevel:      "unknown",
			TrendDirection: "unknown",
		}); err != nil {
			log.Error(err, "Failed to encode response", "handler", "handleExplain")
		}
		return
	}

	// Check cache: valid if issue hash matches and within 5 minutes
	s.mu.RLock()
	currentHash := s.lastIssueHash
	cached := s.lastExplain
	latest := s.latest
	causalCtx := s.latestCausal
	s.mu.RUnlock()

	if cached != nil && cached.IssueHash == currentHash && time.Since(cached.CachedAt) < 5*time.Minute {
		if err := json.NewEncoder(w).Encode(cached.Response); err != nil {
			log.Error(err, "Failed to encode response", "handler", "handleExplain")
		}
		return
	}

	if latest == nil {
		if err := json.NewEncoder(w).Encode(ai.ExplainResponse{
			Narrative:      "No health data available yet.",
			RiskLevel:      "unknown",
			TrendDirection: "unknown",
		}); err != nil {
			log.Error(err, "Failed to encode response", "handler", "handleExplain")
		}
		return
	}

	// Convert health results to generic map for the prompt
	healthMap := make(map[string]any)
	for name, result := range latest.Results {
		healthMap[name] = map[string]any{
			"healthy": result.Healthy,
			"issues":  len(result.Issues),
			"error":   result.Error,
		}
	}
	healthMap["summary"] = latest.Summary

	// Get history for trend context
	historySnapshots := s.history.Last(20)
	historyForPrompt := make([]any, 0, len(historySnapshots))
	for _, snap := range historySnapshots {
		historyForPrompt = append(historyForPrompt, map[string]any{
			"timestamp":   snap.Timestamp.Format(time.RFC3339),
			"healthScore": snap.HealthScore,
			"totalIssues": snap.TotalIssues,
		})
	}

	// Convert causal context
	var causalAnalysis *ai.CausalAnalysisContext
	if causalCtx != nil {
		causalAnalysis = toCausalAnalysisContext(causalCtx)
	}

	// Enrich with prediction data if available
	predResult := prediction.Analyze(historySnapshots)
	if predResult != nil {
		healthMap["prediction"] = map[string]any{
			"trendDirection": predResult.TrendDirection,
			"velocity":       predResult.Velocity,
			"projectedScore": predResult.ProjectedScore,
			"riskyCheckers":  predResult.RiskyCheckers,
		}
	}

	sanitizer := ai.NewSanitizer()
	// Sanitize the prompt output
	prompt := sanitizer.SanitizeString(ai.BuildExplainPrompt(healthMap, causalAnalysis, historyForPrompt))

	// Call AI with explain mode
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	analysisResp, err := provider.Analyze(ctx, ai.AnalysisRequest{
		ExplainMode:    true,
		ExplainContext: prompt,
		ClusterContext: ai.ClusterContext{Namespaces: latest.Namespaces},
		MaxTokens:      4096,
	})
	if err != nil {
		log.Error(err, "Explain AI call failed")
		if err := json.NewEncoder(w).Encode(ai.ExplainResponse{
			Narrative:      "Failed to generate explanation: " + err.Error(),
			RiskLevel:      "unknown",
			TrendDirection: "unknown",
		}); err != nil {
			log.Error(err, "Failed to encode response", "handler", "handleExplain")
		}
		return
	}

	// Parse the response
	explainResp := ai.ParseExplainResponse(analysisResp.Summary, analysisResp.TokensUsed)

	// Cache the result
	s.mu.Lock()
	s.lastExplain = &ExplainCacheEntry{
		Response:  explainResp,
		IssueHash: currentHash,
		CachedAt:  time.Now(),
	}
	s.mu.Unlock()

	if err := json.NewEncoder(w).Encode(explainResp); err != nil {
		log.Error(err, "Failed to encode response", "handler", "handleExplain")
	}
}

// handlePrediction returns health trend analysis using linear regression on history.
// GET /api/prediction/trend
func (s *Server) handlePrediction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Get history snapshots
	snapshots := s.history.Last(50)

	result := prediction.Analyze(snapshots)
	if result == nil {
		// Not enough data
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(map[string]string{
			"status":  "insufficient_data",
			"message": "Need at least 5 health snapshots for prediction",
		}); err != nil {
			log.Error(err, "Failed to encode prediction response")
		}
		return
	}

	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Error(err, "Failed to encode prediction response")
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
