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
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"golang.org/x/time/rate"

	"github.com/osagberg/kube-assist-operator/internal/ai"
	"github.com/osagberg/kube-assist-operator/internal/causal"
	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/datasource"
	"github.com/osagberg/kube-assist-operator/internal/history"
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
	TotalHealthy             int     `json:"totalHealthy"`
	TotalIssues              int     `json:"totalIssues"`
	CriticalCount            int     `json:"criticalCount"`
	WarningCount             int     `json:"warningCount"`
	InfoCount                int     `json:"infoCount"`
	HealthScore              float64 `json:"healthScore"`
	DeploymentReady          int     `json:"deploymentReady"`
	DeploymentDesired        int     `json:"deploymentDesired"`
	DeploymentReadinessScore float64 `json:"deploymentReadinessScore"`
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
	ClusterID                string  `json:"clusterId"`
	HealthScore              float64 `json:"healthScore"`
	TotalIssues              int     `json:"totalIssues"`
	CriticalCount            int     `json:"criticalCount"`
	WarningCount             int     `json:"warningCount"`
	InfoCount                int     `json:"infoCount"`
	DeploymentReady          int     `json:"deploymentReady"`
	DeploymentDesired        int     `json:"deploymentDesired"`
	DeploymentReadinessScore float64 `json:"deploymentReadinessScore"`
	LastUpdated              string  `json:"lastUpdated"`
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

// IsExpired returns true if the issue state is a snoozed entry that has expired.
func (st *IssueState) IsExpired() bool {
	return st.Action == ActionSnoozed && st.SnoozedUntil != nil && st.SnoozedUntil.Before(time.Now())
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
	checkInterval     time.Duration // health check polling interval
	aiAnalysisTimeout time.Duration // AI analysis context timeout
	maxIssuesPerBatch int           // max issues sent to AI per batch
	sseBufferSize     int           // SSE client channel buffer capacity
	historySize       int           // health history ring buffer capacity
	clusterWorkers    int
	correlator        *causal.Correlator
	logContextConfig  *checker.LogContextConfig
	logContextClient  kubernetes.Interface
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
	chatSessions      map[string]*ChatSession
	chatMu            sync.RWMutex
	chatEnabled       bool
	chatMaxTurns      int
	chatTokenBudget   int
	chatSessionTTL    time.Duration
	chatMaxSessions   int
	chatAIKey         string // API key for chat agent
	chatAIModel       string // Model for chat agent
	chatProviderName  string // Provider name for chat agent
	chatEndpoint      string // Endpoint for chat agent
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
		checkInterval:     30 * time.Second,
		aiAnalysisTimeout: 90 * time.Second,
		maxIssuesPerBatch: 15,
		sseBufferSize:     10,
		historySize:       100,
		clusterWorkers:    parseClusterWorkers(),
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
			history:     history.New(s.historySize),
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
	defaultRateLimit      = 10 // requests per second
	defaultRateBurst      = 20
	defaultClusterWorkers = 4
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

func parseClusterWorkers() int {
	v := strings.TrimSpace(os.Getenv("DASHBOARD_CLUSTER_WORKERS"))
	if v == "" {
		return defaultClusterWorkers
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		log.Info("Invalid DASHBOARD_CLUSTER_WORKERS, using default", "value", v, "default", defaultClusterWorkers)
		return defaultClusterWorkers
	}
	return n
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

// validateToken checks the request for a valid auth token via Bearer header
// or session cookie. Returns true if authenticated.
func (s *Server) validateToken(r *http.Request) bool {
	wantHash := sha256.Sum256([]byte(s.authToken))

	// 1. Check Authorization header (programmatic clients / curl)
	if auth := r.Header.Get("Authorization"); auth != "" && strings.HasPrefix(auth, "Bearer ") {
		token := strings.TrimPrefix(auth, "Bearer ")
		gotHash := sha256.Sum256([]byte(token))
		return subtle.ConstantTimeCompare(gotHash[:], wantHash[:]) == 1
	}

	// 2. Check session cookie (browser clients)
	if cookie, err := r.Cookie("__dashboard_session"); err == nil && cookie.Value != "" {
		gotHash := sha256.Sum256([]byte(cookie.Value))
		return subtle.ConstantTimeCompare(gotHash[:], wantHash[:]) == 1
	}

	return false
}

func hasCookie(r *http.Request, name string) bool {
	_, err := r.Cookie(name)
	return err == nil
}

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.authToken == "" {
			next(w, r)
			return
		}

		if s.validateToken(r) {
			next(w, r)
			return
		}

		// Distinguish no-creds (401) from bad-creds (403)
		if r.Header.Get("Authorization") != "" || hasCookie(r, "__dashboard_session") {
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

		if s.validateToken(r) {
			next(w, r)
			return
		}

		if r.Header.Get("Authorization") != "" || hasCookie(r, "__dashboard_session") {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
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

// WithCheckInterval sets the health check polling interval.
func (s *Server) WithCheckInterval(d time.Duration) *Server {
	if d > 0 {
		s.checkInterval = d
	}
	return s
}

// WithAIAnalysisTimeout sets the AI analysis context timeout.
func (s *Server) WithAIAnalysisTimeout(d time.Duration) *Server {
	if d > 0 {
		s.aiAnalysisTimeout = d
	}
	return s
}

// WithMaxIssuesPerBatch sets the max issues sent to AI per batch.
func (s *Server) WithMaxIssuesPerBatch(n int) *Server {
	if n > 0 {
		s.maxIssuesPerBatch = n
	}
	return s
}

// WithSSEBufferSize sets the SSE client channel buffer capacity.
func (s *Server) WithSSEBufferSize(n int) *Server {
	if n > 0 {
		s.sseBufferSize = n
	}
	return s
}

// WithLogContext configures log/event context enrichment for AI analysis.
func (s *Server) WithLogContext(config *checker.LogContextConfig, clientset kubernetes.Interface) *Server {
	s.logContextConfig = config
	s.logContextClient = clientset
	return s
}

// WithHistorySize sets the health history ring buffer capacity.
func (s *Server) WithHistorySize(n int) *Server {
	if n > 0 {
		s.historySize = n
	}
	return s
}

// WithChat configures the NLQ chat interface.
func (s *Server) WithChat(enabled bool, maxTurns, tokenBudget, maxSessions int, sessionTTL time.Duration) *Server {
	s.chatEnabled = enabled
	s.chatMaxTurns = maxTurns
	s.chatTokenBudget = tokenBudget
	s.chatMaxSessions = maxSessions
	s.chatSessionTTL = sessionTTL
	if enabled {
		s.chatSessions = make(map[string]*ChatSession)
	}
	return s
}

// WithChatAIConfig sets the provider config for the chat agent.
func (s *Server) WithChatAIConfig(providerName, apiKey, model, endpoint string) *Server {
	s.chatProviderName = providerName
	s.chatAIKey = apiKey
	s.chatAIModel = model
	s.chatEndpoint = endpoint
	return s
}

// buildHandler constructs the full HTTP handler tree (API routes + SPA serving +
// security headers). Extracted from Start() to enable black-box testing of the
// live handler without starting a real server.
func (s *Server) buildHandler() http.Handler {
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
	mux.HandleFunc("/api/chat", s.authMiddleware(s.rateLimitMiddleware(s.handleChat)))
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

	return s.securityHeaders(mux)
}

// Start starts the dashboard server
func (s *Server) Start(ctx context.Context) error {
	server := &http.Server{
		Addr:         s.addr,
		Handler:      s.buildHandler(),
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

	// Start chat session cleanup if chat is enabled
	if s.chatEnabled {
		go s.cleanupChatSessions(ctx)
	}

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
