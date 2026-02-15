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
	"strings"
	"time"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/datasource"
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
func (c *GitRepositoryChecker) Supports(ctx context.Context, ds datasource.DataSource) bool {
	var grList sourcev1.GitRepositoryList
	err := ds.List(ctx, &grList, client.Limit(1))
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
		if err := checkCtx.DataSource.List(ctx, &grList, client.InNamespace(ns)); err != nil {
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
			Type:      "Suspended",
			Severity:  checker.SeverityWarning,
			Resource:  resourceRef,
			Namespace: gr.Namespace,
			Message:   fmt.Sprintf("GitRepository %s is suspended", gr.Name),
			Suggestion: "This GitRepository is suspended and will not fetch new revisions. " +
				"Resume with: flux resume source git " + gr.Name + " -n " + gr.Namespace + ".",
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
		lastReconcile, ok := parseReconcileRequestTime(gr.Status.LastHandledReconcileAt)
		timestampSource := "status.lastHandledReconcileAt"
		if !ok && gr.Status.Artifact != nil && !gr.Status.Artifact.LastUpdateTime.IsZero() {
			lastReconcile = gr.Status.Artifact.LastUpdateTime.Time
			ok = true
			timestampSource = "status.artifact.lastUpdateTime"
		}
		if ok {
			staleDuration := time.Since(lastReconcile)
			if staleDuration > c.staleThreshold {
				issues = append(issues, checker.Issue{
					Type:      "StaleReconciliation",
					Severity:  checker.SeverityWarning,
					Resource:  resourceRef,
					Namespace: gr.Namespace,
					Message:   fmt.Sprintf("GitRepository last reconciled %s ago", staleDuration.Round(time.Minute)),
					Suggestion: "Check if the source controller is running: kubectl get deploy -n flux-system source-controller. " +
						"Force reconciliation: flux reconcile source git " + gr.Name + " -n " + gr.Namespace + ".",
					Metadata: map[string]string{
						"repository":      gr.Name,
						"lastReconcile":   lastReconcile.Format(time.RFC3339),
						"staleDuration":   staleDuration.String(),
						"timestampSource": timestampSource,
					},
				})
			}
		}
	}

	// Check artifact status - if Ready but no artifact, that's suspicious
	hasArtifact := gr.Status.Artifact != nil && gr.Status.Artifact.Revision != ""
	if !hasArtifact && readyCondition != nil && readyCondition.Status == metav1.ConditionTrue {
		issues = append(issues, checker.Issue{
			Type:      "NoArtifact",
			Severity:  checker.SeverityWarning,
			Resource:  resourceRef,
			Namespace: gr.Namespace,
			Message:   "GitRepository is ready but has no artifact",
			Suggestion: "GitRepository shows Ready but has no artifact. This may indicate a storage issue. " +
				"Check source-controller logs: kubectl logs -n flux-system deploy/source-controller --tail=30 | grep " + gr.Name + ". " +
				"Try forcing a reconciliation: flux reconcile source git " + gr.Name + " -n " + gr.Namespace + ".",
			Metadata: map[string]string{
				"repository": gr.Name,
			},
		})
	}

	// Check if using insecure options
	if gr.Spec.SecretRef == nil && isPrivateRepo(gr.Spec.URL) {
		issues = append(issues, checker.Issue{
			Type:      "NoAuthentication",
			Severity:  checker.SeverityWarning,
			Resource:  resourceRef,
			Namespace: gr.Namespace,
			Message:   "GitRepository appears to be private but has no secretRef configured",
			Suggestion: "This repository uses an SSH or private URL but has no secretRef for authentication. " +
				"Create a Secret with the credentials and reference it: " +
				"kubectl create secret generic git-creds --from-file=identity=<key> -n " + gr.Namespace + ". " +
				"Then add secretRef to the GitRepository spec.",
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
		"GitOperationFailed": "Git operation failed. Check the repository URL and network connectivity. " +
			"Verify the repo exists and is accessible: kubectl describe gitrepository <name> -n <namespace>. " +
			"If behind a proxy, check that the source-controller has proper proxy configuration.",
		"AuthenticationFailed": "Authentication to the Git repository failed. " +
			"Check the credentials Secret: kubectl get secret <secret-name> -n <namespace> -o yaml. " +
			"For SSH: verify the key is not expired. For HTTPS: verify the token has repo access. " +
			"Regenerate credentials if needed and update the Secret.",
		"CheckoutFailed": "Git checkout failed. The branch, tag, or commit reference may not exist. " +
			"Verify the ref: kubectl get gitrepository <name> -n <namespace> -o jsonpath='{.spec.ref}'. " +
			"Ensure the referenced branch/tag exists in the remote repository.",
		"CloneFailed": "Git clone failed. Check repository URL and access: " +
			"kubectl describe gitrepository <name> -n <namespace>. " +
			"Common causes: wrong URL, network issues, or authentication problems. " +
			"Test access: git ls-remote <url>.",
		"StorageOperationFailed": "Source controller storage error. This may indicate disk pressure or a bug. " +
			"Restart the controller: kubectl rollout restart deploy/source-controller -n flux-system. " +
			"Check disk usage on the controller pod: kubectl exec -n flux-system deploy/source-controller -- df -h.",
		"ReconciliationFailed": "Reconciliation failed. Check controller logs: " +
			"kubectl logs -n flux-system deploy/source-controller --tail=50 | grep <name>. " +
			"Force reconciliation: flux reconcile source git <name> -n <namespace>.",
	}

	if s, ok := suggestions[reason]; ok {
		return s
	}
	return "Check GitRepository status: kubectl describe gitrepository <name> -n <namespace>"
}

// isPrivateRepo attempts to detect if a URL might be a private repository
// This is a simple heuristic and not foolproof
func isPrivateRepo(url string) bool {
	return strings.HasPrefix(url, "git@") || strings.HasPrefix(url, "ssh://")
}
