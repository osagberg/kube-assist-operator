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

const testIssueTypePVCPending = "PVCPending"

func TestPVCChecker_Name(t *testing.T) {
	c := NewPVCChecker()
	if c.Name() != PVCCheckerName {
		t.Errorf("Name() = %v, want %v", c.Name(), PVCCheckerName)
	}
}

func TestPVCChecker_Supports(t *testing.T) {
	ds := testutil.NewDataSource(t)
	c := NewPVCChecker()
	if !c.Supports(context.Background(), ds) {
		t.Error("Supports() = false, want true")
	}
}

func TestPVCChecker_HealthyPVC(t *testing.T) {
	storageClass := "standard"
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "healthy-pvc",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClass,
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimBound,
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("10Gi"),
			},
		},
	}

	c := NewPVCChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"}, pvc)

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

func TestPVCChecker_PendingPVC(t *testing.T) {
	storageClass := "nonexistent"
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pending-pvc",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClass,
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimPending,
		},
	}

	c := NewPVCChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"}, pvc)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == testIssueTypePVCPending && issue.Severity == checker.SeverityWarning {
			found = true
			// Verify metadata contains storage class
			if issue.Metadata["storageClass"] != "nonexistent" {
				t.Errorf("metadata storageClass = %s, want nonexistent", issue.Metadata["storageClass"])
			}
			break
		}
	}
	if !found {
		t.Error("Check() did not find expected PVCPending warning issue")
	}
}

func TestPVCChecker_PendingPVCWithResizingCondition(t *testing.T) {
	// When a PVC is pending due to resizing, the message should reflect that
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "resize-pending-pvc",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimPending,
			Conditions: []corev1.PersistentVolumeClaimCondition{
				{
					Type:   corev1.PersistentVolumeClaimResizing,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	c := NewPVCChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"}, pvc)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == testIssueTypePVCPending {
			found = true
			// When the resizing condition is present, message should mention resize
			if issue.Message != "PVC resize-pending-pvc is pending resize" {
				t.Errorf("issue message = %q, want mention of resize", issue.Message)
			}
			break
		}
	}
	if !found {
		t.Error("Check() did not find expected PVCPending issue for resizing PVC")
	}
}

func TestPVCChecker_LostPVC(t *testing.T) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lost-pvc",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			VolumeName: "deleted-pv",
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimLost,
		},
	}

	c := NewPVCChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"}, pvc)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == "PVCLost" && issue.Severity == checker.SeverityCritical {
			found = true
			// Verify metadata includes volume name
			if issue.Metadata["volumeName"] != "deleted-pv" {
				t.Errorf("metadata volumeName = %s, want deleted-pv", issue.Metadata["volumeName"])
			}
			break
		}
	}
	if !found {
		t.Error("Check() did not find expected PVCLost critical issue")
	}
}

func TestPVCChecker_ResizePending(t *testing.T) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "resize-pvc",
			Namespace: "default",
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimBound,
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("10Gi"),
			},
			Conditions: []corev1.PersistentVolumeClaimCondition{
				{
					Type:   corev1.PersistentVolumeClaimFileSystemResizePending,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	c := NewPVCChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"}, pvc)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == "PVCResizePending" && issue.Severity == checker.SeverityInfo {
			found = true
			break
		}
	}
	if !found {
		t.Error("Check() did not find expected PVCResizePending info issue")
	}
}

func TestPVCChecker_BoundNoCapacity(t *testing.T) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "no-capacity-pvc",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			VolumeName: "some-pv",
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase:    corev1.ClaimBound,
			Capacity: nil, // bound but no capacity reported
		},
	}

	c := NewPVCChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"}, pvc)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == "PVCNoCapacity" && issue.Severity == checker.SeverityWarning {
			found = true
			break
		}
	}
	if !found {
		t.Error("Check() did not find expected PVCNoCapacity warning issue")
	}
}

func TestPVCChecker_NoPVCs(t *testing.T) {
	c := NewPVCChecker()
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

func TestPVCChecker_PendingNoStorageClass(t *testing.T) {
	// PVC without storage class specified
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "no-sc-pvc",
			Namespace: "default",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			// No StorageClassName set
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimPending,
		},
	}

	c := NewPVCChecker()
	checkCtx := testutil.NewCheckContext(t, []string{"default"}, pvc)

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == testIssueTypePVCPending {
			found = true
			// storageClass should be empty when not set
			if issue.Metadata["storageClass"] != "" {
				t.Errorf("metadata storageClass = %q, want empty", issue.Metadata["storageClass"])
			}
			break
		}
	}
	if !found {
		t.Error("Check() did not find expected PVCPending issue")
	}
}

func TestFormatAccessModes(t *testing.T) {
	tests := []struct {
		name  string
		modes []corev1.PersistentVolumeAccessMode
		want  string
	}{
		{"empty", nil, ""},
		{"single", []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, "ReadWriteOnce"},
		{"multiple", []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce, corev1.ReadOnlyMany}, "ReadWriteOnce,ReadOnlyMany"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatAccessModes(tt.modes)
			if got != tt.want {
				t.Errorf("formatAccessModes() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetStorageClassName(t *testing.T) {
	tests := []struct {
		name string
		pvc  *corev1.PersistentVolumeClaim
		want string
	}{
		{
			name: "with storage class",
			pvc: &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: strPtr("fast-ssd"),
				},
			},
			want: "fast-ssd",
		},
		{
			name: "without storage class",
			pvc: &corev1.PersistentVolumeClaim{
				Spec: corev1.PersistentVolumeClaimSpec{},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getStorageClassName(tt.pvc)
			if got != tt.want {
				t.Errorf("getStorageClassName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func strPtr(s string) *string {
	return &s
}
