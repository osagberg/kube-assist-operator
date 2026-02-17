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
	"time"

	"github.com/osagberg/kube-assist-operator/internal/ai"
	"github.com/osagberg/kube-assist-operator/internal/causal"
	"github.com/osagberg/kube-assist-operator/internal/history"
	"github.com/osagberg/kube-assist-operator/internal/prediction"
)

// Input length limits for AI settings fields.
const (
	maxAPIKeyLen        = 256
	maxModelLen         = 128
	maxSettingsBodySize = 1 << 20 // 1 MB
)

// Supported AI provider names.
const (
	providerNoop      = "noop"
	providerOpenAI    = "openai"
	providerAnthropic = "anthropic"
)

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
	if !decodeJSONBody(w, r, maxSettingsBodySize, &req) {
		return
	}

	// SECURITY-002: reject direct API key submission — only secretRef-based config is allowed
	if req.APIKey != "" {
		http.Error(w, "Direct apiKey submission is not allowed; use secretRef-based configuration", http.StatusBadRequest)
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
	if providerName != "" && providerName != providerNoop && providerName != providerOpenAI && providerName != providerAnthropic {
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
		// Reconfigure every POST to keep provider/model/api key in sync with staged config.
		if err := mgr.Reconfigure(stagedConfig.Provider, stagedConfig.APIKey, stagedConfig.Model, stagedConfig.ExplainModel); err != nil {
			log.Error(err, "Failed to reconfigure AI provider", "provider", stagedConfig.Provider)
			http.Error(w, "Failed to reconfigure AI provider", http.StatusInternalServerError)
			return
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
			cs.lastExplain = nil
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
			defer func() {
				if r := recover(); r != nil {
					log.Error(fmt.Errorf("panic: %v", r), "recovered panic in AI settings check goroutine")
				}
			}()
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
		cs.lastExplain = nil
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
		defer func() {
			if r := recover(); r != nil {
				log.Error(fmt.Errorf("panic: %v", r), "recovered panic in AI settings check goroutine")
			}
		}()
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

	// CONCURRENCY-011: snapshot all cluster state references under a single RLock
	clusterID := r.URL.Query().Get("clusterId")
	s.mu.RLock()
	cs := s.clusters[clusterID]
	var currentHash string
	var cached *ExplainCacheEntry
	var latest *HealthUpdate
	var causalCtx *causal.CausalContext
	var historyRef *history.RingBuffer
	if cs != nil {
		currentHash = cs.lastIssueHash
		cached = cs.lastExplain
		latest = cs.latest
		causalCtx = cs.latestCausal
		historyRef = cs.history
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

	// Get history for trend context (using snapshot from single lock above)
	historySnapshots := historyRef.Last(20)
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
			Narrative:      "Failed to generate explanation. Please try again later.",
			RiskLevel:      "unknown",
			TrendDirection: "unknown",
		}); err != nil {
			log.Error(err, "Failed to encode response", "handler", "handleExplain")
		}
		return
	}

	// Parse the response
	explainResp := ai.ParseExplainResponse(analysisResp.RawContent, analysisResp.TokensUsed)

	// CONCURRENCY-003: re-lookup cs under write lock before writing lastExplain
	s.mu.Lock()
	if freshCS, ok := s.clusters[clusterID]; ok {
		freshCS.lastExplain = &ExplainCacheEntry{
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
