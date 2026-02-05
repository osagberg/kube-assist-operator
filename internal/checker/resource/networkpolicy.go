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

package resource

import (
	"context"
	"fmt"
	"slices"

	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/osagberg/kube-assist-operator/internal/checker"
)

const (
	// NetworkPolicyCheckerName is the identifier for this checker
	NetworkPolicyCheckerName = "networkpolicies"
)

// NetworkPolicyChecker analyzes NetworkPolicies for health issues
type NetworkPolicyChecker struct{}

// NewNetworkPolicyChecker creates a new NetworkPolicy checker
func NewNetworkPolicyChecker() *NetworkPolicyChecker {
	return &NetworkPolicyChecker{}
}

// Name returns the checker identifier
func (c *NetworkPolicyChecker) Name() string {
	return NetworkPolicyCheckerName
}

// Supports always returns true since NetworkPolicies are networking resources
func (c *NetworkPolicyChecker) Supports(ctx context.Context, cl client.Client) bool {
	return true
}

// Check performs health checks on NetworkPolicies
func (c *NetworkPolicyChecker) Check(ctx context.Context, checkCtx *checker.CheckContext) (*checker.CheckResult, error) {
	result := &checker.CheckResult{
		CheckerName: NetworkPolicyCheckerName,
		Issues:      []checker.Issue{},
	}

	for _, ns := range checkCtx.Namespaces {
		var npList networkingv1.NetworkPolicyList
		if err := checkCtx.Client.List(ctx, &npList, client.InNamespace(ns)); err != nil {
			continue
		}

		// Check if namespace has any network policies
		if len(npList.Items) == 0 {
			result.Issues = append(result.Issues, checker.Issue{
				Type:       "NoNetworkPolicy",
				Severity:   checker.SeverityInfo,
				Resource:   fmt.Sprintf("namespace/%s", ns),
				Namespace:  ns,
				Message:    fmt.Sprintf("Namespace %s has no NetworkPolicies defined", ns),
				Suggestion: "Consider adding NetworkPolicies to restrict pod-to-pod traffic for better security",
				Metadata: map[string]string{
					"namespace": ns,
				},
			})
		} else {
			// Each policy found is healthy
			for _, np := range npList.Items {
				issues := c.checkNetworkPolicy(&np)
				if len(issues) == 0 {
					result.Healthy++
				} else {
					result.Issues = append(result.Issues, issues...)
				}
			}
		}
	}

	return result, nil
}

// checkNetworkPolicy analyzes a single NetworkPolicy
func (c *NetworkPolicyChecker) checkNetworkPolicy(np *networkingv1.NetworkPolicy) []checker.Issue {
	var issues []checker.Issue
	resourceRef := fmt.Sprintf("networkpolicy/%s", np.Name)

	// Check for overly permissive policies
	if c.isAllowAll(np) {
		issues = append(issues, checker.Issue{
			Type:       "OverlyPermissivePolicy",
			Severity:   checker.SeverityWarning,
			Resource:   resourceRef,
			Namespace:  np.Namespace,
			Message:    fmt.Sprintf("NetworkPolicy %s allows all traffic (may be intentional for default-allow patterns)", np.Name),
			Suggestion: "Review if this policy should restrict traffic more specifically",
			Metadata: map[string]string{
				"networkPolicy": np.Name,
			},
		})
	}

	// Check for empty pod selector (applies to all pods)
	if len(np.Spec.PodSelector.MatchLabels) == 0 && len(np.Spec.PodSelector.MatchExpressions) == 0 {
		// This isn't necessarily bad - it's a namespace-wide default policy
		// Just note it for visibility
		issues = append(issues, checker.Issue{
			Type:       "NamespaceWidePolicy",
			Severity:   checker.SeverityInfo,
			Resource:   resourceRef,
			Namespace:  np.Namespace,
			Message:    fmt.Sprintf("NetworkPolicy %s applies to all pods in namespace (empty pod selector)", np.Name),
			Suggestion: "This is a namespace-wide default policy - verify this is intentional",
			Metadata: map[string]string{
				"networkPolicy": np.Name,
			},
		})
	}

	return issues
}

// isAllowAll checks if a NetworkPolicy allows all traffic
func (c *NetworkPolicyChecker) isAllowAll(np *networkingv1.NetworkPolicy) bool {
	// Check ingress
	hasIngressPolicy := slices.Contains(np.Spec.PolicyTypes, networkingv1.PolicyTypeIngress)

	if hasIngressPolicy {
		// If ingress rules are empty, it denies all ingress
		// If there's one rule with no from selector, it allows all
		for _, ingress := range np.Spec.Ingress {
			if len(ingress.From) == 0 && len(ingress.Ports) == 0 {
				return true // Allow all ingress
			}
		}
	}

	// Check egress
	hasEgressPolicy := slices.Contains(np.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)

	if hasEgressPolicy {
		for _, egress := range np.Spec.Egress {
			if len(egress.To) == 0 && len(egress.Ports) == 0 {
				return true // Allow all egress
			}
		}
	}

	return false
}
