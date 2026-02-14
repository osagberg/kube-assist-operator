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
	"encoding/json"
	"net/http"

	"github.com/osagberg/kube-assist-operator/internal/causal"
	"github.com/osagberg/kube-assist-operator/internal/history"
	"github.com/osagberg/kube-assist-operator/internal/prediction"
)

// deepCopyCausalContext returns a deep copy of CausalContext so it can be
// marshaled outside the lock without races on shared slices.
func deepCopyCausalContext(cc *causal.CausalContext) *causal.CausalContext {
	if cc == nil {
		return nil
	}
	cp := &causal.CausalContext{
		UncorrelatedCount: cc.UncorrelatedCount,
		TotalIssues:       cc.TotalIssues,
	}
	if len(cc.Groups) > 0 {
		cp.Groups = make([]causal.CausalGroup, len(cc.Groups))
		for i, g := range cc.Groups {
			cp.Groups[i] = g
			// Deep-copy slices within each group
			if len(g.Events) > 0 {
				events := make([]causal.TimelineEvent, len(g.Events))
				copy(events, g.Events)
				cp.Groups[i].Events = events
			}
			if len(g.AISteps) > 0 {
				steps := make([]string, len(g.AISteps))
				copy(steps, g.AISteps)
				cp.Groups[i].AISteps = steps
			}
		}
	}
	return cp
}

// handleCausalGroups returns the latest causal correlation analysis
func (s *Server) handleCausalGroups(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	clusterID := r.URL.Query().Get("clusterId")

	// CONCURRENCY-005: deep-copy CausalContext under lock, marshal outside
	s.mu.RLock()
	var cc *causal.CausalContext
	if cs, ok := s.clusters[clusterID]; ok {
		cc = deepCopyCausalContext(cs.latestCausal)
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

// handlePrediction returns health trend analysis using linear regression on history.
// GET /api/prediction/trend
func (s *Server) handlePrediction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	clusterID := r.URL.Query().Get("clusterId")

	// CONCURRENCY-004: hold RLock through cs.history.Last(50)
	s.mu.RLock()
	cs, ok := s.clusters[clusterID]
	var snapshots []history.HealthSnapshot
	if ok {
		snapshots = cs.history.Last(50)
	}
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
