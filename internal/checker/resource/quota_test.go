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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/testutil"
)

func TestQuotaChecker_Name(t *testing.T) {
	c := NewQuotaChecker()
	if c.Name() != QuotaCheckerName {
		t.Errorf("Name() = %v, want %v", c.Name(), QuotaCheckerName)
	}
}

func TestQuotaChecker_Supports(t *testing.T) {
	ds := testutil.NewDataSource(t)
	c := NewQuotaChecker()
	if !c.Supports(context.Background(), ds) {
		t.Error("Supports() = false, want true")
	}
}

func TestQuotaChecker_HealthyQuota(t *testing.T) {
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "healthy-quota",
			Namespace: "default",
		},
		Status: corev1.ResourceQuotaStatus{
			Hard: corev1.ResourceList{
				corev1.ResourcePods:   resource.MustParse("100"),
				corev1.ResourceCPU:    resource.MustParse("10"),
				corev1.ResourceMemory: resource.MustParse("20Gi"),
			},
			Used: corev1.ResourceList{
				corev1.ResourcePods:   resource.MustParse("10"),
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
		},
	}

	c := NewQuotaChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"}, quota)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if result.Healthy != 1 {
		t.Errorf("Check() healthy = %d, want 1", result.Healthy)
	}
	if len(result.Issues) != 0 {
		t.Errorf("Check() issues = %d, want 0", len(result.Issues))
	}
}

func TestQuotaChecker_QuotaNearLimit(t *testing.T) {
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "near-limit-quota",
			Namespace: "default",
		},
		Status: corev1.ResourceQuotaStatus{
			Hard: corev1.ResourceList{
				corev1.ResourcePods: resource.MustParse("100"),
			},
			Used: corev1.ResourceList{
				corev1.ResourcePods: resource.MustParse("85"),
			},
		},
	}

	c := NewQuotaChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"}, quota)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == issueTypeQuotaNearLimit && issue.Severity == checker.SeverityWarning {
			found = true
			break
		}
	}
	if !found {
		t.Error("Check() did not find expected quota near limit warning issue")
	}
}

func TestQuotaChecker_QuotaExceeded(t *testing.T) {
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "exceeded-quota",
			Namespace: "default",
		},
		Status: corev1.ResourceQuotaStatus{
			Hard: corev1.ResourceList{
				corev1.ResourcePods: resource.MustParse("100"),
			},
			Used: corev1.ResourceList{
				corev1.ResourcePods: resource.MustParse("105"),
			},
		},
	}

	c := NewQuotaChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"}, quota)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == issueTypeQuotaExceeded && issue.Severity == checker.SeverityCritical {
			found = true
			break
		}
	}
	if !found {
		t.Error("Check() did not find expected QuotaExceeded critical issue")
	}
}

func TestQuotaChecker_WithWarningPercent(t *testing.T) {
	c := NewQuotaChecker().WithWarningPercent(90)
	if c.warningPercent != 90 {
		t.Errorf("warningPercent = %d, want 90", c.warningPercent)
	}
}

func TestQuotaChecker_MultipleResources(t *testing.T) {
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "multi-resource-quota",
			Namespace: "default",
		},
		Status: corev1.ResourceQuotaStatus{
			Hard: corev1.ResourceList{
				corev1.ResourcePods:   resource.MustParse("100"),
				corev1.ResourceCPU:    resource.MustParse("10"),
				corev1.ResourceMemory: resource.MustParse("20Gi"),
			},
			Used: corev1.ResourceList{
				corev1.ResourcePods:   resource.MustParse("50"),
				corev1.ResourceCPU:    resource.MustParse("9"),
				corev1.ResourceMemory: resource.MustParse("21Gi"),
			},
		},
	}

	c := NewQuotaChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"}, quota)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	foundCPUWarning := false
	foundMemoryExceeded := false
	for _, issue := range result.Issues {
		if issue.Type == issueTypeQuotaNearLimit && issue.Metadata["resource"] == "cpu" {
			foundCPUWarning = true
		}
		if issue.Type == issueTypeQuotaExceeded && issue.Metadata["resource"] == "memory" {
			foundMemoryExceeded = true
		}
	}
	if !foundCPUWarning {
		t.Error("Check() did not find expected CPU quota near limit warning")
	}
	if !foundMemoryExceeded {
		t.Error("Check() did not find expected Memory QuotaExceeded critical issue")
	}
}

func TestQuotaChecker_CPUMillicoreUsage(t *testing.T) {
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cpu-milli-quota",
			Namespace: "default",
		},
		Status: corev1.ResourceQuotaStatus{
			Hard: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("1500m"),
			},
			Used: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("1200m"),
			},
		},
	}

	c := NewQuotaChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"}, quota)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	foundCPUWarning := false
	for _, issue := range result.Issues {
		if issue.Type == issueTypeQuotaNearLimit && issue.Metadata["resource"] == "cpu" {
			foundCPUWarning = true
			break
		}
	}
	if !foundCPUWarning {
		t.Error("Check() did not find expected CPU quota near limit warning for millicore quantities")
	}
}

func TestQuotaChecker_NoQuotas(t *testing.T) {
	c := NewQuotaChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"})

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if result.Healthy != 0 {
		t.Errorf("Check() healthy = %d, want 0", result.Healthy)
	}
	if len(result.Issues) != 0 {
		t.Errorf("Check() issues = %d, want 0", len(result.Issues))
	}
}
