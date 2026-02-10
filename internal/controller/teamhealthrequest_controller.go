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
	"math"
	"net/url"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	assistv1alpha1 "github.com/osagberg/kube-assist-operator/api/v1alpha1"
	"github.com/osagberg/kube-assist-operator/internal/ai"
	"github.com/osagberg/kube-assist-operator/internal/causal"
	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/checker/workload"
	"github.com/osagberg/kube-assist-operator/internal/datasource"
	"github.com/osagberg/kube-assist-operator/internal/notifier"
	"github.com/osagberg/kube-assist-operator/internal/scope"
)

const (
	// DefaultCheckerTimeout is the default timeout for running all checkers
	DefaultCheckerTimeout = 2 * time.Minute

	// MaxNamespaces limits the number of namespaces that can be checked at once
	MaxNamespaces = 50
)

// TeamHealthRequestReconciler reconciles a TeamHealthRequest object
type TeamHealthRequestReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	Registry         *checker.Registry
	AIProvider       ai.Provider
	DataSource       datasource.DataSource
	Correlator       *causal.Correlator
	NotifierRegistry *notifier.Registry
	NotifySem        chan struct{}
	Recorder         events.EventRecorder
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
			"namespace": req.Namespace,
		}).Observe(time.Since(startTime).Seconds())
	}()

	// Fetch the TeamHealthRequest
	healthReq := &assistv1alpha1.TeamHealthRequest{}
	if err := r.Get(ctx, req.NamespacedName, healthReq); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to fetch TeamHealthRequest: %w", err)
	}

	// Save original for patch later
	original := healthReq.DeepCopy()

	// Handle TTL cleanup for completed/failed requests
	if healthReq.Status.Phase == assistv1alpha1.TeamHealthPhaseCompleted ||
		healthReq.Status.Phase == assistv1alpha1.TeamHealthPhaseFailed {
		if healthReq.Spec.TTLSecondsAfterFinished != nil && healthReq.Status.CompletedAt != nil {
			ttl := time.Duration(*healthReq.Spec.TTLSecondsAfterFinished) * time.Second
			deadline := healthReq.Status.CompletedAt.Add(ttl)
			remaining := time.Until(deadline)
			if remaining <= 0 {
				log.Info("TTL expired, deleting TeamHealthRequest", "name", healthReq.Name)
				return ctrl.Result{}, r.Delete(ctx, healthReq)
			}
			return ctrl.Result{RequeueAfter: remaining}, nil
		}
		return ctrl.Result{}, nil
	}

	// Initialize status
	healthReq.Status.Phase = assistv1alpha1.TeamHealthPhaseRunning
	if healthReq.Status.Results == nil {
		healthReq.Status.Results = make(map[string]assistv1alpha1.CheckerResult)
	}

	log.Info("Processing TeamHealthRequest", "name", healthReq.Name)

	// Resolve namespaces
	resolver := scope.NewResolver(r.DataSource, healthReq.Namespace)
	namespaces, err := resolver.ResolveNamespaces(ctx, healthReq.Spec.Scope)
	if err != nil {
		return r.setFailed(ctx, original, healthReq, fmt.Sprintf("Failed to resolve namespaces: %v", err))
	}

	if len(namespaces) == 0 {
		return r.setFailed(ctx, original, healthReq, "No namespaces found matching scope")
	}

	// Limit namespace count to prevent resource exhaustion
	originalCount := len(namespaces)
	if len(namespaces) > MaxNamespaces {
		log.Info("Limiting namespaces", "requested", len(namespaces), "max", MaxNamespaces)
		namespaces = namespaces[:MaxNamespaces]
	}

	healthReq.Status.NamespacesChecked = namespaces
	if originalCount > MaxNamespaces {
		r.setCondition(healthReq, assistv1alpha1.TeamHealthConditionNamespacesResolved, metav1.ConditionTrue,
			"Truncated", fmt.Sprintf("Evaluated %d of %d namespace(s) (max: %d)", MaxNamespaces, originalCount, MaxNamespaces))
	} else {
		r.setCondition(healthReq, assistv1alpha1.TeamHealthConditionNamespacesResolved, metav1.ConditionTrue,
			"Resolved", fmt.Sprintf("Found %d namespace(s)", len(namespaces)))
	}

	// Determine which checkers to run
	checkerNames := r.getCheckerNames(healthReq.Spec.Checks)

	// Build check context — AI is disabled during RunAll so we can run
	// causal correlation first, then enhance with AI using correlation data.
	checkCtx := &checker.CheckContext{
		DataSource: r.DataSource,
		Namespaces: namespaces,
		Config:     r.buildCheckerConfig(healthReq.Spec.Config),
		AIProvider: r.AIProvider,
		AIEnabled:  false, // AI runs after causal analysis
	}

	// Create timeout context for checker execution
	checkerCtx, cancelCheckers := context.WithTimeout(ctx, DefaultCheckerTimeout)
	defer cancelCheckers()

	// Run checkers with timeout (without AI)
	log.Info("Running checkers", "checkers", checkerNames, "namespaces", len(namespaces))
	results := r.Registry.RunAll(checkerCtx, checkCtx, checkerNames)

	// Run causal correlation, then AI enhancement with causal context
	if r.Correlator != nil {
		causalCtx := r.Correlator.Analyze(causal.CorrelationInput{
			Results:   results,
			Timestamp: time.Now(),
		})
		if causalCtx != nil && len(causalCtx.Groups) > 0 {
			log.Info("Causal analysis complete", "groups", len(causalCtx.Groups), "uncorrelated", causalCtx.UncorrelatedCount)
			checkCtx.CausalContext = causal.ToAIContext(causalCtx)
		}
	}

	// Now run AI enhancement with causal context included (single batched call)
	if r.AIProvider != nil && r.AIProvider.Available() {
		checkCtx.AIEnabled = true
		checkCtx.AIProvider = r.AIProvider
		enhanced, tokens, totalCount, _, aiErr := checker.EnhanceAllWithAI(checkerCtx, results, checkCtx)
		if aiErr != nil {
			log.Error(aiErr, "AI batch enhancement failed")
		} else if enhanced > 0 {
			log.Info("AI batch enhancement completed", "enhanced", enhanced, "tokens", tokens, "totalIssues", totalCount)
		}
	}

	// Convert results to API format
	totalHealthy := 0
	totalIssues := 0
	criticalCount := 0
	warningCount := 0

	for name, result := range results {
		healthy := min(result.Healthy, math.MaxInt32)
		apiResult := assistv1alpha1.CheckerResult{
			Healthy: int32(healthy), // #nosec G115 -- bounded by min()
		}

		// Track resources checked per checker
		teamHealthResourcesChecked.With(prometheus.Labels{
			"checker": name,
		}).Set(float64(result.Healthy + len(result.Issues)))

		if result.Error != nil {
			apiResult.Error = result.Error.Error()
			log.Error(result.Error, "Checker failed", "checker", name)
		} else {
			apiResult.Issues = checker.ToAPIIssues(result.Issues)
			totalHealthy += result.Healthy
			totalIssues += len(result.Issues)

			// Track issues by checker and severity
			checkerCritical := 0
			checkerWarning := 0
			for _, issue := range result.Issues {
				switch issue.Severity {
				case checker.SeverityCritical:
					criticalCount++
					checkerCritical++
				case checker.SeverityWarning:
					warningCount++
					checkerWarning++
				}
			}

			teamHealthIssues.With(prometheus.Labels{
				"checker":  name,
				"severity": "critical",
			}).Set(float64(checkerCritical))
			teamHealthIssues.With(prometheus.Labels{
				"checker":  name,
				"severity": "warning",
			}).Set(float64(checkerWarning))
		}

		healthReq.Status.Results[name] = apiResult
	}

	// Generate summary
	healthReq.Status.Summary = r.generateSummary(totalHealthy, totalIssues, criticalCount, warningCount)
	if originalCount > MaxNamespaces {
		healthReq.Status.Summary += fmt.Sprintf(" (evaluated %d of %d namespaces)", MaxNamespaces, originalCount)
	}

	// Update completion status
	now := metav1.Now()
	healthReq.Status.LastCheckTime = &now
	healthReq.Status.CompletedAt = &now
	healthReq.Status.Phase = assistv1alpha1.TeamHealthPhaseCompleted

	r.setCondition(healthReq, assistv1alpha1.TeamHealthConditionCheckersCompleted, metav1.ConditionTrue,
		"Completed", fmt.Sprintf("Ran %d checker(s)", len(results)))
	r.setCondition(healthReq, assistv1alpha1.TeamHealthConditionComplete, metav1.ConditionTrue,
		"Complete", "Health check completed")

	// Use patch for status update to avoid conflicts
	patch := client.MergeFrom(original)
	if err := r.Status().Patch(ctx, healthReq, patch); err != nil {
		log.Error(err, "Failed to patch status")
		return ctrl.Result{}, fmt.Errorf("failed to patch TeamHealthRequest status: %w", err)
	}

	// Send notifications if configured
	if len(healthReq.Spec.Notify) > 0 && r.NotifierRegistry != nil {
		r.launchNotifications(ctx, healthReq, totalHealthy, totalIssues, criticalCount, warningCount)
	}

	if r.Recorder != nil {
		r.Recorder.Eventf(healthReq, nil, corev1.EventTypeNormal, "HealthCheckCompleted", "HealthCheckCompleted", "Checked %d namespace(s): %d healthy, %d issue(s)", len(namespaces), totalHealthy, totalIssues)
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
func (r *TeamHealthRequestReconciler) buildCheckerConfig(cfg assistv1alpha1.CheckerConfig) map[string]any {
	config := make(map[string]any)

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
//
//nolint:unparam // result is part of the API signature for consistency
func (r *TeamHealthRequestReconciler) setFailed(
	ctx context.Context,
	original, hr *assistv1alpha1.TeamHealthRequest,
	message string,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("TeamHealthRequest failed", "reason", message, "name", hr.Name)

	hr.Status.Phase = assistv1alpha1.TeamHealthPhaseFailed
	hr.Status.Summary = message
	now := metav1.Now()
	hr.Status.LastCheckTime = &now
	hr.Status.CompletedAt = &now
	if r.Recorder != nil {
		r.Recorder.Eventf(hr, nil, corev1.EventTypeWarning, "HealthCheckFailed", "HealthCheckFailed", "%s", message)
	}

	r.setCondition(hr, assistv1alpha1.TeamHealthConditionComplete, metav1.ConditionFalse,
		"Failed", message)

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

// launchNotifications dispatches webhook notifications in a fire-and-forget
// goroutine. context.Background() is used intentionally because the parent
// reconcile ctx is cancelled when Reconcile returns, which would immediately
// cancel in-flight HTTP requests. The detached context ensures notifications
// can complete independently with their own 30-second timeout.
func (r *TeamHealthRequestReconciler) launchNotifications(
	ctx context.Context,
	hr *assistv1alpha1.TeamHealthRequest,
	totalHealthy, totalIssues, criticalCount, warningCount int,
) {
	if r.NotifySem != nil {
		select {
		case r.NotifySem <- struct{}{}:
			go func() {
				defer func() { <-r.NotifySem }()
				notifyCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				notifyCtx = logf.IntoContext(notifyCtx, logf.FromContext(ctx))
				r.dispatchNotifications(notifyCtx, hr, totalHealthy, totalIssues, criticalCount, warningCount)
			}()
		default:
			log := logf.FromContext(ctx)
			log.Info("Notification semaphore full, skipping dispatch", "name", hr.Name)
		}
	} else {
		go func() {
			notifyCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			notifyCtx = logf.IntoContext(notifyCtx, logf.FromContext(ctx))
			r.dispatchNotifications(notifyCtx, hr, totalHealthy, totalIssues, criticalCount, warningCount)
		}()
	}
}

// dispatchNotifications sends notifications to configured targets
func (r *TeamHealthRequestReconciler) dispatchNotifications(
	ctx context.Context,
	hr *assistv1alpha1.TeamHealthRequest,
	totalHealthy, totalIssues, criticalCount, warningCount int,
) {
	log := logf.FromContext(ctx).WithValues("name", hr.Name, "namespace", hr.Namespace)

	for _, target := range hr.Spec.Notify {
		if target.Type != assistv1alpha1.NotificationTypeWebhook {
			continue
		}

		// Skip if OnCompletion is false and this is a completion notification
		if !target.OnCompletion {
			continue
		}

		// Skip if severity filter doesn't match
		if target.OnSeverity != "" && !r.severityMet(target.OnSeverity, criticalCount, warningCount) {
			continue
		}

		webhookURL := target.URL
		if webhookURL == "" && target.SecretRef != nil {
			// Read URL from secret
			var secret corev1.Secret
			if err := r.Get(ctx, client.ObjectKey{Name: target.SecretRef.Name, Namespace: hr.Namespace}, &secret); err != nil {
				log.Error(err, "Failed to read notification secret", "secret", target.SecretRef.Name)
				continue
			}
			webhookURL = string(secret.Data[target.SecretRef.Key])
		}

		if webhookURL == "" {
			continue
		}
		if err := r.validateNotificationURL(hr, webhookURL); err != nil {
			log.Error(err, "Skipping invalid notification target", "target", "webhook")
			continue
		}

		healthy := totalHealthy
		issues := totalIssues
		score := float64(100)
		if healthy+issues > 0 {
			score = float64(healthy) / float64(healthy+issues) * 100
		}

		n := notifier.Notification{
			Summary:       hr.Status.Summary,
			TotalHealthy:  healthy,
			TotalIssues:   issues,
			CriticalCount: criticalCount,
			WarningCount:  warningCount,
			HealthScore:   score,
			Timestamp:     time.Now(),
			RequestName:   hr.Name,
			Namespace:     hr.Namespace,
		}

		wh := notifier.NewWebhookNotifier(webhookURL)
		if err := wh.Send(ctx, n); err != nil {
			log.Error(err, "Failed to send notification", "target", "webhook")
		}
	}
}

// validateNotificationURL enforces URL shape and scheme for notification targets.
func (r *TeamHealthRequestReconciler) validateNotificationURL(hr *assistv1alpha1.TeamHealthRequest, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid webhook URL: %w", err)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("webhook URL must include a host")
	}
	if u.Scheme != "https" {
		allowHTTP := hr.Annotations != nil && hr.Annotations[assistv1alpha1.AllowHTTPWebhooksAnnotation] == "true"
		if u.Scheme != "http" || !allowHTTP {
			return fmt.Errorf("webhook URL must use HTTPS (annotate with %s=true to override)", assistv1alpha1.AllowHTTPWebhooksAnnotation)
		}
	}
	return nil
}

// severityMet checks if the configured severity threshold is met
func (r *TeamHealthRequestReconciler) severityMet(threshold string, critical, warning int) bool {
	switch threshold {
	case "Critical":
		return critical > 0
	case "Warning":
		return critical > 0 || warning > 0
	case "Info":
		return true
	default:
		return true
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *TeamHealthRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&assistv1alpha1.TeamHealthRequest{}).
		Named("teamhealthrequest").
		Complete(r)
}
