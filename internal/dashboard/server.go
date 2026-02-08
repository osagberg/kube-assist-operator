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
	"bytes"
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
	ClusterID  string                 `json:"clusterId,omitempty"`
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

// clusterState holds per-cluster health state.
type clusterState struct {
	latest             *HealthUpdate
	latestCausal       *causal.CausalContext
	history            *history.RingBuffer
	lastIssueHash      string
	lastAIResult       *AIStatus
	lastAIEnhancements map[string]map[string]aiEnhancement
	lastCausalInsights []ai.CausalGroupInsight
	lastExplain        *ExplainCacheEntry
	checkCounter       uint64
}

// FleetSummary provides an aggregate view of all clusters.
type FleetSummary struct {
	Clusters []FleetClusterEntry `json:"clusters"`
}

// FleetClusterEntry contains health summary for a single cluster in the fleet.
type FleetClusterEntry struct {
	ClusterID     string  `json:"clusterId"`
	HealthScore   float64 `json:"healthScore"`
	TotalIssues   int     `json:"totalIssues"`
	CriticalCount int     `json:"criticalCount"`
	WarningCount  int     `json:"warningCount"`
	InfoCount     int     `json:"infoCount"`
	LastUpdated   string  `json:"lastUpdated"`
}

// AISettingsRequest is the JSON body for POST /api/settings/ai
type AISettingsRequest struct {
	Enabled      bool   `json:"enabled"`
	Provider     string `json:"provider"`
	APIKey       string `json:"apiKey,omitempty"`
	Model        string `json:"model,omitempty"`
	ExplainModel string `json:"explainModel,omitempty"`
}

// AISettingsResponse is the JSON response for GET /api/settings/ai
type AISettingsResponse struct {
	Enabled       bool   `json:"enabled"`
	Provider      string `json:"provider"`
	Model         string `json:"model,omitempty"`
	ExplainModel  string `json:"explainModel,omitempty"`
	HasAPIKey     bool   `json:"hasApiKey"`
	ProviderReady bool   `json:"providerReady"`
}

// Server is the dashboard web server
type Server struct {
	client            datasource.DataSource
	registry          *checker.Registry
	addr              string
	authToken         string
	allowInsecureHTTP bool
	tlsCertFile       string
	tlsKeyFile        string
	aiProvider        ai.Provider
	aiEnabled         bool
	aiConfig          ai.Config
	checkTimeout      time.Duration
	correlator        *causal.Correlator
	mu                sync.RWMutex
	clients           map[chan HealthUpdate]string // chan -> subscribed clusterId ("" = all)
	clusters          map[string]*clusterState     // clusterID -> state ("" key for single-cluster)
	maxSSEClients     int
	running           bool
	checkInFlight     atomic.Bool
	stopCh            chan struct{}
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
		correlator:        causal.NewCorrelator(),
		clients:           make(map[chan HealthUpdate]string),
		clusters:          make(map[string]*clusterState),
		maxSSEClients:     100,
		stopCh:            make(chan struct{}),
	}
}

// getOrCreateClusterState returns the state for a cluster, creating it if needed.
// Must be called with s.mu held (write lock).
func (s *Server) getOrCreateClusterState(clusterID string) *clusterState {
	cs, ok := s.clusters[clusterID]
	if !ok {
		cs = &clusterState{
			history: history.New(100),
		}
		s.clusters[clusterID] = cs
	}
	return cs
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

func (s *Server) sseAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.authToken == "" {
			next(w, r)
			return
		}
		// Check Authorization header first
		auth := r.Header.Get("Authorization")
		if auth != "" && strings.HasPrefix(auth, "Bearer ") {
			token := strings.TrimPrefix(auth, "Bearer ")
			gotHash := sha256.Sum256([]byte(token))
			wantHash := sha256.Sum256([]byte(s.authToken))
			if subtle.ConstantTimeCompare(gotHash[:], wantHash[:]) == 1 {
				next(w, r)
				return
			}
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		// Fallback: check ?token= query parameter (SSE/EventSource can't set headers)
		qToken := r.URL.Query().Get("token")
		if qToken == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		gotHash := sha256.Sum256([]byte(qToken))
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

// WithMaxSSEClients sets the maximum number of concurrent SSE connections.
// WithMaxSSEClients sets the maximum number of concurrent SSE connections.
// Pass 0 for unlimited.
func (s *Server) WithMaxSSEClients(max int) *Server {
	if max >= 0 {
		s.maxSSEClients = max
	}
	return s
}

// Start starts the dashboard server
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/health", s.authMiddleware(s.handleHealth))
	mux.HandleFunc("/api/health/history", s.authMiddleware(s.handleHealthHistory))
	mux.HandleFunc("/api/events", s.sseAuthMiddleware(s.handleSSE))
	mux.HandleFunc("/api/check", s.authMiddleware(s.handleTriggerCheck))
	mux.HandleFunc("/api/settings/ai", s.authMiddleware(s.handleAISettings))
	mux.HandleFunc("/api/causal/groups", s.authMiddleware(s.handleCausalGroups))
	mux.HandleFunc("/api/explain", s.authMiddleware(s.handleExplain))
	mux.HandleFunc("/api/prediction/trend", s.authMiddleware(s.handlePrediction))
	mux.HandleFunc("/api/clusters", s.authMiddleware(s.handleClusters))
	mux.HandleFunc("/api/fleet/summary", s.authMiddleware(s.handleFleetSummary))
	mux.HandleFunc("/api/settings/ai/catalog", s.handleAICatalog)

	// React SPA dashboard
	spaFS, err := fs.Sub(webAssets, "web/dist")
	if err != nil {
		log.Error(err, "Failed to create sub filesystem for SPA assets")
	} else {
		fileServer := http.FileServer(http.FS(spaFS))

		// When auth is configured, inject the token into index.html so the SPA
		// can send it on API requests. The token is injected at serve-time (not
		// build-time) via a <script> tag before </head>.
		var injectedIndex []byte
		if s.authToken != "" {
			raw, readErr := fs.ReadFile(spaFS, "index.html")
			if readErr != nil {
				log.Error(readErr, "Failed to read index.html for token injection")
			} else {
				snippet := fmt.Sprintf(`<script>window.__DASHBOARD_AUTH_TOKEN__=%q;</script></head>`, s.authToken)
				injectedIndex = bytes.Replace(raw, []byte("</head>"), []byte(snippet), 1)
			}
		}

		serveIndex := func(w http.ResponseWriter, r *http.Request) {
			if injectedIndex != nil {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("Cache-Control", "no-cache")
				_, _ = w.Write(injectedIndex)
				return
			}
			http.ServeFileFS(w, r, spaFS, "index.html")
		}

		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			if path == "/" || path == "" {
				serveIndex(w, r)
				return
			}
			f, err := spaFS.Open(path[1:])
			if err != nil {
				serveIndex(w, r)
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
		log.Info("Dashboard authentication enabled for all data endpoints")
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

// broadcastPhase sends a lightweight SSE update with just the current check phase
// for a specific cluster, so the frontend can show a pipeline progress indicator.
func (s *Server) broadcastPhase(clusterID, phase string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cs, ok := s.clusters[clusterID]
	if !ok || cs.latest == nil {
		return
	}
	if cs.latest.AIStatus == nil {
		cs.latest.AIStatus = &AIStatus{}
	}
	cs.latest.AIStatus.CheckPhase = phase
	for clientCh, subscribedID := range s.clients {
		if subscribedID != "" && subscribedID != clusterID {
			continue
		}
		select {
		case clientCh <- *cs.latest:
		default:
		}
	}
}

// runAIAnalysisForCluster handles AI caching, throttling, and enhancement of check results
// for a specific cluster's state. All reads/writes to cs are guarded by s.mu.
func (s *Server) runAIAnalysisForCluster(
	ctx context.Context,
	results map[string]*checker.CheckResult,
	causalCtx *causal.CausalContext,
	checkCtx *checker.CheckContext,
	enabled bool,
	provider ai.Provider,
	cs *clusterState,
) *AIStatus {
	issueHash := computeIssueHash(results)

	if !enabled || provider == nil || !provider.Available() {
		s.mu.Lock()
		cs.checkCounter++
		s.mu.Unlock()
		return nil
	}

	// Read cached state under lock
	s.mu.Lock()
	cs.checkCounter++
	hashChanged := issueHash != cs.lastIssueHash
	cachedResult := cs.lastAIResult
	cachedEnhancements := cs.lastAIEnhancements
	cachedInsights := cs.lastCausalInsights
	s.mu.Unlock()

	if !hashChanged && cachedResult != nil {
		reapplyAIEnhancements(results, cachedEnhancements)
		if len(cachedInsights) > 0 {
			applyCausalInsights(causalCtx, cachedInsights)
		}
		log.Info("AI analysis skipped (issues unchanged)", "hash", issueHash[:12])
		return &AIStatus{
			Enabled:         enabled,
			Provider:        provider.Name(),
			IssuesEnhanced:  cachedResult.IssuesEnhanced,
			CacheHit:        true,
			TotalIssueCount: cachedResult.TotalIssueCount,
			CheckPhase:      "done",
		}
	}

	// Long-running AI call — outside lock
	aiCtx, aiCancel := context.WithTimeout(ctx, 90*time.Second)
	enhanced, tokens, totalCount, aiResp, aiErr := checker.EnhanceAllWithAI(aiCtx, results, checkCtx)
	aiCancel()

	if aiErr != nil {
		return s.handleAIErrorForCluster(aiErr, results, causalCtx, enabled, provider, totalCount, cs)
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
		// Commit AI results atomically under lock
		s.mu.Lock()
		cs.lastIssueHash = issueHash
		cs.lastAIResult = status
		cs.lastAIEnhancements = snapshotAIEnhancements(results)
		if aiResp != nil && len(aiResp.CausalInsights) > 0 {
			applyCausalInsights(causalCtx, aiResp.CausalInsights)
			cs.lastCausalInsights = aiResp.CausalInsights
		}
		s.mu.Unlock()
		return status
	}

	// AI returned 0 enhanced (likely truncated JSON)
	return s.handleAITruncatedForCluster(results, causalCtx, enabled, provider, totalCount, tokens, cs)
}

// handleAIErrorForCluster handles AI call failure, reusing cached results when available.
func (s *Server) handleAIErrorForCluster(
	aiErr error,
	results map[string]*checker.CheckResult,
	causalCtx *causal.CausalContext,
	enabled bool,
	provider ai.Provider,
	totalCount int,
	cs *clusterState,
) *AIStatus {
	s.mu.RLock()
	cachedResult := cs.lastAIResult
	cachedEnhancements := cs.lastAIEnhancements
	cachedInsights := cs.lastCausalInsights
	s.mu.RUnlock()

	if cachedResult != nil && cachedResult.LastError == "" {
		reapplyAIEnhancements(results, cachedEnhancements)
		if len(cachedInsights) > 0 {
			applyCausalInsights(causalCtx, cachedInsights)
		}
		log.Info("AI call failed, reusing last good result", "error", aiErr.Error())
		return &AIStatus{
			Enabled:         enabled,
			Provider:        provider.Name(),
			IssuesEnhanced:  cachedResult.IssuesEnhanced,
			CacheHit:        true,
			TotalIssueCount: cachedResult.TotalIssueCount,
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

// handleAITruncatedForCluster handles AI responses with 0 enhanced issues, reusing cached results when available.
func (s *Server) handleAITruncatedForCluster(
	results map[string]*checker.CheckResult,
	causalCtx *causal.CausalContext,
	enabled bool,
	provider ai.Provider,
	totalCount int,
	tokens int,
	cs *clusterState,
) *AIStatus {
	s.mu.RLock()
	cachedResult := cs.lastAIResult
	cachedEnhancements := cs.lastAIEnhancements
	cachedInsights := cs.lastCausalInsights
	s.mu.RUnlock()

	if cachedResult != nil && cachedResult.IssuesEnhanced > 0 {
		reapplyAIEnhancements(results, cachedEnhancements)
		if len(cachedInsights) > 0 {
			applyCausalInsights(causalCtx, cachedInsights)
		}
		log.Info("AI response truncated (0 enhanced), reusing last good result", "tokens", tokens)
		return &AIStatus{
			Enabled:         enabled,
			Provider:        provider.Name(),
			IssuesEnhanced:  cachedResult.IssuesEnhanced,
			CacheHit:        true,
			TotalIssueCount: cachedResult.TotalIssueCount,
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

// runCheck performs a health check and broadcasts results.
// When using ConsoleDataSource, it iterates all clusters. Otherwise, it uses "" as the cluster key.
func (s *Server) runCheck(ctx context.Context) {
	if cds, ok := s.client.(*datasource.ConsoleDataSource); ok {
		clusters, err := cds.GetClusters(ctx)
		if err != nil {
			log.Error(err, "Failed to get clusters")
			clusters = []string{cds.ClusterID()}
		}
		for _, clusterID := range clusters {
			s.runCheckForCluster(ctx, clusterID, cds.ForCluster(clusterID))
		}
	} else {
		s.runCheckForCluster(ctx, "", s.client)
	}
}

// runCheckForCluster performs a health check for a single cluster and broadcasts results.
func (s *Server) runCheckForCluster(ctx context.Context, clusterID string, ds datasource.DataSource) {
	resolver := scope.NewResolver(ds, "default")
	namespaces, err := resolver.ResolveNamespaces(ctx, assistv1alpha1.ScopeConfig{
		NamespaceSelector: &metav1.LabelSelector{},
	})
	if err != nil {
		log.Error(err, "Failed to resolve namespaces", "cluster", clusterID)
		namespaces = []string{"default"}
	}
	namespaces = scope.FilterSystemNamespaces(namespaces)
	if len(namespaces) == 0 {
		namespaces = []string{"default"}
	}

	checkCtx2, cancel := context.WithTimeout(ctx, s.checkTimeout)
	defer cancel()

	s.broadcastPhase(clusterID, "checkers")

	checkCtx := &checker.CheckContext{
		DataSource: ds,
		Namespaces: namespaces,
		AIEnabled:  false,
	}

	results := s.registry.RunAll(checkCtx2, checkCtx, nil)

	s.broadcastPhase(clusterID, "causal")

	now := time.Now()
	causalCtx := s.correlator.Analyze(causal.CorrelationInput{
		Results:   results,
		Timestamp: now,
	})

	checkCtx.ClusterContext = ai.ClusterContext{
		Namespaces: namespaces,
	}
	checkCtx.CausalContext = toCausalAnalysisContext(causalCtx)

	s.mu.RLock()
	provider := s.aiProvider
	enabled := s.aiEnabled
	s.mu.RUnlock()

	checkCtx.AIEnabled = enabled
	checkCtx.AIProvider = provider

	s.broadcastPhase(clusterID, "ai")

	// Get or create cluster state for AI analysis
	s.mu.Lock()
	cs := s.getOrCreateClusterState(clusterID)
	s.mu.Unlock()

	aiStatus := s.runAIAnalysisForCluster(checkCtx2, results, causalCtx, checkCtx, enabled, provider, cs)

	// Convert to dashboard format
	update := &HealthUpdate{
		Timestamp:  now,
		Namespaces: namespaces,
		Results:    make(map[string]CheckResult),
		AIStatus:   aiStatus,
	}
	if clusterID != "" {
		update.ClusterID = clusterID
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

	// Store latest and broadcast
	s.mu.Lock()
	cs.latest = update
	cs.latestCausal = causalCtx
	for clientCh, subscribedID := range s.clients {
		if subscribedID != "" && subscribedID != clusterID {
			continue
		}
		select {
		case clientCh <- *update:
		default:
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
	cs.history.Add(snapshot)
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
	costPer1K := ai.CostPer1KTokens(provider, "")
	return float64(tokens) / 1000.0 * costPer1K
}

// handleHealth returns current health status
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	clusterID := r.URL.Query().Get("clusterId")

	s.mu.RLock()
	cs, ok := s.clusters[clusterID]
	var latest *HealthUpdate
	if ok {
		latest = cs.latest
	}
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
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	clusterID := r.URL.Query().Get("clusterId")
	clientCh := make(chan HealthUpdate, 10)

	s.mu.Lock()
	if s.maxSSEClients > 0 && len(s.clients) >= s.maxSSEClients {
		s.mu.Unlock()
		http.Error(w, "Too many SSE clients", http.StatusServiceUnavailable)
		return
	}
	s.clients[clientCh] = clusterID
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, clientCh)
		s.mu.Unlock()
		close(clientCh)
	}()

	// Send initial state
	s.mu.RLock()
	if cs, ok := s.clusters[clusterID]; ok && cs.latest != nil {
		data, err := json.Marshal(cs.latest)
		if err != nil {
			log.Error(err, "Failed to marshal SSE initial state")
			s.mu.RUnlock()
			return
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	} else if clusterID == "" {
		// For fleet subscription, send the first available cluster's latest
		for _, cs := range s.clusters {
			if cs.latest != nil {
				data, err := json.Marshal(cs.latest)
				if err != nil {
					log.Error(err, "Failed to marshal SSE initial state")
					break
				}
				_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				break
			}
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
		Enabled:      s.aiEnabled,
		Provider:     s.aiConfig.Provider,
		Model:        s.aiConfig.Model,
		ExplainModel: s.aiConfig.ExplainModel,
		HasAPIKey:    s.aiConfig.APIKey != "",
		ProviderReady: func() bool {
			if mgr, ok := s.aiProvider.(*ai.Manager); ok {
				return mgr.ProviderAvailable()
			}
			return s.aiProvider != nil && s.aiProvider.Available()
		}(),
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

	// Stage new config without committing to server state yet
	s.mu.RLock()
	stagedConfig := s.aiConfig
	s.mu.RUnlock()

	if providerName != "" {
		stagedConfig.Provider = providerName
	}
	if req.APIKey != "" {
		stagedConfig.APIKey = req.APIKey
	}
	if req.Model != "" {
		stagedConfig.Model = req.Model
	}
	stagedConfig.ExplainModel = req.ExplainModel

	// If the provider is an ai.Manager, use Reconfigure() so the controller's
	// AI provider is also updated (Manager is shared between dashboard and controllers).
	s.mu.RLock()
	mgr, isManager := s.aiProvider.(*ai.Manager)
	s.mu.RUnlock()

	if isManager {
		if providerName != "" {
			if err := mgr.Reconfigure(stagedConfig.Provider, stagedConfig.APIKey, stagedConfig.Model, stagedConfig.ExplainModel); err != nil {
				log.Error(err, "Failed to reconfigure AI provider", "provider", stagedConfig.Provider)
				http.Error(w, "Failed to reconfigure AI provider", http.StatusInternalServerError)
				return
			}
		}
		// Set enabled AFTER successful reconfigure to avoid partial state.
		// This also overrides Reconfigure's own m.enabled inference.
		mgr.SetEnabled(req.Enabled)

		// Reconfigure succeeded — commit settings atomically
		s.mu.Lock()
		s.aiEnabled = req.Enabled
		s.aiConfig = stagedConfig
		for _, cs := range s.clusters {
			cs.lastIssueHash = ""
			cs.lastAIResult = nil
			cs.lastAIEnhancements = nil
			cs.lastCausalInsights = nil
			if cs.latest != nil {
				cs.latest.AIStatus = &AIStatus{
					Enabled:  req.Enabled,
					Provider: stagedConfig.Provider,
					Pending:  true,
				}
			}
		}
		s.mu.Unlock()

		resp := AISettingsResponse{
			Enabled:       req.Enabled,
			Provider:      stagedConfig.Provider,
			Model:         stagedConfig.Model,
			ExplainModel:  stagedConfig.ExplainModel,
			HasAPIKey:     stagedConfig.APIKey != "",
			ProviderReady: mgr.ProviderAvailable(),
		}
		log.Info("AI settings updated via dashboard (Manager)", "provider", resp.Provider, "enabled", resp.Enabled)

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
	newProvider, _, _, err := ai.NewProvider(stagedConfig)
	if err != nil {
		log.Error(err, "Failed to create AI provider", "provider", stagedConfig.Provider)
		http.Error(w, "Failed to create AI provider", http.StatusInternalServerError)
		return
	}

	// Provider creation succeeded — commit settings atomically
	s.mu.Lock()
	s.aiEnabled = req.Enabled
	s.aiConfig = stagedConfig
	s.aiProvider = newProvider
	for _, cs := range s.clusters {
		cs.lastIssueHash = ""
		cs.lastAIResult = nil
		cs.lastAIEnhancements = nil
		cs.lastCausalInsights = nil
		if cs.latest != nil {
			cs.latest.AIStatus = &AIStatus{
				Enabled:  req.Enabled,
				Provider: stagedConfig.Provider,
				Pending:  true,
			}
		}
	}
	s.mu.Unlock()

	resp := AISettingsResponse{
		Enabled:       req.Enabled,
		Provider:      stagedConfig.Provider,
		Model:         stagedConfig.Model,
		ExplainModel:  stagedConfig.ExplainModel,
		HasAPIKey:     stagedConfig.APIKey != "",
		ProviderReady: newProvider.Available(),
	}

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
	clusterID := r.URL.Query().Get("clusterId")

	s.mu.RLock()
	cs, ok := s.clusters[clusterID]
	s.mu.RUnlock()

	if !ok {
		if lastParam := r.URL.Query().Get("last"); lastParam != "" {
			n, err := strconv.Atoi(lastParam)
			if err != nil || n < 1 {
				http.Error(w, "invalid 'last' parameter", http.StatusBadRequest)
				return
			}
		}
		if sinceParam := r.URL.Query().Get("since"); sinceParam != "" {
			if _, err := time.Parse(time.RFC3339, sinceParam); err != nil {
				http.Error(w, "invalid 'since' parameter, use RFC3339", http.StatusBadRequest)
				return
			}
		}
		if err := json.NewEncoder(w).Encode([]history.HealthSnapshot{}); err != nil {
			log.Error(err, "Failed to encode response", "handler", "handleHealthHistory")
		}
		return
	}

	if lastParam := r.URL.Query().Get("last"); lastParam != "" {
		n, err := strconv.Atoi(lastParam)
		if err != nil || n < 1 {
			http.Error(w, "invalid 'last' parameter", http.StatusBadRequest)
			return
		}
		if n > 1000 {
			n = 1000
		}
		if err := json.NewEncoder(w).Encode(cs.history.Last(n)); err != nil {
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
		if err := json.NewEncoder(w).Encode(cs.history.Since(t)); err != nil {
			log.Error(err, "Failed to encode response", "handler", "handleHealthHistory")
		}
		return
	}

	// Default: return last 50
	if err := json.NewEncoder(w).Encode(cs.history.Last(50)); err != nil {
		log.Error(err, "Failed to encode response", "handler", "handleHealthHistory")
	}
}

// handleCausalGroups returns the latest causal correlation analysis
func (s *Server) handleCausalGroups(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	clusterID := r.URL.Query().Get("clusterId")

	s.mu.RLock()
	var cc *causal.CausalContext
	if cs, ok := s.clusters[clusterID]; ok {
		cc = cs.latestCausal
	}
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

	// Read from cluster state (use "" key for backward compat)
	clusterID := r.URL.Query().Get("clusterId")
	s.mu.RLock()
	cs := s.clusters[clusterID]
	var currentHash string
	var cached *ExplainCacheEntry
	var latest *HealthUpdate
	var causalCtx *causal.CausalContext
	if cs != nil {
		currentHash = cs.lastIssueHash
		cached = cs.lastExplain
		latest = cs.latest
		causalCtx = cs.latestCausal
	}
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
	historySnapshots := cs.history.Last(20)
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
	if cs != nil {
		cs.lastExplain = &ExplainCacheEntry{
			Response:  explainResp,
			IssueHash: currentHash,
			CachedAt:  time.Now(),
		}
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

	clusterID := r.URL.Query().Get("clusterId")

	s.mu.RLock()
	cs, ok := s.clusters[clusterID]
	s.mu.RUnlock()

	if !ok {
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(map[string]string{
			"status":  "insufficient_data",
			"message": "Need at least 5 health snapshots for prediction",
		}); err != nil {
			log.Error(err, "Failed to encode prediction response")
		}
		return
	}

	snapshots := cs.history.Last(50)

	result := prediction.Analyze(snapshots)
	if result == nil {
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

// handleClusters returns the list of known cluster IDs from the console backend.
// Falls back to an empty array when not running in console mode.
func (s *Server) handleClusters(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if cds, ok := s.client.(*datasource.ConsoleDataSource); ok {
		clusters, err := cds.GetClusters(r.Context())
		if err != nil {
			log.Error(err, "Failed to get clusters")
			if err := json.NewEncoder(w).Encode(map[string]any{"clusters": []string{}}); err != nil {
				log.Error(err, "Failed to encode response", "handler", "handleClusters")
			}
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"clusters": clusters}); err != nil {
			log.Error(err, "Failed to encode response", "handler", "handleClusters")
		}
		return
	}

	if err := json.NewEncoder(w).Encode(map[string]any{"clusters": []string{}}); err != nil {
		log.Error(err, "Failed to encode response", "handler", "handleClusters")
	}
}

// handleFleetSummary returns an aggregate view of all clusters.
func (s *Server) handleFleetSummary(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	s.mu.RLock()
	summary := FleetSummary{
		Clusters: make([]FleetClusterEntry, 0, len(s.clusters)),
	}
	for clusterID, cs := range s.clusters {
		if cs.latest == nil {
			continue
		}
		total := cs.latest.Summary.TotalHealthy + cs.latest.Summary.TotalIssues
		score := float64(100)
		if total > 0 {
			score = float64(cs.latest.Summary.TotalHealthy) / float64(total) * 100
		}
		summary.Clusters = append(summary.Clusters, FleetClusterEntry{
			ClusterID:     clusterID,
			HealthScore:   score,
			TotalIssues:   cs.latest.Summary.TotalIssues,
			CriticalCount: cs.latest.Summary.CriticalCount,
			WarningCount:  cs.latest.Summary.WarningCount,
			InfoCount:     cs.latest.Summary.InfoCount,
			LastUpdated:   cs.latest.Timestamp.Format(time.RFC3339),
		})
	}
	s.mu.RUnlock()

	if err := json.NewEncoder(w).Encode(summary); err != nil {
		log.Error(err, "Failed to encode response", "handler", "handleFleetSummary")
	}
}

// handleAICatalog returns the model catalog, optionally filtered by provider.
// GET /api/settings/ai/catalog?provider=anthropic
func (s *Server) handleAICatalog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	catalog := ai.DefaultCatalog()

	if provider := r.URL.Query().Get("provider"); provider != "" {
		filtered := ai.ModelCatalog{}
		if models := catalog.ForProvider(provider); models != nil {
			filtered[provider] = models
		}
		catalog = filtered
	}

	if err := json.NewEncoder(w).Encode(catalog); err != nil {
		log.Error(err, "Failed to encode response", "handler", "handleAICatalog")
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
