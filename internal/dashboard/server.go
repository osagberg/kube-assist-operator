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
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	assistv1alpha1 "github.com/osagberg/kube-assist-operator/api/v1alpha1"
	"golang.org/x/time/rate"

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
	Timestamp   time.Time              `json:"timestamp"`
	ClusterID   string                 `json:"clusterId,omitempty"`
	Namespaces  []string               `json:"namespaces"`
	Results     map[string]CheckResult `json:"results"`
	Summary     Summary                `json:"summary"`
	AIStatus    *AIStatus              `json:"aiStatus,omitempty"`
	IssueStates map[string]*IssueState `json:"issueStates,omitempty"`
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
	issueStates        map[string]*IssueState // per-cluster issue ack/snooze state
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

// Issue state action constants.
const (
	ActionAcknowledged = "acknowledged"
	ActionSnoozed      = "snoozed"
)

// IssueState represents a user action on an issue (acknowledge or snooze).
type IssueState struct {
	Key          string     `json:"key"`
	Action       string     `json:"action"` // ActionAcknowledged or ActionSnoozed
	Reason       string     `json:"reason,omitempty"`
	SnoozedUntil *time.Time `json:"snoozedUntil,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
}

// AISettingsRequest is the JSON body for POST /api/settings/ai
type AISettingsRequest struct {
	Enabled      bool   `json:"enabled"`
	Provider     string `json:"provider"`
	APIKey       string `json:"apiKey,omitempty"`
	ClearAPIKey  bool   `json:"clearApiKey,omitempty"`
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
	k8sWriter         client.Client // optional: for creating TroubleshootRequest CRs
	scheme            *runtime.Scheme
	mu                sync.RWMutex
	clients           map[chan HealthUpdate]string // chan -> subscribed clusterId ("" = all)
	clusters          map[string]*clusterState     // clusterID -> state ("" key for single-cluster)
	maxSSEClients     int
	sessionTTL        time.Duration
	mutationLimiter   *rate.Limiter
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
		sessionTTL:        parseSessionTTL(),
		mutationLimiter:   newMutationLimiter(),
		stopCh:            make(chan struct{}),
	}
}

// getOrCreateClusterState returns the state for a cluster, creating it if needed.
// Must be called with s.mu held (write lock).
func (s *Server) getOrCreateClusterState(clusterID string) *clusterState {
	cs, ok := s.clusters[clusterID]
	if !ok {
		cs = &clusterState{
			history:     history.New(100),
			issueStates: make(map[string]*IssueState),
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

const defaultSessionTTL = 24 * time.Hour

func parseSessionTTL() time.Duration {
	v := strings.TrimSpace(os.Getenv("DASHBOARD_SESSION_TTL"))
	if v == "" {
		return defaultSessionTTL
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		log.Info("Invalid DASHBOARD_SESSION_TTL, using default", "value", v, "default", defaultSessionTTL)
		return defaultSessionTTL
	}
	return d
}

const (
	defaultRateLimit = 10 // requests per second
	defaultRateBurst = 20
)

func newMutationLimiter() *rate.Limiter {
	rps := defaultRateLimit
	burst := defaultRateBurst
	if v := os.Getenv("DASHBOARD_RATE_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			rps = n
		}
	}
	if v := os.Getenv("DASHBOARD_RATE_BURST"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			burst = n
		}
	}
	return rate.NewLimiter(rate.Limit(rps), burst)
}

// rateLimitMiddleware wraps a handler with token-bucket rate limiting on mutating methods.
func (s *Server) rateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
			if !s.mutationLimiter.Allow() {
				http.Error(w, "Too many requests", http.StatusTooManyRequests)
				return
			}
		}
		next(w, r)
	}
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

		// 1. Check Authorization header (programmatic clients / curl)
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

		// 2. Check session cookie (browser clients — token is never exposed to JS)
		cookie, err := r.Cookie("__dashboard_session")
		if err == nil && cookie.Value != "" {
			gotHash := sha256.Sum256([]byte(cookie.Value))
			wantHash := sha256.Sum256([]byte(s.authToken))
			if subtle.ConstantTimeCompare(gotHash[:], wantHash[:]) == 1 {
				next(w, r)
				return
			}
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}
}

func (s *Server) sseAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.authToken == "" {
			next(w, r)
			return
		}

		// 1. Check Authorization header first
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

		// 2. Check session cookie (set when serving index.html)
		cookie, err := r.Cookie("__dashboard_session")
		if err == nil && cookie.Value != "" {
			gotHash := sha256.Sum256([]byte(cookie.Value))
			wantHash := sha256.Sum256([]byte(s.authToken))
			if subtle.ConstantTimeCompare(gotHash[:], wantHash[:]) == 1 {
				next(w, r)
				return
			}
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		// No valid auth method found
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
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

// WithK8sWriter configures a Kubernetes client for creating TroubleshootRequest CRs.
func (s *Server) WithK8sWriter(c client.Client, scheme *runtime.Scheme) *Server {
	s.k8sWriter = c
	s.scheme = scheme
	return s
}

// Start starts the dashboard server
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/health", s.authMiddleware(s.handleHealth))
	mux.HandleFunc("/api/health/history", s.authMiddleware(s.handleHealthHistory))
	mux.HandleFunc("/api/events", s.sseAuthMiddleware(s.handleSSE))
	mux.HandleFunc("/api/check", s.authMiddleware(s.rateLimitMiddleware(s.handleTriggerCheck)))
	mux.HandleFunc("/api/settings/ai", s.authMiddleware(s.rateLimitMiddleware(s.handleAISettings)))
	mux.HandleFunc("/api/causal/groups", s.authMiddleware(s.handleCausalGroups))
	mux.HandleFunc("/api/explain", s.authMiddleware(s.handleExplain))
	mux.HandleFunc("/api/prediction/trend", s.authMiddleware(s.handlePrediction))
	mux.HandleFunc("/api/clusters", s.authMiddleware(s.handleClusters))
	mux.HandleFunc("/api/fleet/summary", s.authMiddleware(s.handleFleetSummary))
	mux.HandleFunc("/api/settings/ai/catalog", s.authMiddleware(s.handleAICatalog))
	mux.HandleFunc("/api/troubleshoot", s.authMiddleware(s.rateLimitMiddleware(s.handleCreateTroubleshoot)))
	mux.HandleFunc("/api/issues/acknowledge", s.authMiddleware(s.rateLimitMiddleware(s.handleIssueAcknowledge)))
	mux.HandleFunc("/api/issues/snooze", s.authMiddleware(s.rateLimitMiddleware(s.handleIssueSnooze)))
	mux.HandleFunc("/api/issue-states", s.authMiddleware(s.handleIssueStates))
	mux.HandleFunc("/api/capabilities", s.authMiddleware(s.handleCapabilities))
	// React SPA dashboard
	spaFS, err := fs.Sub(webAssets, "web/dist")
	if err != nil {
		log.Error(err, "Failed to create sub filesystem for SPA assets")
	} else {
		fileServer := http.FileServer(http.FS(spaFS))

		// Read index.html once at startup for session cookie injection
		indexHTML, readErr := fs.ReadFile(spaFS, "index.html")
		if readErr != nil {
			log.Error(readErr, "Failed to read index.html from embedded assets")
			indexHTML = []byte("<!DOCTYPE html><html><body>Dashboard unavailable</body></html>")
		}

		serveIndex := func(w http.ResponseWriter, _ *http.Request) {
			if s.authToken != "" {
				// Set HttpOnly session cookie — the sole browser auth mechanism.
				// Token is never exposed to JavaScript (no meta tag, no JS-readable value).
				http.SetCookie(w, &http.Cookie{
					Name:     "__dashboard_session",
					Value:    s.authToken,
					Path:     "/",
					HttpOnly: true,
					Secure:   s.tlsConfigured(),
					SameSite: http.SameSiteStrictMode,
					MaxAge:   int(s.sessionTTL.Seconds()),
				})
				w.Header().Set("Cache-Control", "no-store")
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(indexHTML)
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
		Handler:      s.securityHeaders(mux),
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

	// Snapshot active issue states for summary exclusion
	s.mu.RLock()
	activeStates := make(map[string]*IssueState, len(cs.issueStates))
	now2 := time.Now()
	for k, st := range cs.issueStates {
		if st.Action == ActionSnoozed && st.SnoozedUntil != nil && st.SnoozedUntil.Before(now2) {
			continue // expired snooze
		}
		activeStates[k] = st
	}
	s.mu.RUnlock()

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

				// Exclude muted issues from severity counts
				issueKey := issue.Namespace + "/" + issue.Resource + "/" + issue.Type
				if _, muted := activeStates[issueKey]; muted {
					continue
				}

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
	// Merge non-expired issue states into the update and clean up expired entries
	nowBroadcast := time.Now()
	for k, st := range cs.issueStates {
		if st.Action == ActionSnoozed && st.SnoozedUntil != nil && st.SnoozedUntil.Before(nowBroadcast) {
			delete(cs.issueStates, k)
			continue
		}
		if update.IssueStates == nil {
			update.IssueStates = make(map[string]*IssueState)
		}
		update.IssueStates[k] = st
	}
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
	if ok && cs.latest != nil {
		// Deep-copy to avoid mutating cached state (IssueStates map is shared)
		cp := *cs.latest
		latest = &cp
		// Merge non-expired per-cluster issue states into a fresh map
		nowHealth := time.Now()
		states := make(map[string]*IssueState, len(cs.issueStates))
		for k, st := range cs.issueStates {
			if st.Action == ActionSnoozed && st.SnoozedUntil != nil && st.SnoozedUntil.Before(nowHealth) {
				continue
			}
			states[k] = st
		}
		if len(states) > 0 {
			latest.IssueStates = states
		} else {
			latest.IssueStates = nil
		}
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

// Input length limits for AI settings fields.
const (
	maxAPIKeyLen        = 256
	maxModelLen         = 128
	maxSettingsBodySize = 1 << 20 // 1 MB
)

// handlePostAISettings updates AI configuration at runtime
func (s *Server) handlePostAISettings(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodySize)
	var req AISettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	// Validate field lengths to prevent unbounded input
	if len(req.APIKey) > maxAPIKeyLen {
		http.Error(w, fmt.Sprintf("apiKey exceeds maximum length of %d", maxAPIKeyLen), http.StatusBadRequest)
		return
	}
	if len(req.Model) > maxModelLen {
		http.Error(w, fmt.Sprintf("model exceeds maximum length of %d", maxModelLen), http.StatusBadRequest)
		return
	}
	if len(req.ExplainModel) > maxModelLen {
		http.Error(w, fmt.Sprintf("explainModel exceeds maximum length of %d", maxModelLen), http.StatusBadRequest)
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
	if req.ClearAPIKey {
		stagedConfig.APIKey = ""
	} else if req.APIKey != "" {
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
		if providerName != "" || req.ClearAPIKey {
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

// CreateTroubleshootBody is the JSON body for POST /api/troubleshoot.
type CreateTroubleshootBody struct {
	Namespace string `json:"namespace"`
	Target    struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"target"`
	Actions   []string `json:"actions,omitempty"`
	TailLines int32    `json:"tailLines,omitempty"`
	TTL       *int32   `json:"ttlSecondsAfterFinished,omitempty"`
}

// CreateTroubleshootResponse is the JSON response for POST /api/troubleshoot.
type CreateTroubleshootResponse struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Phase     string `json:"phase"`
}

// TroubleshootRequestSummary is a brief summary of a TroubleshootRequest.
type TroubleshootRequestSummary struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Phase     string `json:"phase"`
	Target    struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"target"`
}

const (
	defaultTargetKind = "Deployment"
	defaultNamespace  = "default"
	maxTailLines      = int32(10000)
	maxTTLSeconds     = int32(86400) // 24 hours
)

// k8sNameRe matches valid Kubernetes resource names (RFC 1123 DNS label).
var k8sNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]{0,61}[a-z0-9])?$`)

var validTargetKinds = map[string]bool{
	defaultTargetKind: true,
	"StatefulSet":     true,
	"DaemonSet":       true,
	"Pod":             true,
	"ReplicaSet":      true,
}

var validActions = map[string]bool{
	"diagnose": true,
	"logs":     true,
	"events":   true,
	"describe": true,
	"all":      true,
}

// handleCreateTroubleshoot handles POST and GET for /api/troubleshoot.
func (s *Server) handleCreateTroubleshoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handlePostTroubleshoot(w, r)
	case http.MethodGet:
		s.handleListTroubleshoot(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePostTroubleshoot(w http.ResponseWriter, r *http.Request) {
	if s.k8sWriter == nil {
		http.Error(w, "TroubleshootRequest creation not available (console mode)", http.StatusServiceUnavailable)
		return
	}

	var body CreateTroubleshootBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	// Validate target name (required, valid K8s name)
	if body.Target.Name == "" {
		http.Error(w, "target.name is required", http.StatusBadRequest)
		return
	}
	if !k8sNameRe.MatchString(body.Target.Name) {
		http.Error(w, "target.name must be a valid Kubernetes name (lowercase, alphanumeric, hyphens)", http.StatusBadRequest)
		return
	}

	// Default target kind
	if body.Target.Kind == "" {
		body.Target.Kind = defaultTargetKind
	}
	if !validTargetKinds[body.Target.Kind] {
		http.Error(w, "invalid target.kind: must be one of Deployment, StatefulSet, DaemonSet, Pod, ReplicaSet", http.StatusBadRequest)
		return
	}

	// Default namespace (validate if provided)
	if body.Namespace == "" {
		body.Namespace = defaultNamespace
	}
	if !k8sNameRe.MatchString(body.Namespace) {
		http.Error(w, "namespace must be a valid Kubernetes name (lowercase, alphanumeric, hyphens)", http.StatusBadRequest)
		return
	}

	// Default actions — normalize "all" (mutually exclusive with specifics)
	if len(body.Actions) == 0 {
		body.Actions = []string{"diagnose"}
	}
	for _, a := range body.Actions {
		if !validActions[a] {
			http.Error(w, fmt.Sprintf("invalid action %q: must be one of diagnose, logs, events, describe, all", a), http.StatusBadRequest)
			return
		}
	}
	// If "all" is specified, normalize to just ["all"]
	if slices.Contains(body.Actions, "all") {
		body.Actions = []string{"all"}
	}

	// Default and bound tailLines
	if body.TailLines <= 0 {
		body.TailLines = 100
	}
	if body.TailLines > maxTailLines {
		http.Error(w, fmt.Sprintf("tailLines must be <= %d", maxTailLines), http.StatusBadRequest)
		return
	}

	// Bound TTL
	if body.TTL != nil && (*body.TTL <= 0 || *body.TTL > maxTTLSeconds) {
		http.Error(w, fmt.Sprintf("ttlSecondsAfterFinished must be between 1 and %d", maxTTLSeconds), http.StatusBadRequest)
		return
	}

	actions := make([]assistv1alpha1.TroubleshootAction, len(body.Actions))
	for i, a := range body.Actions {
		actions[i] = assistv1alpha1.TroubleshootAction(a)
	}

	cr := &assistv1alpha1.TroubleshootRequest{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "dash-",
			Namespace:    body.Namespace,
		},
		Spec: assistv1alpha1.TroubleshootRequestSpec{
			Target: assistv1alpha1.TargetRef{
				Kind: body.Target.Kind,
				Name: body.Target.Name,
			},
			Actions:                 actions,
			TailLines:               body.TailLines,
			TTLSecondsAfterFinished: body.TTL,
		},
	}

	if err := s.k8sWriter.Create(r.Context(), cr); err != nil {
		log.Error(err, "Failed to create TroubleshootRequest")
		http.Error(w, "Failed to create TroubleshootRequest: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := CreateTroubleshootResponse{
		Name:      cr.Name,
		Namespace: cr.Namespace,
		Phase:     string(assistv1alpha1.PhasePending),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Error(err, "Failed to encode response", "handler", "handlePostTroubleshoot")
	}
}

func (s *Server) handleListTroubleshoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.k8sWriter == nil {
		http.Error(w, "TroubleshootRequest listing not available (console mode)", http.StatusServiceUnavailable)
		return
	}

	var list assistv1alpha1.TroubleshootRequestList
	ns := r.URL.Query().Get("namespace")
	var opts []client.ListOption
	if ns != "" {
		opts = append(opts, client.InNamespace(ns))
	}

	if err := s.k8sWriter.List(r.Context(), &list, opts...); err != nil {
		log.Error(err, "Failed to list TroubleshootRequests")
		http.Error(w, "Failed to list TroubleshootRequests: "+err.Error(), http.StatusInternalServerError)
		return
	}

	summaries := make([]TroubleshootRequestSummary, len(list.Items))
	for i, item := range list.Items {
		summaries[i] = TroubleshootRequestSummary{
			Name:      item.Name,
			Namespace: item.Namespace,
			Phase:     string(item.Status.Phase),
		}
		summaries[i].Target.Kind = item.Spec.Target.Kind
		summaries[i].Target.Name = item.Spec.Target.Name
	}

	if err := json.NewEncoder(w).Encode(summaries); err != nil {
		log.Error(err, "Failed to encode response", "handler", "handleListTroubleshoot")
	}
}

// handleCapabilities returns feature flags so the frontend can hide unsupported actions.
func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]bool{
		"troubleshootCreate": s.k8sWriter != nil,
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Error(err, "Failed to encode capabilities response")
	}
}

// Input limits for issue state keys.
const (
	maxIssueKeyLen    = 512
	maxIssueReasonLen = 1024
	maxSnoozeDuration = 24 * time.Hour
)

// issueKeyRe validates the expected format: namespace/resource/type.
var issueKeyRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]{0,61}[a-z0-9])?/.+/.+$`)

// validateIssueKey checks key format and length.
func validateIssueKey(key string) error {
	if key == "" {
		return fmt.Errorf("key is required")
	}
	if len(key) > maxIssueKeyLen {
		return fmt.Errorf("key exceeds maximum length of %d", maxIssueKeyLen)
	}
	if !issueKeyRe.MatchString(key) {
		return fmt.Errorf("key must match format namespace/resource/type")
	}
	return nil
}

// handleIssueAcknowledge handles POST and DELETE for /api/issues/acknowledge.
func (s *Server) handleIssueAcknowledge(w http.ResponseWriter, r *http.Request) {
	clusterID := r.URL.Query().Get("clusterId")

	switch r.Method {
	case http.MethodPost:
		var req struct {
			Key    string `json:"key"`
			Reason string `json:"reason,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON body", http.StatusBadRequest)
			return
		}
		if err := validateIssueKey(req.Key); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(req.Reason) > maxIssueReasonLen {
			http.Error(w, fmt.Sprintf("reason exceeds maximum length of %d", maxIssueReasonLen), http.StatusBadRequest)
			return
		}
		state := &IssueState{
			Key:       req.Key,
			Action:    ActionAcknowledged,
			Reason:    req.Reason,
			CreatedAt: time.Now(),
		}
		s.mu.Lock()
		cs := s.getOrCreateClusterState(clusterID)
		cs.issueStates[req.Key] = state
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(state); err != nil {
			log.Error(err, "Failed to encode response", "handler", "handleIssueAcknowledge")
		}
	case http.MethodDelete:
		var req struct {
			Key string `json:"key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON body", http.StatusBadRequest)
			return
		}
		if req.Key == "" {
			http.Error(w, "key is required", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		if cs, ok := s.clusters[clusterID]; ok {
			delete(cs.issueStates, req.Key)
		}
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "removed"}); err != nil {
			log.Error(err, "Failed to encode response", "handler", "handleIssueAcknowledge")
		}
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleIssueSnooze handles POST and DELETE for /api/issues/snooze.
func (s *Server) handleIssueSnooze(w http.ResponseWriter, r *http.Request) {
	clusterID := r.URL.Query().Get("clusterId")

	switch r.Method {
	case http.MethodPost:
		var req struct {
			Key      string `json:"key"`
			Duration string `json:"duration"`
			Reason   string `json:"reason,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON body", http.StatusBadRequest)
			return
		}
		if err := validateIssueKey(req.Key); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(req.Reason) > maxIssueReasonLen {
			http.Error(w, fmt.Sprintf("reason exceeds maximum length of %d", maxIssueReasonLen), http.StatusBadRequest)
			return
		}
		if req.Duration == "" {
			http.Error(w, "duration is required", http.StatusBadRequest)
			return
		}
		dur, err := time.ParseDuration(req.Duration)
		if err != nil {
			http.Error(w, "invalid duration: "+err.Error(), http.StatusBadRequest)
			return
		}
		if dur <= 0 {
			http.Error(w, "duration must be positive", http.StatusBadRequest)
			return
		}
		if dur > maxSnoozeDuration {
			http.Error(w, fmt.Sprintf("duration must be <= %s", maxSnoozeDuration), http.StatusBadRequest)
			return
		}
		snoozedUntil := time.Now().Add(dur)
		state := &IssueState{
			Key:          req.Key,
			Action:       ActionSnoozed,
			Reason:       req.Reason,
			SnoozedUntil: &snoozedUntil,
			CreatedAt:    time.Now(),
		}
		s.mu.Lock()
		cs := s.getOrCreateClusterState(clusterID)
		cs.issueStates[req.Key] = state
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(state); err != nil {
			log.Error(err, "Failed to encode response", "handler", "handleIssueSnooze")
		}
	case http.MethodDelete:
		var req struct {
			Key string `json:"key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON body", http.StatusBadRequest)
			return
		}
		if req.Key == "" {
			http.Error(w, "key is required", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		if cs, ok := s.clusters[clusterID]; ok {
			delete(cs.issueStates, req.Key)
		}
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "removed"}); err != nil {
			log.Error(err, "Failed to encode response", "handler", "handleIssueSnooze")
		}
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleIssueStates handles GET for /api/issue-states.
func (s *Server) handleIssueStates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	clusterID := r.URL.Query().Get("clusterId")

	s.mu.RLock()
	active := make(map[string]*IssueState)
	if cs, ok := s.clusters[clusterID]; ok {
		nowStates := time.Now()
		for k, st := range cs.issueStates {
			if st.Action == ActionSnoozed && st.SnoozedUntil != nil && st.SnoozedUntil.Before(nowStates) {
				continue
			}
			active[k] = st
		}
	}
	s.mu.RUnlock()

	if err := json.NewEncoder(w).Encode(active); err != nil {
		log.Error(err, "Failed to encode response", "handler", "handleIssueStates")
	}
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if s.tlsConfigured() {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}
