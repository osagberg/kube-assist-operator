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
	"strconv"
	"time"

	"github.com/osagberg/kube-assist-operator/internal/datasource"
	"github.com/osagberg/kube-assist-operator/internal/history"
)

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
		states := make(map[string]*IssueState, len(cs.issueStates))
		for k, st := range cs.issueStates {
			if st.IsExpired() {
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

	// CONCURRENCY-010: disable write timeout for long-lived SSE connections
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	clusterID := r.URL.Query().Get("clusterId")
	clientCh := make(chan HealthUpdate, s.sseBufferSize)

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
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			s.mu.RUnlock()
			return
		}
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
				if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
					break
				}
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
			// CONCURRENCY-012: break on write error
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
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
		defer func() {
			if r := recover(); r != nil {
				log.Error(fmt.Errorf("panic: %v", r), "recovered panic in trigger check goroutine")
			}
		}()
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
		score := cs.latest.Summary.HealthScore
		summary.Clusters = append(summary.Clusters, FleetClusterEntry{
			ClusterID:                clusterID,
			HealthScore:              score,
			TotalIssues:              cs.latest.Summary.TotalIssues,
			CriticalCount:            cs.latest.Summary.CriticalCount,
			WarningCount:             cs.latest.Summary.WarningCount,
			InfoCount:                cs.latest.Summary.InfoCount,
			DeploymentReady:          cs.latest.Summary.DeploymentReady,
			DeploymentDesired:        cs.latest.Summary.DeploymentDesired,
			DeploymentReadinessScore: cs.latest.Summary.DeploymentReadinessScore,
			LastUpdated:              cs.latest.Timestamp.Format(time.RFC3339),
		})
	}
	s.mu.RUnlock()

	if err := json.NewEncoder(w).Encode(summary); err != nil {
		log.Error(err, "Failed to encode response", "handler", "handleFleetSummary")
	}
}

// handleCapabilities returns feature flags so the frontend can hide unsupported actions.
func (s *Server) handleCapabilities(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// CONCURRENCY-006: read chatEnabled/aiEnabled under lock
	s.mu.RLock()
	chatOn := s.chatEnabled && s.aiEnabled
	s.mu.RUnlock()
	resp := map[string]bool{
		"troubleshootCreate": s.k8sWriter != nil,
		"chat":               chatOn,
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Error(err, "Failed to encode capabilities response")
	}
}
