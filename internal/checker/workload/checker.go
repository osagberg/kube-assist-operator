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

// Package workload provides a checker for Kubernetes workload health.
package workload

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/osagberg/kube-assist-operator/internal/checker"
)

const (
	// CheckerName is the identifier for this checker
	CheckerName = "workloads"

	// ConfigRestartThreshold is the config key for restart count threshold
	ConfigRestartThreshold = "restartThreshold"

	// DefaultRestartThreshold is the default restart count that triggers a warning
	DefaultRestartThreshold = 3
)

// Checker analyzes Kubernetes workloads (Deployments, StatefulSets, DaemonSets)
type Checker struct{}

// New creates a new workload checker
func New() *Checker {
	return &Checker{}
}

// Name returns the checker identifier
func (c *Checker) Name() string {
	return CheckerName
}

// Supports returns true - workload checking is always supported
func (c *Checker) Supports(ctx context.Context, cl client.Client) bool {
	return true
}

// Check performs workload health checks across the specified namespaces
func (c *Checker) Check(ctx context.Context, checkCtx *checker.CheckContext) (*checker.CheckResult, error) {
	result := &checker.CheckResult{
		CheckerName: CheckerName,
		Issues:      []checker.Issue{},
	}

	restartThreshold := getRestartThreshold(checkCtx.Config)

	for _, ns := range checkCtx.Namespaces {
		// Check Deployments
		deployments := &appsv1.DeploymentList{}
		if err := checkCtx.Client.List(ctx, deployments, client.InNamespace(ns)); err != nil {
			continue
		}

		for _, deploy := range deployments.Items {
			pods, err := c.getPodsForWorkload(ctx, checkCtx.Client, ns, deploy.Spec.Selector)
			if err != nil {
				continue
			}

			resourceRef := fmt.Sprintf("deployment/%s", deploy.Name)
			issues := c.diagnosePods(pods, ns, resourceRef, restartThreshold)

			if len(issues) == 0 {
				result.Healthy++
			} else {
				result.Issues = append(result.Issues, issues...)
			}
		}

		// Check StatefulSets
		statefulsets := &appsv1.StatefulSetList{}
		if err := checkCtx.Client.List(ctx, statefulsets, client.InNamespace(ns)); err != nil {
			continue
		}

		for _, sts := range statefulsets.Items {
			pods, err := c.getPodsForWorkload(ctx, checkCtx.Client, ns, sts.Spec.Selector)
			if err != nil {
				continue
			}

			resourceRef := fmt.Sprintf("statefulset/%s", sts.Name)
			issues := c.diagnosePods(pods, ns, resourceRef, restartThreshold)

			if len(issues) == 0 {
				result.Healthy++
			} else {
				result.Issues = append(result.Issues, issues...)
			}
		}

		// Check DaemonSets
		daemonsets := &appsv1.DaemonSetList{}
		if err := checkCtx.Client.List(ctx, daemonsets, client.InNamespace(ns)); err != nil {
			continue
		}

		for _, ds := range daemonsets.Items {
			pods, err := c.getPodsForWorkload(ctx, checkCtx.Client, ns, ds.Spec.Selector)
			if err != nil {
				continue
			}

			resourceRef := fmt.Sprintf("daemonset/%s", ds.Name)
			issues := c.diagnosePods(pods, ns, resourceRef, restartThreshold)

			if len(issues) == 0 {
				result.Healthy++
			} else {
				result.Issues = append(result.Issues, issues...)
			}
		}
	}

	return result, nil
}

// getPodsForWorkload returns pods matching a label selector
func (c *Checker) getPodsForWorkload(ctx context.Context, cl client.Client, namespace string, selector *metav1.LabelSelector) ([]corev1.Pod, error) {
	labelSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, err
	}

	podList := &corev1.PodList{}
	if err := cl.List(ctx, podList,
		client.InNamespace(namespace),
		client.MatchingLabelsSelector{Selector: labelSelector}); err != nil {
		return nil, err
	}

	return podList.Items, nil
}

// diagnosePods analyzes a set of pods and returns issues found
func (c *Checker) diagnosePods(pods []corev1.Pod, namespace, resourceRef string, restartThreshold int) []checker.Issue {
	var issues []checker.Issue

	for _, pod := range pods {
		podIssues := c.diagnosePod(&pod, namespace, resourceRef, restartThreshold)
		issues = append(issues, podIssues...)
	}

	return issues
}

// diagnosePod analyzes a single pod and returns issues found
func (c *Checker) diagnosePod(pod *corev1.Pod, namespace, resourceRef string, restartThreshold int) []checker.Issue {
	var issues []checker.Issue

	// Check pod phase
	if pod.Status.Phase != corev1.PodRunning && pod.Status.Phase != corev1.PodSucceeded {
		issues = append(issues, checker.Issue{
			Type:      "PodNotRunning",
			Severity:  checker.SeverityCritical,
			Resource:  resourceRef,
			Namespace: namespace,
			Message:   fmt.Sprintf("Pod %s is in %s phase", pod.Name, pod.Status.Phase),
			Metadata: map[string]string{
				"pod":   pod.Name,
				"phase": string(pod.Status.Phase),
			},
		})
	}

	// Check container statuses
	for _, cs := range pod.Status.ContainerStatuses {
		if !cs.Ready {
			issue := checker.Issue{
				Type:      "ContainerNotReady",
				Severity:  checker.SeverityCritical,
				Resource:  resourceRef,
				Namespace: namespace,
				Message:   fmt.Sprintf("Container %s in pod %s is not ready", cs.Name, pod.Name),
				Metadata: map[string]string{
					"pod":       pod.Name,
					"container": cs.Name,
				},
			}

			// Analyze why
			if cs.State.Waiting != nil {
				issue.Message = fmt.Sprintf("Container %s is waiting: %s", cs.Name, cs.State.Waiting.Reason)
				if cs.State.Waiting.Message != "" {
					issue.Message += " - " + cs.State.Waiting.Message
				}
				issue.Suggestion = GetSuggestionForWaitingReason(cs.State.Waiting.Reason)
				issue.Metadata["reason"] = cs.State.Waiting.Reason
			} else if cs.State.Terminated != nil {
				issue.Message = fmt.Sprintf("Container %s terminated: %s (exit code %d)",
					cs.Name, cs.State.Terminated.Reason, cs.State.Terminated.ExitCode)
				issue.Suggestion = GetSuggestionForTerminatedReason(cs.State.Terminated.Reason, cs.State.Terminated.ExitCode)
				issue.Metadata["reason"] = cs.State.Terminated.Reason
				issue.Metadata["exitCode"] = fmt.Sprintf("%d", cs.State.Terminated.ExitCode)
			}

			issues = append(issues, issue)
		}

		// Check restart count
		if cs.RestartCount > int32(restartThreshold) {
			issues = append(issues, checker.Issue{
				Type:       "HighRestartCount",
				Severity:   checker.SeverityWarning,
				Resource:   resourceRef,
				Namespace:  namespace,
				Message:    fmt.Sprintf("Container %s has restarted %d times", cs.Name, cs.RestartCount),
				Suggestion: "Check logs for crash reasons. Consider increasing resource limits or fixing application bugs.",
				Metadata: map[string]string{
					"pod":          pod.Name,
					"container":    cs.Name,
					"restartCount": fmt.Sprintf("%d", cs.RestartCount),
				},
			})
		}
	}

	// Check for pending conditions
	for _, cond := range pod.Status.Conditions {
		if cond.Status == corev1.ConditionFalse {
			switch cond.Type {
			case corev1.PodScheduled:
				issues = append(issues, checker.Issue{
					Type:       "SchedulingFailed",
					Severity:   checker.SeverityCritical,
					Resource:   resourceRef,
					Namespace:  namespace,
					Message:    fmt.Sprintf("Pod %s failed to schedule: %s", pod.Name, cond.Message),
					Suggestion: "Check node resources, taints/tolerations, and affinity rules.",
					Metadata: map[string]string{
						"pod":    pod.Name,
						"reason": cond.Reason,
					},
				})
			case corev1.PodReady:
				if cond.Reason == "ContainersNotReady" {
					// Already handled above
					continue
				}
				issues = append(issues, checker.Issue{
					Type:      "PodNotReady",
					Severity:  checker.SeverityWarning,
					Resource:  resourceRef,
					Namespace: namespace,
					Message:   fmt.Sprintf("Pod %s not ready: %s", pod.Name, cond.Message),
					Metadata: map[string]string{
						"pod":    pod.Name,
						"reason": cond.Reason,
					},
				})
			}
		}
	}

	// Check resource configuration best practices
	for _, container := range pod.Spec.Containers {
		issues = append(issues, c.checkContainerBestPractices(container, pod.Name, namespace, resourceRef)...)
	}

	return issues
}

// checkContainerBestPractices checks container configuration for best practices
func (c *Checker) checkContainerBestPractices(container corev1.Container, podName, namespace, resourceRef string) []checker.Issue {
	var issues []checker.Issue

	metadata := map[string]string{
		"pod":       podName,
		"container": container.Name,
	}

	// Memory limit
	if container.Resources.Limits.Memory().IsZero() {
		issues = append(issues, checker.Issue{
			Type:       "NoMemoryLimit",
			Severity:   checker.SeverityWarning,
			Resource:   resourceRef,
			Namespace:  namespace,
			Message:    fmt.Sprintf("Container %s has no memory limit set", container.Name),
			Suggestion: "Set memory limits to prevent OOM issues and ensure fair resource sharing.",
			Metadata:   metadata,
		})
	}

	// CPU limit
	if container.Resources.Limits.Cpu().IsZero() {
		issues = append(issues, checker.Issue{
			Type:       "NoCPULimit",
			Severity:   checker.SeverityInfo,
			Resource:   resourceRef,
			Namespace:  namespace,
			Message:    fmt.Sprintf("Container %s has no CPU limit set", container.Name),
			Suggestion: "Consider setting CPU limits for predictable performance.",
			Metadata:   metadata,
		})
	}

	// Resource requests
	if container.Resources.Requests.Memory().IsZero() && container.Resources.Requests.Cpu().IsZero() {
		issues = append(issues, checker.Issue{
			Type:       "NoResourceRequests",
			Severity:   checker.SeverityWarning,
			Resource:   resourceRef,
			Namespace:  namespace,
			Message:    fmt.Sprintf("Container %s has no resource requests set", container.Name),
			Suggestion: "Set resource requests to help the scheduler place pods appropriately.",
			Metadata:   metadata,
		})
	}

	// Liveness probe
	if container.LivenessProbe == nil {
		issues = append(issues, checker.Issue{
			Type:       "NoLivenessProbe",
			Severity:   checker.SeverityInfo,
			Resource:   resourceRef,
			Namespace:  namespace,
			Message:    fmt.Sprintf("Container %s has no liveness probe configured", container.Name),
			Suggestion: "Add a liveness probe to enable automatic container restart on failure.",
			Metadata:   metadata,
		})
	}

	// Readiness probe
	if container.ReadinessProbe == nil {
		issues = append(issues, checker.Issue{
			Type:       "NoReadinessProbe",
			Severity:   checker.SeverityInfo,
			Resource:   resourceRef,
			Namespace:  namespace,
			Message:    fmt.Sprintf("Container %s has no readiness probe configured", container.Name),
			Suggestion: "Add a readiness probe to prevent traffic being sent to unready pods.",
			Metadata:   metadata,
		})
	}

	return issues
}

// GetSuggestionForWaitingReason returns a suggestion based on container waiting reason
func GetSuggestionForWaitingReason(reason string) string {
	suggestions := map[string]string{
		"ImagePullBackOff":           "Check if the image exists and credentials are configured correctly.",
		"ErrImagePull":               "Verify image name and tag. Check registry authentication.",
		"CrashLoopBackOff":           "Application is crashing. Check logs for error messages.",
		"CreateContainerConfigError": "Check ConfigMaps and Secrets referenced by the pod.",
		"ContainerCreating":          "Container is being created. If stuck, check node resources and events.",
		"PodInitializing":            "Pod is initializing. If stuck, check init container status.",
	}
	if s, ok := suggestions[reason]; ok {
		return s
	}
	return "Check pod events for more details."
}

// GetSuggestionForTerminatedReason returns a suggestion based on container termination reason
func GetSuggestionForTerminatedReason(reason string, exitCode int32) string {
	if reason == "OOMKilled" {
		return "Container exceeded memory limit. Increase memory limit or optimize memory usage."
	}
	if exitCode == 137 {
		return "Container was killed (likely OOM or SIGKILL). Check memory limits and node resources."
	}
	if exitCode == 143 {
		return "Container received SIGTERM. This is normal during rolling updates or scale-downs."
	}
	if exitCode == 1 {
		return "Application exited with error. Check application logs for details."
	}
	if exitCode == 126 {
		return "Command cannot be executed. Check file permissions and binary format."
	}
	if exitCode == 127 {
		return "Command not found. Check the container image and command configuration."
	}
	return fmt.Sprintf("Container exited with code %d. Check application logs.", exitCode)
}

// DiagnosePods is a convenience function for diagnosing pods directly
// Used by the TroubleshootRequest controller for backward compatibility
func DiagnosePods(pods []corev1.Pod, namespace, resourceRef string, restartThreshold int) []checker.Issue {
	c := &Checker{}
	return c.diagnosePods(pods, namespace, resourceRef, restartThreshold)
}

// getRestartThreshold extracts the restart threshold from config
func getRestartThreshold(config map[string]any) int {
	if config == nil {
		return DefaultRestartThreshold
	}
	if val, ok := config[ConfigRestartThreshold]; ok {
		if threshold, ok := val.(int); ok {
			return threshold
		}
	}
	return DefaultRestartThreshold
}
