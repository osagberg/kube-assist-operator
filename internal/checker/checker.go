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

// Package checker provides a pluggable architecture for health checks.
package checker

import (
	"context"
	"fmt"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/osagberg/kube-assist-operator/internal/ai"
	"github.com/osagberg/kube-assist-operator/internal/datasource"
)

var log = logf.Log.WithName("checker")

// Severity levels for issues
const (
	SeverityCritical = "Critical"
	SeverityWarning  = "Warning"
	SeverityInfo     = "Info"
)

// Issue represents a single detected problem
type Issue struct {
	// Type categorizes the issue (e.g., "CrashLoopBackOff", "OOMKilled")
	Type string `json:"type"`

	// Severity indicates how serious the issue is (Critical, Warning, Info)
	Severity string `json:"severity"`

	// Resource identifies the affected resource (e.g., "deployment/api-server")
	Resource string `json:"resource"`

	// Namespace where the resource lives
	Namespace string `json:"namespace"`

	// Message is a human-readable description of the issue
	Message string `json:"message"`

	// Suggestion provides actionable advice to fix the issue
	Suggestion string `json:"suggestion,omitempty"`

	// Metadata contains additional context-specific information
	Metadata map[string]string `json:"metadata,omitempty"`
}

// CheckResult contains results from a single checker
type CheckResult struct {
	// CheckerName identifies which checker produced this result
	CheckerName string `json:"checkerName"`

	// Healthy is the count of healthy resources checked
	Healthy int `json:"healthy"`

	// Issues is the list of problems found
	Issues []Issue `json:"issues"`

	// Error is set if the checker encountered an error (not serialized to JSON)
	Error error `json:"-"`
}

// CheckContext provides context for checker execution
type CheckContext struct {
	// DataSource provides read access to Kubernetes resources (or another backend)
	DataSource datasource.DataSource

	// Namespaces to check (empty means all accessible namespaces)
	Namespaces []string

	// Config contains checker-specific configuration
	Config map[string]any

	// AIProvider is an optional AI provider for enhanced analysis
	AIProvider ai.Provider

	// AIEnabled indicates if AI analysis should be performed
	AIEnabled bool

	// ClusterContext provides context for AI analysis
	ClusterContext ai.ClusterContext

	// CausalContext provides correlation data for enhanced AI root cause analysis.
	// Set after running the causal correlator on checker results.
	CausalContext *ai.CausalAnalysisContext
}

// Checker is the interface all health checkers must implement
type Checker interface {
	// Name returns the checker identifier (e.g., "workloads", "helmreleases")
	Name() string

	// Check performs the health check and returns results
	Check(ctx context.Context, checkCtx *CheckContext) (*CheckResult, error)

	// Supports returns true if this checker can run in the current environment
	// (e.g., HelmReleaseChecker returns false if Flux CRDs are not installed)
	Supports(ctx context.Context, ds datasource.DataSource) bool
}

// CountBySeverity returns a count of issues grouped by severity
func (r *CheckResult) CountBySeverity() map[string]int {
	counts := make(map[string]int)
	for _, issue := range r.Issues {
		counts[issue.Severity]++
	}
	return counts
}

// HasCritical returns true if any critical issues were found
func (r *CheckResult) HasCritical() bool {
	for _, issue := range r.Issues {
		if issue.Severity == SeverityCritical {
			return true
		}
	}
	return false
}

// TotalIssues returns the total number of issues found
func (r *CheckResult) TotalIssues() int {
	return len(r.Issues)
}

// EnhanceWithAI uses the AI provider to enhance issue suggestions
func (r *CheckResult) EnhanceWithAI(ctx context.Context, checkCtx *CheckContext) error {
	if !checkCtx.AIEnabled || checkCtx.AIProvider == nil || !checkCtx.AIProvider.Available() {
		return nil
	}

	if len(r.Issues) == 0 {
		return nil
	}

	// Build AI analysis request
	sanitizer := ai.NewSanitizer()
	issueContexts := make([]ai.IssueContext, len(r.Issues))

	for i, issue := range r.Issues {
		issueContexts[i] = ai.IssueContext{
			Type:             issue.Type,
			Severity:         issue.Severity,
			Resource:         issue.Resource,
			Namespace:        issue.Namespace,
			Message:          issue.Message,
			StaticSuggestion: issue.Suggestion,
		}
	}

	request := ai.AnalysisRequest{
		Issues:         issueContexts,
		ClusterContext: checkCtx.ClusterContext,
		CausalContext:  checkCtx.CausalContext,
	}

	// Sanitize before sending to AI
	sanitizedRequest := sanitizer.SanitizeAnalysisRequest(request)

	// Get AI analysis
	response, err := checkCtx.AIProvider.Analyze(ctx, sanitizedRequest)
	if err != nil {
		log.Error(err, "AI analysis failed", "provider", checkCtx.AIProvider.Name(), "issues", len(r.Issues))
		return nil
	}

	log.Info("AI analysis completed", "provider", checkCtx.AIProvider.Name(), "suggestions", len(response.EnhancedSuggestions), "tokens", response.TokensUsed)

	// Enhance issues with AI suggestions — try multiple key formats
	for i := range r.Issues {
		key := r.Issues[i].Namespace + "/" + r.Issues[i].Resource
		enhanced, ok := response.EnhancedSuggestions[key]
		if !ok {
			// Also try just the resource name
			enhanced, ok = response.EnhancedSuggestions[r.Issues[i].Resource]
		}
		if !ok {
			// Try type/resource
			enhanced, ok = response.EnhancedSuggestions[r.Issues[i].Type+"/"+r.Issues[i].Resource]
		}
		if ok {
			if enhanced.Suggestion != "" && enhanced.Confidence > 0.5 {
				// Append AI suggestion to existing suggestion
				if r.Issues[i].Suggestion != "" {
					r.Issues[i].Suggestion += "\n\nAI Analysis: " + enhanced.Suggestion
				} else {
					r.Issues[i].Suggestion = enhanced.Suggestion
				}

				// Add root cause if available
				if enhanced.RootCause != "" {
					if r.Issues[i].Metadata == nil {
						r.Issues[i].Metadata = make(map[string]string)
					}
					r.Issues[i].Metadata["aiRootCause"] = enhanced.RootCause
				}
			}
		}
	}

	return nil
}

// EnhanceAllWithAI collects issues from all results and makes a single batched AI call.
// It uses index-based keys ("issue_0", "issue_1", ...) so the AI response keys match exactly.
func EnhanceAllWithAI(ctx context.Context, results map[string]*CheckResult, checkCtx *CheckContext) (int, int, error) {
	if !checkCtx.AIEnabled || checkCtx.AIProvider == nil || !checkCtx.AIProvider.Available() {
		return 0, 0, nil
	}

	// Track which result and index each issue came from
	type issueRef struct {
		checkerName string
		issueIdx    int
	}

	var issueContexts []ai.IssueContext
	var refs []issueRef

	for checkerName, result := range results {
		if result.Error != nil {
			continue
		}
		for i, issue := range result.Issues {
			issueContexts = append(issueContexts, ai.IssueContext{
				Type:             issue.Type,
				Severity:         issue.Severity,
				Resource:         issue.Resource,
				Namespace:        issue.Namespace,
				Message:          issue.Message,
				StaticSuggestion: issue.Suggestion,
			})
			refs = append(refs, issueRef{checkerName: checkerName, issueIdx: i})
		}
	}

	if len(issueContexts) == 0 {
		return 0, 0, nil
	}

	// Sanitize before sending to AI
	request := ai.AnalysisRequest{
		Issues:         issueContexts,
		ClusterContext: checkCtx.ClusterContext,
		CausalContext:  checkCtx.CausalContext,
	}
	sanitizedRequest := ai.NewSanitizer().SanitizeAnalysisRequest(request)

	// Single batched AI call
	response, err := checkCtx.AIProvider.Analyze(ctx, sanitizedRequest)
	if err != nil {
		log.Error(err, "AI batch analysis failed", "provider", checkCtx.AIProvider.Name(), "issues", len(issueContexts))
		return 0, 0, err
	}

	// Distribute suggestions back to their original issues
	issuesEnhanced := 0
	for i, ref := range refs {
		key := fmt.Sprintf("issue_%d", i)
		enhanced, ok := response.EnhancedSuggestions[key]
		if !ok {
			continue
		}
		if enhanced.Suggestion != "" && enhanced.Confidence > 0.5 {
			issue := &results[ref.checkerName].Issues[ref.issueIdx]
			if issue.Suggestion != "" {
				issue.Suggestion += "\n\nAI Analysis: " + enhanced.Suggestion
			} else {
				issue.Suggestion = "AI Analysis: " + enhanced.Suggestion
			}
			if enhanced.RootCause != "" {
				if issue.Metadata == nil {
					issue.Metadata = make(map[string]string)
				}
				issue.Metadata["aiRootCause"] = enhanced.RootCause
			}
			issuesEnhanced++
		}
	}

	log.Info("AI batch analysis completed",
		"provider", checkCtx.AIProvider.Name(),
		"totalIssues", len(issueContexts),
		"enhanced", issuesEnhanced,
		"tokens", response.TokensUsed,
	)

	return issuesEnhanced, response.TokensUsed, nil
}
