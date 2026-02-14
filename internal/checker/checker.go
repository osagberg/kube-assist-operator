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
	"regexp"
	"sort"
	"strings"

	"k8s.io/client-go/kubernetes"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/osagberg/kube-assist-operator/internal/ai"
	"github.com/osagberg/kube-assist-operator/internal/datasource"
)

// validNamespace matches RFC 1123 label names: lowercase alphanumeric, may contain
// hyphens, must start and end with alphanumeric, max 63 characters.
var validNamespace = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// IsValidNamespace reports whether name conforms to RFC 1123 label format.
func IsValidNamespace(name string) bool {
	return validNamespace.MatchString(name)
}

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

	// MaxIssues caps the number of issues sent to AI (0 = use default)
	MaxIssues int

	// MinConfidence is the minimum AI confidence threshold (0 = use default 0.3)
	MinConfidence float64

	// LogContextConfig controls optional event/log enrichment for AI context.
	LogContextConfig *LogContextConfig

	// Clientset provides access to the Kubernetes API for pod log fetching.
	// Nil when running against a Console datasource or when log context is disabled.
	Clientset kubernetes.Interface
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

	// Enrich with events/logs if log context is enabled
	if checkCtx.LogContextConfig != nil && checkCtx.LogContextConfig.Enabled {
		builder := NewContextBuilder(checkCtx.DataSource, checkCtx.Clientset, *checkCtx.LogContextConfig)
		issueContexts = builder.Enrich(ctx, issueContexts)
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
		return fmt.Errorf("AI analysis failed for %s: %w", checkCtx.AIProvider.Name(), err)
	}

	log.Info("AI analysis completed", "provider", checkCtx.AIProvider.Name(), "suggestions", len(response.EnhancedSuggestions), "tokens", response.TokensUsed)

	// Enhance issues with AI suggestions — try index-based key first, then legacy formats
	for i := range r.Issues {
		// Primary: index-based key (matches batch prompt format)
		key := fmt.Sprintf("issue_%d", i)
		enhanced, ok := response.EnhancedSuggestions[key]
		if !ok {
			// Legacy: namespace/resource
			enhanced, ok = response.EnhancedSuggestions[r.Issues[i].Namespace+"/"+r.Issues[i].Resource]
		}
		if !ok {
			// Legacy: just the resource name
			enhanced, ok = response.EnhancedSuggestions[r.Issues[i].Resource]
		}
		if !ok {
			// Legacy: type/resource
			enhanced, ok = response.EnhancedSuggestions[r.Issues[i].Type+"/"+r.Issues[i].Resource]
		}
		if ok {
			minConf := checkCtx.MinConfidence
			if minConf <= 0 {
				minConf = defaultMinConfidence
			}
			if enhanced.Suggestion != "" && enhanced.Confidence > minConf {
				// Append AI suggestion to existing suggestion
				if r.Issues[i].Suggestion != "" {
					r.Issues[i].Suggestion += "\n\nAI Analysis: " + enhanced.Suggestion
				} else {
					r.Issues[i].Suggestion = "AI Analysis: " + enhanced.Suggestion
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
func EnhanceAllWithAI(ctx context.Context, results map[string]*CheckResult, checkCtx *CheckContext) (int, int, int, *ai.AnalysisResponse, error) {
	if !checkCtx.AIEnabled || checkCtx.AIProvider == nil || !checkCtx.AIProvider.Available() {
		return 0, 0, 0, nil, nil
	}

	// Track which result and index each issue came from
	type issueRef struct {
		checkerName string
		issueIdx    int
	}

	var issueContexts []ai.IssueContext
	var refs []issueRef
	infoFiltered := 0

	for checkerName, result := range results {
		if result.Error != nil {
			continue
		}
		for i, issue := range result.Issues {
			// Skip Info-severity issues from AI analysis
			if issue.Severity == SeverityInfo {
				infoFiltered++
				continue
			}
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

	if infoFiltered > 0 {
		log.Info("Filtered Info-severity issues from AI analysis", "filtered", infoFiltered)
		ai.RecordIssuesFiltered("severity_info", infoFiltered)
	}

	if len(issueContexts) == 0 {
		return 0, 0, 0, nil, nil
	}

	// Cap issues by severity to stay within token limits
	maxIssues := checkCtx.MaxIssues
	if maxIssues <= 0 {
		maxIssues = defaultMaxAIIssues
	}
	totalIssueCount := len(issueContexts)
	if len(issueContexts) > maxIssues {
		type indexed struct {
			ctx ai.IssueContext
			ref issueRef
		}
		pairs := make([]indexed, len(issueContexts))
		for i := range issueContexts {
			pairs[i] = indexed{issueContexts[i], refs[i]}
		}
		sort.SliceStable(pairs, func(i, j int) bool {
			return severityRank(pairs[i].ctx.Severity) < severityRank(pairs[j].ctx.Severity)
		})
		for i := range pairs[:maxIssues] {
			issueContexts[i] = pairs[i].ctx
			refs[i] = pairs[i].ref
		}
		issueContexts = issueContexts[:maxIssues]
		refs = refs[:maxIssues]
		log.Info("Capped AI issues", "total", totalIssueCount, "sent", maxIssues)
	}

	// Deduplication: group identical issues, send only representatives
	type dedupGroup struct {
		representativeIdx int        // index in issueContexts
		refs              []issueRef // all refs with same signature
	}

	dedupMap := make(map[string]*dedupGroup)
	var dedupOrder []string // preserve order
	for i, ic := range issueContexts {
		sig := normalizeIssueSignature(Issue{
			Type:     ic.Type,
			Severity: ic.Severity,
			Message:  ic.Message,
		})
		if g, ok := dedupMap[sig]; ok {
			g.refs = append(g.refs, refs[i])
		} else {
			dedupMap[sig] = &dedupGroup{
				representativeIdx: i,
				refs:              []issueRef{refs[i]},
			}
			dedupOrder = append(dedupOrder, sig)
		}
	}

	duplicateCount := len(issueContexts) - len(dedupOrder)
	if duplicateCount > 0 {
		log.Info("Deduplicated issues for AI analysis", "original", len(issueContexts), "unique", len(dedupOrder), "duplicates", duplicateCount)
		ai.RecordIssuesFiltered("duplicate", duplicateCount)
	}

	// Build deduplicated issue list
	dedupContexts := make([]ai.IssueContext, len(dedupOrder))
	for i, sig := range dedupOrder {
		dedupContexts[i] = issueContexts[dedupMap[sig].representativeIdx]
	}

	// Enrich with events/logs if log context is enabled (after dedup to avoid redundant fetches)
	if checkCtx.LogContextConfig != nil && checkCtx.LogContextConfig.Enabled {
		builder := NewContextBuilder(checkCtx.DataSource, checkCtx.Clientset, *checkCtx.LogContextConfig)
		dedupContexts = builder.Enrich(ctx, dedupContexts)
	}

	// Sanitize before sending to AI
	request := ai.AnalysisRequest{
		Issues:         dedupContexts,
		ClusterContext: checkCtx.ClusterContext,
		CausalContext:  checkCtx.CausalContext,
	}
	sanitizedRequest := ai.NewSanitizer().SanitizeAnalysisRequest(request)

	// Single batched AI call
	response, err := checkCtx.AIProvider.Analyze(ctx, sanitizedRequest)
	if err != nil {
		log.Error(err, "AI batch analysis failed", "provider", checkCtx.AIProvider.Name(), "issues", len(dedupContexts))
		return 0, 0, totalIssueCount, nil, err
	}

	// Fan-out: apply AI response to all issues in each dedup group
	issuesEnhanced := 0
	for i, sig := range dedupOrder {
		key := fmt.Sprintf("issue_%d", i)
		enhanced, ok := response.EnhancedSuggestions[key]
		if !ok {
			continue
		}
		minConf := checkCtx.MinConfidence
		if minConf <= 0 {
			minConf = defaultMinConfidence
		}
		if enhanced.Suggestion == "" || enhanced.Confidence <= minConf {
			continue
		}
		for _, ref := range dedupMap[sig].refs {
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
		"totalIssues", totalIssueCount,
		"unique", len(dedupContexts),
		"enhanced", issuesEnhanced,
		"tokens", response.TokensUsed,
	)

	return issuesEnhanced, response.TokensUsed, totalIssueCount, response, nil
}

// defaultMaxAIIssues is the maximum number of issues to send to AI in a single batch.
const defaultMaxAIIssues = 15

// defaultMinConfidence is the minimum confidence threshold for AI suggestions.
const defaultMinConfidence = 0.3

// severityRank returns a sort rank for severity (lower = higher priority).
func severityRank(s string) int {
	switch s {
	case SeverityCritical:
		return 0
	case SeverityWarning:
		return 1
	case SeverityInfo:
		return 2
	default:
		return 3
	}
}

// Pre-compiled regex for normalizeMessage (avoid recompiling on every call).
var reWhitespace = regexp.MustCompile(`\s+`)

// normalizeMessage strips pod hash suffixes and collapses whitespace for dedup comparison.
func normalizeMessage(msg string) string {
	// Strip pod hash suffixes like -7f8b4c5d9f-x2k4p or -abc12
	msg = PodHashSuffix.ReplaceAllString(msg, "-<pod>")
	// Collapse multiple spaces
	msg = reWhitespace.ReplaceAllString(msg, " ")
	return strings.TrimSpace(msg)
}

// normalizeIssueSignature creates a dedup key from an issue's core identity.
// Non-conforming namespaces are sanitized to prevent injection into AI prompts.
func normalizeIssueSignature(issue Issue) string {
	ns := issue.Namespace
	if ns != "" && !IsValidNamespace(ns) {
		ns = "<invalid>"
	}
	return issue.Type + "|" + issue.Severity + "|" + ns + "|" + normalizeMessage(issue.Message)
}
