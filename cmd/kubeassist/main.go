/*
kubeassist - CLI tool for Kubernetes workload diagnostics and health checks

Usage:

	kubeassist [namespace]           # Diagnose all workloads in namespace
	kubeassist health                # Run comprehensive health checks
	kubeassist -A                    # Diagnose all workloads in all namespaces
	kubeassist -l app=myapp          # Diagnose workloads matching label
	kubeassist --watch               # Continuous monitoring mode
*/
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
)

// Output format and severity constants
const (
	outputFormatJSON = "json"
	outputFormatText = "text"
	namespaceDefault = "default"
	severityCritical = "Critical"
	severityWarning  = "Warning"
)

// Version is set via ldflags at build time: -ldflags "-X main.Version=vX.Y.Z"
var Version = "dev"

var (
	allNamespaces bool
	labelSelector string
	watchMode     bool
	outputJSON    bool
	outputFormat  string
	cleanup       bool
	timeout       time.Duration
	workerCount   int
)

const (
	labelManagedBy  = "app.kubernetes.io/managed-by"
	labelTargetName = "kubeassist.io/target-name"
)

func main() {
	// Check for subcommands first
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "health":
			if err := runHealth(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "%sError: %v%s\n", colorRed, err, colorReset)
				os.Exit(1)
			}
			return
		case "help", "-h", "--help":
			printUsage()
			return
		case "version", "--version":
			fmt.Printf("kubeassist version %s\n", Version)
			return
		}
	}

	// Legacy behavior: workload diagnostics
	flag.BoolVar(&allNamespaces, "A", true, "Diagnose workloads in all namespaces (default: true)")
	flag.BoolVar(&allNamespaces, "all-namespaces", true, "Diagnose workloads in all namespaces (default: true)")
	flag.StringVar(&labelSelector, "l", "", "Label selector to filter workloads")
	flag.StringVar(&labelSelector, "selector", "", "Label selector to filter workloads")
	flag.BoolVar(&watchMode, "watch", false, "Continuous monitoring mode")
	flag.BoolVar(&watchMode, "w", false, "Continuous monitoring mode")
	flag.BoolVar(&outputJSON, "json", false, "Output as JSON (deprecated, use --output json)")
	flag.StringVar(&outputFormat, "output", "text", "Output format: text or json")
	flag.StringVar(&outputFormat, "o", "text", "Output format: text or json")
	flag.BoolVar(&cleanup, "cleanup", true, "Delete TroubleshootRequests after displaying (default: true)")
	flag.DurationVar(&timeout, "timeout", 60*time.Second, "Timeout waiting for diagnostics")
	flag.IntVar(&workerCount, "workers", 5, "Number of parallel workers for processing deployments")
	flag.Parse()

	// Support legacy --json flag
	if outputJSON {
		outputFormat = outputFormatJSON
	}

	namespace := namespaceDefault
	if flag.NArg() > 0 {
		namespace = flag.Arg(0)
	}
	if allNamespaces {
		namespace = ""
	}

	if err := run(namespace); err != nil {
		fmt.Fprintf(os.Stderr, "%sError: %v%s\n", colorRed, err, colorReset)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`kubeassist - Kubernetes workload diagnostics and health checks

Usage:
  kubeassist [OPTIONS] [NAMESPACE]    Diagnose workloads (legacy mode)
  kubeassist health [OPTIONS]         Run comprehensive health checks

Commands:
  health      Run Team Health checks across resources
  help        Show this help message
  version     Show version information

Legacy Options (workload diagnostics):
  -A, --all-namespaces    Diagnose all namespaces (default: true)
  -l, --selector          Label selector to filter workloads
  -w, --watch             Continuous monitoring mode
  -o, --output            Output format: text or json
  --timeout               Timeout waiting for diagnostics
  --cleanup               Delete requests after displaying (default: true)
  --workers               Number of parallel workers (default: 5)

Examples:
  # Diagnose all workloads
  kubeassist

  # Run comprehensive health checks
  kubeassist health

  # Health check specific namespaces
  kubeassist health -n frontend,backend

  # Health check with specific checks
  kubeassist health --checks workloads,helmreleases,secrets

Use "kubeassist health --help" for more information about health checks.
`)
}

func run(namespace string) error {
	// Load kubeconfig
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create clientset: %w", err)
	}

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Get current namespace if not specified
	if namespace == "" && !allNamespaces {
		ns, _, err := kubeConfig.Namespace()
		if err == nil && ns != "" {
			namespace = ns
		} else {
			namespace = namespaceDefault
		}
	}

	if outputFormat != outputFormatJSON {
		printHeader(namespace)
	}

	// List deployments
	listOpts := metav1.ListOptions{}
	if labelSelector != "" {
		listOpts.LabelSelector = labelSelector
	}

	var namespaces []string
	if allNamespaces {
		nsList, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("failed to list namespaces: %w", err)
		}
		for _, ns := range nsList.Items {
			// Skip Kubernetes system namespaces (but not kube-assist-*)
			if ns.Name == "kube-system" || ns.Name == "kube-public" || ns.Name == "kube-node-lease" || ns.Name == "flux-system" {
				continue
			}
			namespaces = append(namespaces, ns.Name)
		}
	} else {
		namespaces = []string{namespace}
	}

	trGVR := schema.GroupVersionResource{
		Group:    "assist.cluster.local",
		Version:  "v1alpha1",
		Resource: "troubleshootrequests",
	}

	// Collect all deployments to process
	type workItem struct {
		namespace string
		name      string
	}
	var workItems []workItem

	for _, ns := range namespaces {
		// Get deployments
		deployments, err := clientset.AppsV1().Deployments(ns).List(ctx, listOpts)
		if err != nil {
			if outputFormat != "json" {
				fmt.Fprintf(os.Stderr, "%sWarning: failed to list deployments in %s: %v%s\n", colorYellow, ns, err, colorReset)
			}
			continue
		}

		for _, deploy := range deployments.Items {
			workItems = append(workItems, workItem{namespace: ns, name: deploy.Name})
		}
	}

	// Set up channels for worker pool
	jobs := make(chan workItem, len(workItems))
	results := make(chan diagResult, len(workItems))

	// Start workers
	var wg sync.WaitGroup
	for range workerCount {
		wg.Go(func() {
			for job := range jobs {
				result := processDiagnostic(ctx, dynClient, trGVR, job.namespace, job.name, cleanup, outputFormat)
				results <- result
			}
		})
	}

	// Send work
	for _, item := range workItems {
		jobs <- item
	}
	close(jobs)

	// Wait for all workers and close results
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	allResults := make([]diagResult, 0, len(workItems))
	for result := range results {
		allResults = append(allResults, result)
	}

	// Sort results: unhealthy first, then by namespace/name
	sort.Slice(allResults, func(i, j int) bool {
		if allResults[i].healthy != allResults[j].healthy {
			return !allResults[i].healthy // unhealthy first
		}
		if allResults[i].critical != allResults[j].critical {
			return allResults[i].critical > allResults[j].critical
		}
		if allResults[i].namespace != allResults[j].namespace {
			return allResults[i].namespace < allResults[j].namespace
		}
		return allResults[i].name < allResults[j].name
	})

	if outputFormat == outputFormatJSON {
		printJSONResults(allResults)
	} else {
		printResults(allResults)
		printSummary(allResults)
	}

	return nil
}

// processDiagnostic creates a TroubleshootRequest and waits for its result
func processDiagnostic(
	ctx context.Context, dynClient dynamic.Interface, gvr schema.GroupVersionResource,
	namespace, deployName string, doCleanup bool, format string,
) diagResult {
	trName := fmt.Sprintf("kubeassist-%s-%d", deployName, time.Now().UnixNano())

	tr := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "assist.cluster.local/v1alpha1",
			"kind":       "TroubleshootRequest",
			"metadata": map[string]any{
				"name":      trName,
				"namespace": namespace,
				"labels": map[string]any{
					labelManagedBy:  "kubeassist-cli",
					labelTargetName: deployName,
				},
			},
			"spec": map[string]any{
				"target": map[string]any{
					"kind": "Deployment",
					"name": deployName,
				},
				"actions":   []any{"all"},
				"tailLines": int64(50),
			},
		},
	}

	_, err := dynClient.Resource(gvr).Namespace(namespace).Create(ctx, tr, metav1.CreateOptions{})
	if err != nil {
		if format != outputFormatJSON {
			fmt.Fprintf(os.Stderr, "%sWarning: failed to create diagnostic for %s/%s: %v%s\n",
				colorYellow, namespace, deployName, err, colorReset)
		}
		return diagResult{
			namespace: namespace,
			name:      deployName,
			phase:     "Error",
			summary:   fmt.Sprintf("failed to create diagnostic: %v", err),
			healthy:   false,
		}
	}

	// Wait for completion
	result, err := waitForDiagnostic(ctx, dynClient, gvr, namespace, trName, deployName)
	if err != nil {
		result = diagResult{
			namespace: namespace,
			name:      deployName,
			phase:     "Error",
			summary:   err.Error(),
			healthy:   false,
		}
	}

	// Cleanup if requested
	if doCleanup {
		_ = dynClient.Resource(gvr).Namespace(namespace).Delete(ctx, trName, metav1.DeleteOptions{})
	}

	return result
}

type diagResult struct {
	namespace string
	name      string
	phase     string
	summary   string
	issues    []issue
	healthy   bool
	critical  int
	warnings  int
}

type issue struct {
	Type       string
	Severity   string
	Message    string
	Suggestion string
}

func waitForDiagnostic(
	ctx context.Context, cl dynamic.Interface, gvr schema.GroupVersionResource,
	namespace, trName, deployName string,
) (diagResult, error) {
	result := diagResult{
		namespace: namespace,
		name:      deployName,
	}

	watcher, err := cl.Resource(gvr).Namespace(namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("metadata.name=%s", trName),
	})
	if err != nil {
		return result, err
	}
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return result, fmt.Errorf("timeout waiting for diagnostic")
		case event := <-watcher.ResultChan():
			if event.Type == watch.Error {
				return result, fmt.Errorf("watch error")
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
					result.phase = phase
					result.summary, _, _ = unstructured.NestedString(status, "summary")

					issuesRaw, _, _ := unstructured.NestedSlice(status, "issues")
					for _, issueRaw := range issuesRaw {
						issueMap, ok := issueRaw.(map[string]any)
						if !ok {
							continue
						}
						iss := issue{
							Type:       getString(issueMap, "type"),
							Severity:   getString(issueMap, "severity"),
							Message:    getString(issueMap, "message"),
							Suggestion: getString(issueMap, "suggestion"),
						}
						result.issues = append(result.issues, iss)
						switch iss.Severity {
						case severityCritical:
							result.critical++
						case severityWarning:
							result.warnings++
						}
					}

					result.healthy = len(result.issues) == 0 || (result.critical == 0 && result.warnings == 0)
					return result, nil
				}
			}
		}
	}
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// JSON output structures
type jsonResult struct {
	Namespace string      `json:"namespace"`
	Name      string      `json:"name"`
	Healthy   bool        `json:"healthy"`
	Phase     string      `json:"phase"`
	Summary   string      `json:"summary,omitempty"`
	Critical  int         `json:"critical"`
	Warnings  int         `json:"warnings"`
	Issues    []jsonIssue `json:"issues,omitempty"`
}

type jsonIssue struct {
	Type       string `json:"type"`
	Severity   string `json:"severity"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

func printJSONResults(results []diagResult) {
	jsonResults := make([]jsonResult, 0, len(results))
	for _, r := range results {
		jr := jsonResult{
			Namespace: r.namespace,
			Name:      r.name,
			Healthy:   r.healthy,
			Phase:     r.phase,
			Summary:   r.summary,
			Critical:  r.critical,
			Warnings:  r.warnings,
		}
		for _, iss := range r.issues {
			jr.Issues = append(jr.Issues, jsonIssue(iss))
		}
		jsonResults = append(jsonResults, jr)
	}

	output, err := json.MarshalIndent(jsonResults, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
		return
	}
	fmt.Println(string(output))
}

func printHeader(namespace string) {
	boxLine := "══════════════════════════════════════════════════════════════"
	fmt.Printf("\n%s%s╔%s╗%s\n", colorBold, colorCyan, boxLine, colorReset)
	fmt.Printf("%s%s║              🔍 KubeAssist Workload Diagnostics              ║%s\n",
		colorBold, colorCyan, colorReset)
	fmt.Printf("%s%s╚%s╝%s\n", colorBold, colorCyan, boxLine, colorReset)
	if namespace != "" {
		fmt.Printf("%sNamespace: %s%s\n", colorDim, namespace, colorReset)
	} else {
		fmt.Printf("%sScanning all namespaces...%s\n", colorDim, colorReset)
	}
	fmt.Println()
}

func printResults(results []diagResult) {
	for _, r := range results {
		var statusIcon, statusColor string
		if r.healthy {
			statusIcon = "✓"
			statusColor = colorGreen
		} else if r.critical > 0 {
			statusIcon = "✗"
			statusColor = colorRed
		} else {
			statusIcon = "⚠"
			statusColor = colorYellow
		}

		// Print workload header
		fmt.Printf("%s%s %s%s/%s%s\n", statusColor, statusIcon, colorBold, r.namespace, r.name, colorReset)

		if r.healthy {
			fmt.Printf("   %s%s%s\n", colorGreen, r.summary, colorReset)
		} else {
			// Print issues
			for _, iss := range r.issues {
				var sevColor string
				var sevIcon string
				switch iss.Severity {
				case "Critical":
					sevColor = colorRed
					sevIcon = "●"
				case "Warning":
					sevColor = colorYellow
					sevIcon = "○"
				default:
					sevColor = colorBlue
					sevIcon = "·"
				}

				fmt.Printf("   %s%s [%s]%s %s\n", sevColor, sevIcon, iss.Severity, colorReset, iss.Message)
				if iss.Suggestion != "" {
					fmt.Printf("     %s→ %s%s\n", colorDim, iss.Suggestion, colorReset)
				}
			}
		}
		fmt.Println()
	}
}

func printSummary(results []diagResult) {
	healthy := 0
	unhealthy := 0
	totalCritical := 0
	totalWarnings := 0

	for _, r := range results {
		if r.healthy {
			healthy++
		} else {
			unhealthy++
		}
		totalCritical += r.critical
		totalWarnings += r.warnings
	}

	fmt.Printf("%s────────────────────────────────────────────────────────────────%s\n", colorDim, colorReset)
	fmt.Printf("%sSummary:%s ", colorBold, colorReset)

	if unhealthy == 0 {
		fmt.Printf("%s✓ All %d workloads healthy%s\n", colorGreen, healthy, colorReset)
	} else {
		fmt.Printf("%s%d healthy%s, ", colorGreen, healthy, colorReset)
		fmt.Printf("%s%d unhealthy%s ", colorRed, unhealthy, colorReset)
		fmt.Printf("(%s%d critical%s, %s%d warnings%s)\n",
			colorRed, totalCritical, colorReset,
			colorYellow, totalWarnings, colorReset)
	}
	fmt.Println()
}
