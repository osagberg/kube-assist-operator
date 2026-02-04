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

package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	assistv1alpha1 "github.com/osagberg/kube-assist-operator/api/v1alpha1"
	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/checker/workload"
	"github.com/osagberg/kube-assist-operator/internal/scope"
)

// TeamHealthRequestReconciler reconciles a TeamHealthRequest object
type TeamHealthRequestReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Registry *checker.Registry
}

// +kubebuilder:rbac:groups=assist.cluster.local,resources=teamhealthrequests,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=assist.cluster.local,resources=teamhealthrequests/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=assist.cluster.local,resources=teamhealthrequests/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments;statefulsets;daemonsets;replicasets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods;events,verbs=get;list;watch
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases,verbs=get;list;watch
// +kubebuilder:rbac:groups=kustomize.toolkit.fluxcd.io,resources=kustomizations,verbs=get;list;watch
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=gitrepositories,verbs=get;list;watch

func (r *TeamHealthRequestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Record reconcile duration
	startTime := time.Now()
	defer func() {
		reconcileDuration.With(prometheus.Labels{
			"name":      req.Name,
			"namespace": req.Namespace,
		}).Observe(time.Since(startTime).Seconds())
	}()

	// Fetch the TeamHealthRequest
	healthReq := &assistv1alpha1.TeamHealthRequest{}
	if err := r.Get(ctx, req.NamespacedName, healthReq); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Save original for patch later
	original := healthReq.DeepCopy()

	// Skip if already completed or failed
	if healthReq.Status.Phase == assistv1alpha1.TeamHealthPhaseCompleted ||
		healthReq.Status.Phase == assistv1alpha1.TeamHealthPhaseFailed {
		return ctrl.Result{}, nil
	}

	// Initialize status
	healthReq.Status.Phase = assistv1alpha1.TeamHealthPhaseRunning
	if healthReq.Status.Results == nil {
		healthReq.Status.Results = make(map[string]assistv1alpha1.CheckerResult)
	}

	log.Info("Processing TeamHealthRequest", "name", healthReq.Name)

	// Resolve namespaces
	resolver := scope.NewResolver(r.Client, healthReq.Namespace)
	namespaces, err := resolver.ResolveNamespaces(ctx, healthReq.Spec.Scope)
	if err != nil {
		return r.setFailed(ctx, original, healthReq, fmt.Sprintf("Failed to resolve namespaces: %v", err))
	}

	if len(namespaces) == 0 {
		return r.setFailed(ctx, original, healthReq, "No namespaces found matching scope")
	}

	healthReq.Status.NamespacesChecked = namespaces
	r.setCondition(healthReq, assistv1alpha1.TeamHealthConditionNamespacesResolved, metav1.ConditionTrue,
		"Resolved", fmt.Sprintf("Found %d namespace(s)", len(namespaces)))

	// Determine which checkers to run
	checkerNames := r.getCheckerNames(healthReq.Spec.Checks)

	// Build check context
	checkCtx := &checker.CheckContext{
		Client:     r.Client,
		Namespaces: namespaces,
		Config:     r.buildCheckerConfig(healthReq.Spec.Config),
	}

	// Run checkers
	results := r.Registry.RunAll(ctx, checkCtx, checkerNames)

	// Convert results to API format
	totalHealthy := 0
	totalIssues := 0
	criticalCount := 0
	warningCount := 0

	for name, result := range results {
		apiResult := assistv1alpha1.CheckerResult{
			Healthy: int32(result.Healthy),
		}

		if result.Error != nil {
			apiResult.Error = result.Error.Error()
		} else {
			apiResult.Issues = checker.ToAPIIssues(result.Issues)
			totalHealthy += result.Healthy
			totalIssues += len(result.Issues)

			for _, issue := range result.Issues {
				switch issue.Severity {
				case checker.SeverityCritical:
					criticalCount++
				case checker.SeverityWarning:
					warningCount++
				}
			}
		}

		healthReq.Status.Results[name] = apiResult
	}

	// Generate summary
	healthReq.Status.Summary = r.generateSummary(totalHealthy, totalIssues, criticalCount, warningCount)

	// Update completion status
	now := metav1.Now()
	healthReq.Status.LastCheckTime = &now
	healthReq.Status.Phase = assistv1alpha1.TeamHealthPhaseCompleted

	r.setCondition(healthReq, assistv1alpha1.TeamHealthConditionCheckersCompleted, metav1.ConditionTrue,
		"Completed", fmt.Sprintf("Ran %d checker(s)", len(results)))
	r.setCondition(healthReq, assistv1alpha1.TeamHealthConditionComplete, metav1.ConditionTrue,
		"Complete", "Health check completed")

	// Use patch for status update to avoid conflicts
	patch := client.MergeFrom(original)
	if err := r.Status().Patch(ctx, healthReq, patch); err != nil {
		log.Error(err, "Failed to patch status")
		return ctrl.Result{}, err
	}

	log.Info("TeamHealthRequest completed",
		"namespaces", len(namespaces),
		"healthy", totalHealthy,
		"issues", totalIssues)

	return ctrl.Result{}, nil
}

// getCheckerNames returns the list of checkers to run
func (r *TeamHealthRequestReconciler) getCheckerNames(checks []assistv1alpha1.CheckerName) []string {
	if len(checks) == 0 {
		// Default to all registered checkers
		return r.Registry.List()
	}

	names := make([]string, 0, len(checks))
	for _, check := range checks {
		names = append(names, string(check))
	}
	return names
}

// buildCheckerConfig converts API config to checker config map
func (r *TeamHealthRequestReconciler) buildCheckerConfig(cfg assistv1alpha1.CheckerConfig) map[string]interface{} {
	config := make(map[string]interface{})

	if cfg.Workloads != nil {
		if cfg.Workloads.RestartThreshold > 0 {
			config[workload.ConfigRestartThreshold] = int(cfg.Workloads.RestartThreshold)
		}
	}

	// Add other checker configs as they are implemented
	// if cfg.Secrets != nil { ... }
	// if cfg.Quotas != nil { ... }

	return config
}

// generateSummary creates a human-readable summary
func (r *TeamHealthRequestReconciler) generateSummary(healthy, issues, critical, warning int) string {
	if issues == 0 {
		return fmt.Sprintf("All %d resource(s) healthy - no issues found", healthy)
	}

	if critical > 0 {
		return fmt.Sprintf("%d critical, %d warning issue(s) found across %d resource(s)", critical, warning, healthy+issues)
	}

	return fmt.Sprintf("%d warning issue(s) found across %d resource(s)", warning, healthy+issues)
}

// setFailed updates the status to Failed phase
func (r *TeamHealthRequestReconciler) setFailed(ctx context.Context, original, hr *assistv1alpha1.TeamHealthRequest, message string) (ctrl.Result, error) {
	hr.Status.Phase = assistv1alpha1.TeamHealthPhaseFailed
	hr.Status.Summary = message
	now := metav1.Now()
	hr.Status.LastCheckTime = &now

	patch := client.MergeFrom(original)
	if err := r.Status().Patch(ctx, hr, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// setCondition updates a condition on the TeamHealthRequest
func (r *TeamHealthRequestReconciler) setCondition(hr *assistv1alpha1.TeamHealthRequest, condType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&hr.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		ObservedGeneration: hr.Generation,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *TeamHealthRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&assistv1alpha1.TeamHealthRequest{}).
		Named("teamhealthrequest").
		Complete(r)
}
