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
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/osagberg/kube-assist-operator/internal/checker"
)

func TestPVCChecker_Name(t *testing.T) {
	c := NewPVCChecker()
	if c.Name() != PVCCheckerName {
		t.Errorf("Name() = %v, want %v", c.Name(), PVCCheckerName)
	}
}

func TestPVCChecker_Supports(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	c := NewPVCChecker()
	if !c.Supports(context.Background(), client) {
		t.Error("Supports() = false, want true")
	}
}

func TestPVCChecker_HealthyPVC(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

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

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(pvc).
		Build()

	c := NewPVCChecker()
	checkCtx := &checker.CheckContext{
		Client:     client,
		Namespaces: []string{"default"},
	}

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
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

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

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(pvc).
		Build()

	c := NewPVCChecker()
	checkCtx := &checker.CheckContext{
		Client:     client,
		Namespaces: []string{"default"},
	}

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == "PVCPending" && issue.Severity == checker.SeverityWarning {
			found = true
			break
		}
	}
	if !found {
		t.Error("Check() did not find expected PVCPending warning issue")
	}
}

func TestPVCChecker_LostPVC(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

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

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(pvc).
		Build()

	c := NewPVCChecker()
	checkCtx := &checker.CheckContext{
		Client:     client,
		Namespaces: []string{"default"},
	}

	result, err := c.Check(context.Background(), checkCtx)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	found := false
	for _, issue := range result.Issues {
		if issue.Type == "PVCLost" && issue.Severity == checker.SeverityCritical {
			found = true
			break
		}
	}
	if !found {
		t.Error("Check() did not find expected PVCLost critical issue")
	}
}

func TestPVCChecker_ResizePending(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

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

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(pvc).
		Build()

	c := NewPVCChecker()
	checkCtx := &checker.CheckContext{
		Client:     client,
		Namespaces: []string{"default"},
	}

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
