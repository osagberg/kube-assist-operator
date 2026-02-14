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
	"math"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/datasource"
)

var log = logf.Log.WithName("workload-checker")

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
func (c *Checker) Supports(ctx context.Context, ds datasource.DataSource) bool {
	return true
}

// Check performs workload health checks across the specified namespaces
func (c *Checker) Check(ctx context.Context, checkCtx *checker.CheckContext) (*checker.CheckResult, error) {
	result := &checker.CheckResult{
		CheckerName: CheckerName,
		Issues:      []checker.Issue{},
	}

	restartThreshold := getRestartThreshold(checkCtx.Config)

	// PERF-003: Namespace-level pod cache — fetch all pods once per namespace
	// and filter by label selector locally, avoiding redundant API calls.
	podCache := make(map[string]*corev1.PodList)

	for _, ns := range checkCtx.Namespaces {
		// Pre-fetch all pods in this namespace once for reuse across workloads.
		if _, ok := podCache[ns]; !ok {
			allPods := &corev1.PodList{}
			if err := checkCtx.DataSource.List(ctx, allPods, client.InNamespace(ns)); err != nil {
				log.Error(err, "Failed to list pods", "namespace", ns)
				podCache[ns] = &corev1.PodList{}
			} else {
				podCache[ns] = allPods
			}
		}

		// Check Deployments
		deployments := &appsv1.DeploymentList{}
		if err := checkCtx.DataSource.List(ctx, deployments, client.InNamespace(ns)); err != nil {
			log.Error(err, "Failed to list deployments", "namespace", ns)
			continue
		}

		for _, deploy := range deployments.Items {
			pods, err := filterPodsForWorkload(podCache[ns], deploy.Spec.Selector)
			if err != nil {
				log.Error(err, "Failed to filter pods for deployment", "namespace", ns, "deployment", deploy.Name)
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
		if err := checkCtx.DataSource.List(ctx, statefulsets, client.InNamespace(ns)); err != nil {
			log.Error(err, "Failed to list statefulsets", "namespace", ns)
			continue
		}

		for _, sts := range statefulsets.Items {
			pods, err := filterPodsForWorkload(podCache[ns], sts.Spec.Selector)
			if err != nil {
				log.Error(err, "Failed to filter pods for statefulset", "namespace", ns, "statefulset", sts.Name)
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
		if err := checkCtx.DataSource.List(ctx, daemonsets, client.InNamespace(ns)); err != nil {
			log.Error(err, "Failed to list daemonsets", "namespace", ns)
			continue
		}

		for _, ds := range daemonsets.Items {
			pods, err := filterPodsForWorkload(podCache[ns], ds.Spec.Selector)
			if err != nil {
				log.Error(err, "Failed to filter pods for daemonset", "namespace", ns, "daemonset", ds.Name)
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

// filterPodsForWorkload filters cached pods by a label selector.
func filterPodsForWorkload(podList *corev1.PodList, selector *metav1.LabelSelector) ([]corev1.Pod, error) {
	labelSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, err
	}

	var matched []corev1.Pod
	for _, pod := range podList.Items {
		if labelSelector.Matches(labels.Set(pod.Labels)) {
			matched = append(matched, pod)
		}
	}
	return matched, nil
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
		threshold := int32(min(restartThreshold, math.MaxInt32)) // #nosec G115 -- bounded by min()
		if restartThreshold >= 0 && cs.RestartCount > threshold {
			issues = append(issues, checker.Issue{
				Type:      "HighRestartCount",
				Severity:  checker.SeverityWarning,
				Resource:  resourceRef,
				Namespace: namespace,
				Message:   fmt.Sprintf("Container %s has restarted %d times (threshold: %d)", cs.Name, cs.RestartCount, restartThreshold),
				Suggestion: fmt.Sprintf("Check previous logs: kubectl logs %s -c %s --previous -n %s. "+
					"Common causes: OOM kills, liveness probe failures, or application crashes. "+
					"Check exit reason: kubectl describe pod %s -n %s | grep -A5 'Last State'.",
					pod.Name, cs.Name, namespace, pod.Name, namespace),
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
					Type:      "SchedulingFailed",
					Severity:  checker.SeverityCritical,
					Resource:  resourceRef,
					Namespace: namespace,
					Message:   fmt.Sprintf("Pod %s failed to schedule: %s", pod.Name, cond.Message),
					Suggestion: "Check node capacity: kubectl describe nodes | grep -A5 'Allocated resources'. " +
						"Verify taints/tolerations: kubectl get nodes -o custom-columns=NAME:.metadata.name,TAINTS:.spec.taints. " +
						"Check pod affinity rules: kubectl get pod " + pod.Name + " -n " + namespace + " -o jsonpath='{.spec.affinity}'. " +
						"See Kubernetes docs on scheduling and eviction.",
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
			Type:      "NoMemoryLimit",
			Severity:  checker.SeverityWarning,
			Resource:  resourceRef,
			Namespace: namespace,
			Message:   fmt.Sprintf("Container %s has no memory limit set", container.Name),
			Suggestion: "Set a memory limit to prevent OOM issues and ensure fair resource sharing. " +
				"Without a limit, the container can consume all available node memory. " +
				"Check current usage: kubectl top pod <pod> -n " + namespace + ". " +
				"See Kubernetes docs on resource management for containers.",
			Metadata: metadata,
		})
	}

	// CPU limit
	if container.Resources.Limits.Cpu().IsZero() {
		issues = append(issues, checker.Issue{
			Type:      "NoCPULimit",
			Severity:  checker.SeverityInfo,
			Resource:  resourceRef,
			Namespace: namespace,
			Message:   fmt.Sprintf("Container %s has no CPU limit set", container.Name),
			Suggestion: "Consider setting CPU limits for predictable performance. " +
				"Note: some teams intentionally omit CPU limits to avoid throttling. " +
				"Check current usage: kubectl top pod <pod> -n " + namespace + ".",
			Metadata: metadata,
		})
	}

	// Resource requests
	if container.Resources.Requests.Memory().IsZero() && container.Resources.Requests.Cpu().IsZero() {
		issues = append(issues, checker.Issue{
			Type:      "NoResourceRequests",
			Severity:  checker.SeverityWarning,
			Resource:  resourceRef,
			Namespace: namespace,
			Message:   fmt.Sprintf("Container %s has no resource requests set", container.Name),
			Suggestion: "Set resource requests so the scheduler can place pods on nodes with sufficient capacity. " +
				"Without requests, pods get BestEffort QoS and are evicted first under memory pressure. " +
				"See Kubernetes docs on pod QoS classes.",
			Metadata: metadata,
		})
	}

	// Liveness probe
	if container.LivenessProbe == nil {
		issues = append(issues, checker.Issue{
			Type:      "NoLivenessProbe",
			Severity:  checker.SeverityInfo,
			Resource:  resourceRef,
			Namespace: namespace,
			Message:   fmt.Sprintf("Container %s has no liveness probe configured", container.Name),
			Suggestion: "Add a liveness probe to enable automatic container restart when the application " +
				"becomes unresponsive. Use httpGet for HTTP services or exec for CLI-based checks. " +
				"See Kubernetes docs on configure liveness, readiness and startup probes.",
			Metadata: metadata,
		})
	}

	// Readiness probe
	if container.ReadinessProbe == nil {
		issues = append(issues, checker.Issue{
			Type:      "NoReadinessProbe",
			Severity:  checker.SeverityInfo,
			Resource:  resourceRef,
			Namespace: namespace,
			Message:   fmt.Sprintf("Container %s has no readiness probe configured", container.Name),
			Suggestion: "Add a readiness probe to prevent traffic from being sent to pods that are not ready " +
				"to serve requests. This is especially important for services behind a Service/Ingress. " +
				"See Kubernetes docs on configure liveness, readiness and startup probes.",
			Metadata: metadata,
		})
	}

	return issues
}

// GetSuggestionForWaitingReason returns a suggestion based on container waiting reason
func GetSuggestionForWaitingReason(reason string) string {
	suggestions := map[string]string{
		"ImagePullBackOff": "The container image cannot be pulled. Common causes: typo in image name/tag, " +
			"missing imagePullSecrets, or private registry without credentials. " +
			"Investigate with: kubectl describe pod <pod> | grep -A5 'Events' " +
			"and kubectl get events --field-selector reason=Failed",
		"ErrImagePull": "Image pull failed. Verify the image exists: docker manifest inspect <image>:<tag>. " +
			"For private registries, ensure imagePullSecrets are configured: " +
			"kubectl get pod <pod> -o jsonpath='{.spec.imagePullSecrets}'. " +
			"See Kubernetes docs on private registries.",
		"CrashLoopBackOff": "The container is repeatedly crashing. Check recent logs with: " +
			"kubectl logs <pod> --previous. Common causes: missing config/secrets, " +
			"incorrect entrypoint, or application errors. " +
			"If OOM-related, check: kubectl describe pod <pod> | grep -i oom",
		"CreateContainerConfigError": "Container configuration is invalid. Usually a missing ConfigMap or Secret. " +
			"Check which references are broken: kubectl describe pod <pod> | grep -A3 'Warning'. " +
			"Verify referenced ConfigMaps/Secrets exist: kubectl get cm,secret -n <namespace>",
		"ContainerCreating": "Container is being created. If stuck for more than a few minutes, " +
			"check events: kubectl describe pod <pod>. Common causes: slow image pull, " +
			"volume mount issues, or node resource pressure.",
		"PodInitializing": "Init containers are running. If stuck, check init container logs: " +
			"kubectl logs <pod> -c <init-container-name>. " +
			"List init containers: kubectl get pod <pod> -o jsonpath='{.spec.initContainers[*].name}'",
	}
	if s, ok := suggestions[reason]; ok {
		return s
	}
	return "Check pod events for more details: kubectl describe pod <pod>"
}

// GetSuggestionForTerminatedReason returns a suggestion based on container termination reason
func GetSuggestionForTerminatedReason(reason string, exitCode int32) string {
	if reason == "OOMKilled" {
		return "Container exceeded its memory limit and was OOM-killed. " +
			"Check current limits: kubectl describe pod <pod> | grep -A2 'Limits'. " +
			"Increase the memory limit in the pod spec, or profile the application to reduce memory usage. " +
			"See Kubernetes docs on managing resources for containers."
	}
	if exitCode == 137 {
		return "Container was killed with SIGKILL (exit code 137). This typically indicates OOM kill or " +
			"an external signal. Check: kubectl describe pod <pod> | grep -i oom, and " +
			"kubectl top pod <pod> to see current memory usage. " +
			"Also check node memory pressure: kubectl describe node <node> | grep -A5 'Conditions'."
	}
	if exitCode == 143 {
		return "Container received SIGTERM (exit code 143). This is normal during rolling updates, " +
			"scale-downs, or pod evictions. If unexpected, check: " +
			"kubectl get events --field-selector reason=Killing -n <namespace>."
	}
	if exitCode == 1 {
		return "Application exited with error code 1. Check logs: kubectl logs <pod> --previous. " +
			"Common causes: unhandled exceptions, missing configuration, or failed startup checks."
	}
	if exitCode == 126 {
		return "Command cannot be executed (exit code 126). The entrypoint or command exists but " +
			"is not executable. Check file permissions in the container image and verify the binary format " +
			"matches the container architecture (e.g., amd64 vs arm64)."
	}
	if exitCode == 127 {
		return "Command not found (exit code 127). The entrypoint or command does not exist in the container. " +
			"Verify the image contains the expected binary: kubectl exec <pod> -- ls /path/to/binary. " +
			"Check the container's command and args fields in the pod spec."
	}
	return fmt.Sprintf("Container exited with code %d. Check application logs: kubectl logs <pod> --previous.", exitCode)
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
