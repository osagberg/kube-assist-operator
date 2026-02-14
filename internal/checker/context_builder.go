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

package checker

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/osagberg/kube-assist-operator/internal/ai"
	"github.com/osagberg/kube-assist-operator/internal/datasource"
)

// LogContextConfig controls event/log enrichment for AI context.
type LogContextConfig struct {
	Enabled           bool
	MaxEventsPerIssue int // default 10
	MaxLogLines       int // default 50
	MaxTotalChars     int // default 30000 bytes (~7500 tokens)
}

// ContextBuilder enriches IssueContext slices with events and pod logs.
type ContextBuilder struct {
	ds        datasource.DataSource
	clientset kubernetes.Interface // nil = skip logs
	config    LogContextConfig
}

// NewContextBuilder creates a ContextBuilder for enriching AI issue context.
func NewContextBuilder(ds datasource.DataSource, clientset kubernetes.Interface, config LogContextConfig) *ContextBuilder {
	return &ContextBuilder{
		ds:        ds,
		clientset: clientset,
		config:    config,
	}
}

// rsHashSuffix matches the standard Kubernetes ReplicaSet hash suffix.
// ReplicaSet names follow the pattern: {deployment-name}-{8-10 char alphanumeric hash}
var rsHashSuffix = regexp.MustCompile(`-[a-z0-9]{8,10}$`)

// crashTypes are issue types that indicate a pod has crashed and may have
// useful previous-container logs.
var crashTypes = map[string]bool{
	"CrashLoopBackOff": true,
	"OOMKilled":        true,
	"Error":            true,
}

// Enrich populates Events and Logs on each IssueContext. Errors during
// event/log fetching are logged but never returned — the method always
// succeeds, just with potentially empty Events/Logs.
func (b *ContextBuilder) Enrich(ctx context.Context, issues []ai.IssueContext) []ai.IssueContext {
	if !b.config.Enabled || len(issues) == 0 {
		return issues
	}

	maxEvents := b.config.MaxEventsPerIssue
	if maxEvents <= 0 {
		maxEvents = 10
	}
	maxLines := b.config.MaxLogLines
	if maxLines <= 0 {
		maxLines = 50
	}

	for i := range issues {
		b.enrichEvents(ctx, &issues[i], maxEvents)
		b.enrichLogs(ctx, &issues[i], maxLines)
	}

	issues = b.capTokenBudget(issues)
	return issues
}

// enrichEvents fetches recent events for the issue's resource and populates
// IssueContext.Events.
func (b *ContextBuilder) enrichEvents(ctx context.Context, issue *ai.IssueContext, maxEvents int) {
	kind, name := parseResource(issue.Resource)
	if kind == "" || name == "" {
		return
	}

	eventList := &corev1.EventList{}
	if err := b.ds.List(ctx, eventList, client.InNamespace(issue.Namespace)); err != nil {
		log.V(1).Info("Failed to list events for context enrichment",
			"namespace", issue.Namespace, "resource", issue.Resource, "error", err)
		return
	}

	// Filter events matching the resource kind and name
	var matching []corev1.Event
	for _, ev := range eventList.Items {
		if strings.EqualFold(ev.InvolvedObject.Kind, kind) && ev.InvolvedObject.Name == name {
			matching = append(matching, ev)
		}
	}

	// Sort by LastTimestamp descending (most recent first)
	sort.Slice(matching, func(i, j int) bool {
		ti := eventTime(matching[i])
		tj := eventTime(matching[j])
		return ti.After(tj)
	})

	// Take up to maxEvents
	if len(matching) > maxEvents {
		matching = matching[:maxEvents]
	}

	events := make([]string, 0, len(matching))
	for _, ev := range matching {
		ts := eventTime(ev)
		events = append(events, fmt.Sprintf("%s: %s (%s)", ev.Reason, ev.Message, ts.Format(time.RFC3339)))
	}
	issue.Events = events
}

// eventTime returns the best available timestamp for an event.
func eventTime(ev corev1.Event) time.Time {
	if !ev.LastTimestamp.IsZero() {
		return ev.LastTimestamp.Time
	}
	if ev.EventTime.Time.IsZero() {
		return ev.CreationTimestamp.Time
	}
	return ev.EventTime.Time
}

// enrichLogs fetches pod logs for crash-type issues and populates
// IssueContext.Logs.
func (b *ContextBuilder) enrichLogs(ctx context.Context, issue *ai.IssueContext, maxLines int) {
	if b.clientset == nil {
		return
	}
	if !crashTypes[issue.Type] {
		return
	}

	kind, name := parseResource(issue.Resource)
	if kind == "" || name == "" {
		return
	}

	// If the resource is a pod, fetch logs directly. Otherwise, find owned pods.
	var podNames []string
	switch strings.ToLower(kind) {
	case "pod":
		podNames = []string{name}
	case "deployment", "statefulset", "daemonset", "replicaset":
		podNames = b.findOwnedPods(ctx, issue.Namespace, kind, name)
	default:
		return
	}

	if len(podNames) == 0 {
		return
	}

	// Fetch logs from the first pod (usually the most relevant)
	podName := podNames[0]
	tailLines := int64(maxLines)
	logOpts := &corev1.PodLogOptions{
		TailLines: &tailLines,
		Previous:  true, // Get the crashed container's logs
	}

	req := b.clientset.CoreV1().Pods(issue.Namespace).GetLogs(podName, logOpts)
	stream, err := req.Stream(ctx)
	if err != nil {
		// Previous logs may not exist, try current logs
		logOpts.Previous = false
		req = b.clientset.CoreV1().Pods(issue.Namespace).GetLogs(podName, logOpts)
		stream, err = req.Stream(ctx)
		if err != nil {
			log.V(1).Info("Failed to fetch pod logs for context enrichment",
				"pod", podName, "namespace", issue.Namespace, "error", err)
			return
		}
	}
	defer func() { _ = stream.Close() }()

	lines := readLines(stream, maxLines)
	issue.Logs = lines
}

// findOwnedPods lists pods in a namespace and returns names of those owned by
// the given workload resource.
func (b *ContextBuilder) findOwnedPods(ctx context.Context, namespace, kind, name string) []string {
	podList := &corev1.PodList{}
	if err := b.ds.List(ctx, podList, client.InNamespace(namespace)); err != nil {
		return nil
	}

	var podNames []string
	for _, pod := range podList.Items {
		for _, ref := range pod.OwnerReferences {
			if strings.EqualFold(ref.Kind, kind) && ref.Name == name {
				podNames = append(podNames, pod.Name)
				break
			}
			// Deployments own ReplicaSets, not pods directly. Strip the
			// standard hash suffix to recover the deployment name.
			if strings.EqualFold(kind, "deployment") && strings.EqualFold(ref.Kind, "ReplicaSet") {
				deployName := rsHashSuffix.ReplaceAllString(ref.Name, "")
				if deployName == name {
					podNames = append(podNames, pod.Name)
					break
				}
			}
		}
	}
	return podNames
}

// readLines reads up to maxLines lines from a reader.
func readLines(r io.Reader, maxLines int) []string {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // Allow up to 1MB lines
	var lines []string
	for scanner.Scan() && len(lines) < maxLines {
		lines = append(lines, scanner.Text())
	}
	return lines
}

// parseResource splits a resource string like "deployment/my-app" into
// kind and name. Returns empty strings if the format is unexpected.
func parseResource(resource string) (kind, name string) {
	parts := strings.SplitN(resource, "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

// capTokenBudget trims Events and Logs across all issues so the total
// byte count stays within MaxTotalChars.
func (b *ContextBuilder) capTokenBudget(issues []ai.IssueContext) []ai.IssueContext {
	maxChars := b.config.MaxTotalChars
	if maxChars <= 0 {
		maxChars = 30000
	}

	total := contextChars(issues)
	if total <= maxChars {
		return issues
	}

	// First pass: trim oldest events (from the end of each Events slice)
	for total > maxChars {
		trimmed := false
		for i := range issues {
			if len(issues[i].Events) > 1 {
				removed := issues[i].Events[len(issues[i].Events)-1]
				issues[i].Events = issues[i].Events[:len(issues[i].Events)-1]
				total -= len(removed)
				trimmed = true
				if total <= maxChars {
					return issues
				}
			}
		}
		if !trimmed {
			break
		}
	}

	// Second pass: truncate log lines (from the beginning — oldest lines)
	for total > maxChars {
		trimmed := false
		for i := range issues {
			if len(issues[i].Logs) > 1 {
				removed := issues[i].Logs[0]
				issues[i].Logs = issues[i].Logs[1:]
				total -= len(removed)
				trimmed = true
				if total <= maxChars {
					return issues
				}
			}
		}
		if !trimmed {
			break
		}
	}

	// Final pass: clear remaining single-entry slices if still over budget
	for total > maxChars {
		cleared := false
		for i := range issues {
			if len(issues[i].Events) > 0 {
				for _, e := range issues[i].Events {
					total -= len(e)
				}
				issues[i].Events = nil
				cleared = true
				if total <= maxChars {
					return issues
				}
			}
			if len(issues[i].Logs) > 0 {
				for _, l := range issues[i].Logs {
					total -= len(l)
				}
				issues[i].Logs = nil
				cleared = true
				if total <= maxChars {
					return issues
				}
			}
		}
		if !cleared {
			break
		}
	}

	return issues
}

// contextChars returns the total character count of all Events and Logs
// across all issues.
func contextChars(issues []ai.IssueContext) int {
	total := 0
	for _, issue := range issues {
		for _, e := range issue.Events {
			total += len(e)
		}
		for _, l := range issue.Logs {
			total += len(l)
		}
	}
	return total
}
