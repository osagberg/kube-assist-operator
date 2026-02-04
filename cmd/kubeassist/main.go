/*
kubeassist - CLI tool for quick workload diagnostics

Usage:

	kubeassist [namespace]           # Diagnose all workloads in namespace
	kubeassist -A                    # Diagnose all workloads in all namespaces
	kubeassist -l app=myapp          # Diagnose workloads matching label
	kubeassist --watch               # Continuous monitoring mode
*/
package main

import (
	"context"
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

var (
	allNamespaces bool
	labelSelector string
	watchMode     bool
	outputJSON    bool
	cleanup       bool
	timeout       time.Duration
)

func main() {
	flag.BoolVar(&allNamespaces, "A", true, "Diagnose workloads in all namespaces (default: true)")
	flag.BoolVar(&allNamespaces, "all-namespaces", true, "Diagnose workloads in all namespaces (default: true)")
	flag.StringVar(&labelSelector, "l", "", "Label selector to filter workloads")
	flag.StringVar(&labelSelector, "selector", "", "Label selector to filter workloads")
	flag.BoolVar(&watchMode, "watch", false, "Continuous monitoring mode")
	flag.BoolVar(&watchMode, "w", false, "Continuous monitoring mode")
	flag.BoolVar(&outputJSON, "json", false, "Output as JSON")
	flag.BoolVar(&cleanup, "cleanup", true, "Delete TroubleshootRequests after displaying (default: true)")
	flag.DurationVar(&timeout, "timeout", 60*time.Second, "Timeout waiting for diagnostics")
	flag.Parse()

	namespace := "default"
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
			namespace = "default"
		}
	}

	printHeader(namespace)

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

	var allResults []diagResult

	for _, ns := range namespaces {
		// Get deployments
		deployments, err := clientset.AppsV1().Deployments(ns).List(ctx, listOpts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%sWarning: failed to list deployments in %s: %v%s\n", colorYellow, ns, err, colorReset)
			continue
		}

		for _, deploy := range deployments.Items {
			// Create TroubleshootRequest
			trName := fmt.Sprintf("kubeassist-%s-%d", deploy.Name, time.Now().Unix())

			tr := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "assist.cluster.local/v1alpha1",
					"kind":       "TroubleshootRequest",
					"metadata": map[string]interface{}{
						"name":      trName,
						"namespace": ns,
						"labels": map[string]interface{}{
							"app.kubernetes.io/managed-by": "kubeassist-cli",
						},
					},
					"spec": map[string]interface{}{
						"target": map[string]interface{}{
							"kind": "Deployment",
							"name": deploy.Name,
						},
						"actions":   []interface{}{"all"},
						"tailLines": int64(50),
					},
				},
			}

			_, err := dynClient.Resource(trGVR).Namespace(ns).Create(ctx, tr, metav1.CreateOptions{})
			if err != nil {
				fmt.Fprintf(os.Stderr, "%sWarning: failed to create diagnostic for %s/%s: %v%s\n",
					colorYellow, ns, deploy.Name, err, colorReset)
				continue
			}

			// Wait for completion
			result, err := waitForDiagnostic(ctx, dynClient, trGVR, ns, trName)
			if err != nil {
				result = diagResult{
					namespace: ns,
					name:      deploy.Name,
					phase:     "Error",
					summary:   err.Error(),
					issues:    nil,
					healthy:   false,
					critical:  0,
					warnings:  0,
				}
			}

			allResults = append(allResults, result)

			// Cleanup if requested
			if cleanup {
				_ = dynClient.Resource(trGVR).Namespace(ns).Delete(ctx, trName, metav1.DeleteOptions{})
			}
		}
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

	printResults(allResults)
	printSummary(allResults)

	return nil
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

func waitForDiagnostic(ctx context.Context, client dynamic.Interface, gvr schema.GroupVersionResource, namespace, name string) (diagResult, error) {
	result := diagResult{
		namespace: namespace,
		name:      strings.TrimPrefix(name, "kubeassist-"),
	}
	// Extract actual deployment name
	parts := strings.Split(result.name, "-")
	if len(parts) > 1 {
		result.name = strings.Join(parts[:len(parts)-1], "-")
	}

	watcher, err := client.Resource(gvr).Namespace(namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("metadata.name=%s", name),
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
			if event.Type == watch.Modified || event.Type == watch.Added {
				obj := event.Object.(*unstructured.Unstructured)
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
						issueMap := issueRaw.(map[string]interface{})
						iss := issue{
							Type:       getString(issueMap, "type"),
							Severity:   getString(issueMap, "severity"),
							Message:    getString(issueMap, "message"),
							Suggestion: getString(issueMap, "suggestion"),
						}
						result.issues = append(result.issues, iss)
						if iss.Severity == "Critical" {
							result.critical++
						} else if iss.Severity == "Warning" {
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

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func printHeader(namespace string) {
	fmt.Printf("\n%s%s╔══════════════════════════════════════════════════════════════╗%s\n", colorBold, colorCyan, colorReset)
	fmt.Printf("%s%s║              🔍 KubeAssist Workload Diagnostics              ║%s\n", colorBold, colorCyan, colorReset)
	fmt.Printf("%s%s╚══════════════════════════════════════════════════════════════╝%s\n", colorBold, colorCyan, colorReset)
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
