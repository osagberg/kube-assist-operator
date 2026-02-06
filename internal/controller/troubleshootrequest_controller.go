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
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	assistv1alpha1 "github.com/osagberg/kube-assist-operator/api/v1alpha1"
	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/checker/workload"
)

// TroubleshootRequestReconciler reconciles a TroubleshootRequest object
type TroubleshootRequestReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Clientset *kubernetes.Clientset

	// Registry is the checker registry (optional, for future use)
	Registry *checker.Registry
}

// +kubebuilder:rbac:groups=assist.cluster.local,resources=troubleshootrequests,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=assist.cluster.local,resources=troubleshootrequests/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=assist.cluster.local,resources=troubleshootrequests/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments;statefulsets;daemonsets;replicasets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods;pods/log;events;configmaps,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

func (r *TroubleshootRequestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Record reconcile duration
	startTime := time.Now()
	defer func() {
		reconcileDuration.With(prometheus.Labels{
			"namespace": req.Namespace,
		}).Observe(time.Since(startTime).Seconds())
	}()

	// Fetch the TroubleshootRequest
	troubleshoot := &assistv1alpha1.TroubleshootRequest{}
	if err := r.Get(ctx, req.NamespacedName, troubleshoot); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		reconcileTotal.With(prometheus.Labels{
			"namespace": req.Namespace,
			"result":    "error",
		}).Inc()
		return ctrl.Result{}, err
	}
	// Save original for patch later
	original := troubleshoot.DeepCopy()

	// Handle TTL cleanup for completed/failed requests
	if troubleshoot.Status.Phase == assistv1alpha1.PhaseCompleted ||
		troubleshoot.Status.Phase == assistv1alpha1.PhaseFailed {
		if troubleshoot.Spec.TTLSecondsAfterFinished != nil && troubleshoot.Status.CompletedAt != nil {
			ttl := time.Duration(*troubleshoot.Spec.TTLSecondsAfterFinished) * time.Second
			deadline := troubleshoot.Status.CompletedAt.Add(ttl)
			remaining := time.Until(deadline)
			if remaining <= 0 {
				log.Info("TTL expired, deleting TroubleshootRequest", "name", troubleshoot.Name)
				return ctrl.Result{}, r.Delete(ctx, troubleshoot)
			}
			return ctrl.Result{RequeueAfter: remaining}, nil
		}
		return ctrl.Result{}, nil
	}

	// Initialize timestamps if needed (will be saved at end)
	if troubleshoot.Status.StartedAt == nil {
		now := metav1.Now()
		troubleshoot.Status.StartedAt = &now
	}
	troubleshoot.Status.Phase = assistv1alpha1.PhaseRunning

	log.Info("Troubleshooting workload",
		"target", troubleshoot.Spec.Target.Name,
		"kind", troubleshoot.Spec.Target.Kind,
		"actions", troubleshoot.Spec.Actions)

	// Find the target workload
	pods, err := r.findTargetPods(ctx, troubleshoot)
	if err != nil {
		if patchErr := r.setFailed(ctx, original, troubleshoot, fmt.Sprintf("Failed to find target: %v", err)); patchErr != nil {
			return ctrl.Result{}, patchErr
		}
		return ctrl.Result{}, nil
	}

	if len(pods) == 0 {
		if patchErr := r.setFailed(ctx, original, troubleshoot, "No pods found for target workload"); patchErr != nil {
			return ctrl.Result{}, patchErr
		}
		return ctrl.Result{}, nil
	}

	r.setCondition(troubleshoot, assistv1alpha1.ConditionTargetFound, metav1.ConditionTrue,
		"Found", fmt.Sprintf("Found %d pod(s)", len(pods)))

	// Perform diagnostics
	var issues []assistv1alpha1.DiagnosticIssue

	for _, action := range troubleshoot.Spec.Actions {
		switch action {
		case assistv1alpha1.ActionDiagnose, assistv1alpha1.ActionAll, assistv1alpha1.ActionDescribe:
			diagIssues := r.diagnosePodsDetailed(pods)
			issues = append(issues, diagIssues...)
		}
	}

	// Collect logs if requested
	for _, action := range troubleshoot.Spec.Actions {
		if action == assistv1alpha1.ActionLogs || action == assistv1alpha1.ActionAll || action == assistv1alpha1.ActionDescribe {
			cmName, err := r.collectLogs(ctx, troubleshoot, pods)
			if err != nil {
				log.Error(err, "Failed to collect logs")
			} else {
				troubleshoot.Status.LogsConfigMap = cmName
				r.setCondition(troubleshoot, assistv1alpha1.ConditionLogsCollected, metav1.ConditionTrue,
					"Collected", "Logs stored in ConfigMap")
			}
			break
		}
	}

	// Collect events if requested
	for _, action := range troubleshoot.Spec.Actions {
		if action == assistv1alpha1.ActionEvents || action == assistv1alpha1.ActionAll || action == assistv1alpha1.ActionDescribe {
			cmName, err := r.collectEvents(ctx, troubleshoot, pods)
			if err != nil {
				log.Error(err, "Failed to collect events")
			} else {
				troubleshoot.Status.EventsConfigMap = cmName
			}
			break
		}
	}

	// Update status with findings
	troubleshoot.Status.Issues = issues
	troubleshoot.Status.Summary = r.generateSummary(pods, issues)
	troubleshoot.Status.Phase = assistv1alpha1.PhaseCompleted
	now := metav1.Now()
	troubleshoot.Status.CompletedAt = &now

	r.setCondition(troubleshoot, assistv1alpha1.ConditionDiagnosed, metav1.ConditionTrue,
		"Diagnosed", fmt.Sprintf("Found %d issue(s)", len(issues)))
	r.setCondition(troubleshoot, assistv1alpha1.ConditionComplete, metav1.ConditionTrue,
		"Complete", "Troubleshooting completed")

	// Use patch for status update to avoid conflicts
	patch := client.MergeFrom(original)
	if err := r.Status().Patch(ctx, troubleshoot, patch); err != nil {
		log.Error(err, "Failed to patch status")
		reconcileTotal.With(prometheus.Labels{
			"namespace": req.Namespace,
			"result":    "error",
		}).Inc()
		return ctrl.Result{}, err
	}

	// Record success metric
	reconcileTotal.With(prometheus.Labels{
		"namespace": req.Namespace,
		"result":    "success",
	}).Inc()

	// Update issues gauge by severity
	r.updateIssuesMetrics(troubleshoot.Namespace, issues)

	log.Info("Troubleshooting completed", "issues", len(issues), "summary", troubleshoot.Status.Summary)
	return ctrl.Result{}, nil
}

// findTargetPods locates pods belonging to the target workload
func (r *TroubleshootRequestReconciler) findTargetPods(ctx context.Context, tr *assistv1alpha1.TroubleshootRequest) ([]corev1.Pod, error) {
	var selector *metav1.LabelSelector

	switch tr.Spec.Target.Kind {
	case "Deployment", "":
		deploy := &appsv1.Deployment{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: tr.Namespace, Name: tr.Spec.Target.Name}, deploy); err != nil {
			return nil, err
		}
		selector = deploy.Spec.Selector
	case "StatefulSet":
		sts := &appsv1.StatefulSet{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: tr.Namespace, Name: tr.Spec.Target.Name}, sts); err != nil {
			return nil, err
		}
		selector = sts.Spec.Selector
	case "DaemonSet":
		ds := &appsv1.DaemonSet{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: tr.Namespace, Name: tr.Spec.Target.Name}, ds); err != nil {
			return nil, err
		}
		selector = ds.Spec.Selector
	case "ReplicaSet":
		rs := &appsv1.ReplicaSet{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: tr.Namespace, Name: tr.Spec.Target.Name}, rs); err != nil {
			return nil, err
		}
		selector = rs.Spec.Selector
	case "Pod":
		pod := &corev1.Pod{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: tr.Namespace, Name: tr.Spec.Target.Name}, pod); err != nil {
			return nil, err
		}
		return []corev1.Pod{*pod}, nil
	default:
		return nil, fmt.Errorf("unsupported target kind: %s", tr.Spec.Target.Kind)
	}

	// List pods matching selector
	labelSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, err
	}

	podList := &corev1.PodList{}
	if err := r.List(ctx, podList,
		client.InNamespace(tr.Namespace),
		client.MatchingLabelsSelector{Selector: labelSelector}); err != nil {
		return nil, err
	}

	return podList.Items, nil
}

// diagnosePodsDetailed performs detailed diagnostics on pods
func (r *TroubleshootRequestReconciler) diagnosePodsDetailed(pods []corev1.Pod) []assistv1alpha1.DiagnosticIssue {
	var issues []assistv1alpha1.DiagnosticIssue

	for _, pod := range pods {
		// Check pod phase
		if pod.Status.Phase != corev1.PodRunning {
			issues = append(issues, assistv1alpha1.DiagnosticIssue{
				Type:     "PodNotRunning",
				Severity: "Critical",
				Message:  fmt.Sprintf("Pod %s is in %s phase", pod.Name, pod.Status.Phase),
			})
		}

		// Check container statuses
		for _, cs := range pod.Status.ContainerStatuses {
			if !cs.Ready {
				issue := assistv1alpha1.DiagnosticIssue{
					Type:     "ContainerNotReady",
					Severity: "Critical",
					Message:  fmt.Sprintf("Container %s in pod %s is not ready", cs.Name, pod.Name),
				}

				// Analyze why
				if cs.State.Waiting != nil {
					issue.Message = fmt.Sprintf("Container %s is waiting: %s - %s",
						cs.Name, cs.State.Waiting.Reason, cs.State.Waiting.Message)
					issue.Suggestion = r.getSuggestionForWaitingReason(cs.State.Waiting.Reason)
				} else if cs.State.Terminated != nil {
					issue.Message = fmt.Sprintf("Container %s terminated: %s (exit code %d)",
						cs.Name, cs.State.Terminated.Reason, cs.State.Terminated.ExitCode)
					issue.Suggestion = r.getSuggestionForTerminatedReason(cs.State.Terminated.Reason, cs.State.Terminated.ExitCode)
				}

				issues = append(issues, issue)
			}

			// Check restart count
			if cs.RestartCount > 3 {
				issues = append(issues, assistv1alpha1.DiagnosticIssue{
					Type:     "HighRestartCount",
					Severity: "Warning",
					Message:  fmt.Sprintf("Container %s has restarted %d times", cs.Name, cs.RestartCount),
					Suggestion: fmt.Sprintf("Check previous logs: kubectl logs %s -c %s --previous -n %s. "+
						"Common causes: OOM kills, liveness probe failures, or application crashes.",
						pod.Name, cs.Name, pod.Namespace),
				})
			}
		}

		// Check for pending conditions
		for _, cond := range pod.Status.Conditions {
			if cond.Status == corev1.ConditionFalse {
				switch cond.Type {
				case corev1.PodScheduled:
					issues = append(issues, assistv1alpha1.DiagnosticIssue{
						Type:     "SchedulingFailed",
						Severity: "Critical",
						Message:  fmt.Sprintf("Pod %s failed to schedule: %s", pod.Name, cond.Message),
						Suggestion: fmt.Sprintf("Check node capacity: kubectl describe nodes | grep -A5 'Allocated resources'. "+
							"Verify taints/tolerations: kubectl get nodes -o custom-columns=NAME:.metadata.name,TAINTS:.spec.taints. "+
							"Check pod affinity: kubectl get pod %s -n %s -o jsonpath='{.spec.affinity}'.",
							pod.Name, pod.Namespace),
					})
				case corev1.PodReady:
					if cond.Reason == "ContainersNotReady" {
						// Already handled above
						continue
					}
					issues = append(issues, assistv1alpha1.DiagnosticIssue{
						Type:     "PodNotReady",
						Severity: "Warning",
						Message:  fmt.Sprintf("Pod %s not ready: %s", pod.Name, cond.Message),
					})
				}
			}
		}

		// Check resource usage and best practices
		for _, container := range pod.Spec.Containers {
			// Memory limit
			if container.Resources.Limits.Memory().IsZero() {
				issues = append(issues, assistv1alpha1.DiagnosticIssue{
					Type:     "NoMemoryLimit",
					Severity: "Warning",
					Message:  fmt.Sprintf("Container %s has no memory limit set", container.Name),
					Suggestion: "Set a memory limit to prevent OOM issues and ensure fair resource sharing. " +
						"Check current usage: kubectl top pod " + pod.Name + " -n " + pod.Namespace + ". " +
						"See Kubernetes docs on resource management for containers.",
				})
			}

			// CPU limit
			if container.Resources.Limits.Cpu().IsZero() {
				issues = append(issues, assistv1alpha1.DiagnosticIssue{
					Type:     "NoCPULimit",
					Severity: "Info",
					Message:  fmt.Sprintf("Container %s has no CPU limit set", container.Name),
					Suggestion: "Consider setting CPU limits for predictable performance. " +
						"Note: some teams intentionally omit CPU limits to avoid throttling.",
				})
			}

			// Resource requests
			if container.Resources.Requests.Memory().IsZero() && container.Resources.Requests.Cpu().IsZero() {
				issues = append(issues, assistv1alpha1.DiagnosticIssue{
					Type:     "NoResourceRequests",
					Severity: "Warning",
					Message:  fmt.Sprintf("Container %s has no resource requests set", container.Name),
					Suggestion: "Set resource requests so the scheduler can place pods on nodes with sufficient capacity. " +
						"Without requests, pods get BestEffort QoS and are evicted first under memory pressure.",
				})
			}

			// Liveness probe
			if container.LivenessProbe == nil {
				issues = append(issues, assistv1alpha1.DiagnosticIssue{
					Type:     "NoLivenessProbe",
					Severity: "Info",
					Message:  fmt.Sprintf("Container %s has no liveness probe configured", container.Name),
					Suggestion: "Add a liveness probe to enable automatic container restart when the application " +
						"becomes unresponsive. See Kubernetes docs on configure liveness, readiness and startup probes.",
				})
			}

			// Readiness probe
			if container.ReadinessProbe == nil {
				issues = append(issues, assistv1alpha1.DiagnosticIssue{
					Type:     "NoReadinessProbe",
					Severity: "Info",
					Message:  fmt.Sprintf("Container %s has no readiness probe configured", container.Name),
					Suggestion: "Add a readiness probe to prevent traffic from being sent to pods that are not ready. " +
						"This is especially important for services behind a Service/Ingress.",
				})
			}
		}
	}

	return issues
}

func (r *TroubleshootRequestReconciler) getSuggestionForWaitingReason(reason string) string {
	// Delegate to workload checker for consistent suggestions
	return workload.GetSuggestionForWaitingReason(reason)
}

func (r *TroubleshootRequestReconciler) getSuggestionForTerminatedReason(reason string, exitCode int32) string {
	// Delegate to workload checker for consistent suggestions
	return workload.GetSuggestionForTerminatedReason(reason, exitCode)
}

// collectLogs gathers logs from target pods and stores in ConfigMap
func (r *TroubleshootRequestReconciler) collectLogs(ctx context.Context, tr *assistv1alpha1.TroubleshootRequest, pods []corev1.Pod) (string, error) {
	if r.Clientset == nil {
		return "", fmt.Errorf("clientset not initialized")
	}

	cmName := fmt.Sprintf("%s-logs", tr.Name)
	data := make(map[string]string)

	tailLines := int64(tr.Spec.TailLines)
	if tailLines == 0 {
		tailLines = 100
	}

	for _, pod := range pods {
		for _, container := range pod.Spec.Containers {
			logOpts := &corev1.PodLogOptions{
				Container: container.Name,
				TailLines: &tailLines,
			}

			req := r.Clientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, logOpts)
			stream, err := req.Stream(ctx)
			if err != nil {
				data[fmt.Sprintf("%s-%s", pod.Name, container.Name)] = fmt.Sprintf("Error getting logs: %v", err)
				continue
			}

			buf := new(bytes.Buffer)
			_, err = io.Copy(buf, stream)
			_ = stream.Close() // Close immediately, not defer
			if err != nil {
				data[fmt.Sprintf("%s-%s", pod.Name, container.Name)] = fmt.Sprintf("Error reading logs: %v", err)
				continue
			}

			data[fmt.Sprintf("%s-%s", pod.Name, container.Name)] = buf.String()
		}
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: tr.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "kube-assist",
				"assist.cluster.local/request": tr.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(tr, assistv1alpha1.GroupVersion.WithKind("TroubleshootRequest")),
			},
		},
		Data: data,
	}

	if err := r.Create(ctx, cm); err != nil {
		if apierrors.IsAlreadyExists(err) {
			// Update existing
			existing := &corev1.ConfigMap{}
			if err := r.Get(ctx, client.ObjectKey{Namespace: tr.Namespace, Name: cmName}, existing); err != nil {
				return "", err
			}
			existing.Data = data
			if err := r.Update(ctx, existing); err != nil {
				return "", err
			}
		} else {
			return "", err
		}
	}

	return cmName, nil
}

// collectEvents gathers events for the target workload
func (r *TroubleshootRequestReconciler) collectEvents(ctx context.Context, tr *assistv1alpha1.TroubleshootRequest, pods []corev1.Pod) (string, error) {
	cmName := fmt.Sprintf("%s-events", tr.Name)

	eventList := &corev1.EventList{}
	if err := r.List(ctx, eventList, client.InNamespace(tr.Namespace)); err != nil {
		return "", err
	}

	var relevantEvents []string
	podNames := make(map[string]bool)
	for _, pod := range pods {
		podNames[pod.Name] = true
	}

	for _, event := range eventList.Items {
		// Filter events for our pods or the target workload
		if podNames[event.InvolvedObject.Name] || event.InvolvedObject.Name == tr.Spec.Target.Name {
			eventStr := fmt.Sprintf("[%s] %s %s: %s",
				event.LastTimestamp.Format(time.RFC3339),
				event.Type,
				event.Reason,
				event.Message)
			relevantEvents = append(relevantEvents, eventStr)
		}
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: tr.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "kube-assist",
				"assist.cluster.local/request": tr.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(tr, assistv1alpha1.GroupVersion.WithKind("TroubleshootRequest")),
			},
		},
		Data: map[string]string{
			"events": strings.Join(relevantEvents, "\n"),
		},
	}

	if err := r.Create(ctx, cm); err != nil {
		if apierrors.IsAlreadyExists(err) {
			existing := &corev1.ConfigMap{}
			if err := r.Get(ctx, client.ObjectKey{Namespace: tr.Namespace, Name: cmName}, existing); err != nil {
				return "", err
			}
			existing.Data = cm.Data
			if err := r.Update(ctx, existing); err != nil {
				return "", err
			}
		} else {
			return "", err
		}
	}

	return cmName, nil
}

// generateSummary creates a human-readable summary
func (r *TroubleshootRequestReconciler) generateSummary(pods []corev1.Pod, issues []assistv1alpha1.DiagnosticIssue) string {
	if len(issues) == 0 {
		return fmt.Sprintf("All %d pod(s) healthy - no issues found", len(pods))
	}

	// Count by severity
	critical := 0
	warning := 0
	for _, issue := range issues {
		switch issue.Severity {
		case "Critical":
			critical++
		case "Warning":
			warning++
		}
	}

	if critical > 0 {
		return fmt.Sprintf("%d critical, %d warning issue(s) found", critical, warning)
	}
	return fmt.Sprintf("%d warning issue(s) found", warning)
}

func (r *TroubleshootRequestReconciler) setFailed(ctx context.Context, original, tr *assistv1alpha1.TroubleshootRequest, message string) error {
	log := logf.FromContext(ctx)
	log.Info("Troubleshooting failed", "reason", message, "target", tr.Spec.Target.Name)

	tr.Status.Phase = assistv1alpha1.PhaseFailed
	tr.Status.Summary = message
	now := metav1.Now()
	tr.Status.CompletedAt = &now

	r.setCondition(tr, assistv1alpha1.ConditionComplete, metav1.ConditionFalse,
		"Failed", message)

	patch := client.MergeFrom(original)
	return r.Status().Patch(ctx, tr, patch)
}

//nolint:unparam // status is part of the API signature for consistency
func (r *TroubleshootRequestReconciler) setCondition(tr *assistv1alpha1.TroubleshootRequest, condType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&tr.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		ObservedGeneration: tr.Generation,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
}

// updateIssuesMetrics updates the issues gauge metrics by severity
func (r *TroubleshootRequestReconciler) updateIssuesMetrics(namespace string, issues []assistv1alpha1.DiagnosticIssue) {
	// Count issues by severity
	severityCounts := map[string]float64{
		"Critical": 0,
		"Warning":  0,
		"Info":     0,
	}

	for _, issue := range issues {
		if _, ok := severityCounts[issue.Severity]; ok {
			severityCounts[issue.Severity]++
		} else {
			// Handle unknown severity as Info
			severityCounts["Info"]++
		}
	}

	// Update gauge for each severity
	for severity, count := range severityCounts {
		issuesTotal.With(prometheus.Labels{
			"namespace": namespace,
			"severity":  severity,
		}).Set(count)
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *TroubleshootRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&assistv1alpha1.TroubleshootRequest{}).
		Named("troubleshootrequest").
		Complete(r)
}
