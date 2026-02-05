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

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

// healthCmd implements the "kubeassist health" subcommand
type healthCmd struct {
	namespaces        string
	namespaceSelector string
	checks            string
	outputFormat      string
	cleanup           bool
	timeout           time.Duration
}

func newHealthCmd() *healthCmd {
	return &healthCmd{}
}

func (h *healthCmd) bindFlags(fs *flag.FlagSet) {
	fs.StringVar(&h.namespaces, "n", "", "Comma-separated list of namespaces to check (default: current namespace)")
	fs.StringVar(&h.namespaces, "namespaces", "", "Comma-separated list of namespaces to check")
	fs.StringVar(&h.namespaceSelector, "namespace-selector", "", "Label selector for namespaces (e.g., team=frontend)")
	fs.StringVar(&h.checks, "checks", "", "Comma-separated list of checks to run (default: all)")
	fs.StringVar(&h.outputFormat, "o", "text", "Output format: text or json")
	fs.StringVar(&h.outputFormat, "output", "text", "Output format: text or json")
	fs.BoolVar(&h.cleanup, "cleanup", true, "Delete TeamHealthRequest after displaying results")
	fs.DurationVar(&h.timeout, "timeout", 120*time.Second, "Timeout waiting for health check")
}

func (h *healthCmd) run() error {
	// Load kubeconfig
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
	defer cancel()

	// Get current namespace for creating the TeamHealthRequest
	currentNS, _, err := kubeConfig.Namespace()
	if err != nil || currentNS == "" {
		currentNS = "default"
	}

	if h.outputFormat != outputFormatJSON {
		h.printHeader()
	}

	thrGVR := schema.GroupVersionResource{
		Group:    "assist.cluster.local",
		Version:  "v1alpha1",
		Resource: "teamhealthrequests",
	}

	// Create TeamHealthRequest
	thrName := fmt.Sprintf("kubeassist-health-%d", time.Now().UnixNano())
	spec := h.buildSpec()

	thr := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "assist.cluster.local/v1alpha1",
			"kind":       "TeamHealthRequest",
			"metadata": map[string]any{
				"name":      thrName,
				"namespace": currentNS,
				"labels": map[string]any{
					"app.kubernetes.io/managed-by": "kubeassist-cli",
				},
			},
			"spec": spec,
		},
	}

	_, err = dynClient.Resource(thrGVR).Namespace(currentNS).Create(ctx, thr, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create TeamHealthRequest: %w", err)
	}

	// Wait for completion and get results
	result, err := h.waitForHealthCheck(ctx, dynClient, thrGVR, currentNS, thrName)
	if err != nil {
		return err
	}

	// Cleanup if requested
	if h.cleanup {
		_ = dynClient.Resource(thrGVR).Namespace(currentNS).Delete(ctx, thrName, metav1.DeleteOptions{})
	}

	// Output results
	if h.outputFormat == outputFormatJSON {
		return h.printJSONResults(result)
	}
	h.printTextResults(result)
	return nil
}

func (h *healthCmd) buildSpec() map[string]any {
	spec := map[string]any{}

	// Build scope
	scope := map[string]any{}
	if h.namespaceSelector != "" {
		// Parse label selector
		selector := map[string]any{}
		labels := map[string]any{}
		for part := range strings.SplitSeq(h.namespaceSelector, ",") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) == 2 {
				labels[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
			}
		}
		if len(labels) > 0 {
			selector["matchLabels"] = labels
			scope["namespaceSelector"] = selector
		}
	} else if h.namespaces != "" {
		scope["namespaces"] = strings.Split(h.namespaces, ",")
	} else {
		scope["currentNamespace"] = true
	}
	spec["scope"] = scope

	// Build checks list if specified
	if h.checks != "" {
		checkList := []any{}
		for c := range strings.SplitSeq(h.checks, ",") {
			checkList = append(checkList, strings.TrimSpace(c))
		}
		spec["checks"] = checkList
	}

	return spec
}

type healthResult struct {
	Phase          string
	Summary        string
	LastCheck      string
	CheckerResults map[string]checkerResult
}

type checkerResult struct {
	Healthy int
	Issues  []healthIssue
}

type healthIssue struct {
	Type       string
	Severity   string
	Resource   string
	Namespace  string
	Message    string
	Suggestion string
}

func (h *healthCmd) waitForHealthCheck(
	ctx context.Context, cl dynamic.Interface, gvr schema.GroupVersionResource, namespace, name string,
) (*healthResult, error) {
	watcher, err := cl.Resource(gvr).Namespace(namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("metadata.name=%s", name),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to watch TeamHealthRequest: %w", err)
	}
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for health check")
		case event := <-watcher.ResultChan():
			if event.Type == watch.Error {
				return nil, fmt.Errorf("watch error")
			}
			if event.Type == watch.Modified || event.Type == watch.Added {
				obj, ok := event.Object.(*unstructured.Unstructured)
				if !ok {
					continue
				}
				status, found, _ := unstructured.NestedMap(obj.Object, "status")
				if !found {
					continue
				}

				phase, _, _ := unstructured.NestedString(status, "phase")
				if phase == "Completed" || phase == "Failed" {
					return h.parseStatus(status), nil
				}
			}
		}
	}
}

func (h *healthCmd) parseStatus(status map[string]any) *healthResult {
	result := &healthResult{
		CheckerResults: make(map[string]checkerResult),
	}

	result.Phase, _, _ = unstructured.NestedString(status, "phase")
	result.Summary, _, _ = unstructured.NestedString(status, "summary")
	result.LastCheck, _, _ = unstructured.NestedString(status, "lastCheckTime")

	// Parse results by checker
	results, _, _ := unstructured.NestedMap(status, "results")
	for checkerName, checkerData := range results {
		checkerMap, ok := checkerData.(map[string]any)
		if !ok {
			continue
		}

		cr := checkerResult{}
		healthy, _, _ := unstructured.NestedInt64(checkerMap, "healthy")
		cr.Healthy = int(healthy)

		issuesRaw, _, _ := unstructured.NestedSlice(checkerMap, "issues")
		for _, issueRaw := range issuesRaw {
			issueMap, ok := issueRaw.(map[string]any)
			if !ok {
				continue
			}

			issue := healthIssue{
				Type:       getStringFromMap(issueMap, "type"),
				Severity:   getStringFromMap(issueMap, "severity"),
				Resource:   getStringFromMap(issueMap, "resource"),
				Namespace:  getStringFromMap(issueMap, "namespace"),
				Message:    getStringFromMap(issueMap, "message"),
				Suggestion: getStringFromMap(issueMap, "suggestion"),
			}
			cr.Issues = append(cr.Issues, issue)
		}

		result.CheckerResults[checkerName] = cr
	}

	return result
}

func getStringFromMap(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func (h *healthCmd) printHeader() {
	boxLine := "══════════════════════════════════════════════════════════════"
	fmt.Printf("\n%s%s╔%s╗%s\n", colorBold, colorCyan, boxLine, colorReset)
	fmt.Printf("%s%s║              🏥 KubeAssist Team Health Dashboard             ║%s\n",
		colorBold, colorCyan, colorReset)
	fmt.Printf("%s%s╚%s╝%s\n", colorBold, colorCyan, boxLine, colorReset)
	fmt.Println()
}

func (h *healthCmd) printTextResults(result *healthResult) {
	// Print summary
	fmt.Printf("%sSummary:%s %s\n", colorBold, colorReset, result.Summary)
	if result.LastCheck != "" {
		fmt.Printf("%sLast Check:%s %s\n", colorDim, colorReset, result.LastCheck)
	}
	fmt.Println()

	// Get sorted checker names
	checkerNames := make([]string, 0, len(result.CheckerResults))
	for name := range result.CheckerResults {
		checkerNames = append(checkerNames, name)
	}
	sort.Strings(checkerNames)

	// Track total issues
	totalCritical := 0
	totalWarnings := 0
	totalHealthy := 0

	for _, checkerName := range checkerNames {
		cr := result.CheckerResults[checkerName]
		totalHealthy += cr.Healthy

		// Count severity
		critical := 0
		warnings := 0
		for _, issue := range cr.Issues {
			switch issue.Severity {
			case severityCritical:
				critical++
				totalCritical++
			case severityWarning:
				warnings++
				totalWarnings++
			}
		}

		// Print checker header
		var statusIcon, statusColor string
		if len(cr.Issues) == 0 {
			statusIcon = "✓"
			statusColor = colorGreen
		} else if critical > 0 {
			statusIcon = "✗"
			statusColor = colorRed
		} else {
			statusIcon = "⚠"
			statusColor = colorYellow
		}

		fmt.Printf("%s%s%s %s%s%s ", statusColor, colorBold, statusIcon, checkerName, colorReset, colorReset)
		fmt.Printf("(%s%d healthy%s", colorGreen, cr.Healthy, colorReset)
		if critical > 0 {
			fmt.Printf(", %s%d critical%s", colorRed, critical, colorReset)
		}
		if warnings > 0 {
			fmt.Printf(", %s%d warnings%s", colorYellow, warnings, colorReset)
		}
		fmt.Println(")")

		// Print issues
		for _, issue := range cr.Issues {
			var sevColor, sevIcon string
			switch issue.Severity {
			case severityCritical:
				sevColor = colorRed
				sevIcon = "●"
			case severityWarning:
				sevColor = colorYellow
				sevIcon = "○"
			default:
				sevColor = colorBlue
				sevIcon = "·"
			}

			// Include namespace if available
			resourceStr := issue.Resource
			if issue.Namespace != "" {
				resourceStr = fmt.Sprintf("%s/%s", issue.Namespace, issue.Resource)
			}

			fmt.Printf("   %s%s [%s]%s %s\n", sevColor, sevIcon, issue.Severity, colorReset, resourceStr)
			fmt.Printf("     %s%s\n", colorReset, issue.Message)
			if issue.Suggestion != "" {
				fmt.Printf("     %s→ %s%s\n", colorDim, issue.Suggestion, colorReset)
			}
		}
		fmt.Println()
	}

	// Print overall summary
	fmt.Printf("%s────────────────────────────────────────────────────────────────%s\n", colorDim, colorReset)
	fmt.Printf("%sTotal:%s %s%d healthy%s", colorBold, colorReset, colorGreen, totalHealthy, colorReset)
	if totalCritical > 0 || totalWarnings > 0 {
		fmt.Printf(", %s%d critical%s, %s%d warnings%s",
			colorRed, totalCritical, colorReset,
			colorYellow, totalWarnings, colorReset)
	}
	fmt.Println()
	fmt.Println()
}

// JSON output types
type jsonHealthResult struct {
	Phase     string                       `json:"phase"`
	Summary   string                       `json:"summary"`
	LastCheck string                       `json:"lastCheck,omitempty"`
	Results   map[string]jsonCheckerResult `json:"results"`
	Totals    jsonTotals                   `json:"totals"`
}

type jsonCheckerResult struct {
	Healthy int               `json:"healthy"`
	Issues  []jsonHealthIssue `json:"issues,omitempty"`
}

type jsonHealthIssue struct {
	Type       string `json:"type"`
	Severity   string `json:"severity"`
	Resource   string `json:"resource"`
	Namespace  string `json:"namespace,omitempty"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

type jsonTotals struct {
	Healthy  int `json:"healthy"`
	Critical int `json:"critical"`
	Warnings int `json:"warnings"`
}

func (h *healthCmd) printJSONResults(result *healthResult) error {
	output := jsonHealthResult{
		Phase:     result.Phase,
		Summary:   result.Summary,
		LastCheck: result.LastCheck,
		Results:   make(map[string]jsonCheckerResult),
	}

	for checkerName, cr := range result.CheckerResults {
		jcr := jsonCheckerResult{
			Healthy: cr.Healthy,
		}
		output.Totals.Healthy += cr.Healthy

		for _, issue := range cr.Issues {
			jcr.Issues = append(jcr.Issues, jsonHealthIssue(issue))

			switch issue.Severity {
			case severityCritical:
				output.Totals.Critical++
			case severityWarning:
				output.Totals.Warnings++
			}
		}

		output.Results[checkerName] = jcr
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

// runHealth is the entry point for the health subcommand
func runHealth(args []string) error {
	cmd := newHealthCmd()
	fs := flag.NewFlagSet("health", flag.ExitOnError)
	cmd.bindFlags(fs)

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: kubeassist health [OPTIONS]

Run comprehensive health checks across your Kubernetes resources.

Options:
`)
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Examples:
  # Health check for current namespace
  kubeassist health

  # Health check for specific namespaces
  kubeassist health -n team-frontend,team-backend

  # Health check for namespaces matching a label selector
  kubeassist health --namespace-selector team=frontend

  # Run only specific checks
  kubeassist health --checks workloads,helmreleases,secrets

  # Output as JSON
  kubeassist health -o json
`)
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	return cmd.run()
}
