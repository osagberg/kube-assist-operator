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

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/osagberg/kube-assist-operator/internal/checker"
)

const (
	// HelmReleaseCheckerName is the identifier for this checker
	HelmReleaseCheckerName = "helmreleases"

	// Default stale threshold (1 hour)
	DefaultStaleThreshold = time.Hour
)

// HelmReleaseChecker analyzes Flux HelmRelease resources
type HelmReleaseChecker struct {
	staleThreshold time.Duration
}

// NewHelmReleaseChecker creates a new HelmRelease checker
func NewHelmReleaseChecker() *HelmReleaseChecker {
	return &HelmReleaseChecker{
		staleThreshold: DefaultStaleThreshold,
	}
}

// Name returns the checker identifier
func (c *HelmReleaseChecker) Name() string {
	return HelmReleaseCheckerName
}

// Supports returns true if Flux HelmRelease CRD is installed
func (c *HelmReleaseChecker) Supports(ctx context.Context, cl client.Client) bool {
	// Try to list HelmReleases - if it fails with no match error, CRD not installed
	var hrList helmv2.HelmReleaseList
	err := cl.List(ctx, &hrList, client.Limit(1))
	return !meta.IsNoMatchError(err)
}

// Check performs health checks on HelmReleases
func (c *HelmReleaseChecker) Check(ctx context.Context, checkCtx *checker.CheckContext) (*checker.CheckResult, error) {
	result := &checker.CheckResult{
		CheckerName: HelmReleaseCheckerName,
		Issues:      []checker.Issue{},
	}

	for _, ns := range checkCtx.Namespaces {
		var hrList helmv2.HelmReleaseList
		if err := checkCtx.Client.List(ctx, &hrList, client.InNamespace(ns)); err != nil {
			// Skip if can't list - might be permission issue
			continue
		}

		for _, hr := range hrList.Items {
			issues := c.checkHelmRelease(&hr)
			if len(issues) == 0 {
				result.Healthy++
			} else {
				result.Issues = append(result.Issues, issues...)
			}
		}
	}

	return result, nil
}

// checkHelmRelease analyzes a single HelmRelease
func (c *HelmReleaseChecker) checkHelmRelease(hr *helmv2.HelmRelease) []checker.Issue {
	var issues []checker.Issue
	resourceRef := fmt.Sprintf("helmrelease/%s", hr.Name)

	// Check if suspended
	if hr.Spec.Suspend {
		issues = append(issues, checker.Issue{
			Type:      "Suspended",
			Severity:  checker.SeverityWarning,
			Resource:  resourceRef,
			Namespace: hr.Namespace,
			Message:   fmt.Sprintf("HelmRelease %s is suspended", hr.Name),
			Suggestion: "This HelmRelease is suspended and will not be reconciled. " +
				"Resume with: flux resume helmrelease " + hr.Name + " -n " + hr.Namespace + ". " +
				"Check who suspended it: kubectl get helmrelease " + hr.Name + " -n " + hr.Namespace + " -o jsonpath='{.metadata.annotations}'.",
			Metadata: map[string]string{
				"release": hr.Name,
			},
		})
	}

	// Check Ready condition
	readyCondition := findCondition(hr.Status.Conditions, "Ready")
	if readyCondition != nil {
		if readyCondition.Status != metav1.ConditionTrue {
			severity := checker.SeverityWarning
			issueType := readyCondition.Reason

			// Classify severity based on reason
			switch readyCondition.Reason {
			case "UpgradeFailed", "InstallFailed", "UninstallFailed":
				severity = checker.SeverityCritical
			case "ArtifactFailed", "ChartPullFailed":
				severity = checker.SeverityCritical
			case "ReconciliationFailed":
				severity = checker.SeverityCritical
			}

			issues = append(issues, checker.Issue{
				Type:       issueType,
				Severity:   severity,
				Resource:   resourceRef,
				Namespace:  hr.Namespace,
				Message:    truncateMessage(readyCondition.Message, 200),
				Suggestion: getSuggestionForHelmReason(readyCondition.Reason),
				Metadata: map[string]string{
					"release": hr.Name,
					"reason":  readyCondition.Reason,
				},
			})
		}
	}

	// Check for stale reconciliation (only if not suspended and Ready)
	if !hr.Spec.Suspend && readyCondition != nil && readyCondition.Status == metav1.ConditionTrue {
		lastReconcile := readyCondition.LastTransitionTime.Time
		staleDuration := time.Since(lastReconcile)
		if staleDuration > c.staleThreshold {
			issues = append(issues, checker.Issue{
				Type:      "StaleReconciliation",
				Severity:  checker.SeverityWarning,
				Resource:  resourceRef,
				Namespace: hr.Namespace,
				Message:   fmt.Sprintf("HelmRelease last reconciled %s ago", staleDuration.Round(time.Minute)),
				Suggestion: "Check if the Helm controller is running: kubectl get deploy -n flux-system helm-controller. " +
					"Force reconciliation: flux reconcile helmrelease " + hr.Name + " -n " + hr.Namespace + ". " +
					"Check controller logs: kubectl logs -n flux-system deploy/helm-controller --tail=20.",
				Metadata: map[string]string{
					"release":       hr.Name,
					"lastReconcile": lastReconcile.Format(time.RFC3339),
					"staleDuration": staleDuration.String(),
				},
			})
		}
	}

	// Check history for deployment issues
	if len(hr.Status.History) > 0 {
		latestSnapshot := hr.Status.History.Latest()
		if latestSnapshot != nil {
			// Check if latest release failed
			if latestSnapshot.Status == "failed" {
				issues = append(issues, checker.Issue{
					Type:      "ReleaseFailed",
					Severity:  checker.SeverityCritical,
					Resource:  resourceRef,
					Namespace: hr.Namespace,
					Message:   fmt.Sprintf("HelmRelease %s version %d is in failed state", latestSnapshot.ChartName, latestSnapshot.Version),
					Suggestion: "Release is in failed state. Check release history: helm history " + hr.Name + " -n " + hr.Namespace + ". " +
						"View events: kubectl describe helmrelease " + hr.Name + " -n " + hr.Namespace + ". " +
						"Consider rolling back: flux reconcile helmrelease " + hr.Name + " -n " + hr.Namespace + " --force.",
					Metadata: map[string]string{
						"release":      hr.Name,
						"chartName":    latestSnapshot.ChartName,
						"chartVersion": latestSnapshot.ChartVersion,
						"version":      fmt.Sprintf("%d", latestSnapshot.Version),
					},
				})
			}
		}
	}

	return issues
}

// getSuggestionForHelmReason returns a suggestion based on the HelmRelease reason
func getSuggestionForHelmReason(reason string) string {
	suggestions := map[string]string{
		"UpgradeFailed": "Helm upgrade failed. Investigate with: " +
			"kubectl describe helmrelease <name> -n <namespace> and " +
			"kubectl logs -n flux-system deploy/helm-controller | grep <name>. " +
			"Common causes: incompatible values, removed/renamed chart fields, or resource conflicts. " +
			"Check release history: helm history <name> -n <namespace>.",
		"InstallFailed": "Helm install failed. Verify the chart exists and values are valid: " +
			"kubectl describe helmrelease <name> -n <namespace>. " +
			"Common causes: chart not found, invalid values, or pre-existing resources that conflict. " +
			"Check: kubectl get events -n <namespace> --field-selector reason=InstallFailed.",
		"UninstallFailed": "Helm uninstall blocked. Check for finalizers: " +
			"kubectl get helmrelease <name> -n <namespace> -o jsonpath='{.metadata.finalizers}'. " +
			"Resources with finalizers or PVCs with retain policy can block deletion.",
		"ArtifactFailed": "Chart artifact unavailable. Verify the source: " +
			"kubectl get helmrepository,gitrepository -n flux-system. " +
			"Check source readiness: flux get sources all -n flux-system.",
		"ChartPullFailed": "Chart pull failed. Check repository access: " +
			"kubectl describe helmrepository <repo> -n flux-system. " +
			"Verify credentials in the referenced Secret and network access to the registry.",
		"ReconciliationFailed": "Reconciliation failed. Check controller logs: " +
			"kubectl logs -n flux-system deploy/helm-controller --tail=50 | grep <name>. " +
			"Force reconciliation: flux reconcile helmrelease <name> -n <namespace>.",
		"DependencyNotReady": "A dependency is not ready. Check dependent HelmReleases: " +
			"kubectl get helmrelease -n <namespace>. " +
			"Resolve the upstream dependency first before this release can proceed.",
		"ValuesValidationFailed": "Values do not match the chart schema. Inspect the chart's values.schema.json: " +
			"helm show values <chart> and helm show readme <chart>. " +
			"Check which values are invalid in the HelmRelease events.",
	}

	if s, ok := suggestions[reason]; ok {
		return s
	}
	return "Check HelmRelease status: kubectl describe helmrelease <name> -n <namespace>"
}

// findCondition finds a condition by type in the conditions slice
func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

// truncateMessage truncates a message to a maximum length
func truncateMessage(msg string, maxLen int) string {
	if len(msg) <= maxLen {
		return msg
	}
	return msg[:maxLen-3] + "..."
}
