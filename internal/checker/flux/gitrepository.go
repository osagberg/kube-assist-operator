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

package flux

import (
	"context"
	"fmt"
	"time"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/osagberg/kube-assist-operator/internal/checker"
)

const (
	// GitRepositoryCheckerName is the identifier for this checker
	GitRepositoryCheckerName = "gitrepositories"
)

// GitRepositoryChecker analyzes Flux GitRepository resources
type GitRepositoryChecker struct {
	staleThreshold time.Duration
}

// NewGitRepositoryChecker creates a new GitRepository checker
func NewGitRepositoryChecker() *GitRepositoryChecker {
	return &GitRepositoryChecker{
		staleThreshold: DefaultStaleThreshold,
	}
}

// Name returns the checker identifier
func (c *GitRepositoryChecker) Name() string {
	return GitRepositoryCheckerName
}

// Supports returns true if Flux GitRepository CRD is installed
func (c *GitRepositoryChecker) Supports(ctx context.Context, cl client.Client) bool {
	var grList sourcev1.GitRepositoryList
	err := cl.List(ctx, &grList, client.Limit(1))
	return !meta.IsNoMatchError(err)
}

// Check performs health checks on GitRepositories
func (c *GitRepositoryChecker) Check(ctx context.Context, checkCtx *checker.CheckContext) (*checker.CheckResult, error) {
	result := &checker.CheckResult{
		CheckerName: GitRepositoryCheckerName,
		Issues:      []checker.Issue{},
	}

	for _, ns := range checkCtx.Namespaces {
		var grList sourcev1.GitRepositoryList
		if err := checkCtx.Client.List(ctx, &grList, client.InNamespace(ns)); err != nil {
			continue
		}

		for _, gr := range grList.Items {
			issues := c.checkGitRepository(&gr)
			if len(issues) == 0 {
				result.Healthy++
			} else {
				result.Issues = append(result.Issues, issues...)
			}
		}
	}

	return result, nil
}

// checkGitRepository analyzes a single GitRepository
func (c *GitRepositoryChecker) checkGitRepository(gr *sourcev1.GitRepository) []checker.Issue {
	var issues []checker.Issue
	resourceRef := fmt.Sprintf("gitrepository/%s", gr.Name)

	// Check if suspended
	if gr.Spec.Suspend {
		issues = append(issues, checker.Issue{
			Type:       "Suspended",
			Severity:   checker.SeverityWarning,
			Resource:   resourceRef,
			Namespace:  gr.Namespace,
			Message:    fmt.Sprintf("GitRepository %s is suspended", gr.Name),
			Suggestion: "Review why this GitRepository was suspended and resume if appropriate",
			Metadata: map[string]string{
				"repository": gr.Name,
				"url":        gr.Spec.URL,
			},
		})
	}

	// Check Ready condition
	readyCondition := findCondition(gr.Status.Conditions, "Ready")
	if readyCondition != nil {
		if readyCondition.Status != metav1.ConditionTrue {
			severity := checker.SeverityWarning
			issueType := readyCondition.Reason

			// Classify severity based on reason
			switch readyCondition.Reason {
			case "GitOperationFailed", "AuthenticationFailed":
				severity = checker.SeverityCritical
			case "CheckoutFailed", "CloneFailed":
				severity = checker.SeverityCritical
			case "StorageOperationFailed":
				severity = checker.SeverityCritical
			}

			issues = append(issues, checker.Issue{
				Type:       issueType,
				Severity:   severity,
				Resource:   resourceRef,
				Namespace:  gr.Namespace,
				Message:    truncateMessage(readyCondition.Message, 200),
				Suggestion: getSuggestionForGitRepoReason(readyCondition.Reason),
				Metadata: map[string]string{
					"repository": gr.Name,
					"url":        gr.Spec.URL,
					"reason":     readyCondition.Reason,
				},
			})
		}
	}

	// Check for stale reconciliation
	if !gr.Spec.Suspend && readyCondition != nil && readyCondition.Status == metav1.ConditionTrue {
		lastReconcile := readyCondition.LastTransitionTime.Time
		staleDuration := time.Since(lastReconcile)
		if staleDuration > c.staleThreshold {
			issues = append(issues, checker.Issue{
				Type:       "StaleReconciliation",
				Severity:   checker.SeverityWarning,
				Resource:   resourceRef,
				Namespace:  gr.Namespace,
				Message:    fmt.Sprintf("GitRepository last reconciled %s ago", staleDuration.Round(time.Minute)),
				Suggestion: "Check if source-controller is running and healthy",
				Metadata: map[string]string{
					"repository":     gr.Name,
					"lastReconcile":  lastReconcile.Format(time.RFC3339),
					"staleDuration":  staleDuration.String(),
				},
			})
		}
	}

	// Check artifact status - if Ready but no artifact, that's suspicious
	hasArtifact := gr.Status.Artifact != nil && gr.Status.Artifact.Revision != ""
	if !hasArtifact && readyCondition != nil && readyCondition.Status == metav1.ConditionTrue {
		issues = append(issues, checker.Issue{
			Type:       "NoArtifact",
			Severity:   checker.SeverityWarning,
			Resource:   resourceRef,
			Namespace:  gr.Namespace,
			Message:    "GitRepository is ready but has no artifact",
			Suggestion: "Check source-controller logs for issues with artifact storage",
			Metadata: map[string]string{
				"repository": gr.Name,
			},
		})
	}

	// Check if using insecure options
	if gr.Spec.SecretRef == nil && isPrivateRepo(gr.Spec.URL) {
		issues = append(issues, checker.Issue{
			Type:       "NoAuthentication",
			Severity:   checker.SeverityWarning,
			Resource:   resourceRef,
			Namespace:  gr.Namespace,
			Message:    "GitRepository appears to be private but has no secretRef configured",
			Suggestion: "Add authentication credentials via secretRef if this is a private repository",
			Metadata: map[string]string{
				"repository": gr.Name,
				"url":        gr.Spec.URL,
			},
		})
	}

	return issues
}

// getSuggestionForGitRepoReason returns a suggestion based on the GitRepository reason
func getSuggestionForGitRepoReason(reason string) string {
	suggestions := map[string]string{
		"GitOperationFailed":     "Check repository URL and network connectivity. Verify the repository exists.",
		"AuthenticationFailed":   "Verify credentials in the referenced Secret. Check SSH key or token validity.",
		"CheckoutFailed":         "Verify the branch/tag/commit reference exists in the repository.",
		"CloneFailed":            "Check repository URL, network access, and authentication credentials.",
		"StorageOperationFailed": "Check source-controller storage. May need to restart the controller.",
		"ReconciliationFailed":   "Check GitRepository status and source-controller logs.",
	}

	if s, ok := suggestions[reason]; ok {
		return s
	}
	return "Check GitRepository status and events for more details"
}

// isPrivateRepo attempts to detect if a URL might be a private repository
// This is a simple heuristic and not foolproof
func isPrivateRepo(url string) bool {
	// SSH URLs are typically private
	if len(url) > 4 && url[:4] == "git@" {
		return true
	}
	// URLs with gitlab.com or github.com using SSH pattern
	if len(url) > 6 && url[:6] == "ssh://" {
		return true
	}
	return false
}
