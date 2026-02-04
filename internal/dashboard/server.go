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
	"net/http"
	"sync"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/osagberg/kube-assist-operator/internal/checker"
)

var log = logf.Log.WithName("dashboard")

// HealthUpdate represents a health check update sent via SSE
type HealthUpdate struct {
	Timestamp   time.Time              `json:"timestamp"`
	Namespaces  []string               `json:"namespaces"`
	Results     map[string]CheckResult `json:"results"`
	Summary     Summary                `json:"summary"`
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

// Server is the dashboard web server
type Server struct {
	client    client.Client
	registry  *checker.Registry
	addr      string
	mu        sync.RWMutex
	clients   map[chan HealthUpdate]bool
	latest    *HealthUpdate
	running   bool
	stopCh    chan struct{}
}

// NewServer creates a new dashboard server
func NewServer(cl client.Client, registry *checker.Registry, addr string) *Server {
	return &Server{
		client:   cl,
		registry: registry,
		addr:     addr,
		clients:  make(map[chan HealthUpdate]bool),
		stopCh:   make(chan struct{}),
	}
}

// Start starts the dashboard server
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/events", s.handleSSE)
	mux.HandleFunc("/api/check", s.handleTriggerCheck)

	// Dashboard UI
	mux.HandleFunc("/", s.handleDashboard)

	server := &http.Server{
		Addr:         s.addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	s.running = true

	// Start background health checker
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

// runHealthChecker periodically runs health checks
func (s *Server) runHealthChecker(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Run initial check
	s.runCheck(ctx)

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
	// Get all namespaces (simplified - in production, use scope resolver)
	namespaces := []string{"default", "flux-system"}

	checkCtx := &checker.CheckContext{
		Client:     s.client,
		Namespaces: namespaces,
	}

	results := s.registry.RunAll(ctx, checkCtx, nil)

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
					Severity:   string(issue.Severity),
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

	// Store latest and broadcast
	s.mu.Lock()
	s.latest = update
	for clientCh := range s.clients {
		select {
		case clientCh <- *update:
		default:
			// Client slow, skip
		}
	}
	s.mu.Unlock()
}

// handleHealth returns current health status
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	s.mu.RLock()
	latest := s.latest
	s.mu.RUnlock()

	if latest == nil {
		json.NewEncoder(w).Encode(map[string]string{"status": "initializing"})
		return
	}

	json.NewEncoder(w).Encode(latest)
}

// handleSSE handles Server-Sent Events connections
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

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
		data, _ := json.Marshal(s.latest)
		fmt.Fprintf(w, "data: %s\n\n", data)
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
			data, _ := json.Marshal(update)
			fmt.Fprintf(w, "data: %s\n\n", data)
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

	go s.runCheck(r.Context())

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "check triggered"})
}

// handleDashboard serves the dashboard HTML
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(dashboardHTML))
}
