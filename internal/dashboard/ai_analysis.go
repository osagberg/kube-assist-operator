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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	assistv1alpha1 "github.com/osagberg/kube-assist-operator/api/v1alpha1"
	"github.com/osagberg/kube-assist-operator/internal/ai"
	"github.com/osagberg/kube-assist-operator/internal/causal"
	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/datasource"
	"github.com/osagberg/kube-assist-operator/internal/history"
	"github.com/osagberg/kube-assist-operator/internal/scope"
)

// PERF-006: health check duration histogram
var healthCheckDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
	Name:    "kubeassist_dashboard_health_check_duration_seconds",
	Help:    "Duration of dashboard health check polling cycles",
	Buckets: prometheus.DefBuckets,
})

func init() {
	metrics.Registry.MustRegister(healthCheckDuration)
}

// runHealthChecker periodically runs health checks.
// The first check is already performed synchronously in Start().
func (s *Server) runHealthChecker(ctx context.Context) {
	ticker := time.NewTicker(s.checkInterval)
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
// CONCURRENCY-013: brief write lock for state update, then RLock for broadcast.
func (s *Server) broadcastPhase(clusterID, phase string) {
	s.mu.Lock()
	cs, ok := s.clusters[clusterID]
	if !ok || cs.latest == nil {
		s.mu.Unlock()
		return
	}
	if cs.latest.AIStatus == nil {
		cs.latest.AIStatus = &AIStatus{}
	}
	cs.latest.AIStatus.CheckPhase = phase
	update := *cs.latest
	s.mu.Unlock()

	s.mu.RLock()
	for clientCh, subscribedID := range s.clients {
		if subscribedID != "" && subscribedID != clusterID {
			continue
		}
		select {
		case clientCh <- update:
		default:
		}
	}
	s.mu.RUnlock()
}

// runAIAnalysisForCluster handles synchronous AI analysis with caching for a specific
// cluster's state. Used as a fallback when the enrichment queue is not available (tests).
// The caller must increment cs.checkCounter before calling this method.
// All reads/writes to cs are guarded by s.mu.
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
		return nil
	}

	// AI-012: read model name under lock for cost estimation
	s.mu.RLock()
	hashChanged := issueHash != cs.lastAICacheHash
	cachedResult := cs.lastAIResult
	cachedEnhancements := cs.lastAIEnhancements
	cachedInsights := cs.lastCausalInsights
	aiModel := s.aiConfig.Model
	s.mu.RUnlock()

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

	// Synchronous AI call using runAIEnrichment helper
	aiStatus, enhancements, insights, aiErr := runAIEnrichment(
		ctx, results, causalCtx, checkCtx, provider, aiModel, s.aiAnalysisTimeout,
	)

	if aiErr != nil {
		return s.handleAIErrorForCluster(aiErr, results, causalCtx, enabled, provider, 0, cs)
	}

	if aiStatus != nil && aiStatus.IssuesEnhanced > 0 {
		// Commit AI results atomically under lock
		s.mu.Lock()
		cs.lastAICacheHash = issueHash
		cs.lastAIResult = aiStatus
		cs.lastAIEnhancements = enhancements
		if len(insights) > 0 {
			cs.lastCausalInsights = insights
		}
		s.mu.Unlock()
		return aiStatus
	}

	// AI returned 0 enhanced (likely truncated JSON) — handled by runAIEnrichment
	if aiStatus != nil {
		return aiStatus
	}
	return &AIStatus{
		Enabled:    enabled,
		Provider:   provider.Name(),
		CheckPhase: "done",
	}
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
	aiResp *ai.AnalysisResponse,
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
		log.Info("AI returned 0 enhanced, reusing last good result",
			"tokens", tokens, "apiTruncated", aiResp != nil && aiResp.Truncated, "parseFailed", aiResp != nil && aiResp.ParseFailed)
		return &AIStatus{
			Enabled:         enabled,
			Provider:        provider.Name(),
			IssuesEnhanced:  cachedResult.IssuesEnhanced,
			CacheHit:        true,
			TotalIssueCount: cachedResult.TotalIssueCount,
			CheckPhase:      "done",
		}
	}
	log.Info("AI returned 0 enhanced, no cached result to fall back on",
		"tokens", tokens, "apiTruncated", aiResp != nil && aiResp.Truncated, "parseFailed", aiResp != nil && aiResp.ParseFailed)
	return &AIStatus{
		Enabled:         enabled,
		Provider:        provider.Name(),
		TotalIssueCount: totalCount,
		LastError:       aiTruncationReason(aiResp),
		CheckPhase:      "done",
	}
}

func aiTruncationReason(resp *ai.AnalysisResponse) string {
	if resp != nil && resp.Truncated {
		return "AI response truncated by token limit — retrying next cycle"
	}
	if resp != nil && resp.ParseFailed {
		return "AI response could not be parsed — retrying next cycle"
	}
	return "AI returned no suggestions — retrying next cycle"
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
		if len(clusters) == 0 {
			return
		}
		workers := s.clusterWorkers
		if workers <= 0 {
			workers = 1
		}
		if workers > len(clusters) {
			workers = len(clusters)
		}
		// Run per-cluster checks concurrently to reduce end-to-end fleet latency.
		sem := make(chan struct{}, workers)
		var wg sync.WaitGroup
		for _, clusterID := range clusters {
			wg.Go(func() {
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					return
				}
				defer func() { <-sem }()
				clusterDS, err := cds.ForCluster(clusterID)
				if err != nil {
					log.Error(err, "Failed to get datasource for cluster", "cluster", clusterID)
					return
				}
				s.runCheckForCluster(ctx, clusterID, clusterDS)
			})
		}
		wg.Wait()
	} else {
		s.runCheckForCluster(ctx, "", s.client)
	}
}

// runCheckForCluster performs a health check for a single cluster and broadcasts results.
// AI enrichment is handled asynchronously via the enrichment queue.
func (s *Server) runCheckForCluster(ctx context.Context, clusterID string, ds datasource.DataSource) {
	checkStart := time.Now()
	defer func() {
		healthCheckDuration.Observe(time.Since(checkStart).Seconds())
	}()
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
		DataSource:       ds,
		Namespaces:       namespaces,
		AIEnabled:        false,
		MaxIssues:        s.maxIssuesPerBatch,
		LogContextConfig: s.logContextConfig,
		Clientset:        s.logContextClient,
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

	// Increment check counter and capture generation for staleness checks
	s.mu.Lock()
	cs := s.getOrCreateClusterState(clusterID)
	cs.checkCounter++
	generation := cs.checkCounter
	s.mu.Unlock()

	issueHash := computeIssueHash(results)

	// Snapshot cache state under lock
	s.mu.RLock()
	hashChanged := issueHash != cs.lastAICacheHash
	cachedResult := cs.lastAIResult
	cachedEnhancements := cs.lastAIEnhancements
	cachedInsights := cs.lastCausalInsights
	aiModel := s.aiConfig.Model
	s.mu.RUnlock()

	var aiStatus *AIStatus

	if enabled && provider != nil && provider.Available() && !hashChanged && cachedResult != nil {
		// AC-7: Cache hit → sync path (no queue needed)
		s.broadcastPhase(clusterID, "ai")
		reapplyAIEnhancements(results, cachedEnhancements)
		if len(cachedInsights) > 0 {
			applyCausalInsights(causalCtx, cachedInsights)
		}
		aiStatus = &AIStatus{
			Enabled:         true,
			Provider:        provider.Name(),
			IssuesEnhanced:  cachedResult.IssuesEnhanced,
			CacheHit:        true,
			TotalIssueCount: cachedResult.TotalIssueCount,
			CheckPhase:      "done",
		}
	} else if enabled && provider != nil && provider.Available() {
		// Cache miss → baseline with Pending, enqueue async
		s.broadcastPhase(clusterID, "ai")
		aiStatus = &AIStatus{
			Enabled:    true,
			Provider:   provider.Name(),
			Pending:    true,
			CheckPhase: "ai",
		}
	}

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

	deployReady, deployDesired, deployReadiness := computeDeploymentReadiness(checkCtx2, ds, namespaces)

	// Snapshot active issue states for summary exclusion
	s.mu.RLock()
	activeStates := make(map[string]*IssueState, len(cs.issueStates))
	for k, st := range cs.issueStates {
		if st.IsExpired() {
			continue // expired snooze
		}
		activeStates[k] = st
	}
	s.mu.RUnlock()

	summary := buildDashboardResults(update, results, activeStates)
	summary.HealthScore = computeOperationalHealthScore(summary.scoreChecked, summary.scoreCritical)
	summary.DeploymentReady = deployReady
	summary.DeploymentDesired = deployDesired
	summary.DeploymentReadinessScore = deployReadiness
	update.Summary = summary.Summary

	// Store latest and broadcast — lock-free broadcasting: write lock for state,
	// then release and use broadcastUpdate (which takes read lock for client iteration).
	s.mu.Lock()
	// Merge non-expired issue states into the update and clean up expired entries
	for k, st := range cs.issueStates {
		if st.IsExpired() {
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
	cs.currentIssueHash = issueHash
	providerName := ""
	if provider != nil {
		providerName = provider.Name()
	}
	cs.aiConfigVersion = configVersionHash(providerName, aiModel)
	updateCopy := cloneHealthUpdate(cs.latest)
	s.mu.Unlock()

	s.broadcastUpdate(clusterID, updateCopy)

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
		HealthScore: update.Summary.HealthScore,
	}
	for name, result := range update.Results {
		snapshot.ByChecker[name] = len(result.Issues)
	}
	cs.history.Add(snapshot)

	// Enqueue async AI enrichment if needed
	if aiStatus != nil && aiStatus.Pending && s.enrichQueue != nil {
		cfgVer := configVersionHash(providerName, aiModel)
		req := enrichmentRequest{
			clusterID:     clusterID,
			generation:    generation,
			issueHash:     issueHash,
			configVersion: cfgVer,
			results:       deepCopyResults(results),
			causalCtx:     deepCopyCausalContext(causalCtx),
			checkCtx:      minimalCheckContext(checkCtx, provider),
			provider:      provider,
			aiModel:       aiModel,
			enqueuedAt:    time.Now(),
		}
		if !s.enrichQueue.Enqueue(req) {
			// Queue full — set degraded status only if this generation is still current
			s.mu.Lock()
			if cs.checkCounter == generation && cs.latest != nil && cs.latest.AIStatus != nil {
				cs.latest.AIStatus.Pending = false
				cs.latest.AIStatus.LastError = "enrichment queue full"
				cs.latest.AIStatus.CheckPhase = "done"
			}
			if cs.latest != nil {
				queueFullUpdate := cloneHealthUpdate(cs.latest)
				s.mu.Unlock()
				s.broadcastUpdate(clusterID, queueFullUpdate)
			} else {
				s.mu.Unlock()
			}
		}
	}
}

// buildResult holds intermediate summary data including internal counters.
type buildResult struct {
	Summary
	scoreChecked  int
	scoreCritical int
}

// buildDashboardResults converts checker results into dashboard format, populating
// update.Results and returning an aggregated summary with scoring counters.
func buildDashboardResults(update *HealthUpdate, results map[string]*checker.CheckResult, activeStates map[string]*IssueState) buildResult {
	var br buildResult
	for name, result := range results {
		cr := CheckResult{
			Name:    name,
			Healthy: result.Healthy,
		}

		if result.Error != nil {
			cr.Error = result.Error.Error()
		} else {
			br.TotalHealthy += result.Healthy
			br.TotalIssues += len(result.Issues)
			checked, critical := computeCheckerOperationalCounts(result.Healthy, result.Issues, activeStates)
			br.scoreChecked += checked
			br.scoreCritical += critical

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

				issueKey := issue.Namespace + "/" + issue.Resource + "/" + issue.Type
				if _, muted := activeStates[issueKey]; muted {
					continue
				}

				switch issue.Severity {
				case checker.SeverityCritical:
					br.CriticalCount++
				case checker.SeverityWarning:
					br.WarningCount++
				case checker.SeverityInfo:
					br.InfoCount++
				}
			}
		}

		update.Results[name] = cr
	}
	return br
}

// computeCheckerOperationalCounts treats a resource as unhealthy only when it has
// at least one unmuted critical issue. Warning/Info-only resources remain healthy.
func computeCheckerOperationalCounts(healthy int, issues []checker.Issue, activeStates map[string]*IssueState) (int, int) {
	resourceSeverity := make(map[string]string)
	for _, issue := range issues {
		issueKey := issue.Namespace + "/" + issue.Resource + "/" + issue.Type
		if _, muted := activeStates[issueKey]; muted {
			continue
		}
		resourceKey := issue.Namespace + "/" + issue.Resource
		prev := resourceSeverity[resourceKey]
		if severityRank(issue.Severity) > severityRank(prev) {
			resourceSeverity[resourceKey] = issue.Severity
		}
	}

	checkedResources := healthy + len(resourceSeverity)
	criticalResources := 0
	for _, sev := range resourceSeverity {
		if sev == checker.SeverityCritical {
			criticalResources++
		}
	}

	return checkedResources, criticalResources
}

func severityRank(severity string) int {
	switch severity {
	case checker.SeverityCritical:
		return 3
	case checker.SeverityWarning:
		return 2
	case checker.SeverityInfo:
		return 1
	default:
		return 0
	}
}

func computeOperationalHealthScore(checkedResources, criticalResources int) float64 {
	if checkedResources <= 0 {
		return 100
	}
	if criticalResources < 0 {
		criticalResources = 0
	}
	if criticalResources > checkedResources {
		criticalResources = checkedResources
	}
	healthyResources := checkedResources - criticalResources
	return float64(healthyResources) / float64(checkedResources) * 100
}

func computeDeploymentReadiness(ctx context.Context, ds datasource.DataSource, namespaces []string) (int, int, float64) {
	totalReady := 0
	totalDesired := 0

	for _, ns := range namespaces {
		deployments := &appsv1.DeploymentList{}
		if err := ds.List(ctx, deployments, client.InNamespace(ns)); err != nil {
			log.Error(err, "Failed to list deployments for readiness score", "namespace", ns)
			continue
		}
		for _, deployment := range deployments.Items {
			desired := 1
			if deployment.Spec.Replicas != nil {
				desired = int(*deployment.Spec.Replicas)
			}
			desired = max(desired, 0)
			ready := int(deployment.Status.ReadyReplicas)
			ready = max(ready, 0)
			ready = min(ready, desired)
			totalDesired += desired
			totalReady += ready
		}
	}

	if totalDesired == 0 {
		return totalReady, totalDesired, 100
	}
	return totalReady, totalDesired, float64(totalReady) / float64(totalDesired) * 100
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

// estimateCost estimates the USD cost of an AI call based on provider, model, and tokens.
func estimateCost(provider, model string, tokens int) float64 {
	if tokens <= 0 {
		return 0
	}
	costPer1K := ai.CostPer1KTokens(provider, model)
	return float64(tokens) / 1000.0 * costPer1K
}

// runAIEnrichment performs the AI call and returns results without touching any server state.
// It is safe to call from any goroutine. Has ZERO access to s, cs, or s.mu.
func runAIEnrichment(
	ctx context.Context,
	results map[string]*checker.CheckResult,
	causalCtx *causal.CausalContext,
	checkCtx *checker.CheckContext,
	provider ai.Provider,
	aiModel string,
	timeout time.Duration,
) (aiStatus *AIStatus, enhancements map[string]map[string]aiEnhancement, insights []ai.CausalGroupInsight, err error) {
	aiCtx, aiCancel := context.WithTimeout(ctx, timeout)
	enhanced, tokens, totalCount, aiResp, aiErr := checker.EnhanceAllWithAI(aiCtx, results, checkCtx)
	aiCancel()

	if aiErr != nil {
		return nil, nil, nil, aiErr
	}

	if enhanced > 0 {
		status := &AIStatus{
			Enabled:          true,
			Provider:         provider.Name(),
			IssuesEnhanced:   enhanced,
			TokensUsed:       tokens,
			EstimatedCostUSD: estimateCost(provider.Name(), aiModel, tokens),
			IssuesCapped:     totalCount > enhanced && totalCount > 0,
			TotalIssueCount:  totalCount,
			CheckPhase:       "done",
		}
		enhancements = snapshotAIEnhancements(results)
		if aiResp != nil && len(aiResp.CausalInsights) > 0 {
			applyCausalInsights(causalCtx, aiResp.CausalInsights)
			insights = aiResp.CausalInsights
		}
		return status, enhancements, insights, nil
	}

	// AI returned 0 enhanced (likely truncated JSON)
	return &AIStatus{
		Enabled:         true,
		Provider:        provider.Name(),
		TotalIssueCount: totalCount,
		LastError:       aiTruncationReason(aiResp),
		CheckPhase:      "done",
	}, nil, nil, nil
}

// handleEnrichmentResult is called by the enrichment queue worker. It performs the
// AI call on the snapshot and patches cs.latest if the enrichment is still current.
// The ctx comes from Run (cancelable on shutdown).
func (s *Server) handleEnrichmentResult(ctx context.Context, req enrichmentRequest) {
	defer s.enrichQueue.markDone(req.clusterID)
	start := time.Now()

	s.mu.RLock()
	cs, ok := s.clusters[req.clusterID]
	s.mu.RUnlock()
	if !ok {
		enrichmentProcessed.WithLabelValues("stale").Inc()
		return
	}

	// Pre-flight staleness: generation + configVersion
	s.mu.RLock()
	stale := cs.checkCounter != req.generation ||
		cs.aiConfigVersion != req.configVersion
	s.mu.RUnlock()
	if stale {
		enrichmentProcessed.WithLabelValues("stale").Inc()
		return
	}

	// AI call — uses server ctx (cancelable on shutdown), NOT context.Background()
	aiStatus, enhancements, insights, err := runAIEnrichment(
		ctx, req.results, req.causalCtx, req.checkCtx,
		req.provider, req.aiModel, s.aiAnalysisTimeout,
	)
	enrichmentDuration.Observe(time.Since(start).Seconds())

	if err != nil {
		// Set degraded only if still current generation
		s.mu.Lock()
		if cs.checkCounter == req.generation && cs.latest != nil && cs.latest.AIStatus != nil {
			cs.latest.AIStatus.Pending = false
			cs.latest.AIStatus.LastError = err.Error()
			cs.latest.AIStatus.CheckPhase = "done"
			errUpdate := cloneHealthUpdate(cs.latest)
			s.mu.Unlock()
			s.broadcastUpdate(req.clusterID, errUpdate)
		} else {
			s.mu.Unlock()
		}
		enrichmentProcessed.WithLabelValues("error").Inc()
		return
	}

	// Post-flight staleness: generation + issueHash + configVersion
	s.mu.Lock()
	if cs.checkCounter != req.generation ||
		cs.currentIssueHash != req.issueHash ||
		cs.aiConfigVersion != req.configVersion {
		s.mu.Unlock()
		enrichmentProcessed.WithLabelValues("stale").Inc()
		return
	}
	if cs.latest == nil {
		s.mu.Unlock()
		enrichmentProcessed.WithLabelValues("stale").Inc()
		return
	}

	// Commit AI cache state
	cs.lastAICacheHash = req.issueHash
	cs.lastAIResult = aiStatus
	cs.lastAIEnhancements = enhancements
	if len(insights) > 0 {
		cs.lastCausalInsights = insights
		if cs.latestCausal != nil {
			updatedCausal := deepCopyCausalContext(cs.latestCausal)
			applyCausalInsights(updatedCausal, insights)
			cs.latestCausal = updatedCausal
		}
	}

	// Deep-clone cs.latest, patch the clone, then swap atomically
	enriched := cloneHealthUpdate(cs.latest)
	enriched.AIStatus = aiStatus
	patchDashboardResults(enriched.Results, req.results)
	cs.latest = &enriched
	s.mu.Unlock()

	s.broadcastUpdate(req.clusterID, enriched)
	enrichmentProcessed.WithLabelValues("success").Inc()
}

// broadcastUpdate sends a HealthUpdate to all subscribed SSE clients for a cluster.
// Uses read lock for client iteration — channel sends NEVER under write lock.
func (s *Server) broadcastUpdate(clusterID string, update HealthUpdate) {
	s.mu.RLock()
	for clientCh, subscribedID := range s.clients {
		if subscribedID != "" && subscribedID != clusterID {
			continue
		}
		select {
		case clientCh <- update:
		default:
		}
	}
	s.mu.RUnlock()
}

// patchDashboardResults walks enriched checker results (from AI worker) and patches
// the corresponding dashboard CheckResult.Issues in the health update.
func patchDashboardResults(dashResults map[string]CheckResult, enrichedResults map[string]*checker.CheckResult) {
	for name, enriched := range enrichedResults {
		dr, ok := dashResults[name]
		if !ok || enriched.Error != nil {
			continue
		}
		for i := range dr.Issues {
			if i >= len(enriched.Issues) {
				break
			}
			src := enriched.Issues[i]
			if strings.Contains(src.Suggestion, "AI Analysis:") {
				suggestion := src.Suggestion
				if idx := strings.Index(suggestion, "AI Analysis: "); idx >= 0 {
					suggestion = suggestion[idx+len("AI Analysis: "):]
				}
				dr.Issues[i].Suggestion = suggestion
				dr.Issues[i].AIEnhanced = true
				if rc, ok := src.Metadata["aiRootCause"]; ok {
					dr.Issues[i].RootCause = rc
				}
			}
		}
		dashResults[name] = dr // write back (map value is a struct, not pointer)
	}
}
