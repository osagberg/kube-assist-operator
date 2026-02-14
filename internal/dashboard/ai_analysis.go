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

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	assistv1alpha1 "github.com/osagberg/kube-assist-operator/api/v1alpha1"
	"github.com/osagberg/kube-assist-operator/internal/ai"
	"github.com/osagberg/kube-assist-operator/internal/causal"
	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/datasource"
	"github.com/osagberg/kube-assist-operator/internal/history"
	"github.com/osagberg/kube-assist-operator/internal/scope"
)

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
	aiCtx, aiCancel := context.WithTimeout(ctx, s.aiAnalysisTimeout)
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
	return s.handleAITruncatedForCluster(results, causalCtx, enabled, provider, totalCount, tokens, cs, aiResp)
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
				s.runCheckForCluster(ctx, clusterID, cds.ForCluster(clusterID))
			})
		}
		wg.Wait()
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

	var summary Summary
	scoreCheckedResources := 0
	scoreCriticalResources := 0
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
			checkedResources, criticalResources := computeCheckerOperationalCounts(result.Healthy, result.Issues, activeStates)
			scoreCheckedResources += checkedResources
			scoreCriticalResources += criticalResources

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
	summary.HealthScore = computeOperationalHealthScore(scoreCheckedResources, scoreCriticalResources)
	summary.DeploymentReady = deployReady
	summary.DeploymentDesired = deployDesired
	summary.DeploymentReadinessScore = deployReadiness
	update.Summary = summary

	// Store latest and broadcast
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
		HealthScore: update.Summary.HealthScore,
	}
	for name, result := range update.Results {
		snapshot.ByChecker[name] = len(result.Issues)
	}
	cs.history.Add(snapshot)
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

// estimateCost estimates the USD cost of an AI call based on provider and tokens.
func estimateCost(provider string, tokens int) float64 {
	if tokens <= 0 {
		return 0
	}
	costPer1K := ai.CostPer1KTokens(provider, "")
	return float64(tokens) / 1000.0 * costPer1K
}
