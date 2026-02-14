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

func TestNetworkPolicyChecker_AllowAllEgress(t *testing.T) {
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "allow-all-egress",
			Namespace: "default",
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "web",
				},
			},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{}, // empty To and Ports = allow all
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
		t.Error("Check() did not find expected OverlyPermissivePolicy warning issue for allow-all egress")
	}
}

func TestNetworkPolicyChecker_NamespaceWidePolicy(t *testing.T) {
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-deny",
			Namespace: "default",
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{}, // empty = all pods
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

func TestNetworkPolicyChecker_IsAllowAll(t *testing.T) {
	c := NewNetworkPolicyChecker()

	tests := []struct {
		name string
		np   *networkingv1.NetworkPolicy
		want bool
	}{
		{
			name: "allow-all ingress - empty From and Ports",
			np: &networkingv1.NetworkPolicy{
				Spec: networkingv1.NetworkPolicySpec{
					PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
					Ingress: []networkingv1.NetworkPolicyIngressRule{
						{}, // empty From and Ports
					},
				},
			},
			want: true,
		},
		{
			name: "allow-all egress - empty To and Ports",
			np: &networkingv1.NetworkPolicy{
				Spec: networkingv1.NetworkPolicySpec{
					PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
					Egress: []networkingv1.NetworkPolicyEgressRule{
						{}, // empty To and Ports
					},
				},
			},
			want: true,
		},
		{
			name: "deny-all ingress - no rules",
			np: &networkingv1.NetworkPolicy{
				Spec: networkingv1.NetworkPolicySpec{
					PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
					Ingress:     []networkingv1.NetworkPolicyIngressRule{},
				},
			},
			want: false,
		},
		{
			name: "deny-all egress - no rules",
			np: &networkingv1.NetworkPolicy{
				Spec: networkingv1.NetworkPolicySpec{
					PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
					Egress:      []networkingv1.NetworkPolicyEgressRule{},
				},
			},
			want: false,
		},
		{
			name: "restrictive ingress with From selectors",
			np: &networkingv1.NetworkPolicy{
				Spec: networkingv1.NetworkPolicySpec{
					PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
					Ingress: []networkingv1.NetworkPolicyIngressRule{
						{
							From: []networkingv1.NetworkPolicyPeer{
								{
									PodSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{"role": "frontend"},
									},
								},
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "restrictive egress with To selectors",
			np: &networkingv1.NetworkPolicy{
				Spec: networkingv1.NetworkPolicySpec{
					PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
					Egress: []networkingv1.NetworkPolicyEgressRule{
						{
							To: []networkingv1.NetworkPolicyPeer{
								{
									PodSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{"role": "db"},
									},
								},
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "no policy types at all",
			np: &networkingv1.NetworkPolicy{
				Spec: networkingv1.NetworkPolicySpec{
					PolicyTypes: []networkingv1.PolicyType{},
				},
			},
			want: false,
		},
		{
			name: "both policy types but only ingress is allow-all",
			np: &networkingv1.NetworkPolicy{
				Spec: networkingv1.NetworkPolicySpec{
					PolicyTypes: []networkingv1.PolicyType{
						networkingv1.PolicyTypeIngress,
						networkingv1.PolicyTypeEgress,
					},
					Ingress: []networkingv1.NetworkPolicyIngressRule{
						{}, // allow all ingress
					},
					Egress: []networkingv1.NetworkPolicyEgressRule{
						{
							To: []networkingv1.NetworkPolicyPeer{
								{
									PodSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{"role": "db"},
									},
								},
							},
						},
					},
				},
			},
			want: true, // ingress is allow-all, so isAllowAll returns true
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.isAllowAll(tt.np); got != tt.want {
				t.Errorf("isAllowAll() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNetworkPolicyChecker_MatchExpressionSelector(t *testing.T) {
	// A policy with MatchExpressions (not MatchLabels) should NOT trigger
	// NamespaceWidePolicy because the pod selector is not empty.
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "expression-selector",
			Namespace: "default",
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "app",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"web", "api"},
					},
				},
			},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						{
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"role": "frontend"},
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

	for _, issue := range result.Issues {
		if issue.Type == "NamespaceWidePolicy" {
			t.Error("Check() should not report NamespaceWidePolicy for policy with MatchExpressions")
		}
	}
}
