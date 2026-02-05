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

	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/osagberg/kube-assist-operator/internal/checker"
)

const (
	// KustomizationCheckerName is the identifier for this checker
	KustomizationCheckerName = "kustomizations"
)

// KustomizationChecker analyzes Flux Kustomization resources
type KustomizationChecker struct {
	staleThreshold time.Duration
}

// NewKustomizationChecker creates a new Kustomization checker
func NewKustomizationChecker() *KustomizationChecker {
	return &KustomizationChecker{
		staleThreshold: DefaultStaleThreshold,
	}
}

// Name returns the checker identifier
func (c *KustomizationChecker) Name() string {
	return KustomizationCheckerName
}

// Supports returns true if Flux Kustomization CRD is installed
func (c *KustomizationChecker) Supports(ctx context.Context, cl client.Client) bool {
	var ksList kustomizev1.KustomizationList
	err := cl.List(ctx, &ksList, client.Limit(1))
	return !meta.IsNoMatchError(err)
}

// Check performs health checks on Kustomizations
func (c *KustomizationChecker) Check(ctx context.Context, checkCtx *checker.CheckContext) (*checker.CheckResult, error) {
	result := &checker.CheckResult{
		CheckerName: KustomizationCheckerName,
		Issues:      []checker.Issue{},
	}

	for _, ns := range checkCtx.Namespaces {
		var ksList kustomizev1.KustomizationList
		if err := checkCtx.Client.List(ctx, &ksList, client.InNamespace(ns)); err != nil {
			continue
		}

		for _, ks := range ksList.Items {
			issues := c.checkKustomization(&ks)
			if len(issues) == 0 {
				result.Healthy++
			} else {
				result.Issues = append(result.Issues, issues...)
			}
		}
	}

	return result, nil
}

// checkKustomization analyzes a single Kustomization
func (c *KustomizationChecker) checkKustomization(ks *kustomizev1.Kustomization) []checker.Issue {
	var issues []checker.Issue
	resourceRef := fmt.Sprintf("kustomization/%s", ks.Name)

	// Check if suspended
	if ks.Spec.Suspend {
		issues = append(issues, checker.Issue{
			Type:      "Suspended",
			Severity:  checker.SeverityWarning,
			Resource:  resourceRef,
			Namespace: ks.Namespace,
			Message:   fmt.Sprintf("Kustomization %s is suspended", ks.Name),
			Suggestion: "This Kustomization is suspended and will not be reconciled. " +
				"Resume with: flux resume kustomization " + ks.Name + " -n " + ks.Namespace + ". " +
				"Check who suspended it: kubectl get kustomization " + ks.Name + " -n " + ks.Namespace + " -o jsonpath='{.metadata.annotations}'.",
			Metadata: map[string]string{
				"kustomization": ks.Name,
			},
		})
	}

	// Check Ready condition
	readyCondition := findCondition(ks.Status.Conditions, "Ready")
	if readyCondition != nil {
		if readyCondition.Status != metav1.ConditionTrue {
			severity := checker.SeverityWarning
			issueType := readyCondition.Reason

			// Classify severity based on reason
			switch readyCondition.Reason {
			case "BuildFailed", "ValidationFailed", "HealthCheckFailed":
				severity = checker.SeverityCritical
			case "ArtifactFailed", "ReconciliationFailed":
				severity = checker.SeverityCritical
			case "DependencyNotReady":
				severity = checker.SeverityWarning
			}

			issues = append(issues, checker.Issue{
				Type:       issueType,
				Severity:   severity,
				Resource:   resourceRef,
				Namespace:  ks.Namespace,
				Message:    truncateMessage(readyCondition.Message, 200),
				Suggestion: getSuggestionForKustomizationReason(readyCondition.Reason),
				Metadata: map[string]string{
					"kustomization": ks.Name,
					"reason":        readyCondition.Reason,
				},
			})
		}
	}

	// Check for stale reconciliation
	if !ks.Spec.Suspend && readyCondition != nil && readyCondition.Status == metav1.ConditionTrue {
		if ks.Status.LastAttemptedRevision != "" {
			lastReconcile := readyCondition.LastTransitionTime.Time
			staleDuration := time.Since(lastReconcile)
			if staleDuration > c.staleThreshold {
				issues = append(issues, checker.Issue{
					Type:      "StaleReconciliation",
					Severity:  checker.SeverityWarning,
					Resource:  resourceRef,
					Namespace: ks.Namespace,
					Message:   fmt.Sprintf("Kustomization last reconciled %s ago", staleDuration.Round(time.Minute)),
					Suggestion: "Check if the Kustomize controller is running: kubectl get deploy -n flux-system kustomize-controller. " +
						"Force reconciliation: flux reconcile kustomization " + ks.Name + " -n " + ks.Namespace + ". " +
						"Check controller logs: kubectl logs -n flux-system deploy/kustomize-controller --tail=20.",
					Metadata: map[string]string{
						"kustomization": ks.Name,
						"lastReconcile": lastReconcile.Format(time.RFC3339),
						"staleDuration": staleDuration.String(),
					},
				})
			}
		}
	}

	// Check for pending changes with different revision
	if ks.Status.LastAttemptedRevision != "" && ks.Status.LastAppliedRevision != "" {
		if ks.Status.LastAttemptedRevision != ks.Status.LastAppliedRevision {
			issues = append(issues, checker.Issue{
				Type:      "PendingChanges",
				Severity:  checker.SeverityInfo,
				Resource:  resourceRef,
				Namespace: ks.Namespace,
				Message:   fmt.Sprintf("Kustomization has pending changes from %s to %s", ks.Status.LastAppliedRevision, ks.Status.LastAttemptedRevision),
				Suggestion: "Kustomization has unapplied changes. This can be normal during reconciliation. " +
					"Check status: flux get kustomization " + ks.Name + " -n " + ks.Namespace + ". " +
					"If stuck, force reconciliation: flux reconcile kustomization " + ks.Name + " -n " + ks.Namespace + ".",
				Metadata: map[string]string{
					"kustomization": ks.Name,
					"fromRevision":  ks.Status.LastAppliedRevision,
					"toRevision":    ks.Status.LastAttemptedRevision,
				},
			})
		}
	}

	// Check health check status
	healthyCondition := findCondition(ks.Status.Conditions, "Healthy")
	if healthyCondition != nil && healthyCondition.Status == metav1.ConditionFalse {
		issues = append(issues, checker.Issue{
			Type:      "UnhealthyResources",
			Severity:  checker.SeverityWarning,
			Resource:  resourceRef,
			Namespace: ks.Namespace,
			Message:   truncateMessage(healthyCondition.Message, 200),
			Suggestion: "Resources managed by this Kustomization are unhealthy. " +
				"Check which resources are failing: kubectl describe kustomization " + ks.Name + " -n " + ks.Namespace + ". " +
				"The health check message lists specific failing resources and their status.",
			Metadata: map[string]string{
				"kustomization": ks.Name,
				"healthReason":  healthyCondition.Reason,
			},
		})
	}

	return issues
}

// getSuggestionForKustomizationReason returns a suggestion based on the Kustomization reason
func getSuggestionForKustomizationReason(reason string) string {
	suggestions := map[string]string{
		"BuildFailed": "Kustomize build failed. Test locally: kustomize build <path>. " +
			"Check controller logs: kubectl logs -n flux-system deploy/kustomize-controller --tail=50 | grep <name>. " +
			"Common causes: missing bases, invalid patches, or YAML syntax errors.",
		"ValidationFailed": "Manifest validation failed against the Kubernetes API schema. " +
			"Validate locally: kubectl apply --dry-run=server -f <manifests>. " +
			"Common causes: deprecated API versions, invalid field names, or missing required fields.",
		"HealthCheckFailed": "Deployed resources failed health checks. Check which resources are unhealthy: " +
			"kubectl describe kustomization <name> -n <namespace>. " +
			"The health check message usually lists specific failing resources.",
		"ArtifactFailed": "Source artifact is unavailable. Check source status: " +
			"flux get sources all -n flux-system. " +
			"Verify the GitRepository is ready: kubectl describe gitrepository <name> -n flux-system.",
		"ReconciliationFailed": "Reconciliation failed. Check controller logs: " +
			"kubectl logs -n flux-system deploy/kustomize-controller --tail=50 | grep <name>. " +
			"Force reconciliation: flux reconcile kustomization <name> -n <namespace>.",
		"DependencyNotReady": "A dependency is not ready. Check the dependency chain: " +
			"flux get kustomizations -n <namespace>. " +
			"Resolve the upstream dependency first before this Kustomization can proceed.",
		"PruneFailed": "Resource pruning failed. Check for finalizers or deletion protection: " +
			"kubectl get <resource> -o jsonpath='{.metadata.finalizers}'. " +
			"Resources with Helm ownership or custom finalizers may block pruning.",
	}

	if s, ok := suggestions[reason]; ok {
		return s
	}
	return "Check Kustomization status: kubectl describe kustomization <name> -n <namespace>"
}
