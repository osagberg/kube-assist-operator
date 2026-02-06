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
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/testutil"
)

func TestNetworkPolicyChecker_Name(t *testing.T) {
	c := NewNetworkPolicyChecker()
	if c.Name() != NetworkPolicyCheckerName {
		t.Errorf("Name() = %v, want %v", c.Name(), NetworkPolicyCheckerName)
	}
}

func TestNetworkPolicyChecker_Supports(t *testing.T) {
	ds := testutil.NewDataSource(t)
	c := NewNetworkPolicyChecker()
	if !c.Supports(context.Background(), ds) {
		t.Error("Supports() = false, want true")
	}
}

func TestNetworkPolicyChecker_NoNetworkPolicies(t *testing.T) {
	c := NewNetworkPolicyChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"})

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == "NoNetworkPolicy" && issue.Severity == checker.SeverityInfo {
			found = true
			break
		}
	}
	if !found {
		t.Error("Check() did not find expected NoNetworkPolicy info issue")
	}
}

func TestNetworkPolicyChecker_HealthyRestrictivePolicy(t *testing.T) {
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "restrict-ingress",
			Namespace: "default",
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "web",
				},
			},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						{
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app": "frontend",
								},
							},
						},
					},
				},
			},
		},
	}

	c := NewNetworkPolicyChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"}, np)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if result.Healthy != 1 {
		t.Errorf("Check() healthy = %d, want 1", result.Healthy)
	}
}

func TestNetworkPolicyChecker_AllowAllIngress(t *testing.T) {
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "allow-all-ingress",
			Namespace: "default",
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "web",
				},
			},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{},
			},
		},
	}

	c := NewNetworkPolicyChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"}, np)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == "OverlyPermissivePolicy" && issue.Severity == checker.SeverityWarning {
			found = true
			break
		}
	}
	if !found {
		t.Error("Check() did not find expected OverlyPermissivePolicy warning issue")
	}
}

func TestNetworkPolicyChecker_NamespaceWidePolicy(t *testing.T) {
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-deny",
			Namespace: "default",
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
		},
	}

	c := NewNetworkPolicyChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"}, np)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == "NamespaceWidePolicy" && issue.Severity == checker.SeverityInfo {
			found = true
			break
		}
	}
	if !found {
		t.Error("Check() did not find expected NamespaceWidePolicy info issue")
	}
}

func TestNetworkPolicyChecker_MultipleNamespaces(t *testing.T) {
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "policy",
			Namespace: "ns-with-policy",
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						{PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}}},
					},
				},
			},
		},
	}

	c := NewNetworkPolicyChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"ns-with-policy", "ns-without-policy"}, np)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if result.Healthy != 1 {
		t.Errorf("Check() healthy = %d, want 1", result.Healthy)
	}

	foundNoPolicy := false
	for _, issue := range result.Issues {
		if issue.Type == "NoNetworkPolicy" && issue.Namespace == "ns-without-policy" {
			foundNoPolicy = true
			break
		}
	}
	if !foundNoPolicy {
		t.Error("Check() did not find expected NoNetworkPolicy issue for ns-without-policy")
	}
}
