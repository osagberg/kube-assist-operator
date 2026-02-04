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

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/osagberg/kube-assist-operator/internal/checker"
)

const (
	// PVCCheckerName is the identifier for this checker
	PVCCheckerName = "pvcs"
)

// PVCChecker analyzes PersistentVolumeClaims for health issues
type PVCChecker struct{}

// NewPVCChecker creates a new PVC checker
func NewPVCChecker() *PVCChecker {
	return &PVCChecker{}
}

// Name returns the checker identifier
func (c *PVCChecker) Name() string {
	return PVCCheckerName
}

// Supports always returns true since PVCs are core resources
func (c *PVCChecker) Supports(ctx context.Context, cl client.Client) bool {
	return true
}

// Check performs health checks on PVCs
func (c *PVCChecker) Check(ctx context.Context, checkCtx *checker.CheckContext) (*checker.CheckResult, error) {
	result := &checker.CheckResult{
		CheckerName: PVCCheckerName,
		Issues:      []checker.Issue{},
	}

	for _, ns := range checkCtx.Namespaces {
		var pvcList corev1.PersistentVolumeClaimList
		if err := checkCtx.Client.List(ctx, &pvcList, client.InNamespace(ns)); err != nil {
			continue
		}

		for _, pvc := range pvcList.Items {
			issues := c.checkPVC(&pvc)
			if len(issues) == 0 {
				result.Healthy++
			} else {
				result.Issues = append(result.Issues, issues...)
			}
		}
	}

	return result, nil
}

// checkPVC analyzes a single PVC
func (c *PVCChecker) checkPVC(pvc *corev1.PersistentVolumeClaim) []checker.Issue {
	var issues []checker.Issue
	resourceRef := fmt.Sprintf("pvc/%s", pvc.Name)

	// Check PVC phase
	switch pvc.Status.Phase {
	case corev1.ClaimPending:
		message := fmt.Sprintf("PVC %s is in Pending state", pvc.Name)
		suggestion := "Check if there are available PersistentVolumes that match the PVC requirements"

		// Try to provide more context based on conditions
		for _, condition := range pvc.Status.Conditions {
			if condition.Type == corev1.PersistentVolumeClaimResizing {
				message = fmt.Sprintf("PVC %s is pending resize", pvc.Name)
				suggestion = "Wait for the resize operation to complete"
			}
		}

		issues = append(issues, checker.Issue{
			Type:       "PVCPending",
			Severity:   checker.SeverityWarning,
			Resource:   resourceRef,
			Namespace:  pvc.Namespace,
			Message:    message,
			Suggestion: suggestion,
			Metadata: map[string]string{
				"pvc":          pvc.Name,
				"storageClass": getStorageClassName(pvc),
				"accessModes":  formatAccessModes(pvc.Spec.AccessModes),
			},
		})

	case corev1.ClaimLost:
		issues = append(issues, checker.Issue{
			Type:       "PVCLost",
			Severity:   checker.SeverityCritical,
			Resource:   resourceRef,
			Namespace:  pvc.Namespace,
			Message:    fmt.Sprintf("PVC %s has lost its bound PersistentVolume", pvc.Name),
			Suggestion: "The underlying PV may have been deleted. Check PV status and consider recreating the PVC",
			Metadata: map[string]string{
				"pvc":          pvc.Name,
				"volumeName":   pvc.Spec.VolumeName,
				"storageClass": getStorageClassName(pvc),
			},
		})
	}

	// Check for resize conditions
	for _, condition := range pvc.Status.Conditions {
		if condition.Type == corev1.PersistentVolumeClaimFileSystemResizePending {
			if condition.Status == corev1.ConditionTrue {
				issues = append(issues, checker.Issue{
					Type:       "PVCResizePending",
					Severity:   checker.SeverityInfo,
					Resource:   resourceRef,
					Namespace:  pvc.Namespace,
					Message:    "PVC filesystem resize is pending - requires pod restart",
					Suggestion: "Restart the pod using this PVC to complete the resize",
					Metadata: map[string]string{
						"pvc": pvc.Name,
					},
				})
			}
		}
	}

	// Check if PVC is bound but has no capacity (unusual state)
	if pvc.Status.Phase == corev1.ClaimBound && pvc.Status.Capacity == nil {
		issues = append(issues, checker.Issue{
			Type:       "PVCNoCapacity",
			Severity:   checker.SeverityWarning,
			Resource:   resourceRef,
			Namespace:  pvc.Namespace,
			Message:    "PVC is bound but reports no capacity",
			Suggestion: "Check the underlying PersistentVolume status",
			Metadata: map[string]string{
				"pvc":        pvc.Name,
				"volumeName": pvc.Spec.VolumeName,
			},
		})
	}

	return issues
}

// getStorageClassName extracts storage class name from PVC
func getStorageClassName(pvc *corev1.PersistentVolumeClaim) string {
	if pvc.Spec.StorageClassName != nil {
		return *pvc.Spec.StorageClassName
	}
	return ""
}

// formatAccessModes converts access modes to string
func formatAccessModes(modes []corev1.PersistentVolumeAccessMode) string {
	if len(modes) == 0 {
		return ""
	}
	result := ""
	for i, mode := range modes {
		if i > 0 {
			result += ","
		}
		result += string(mode)
	}
	return result
}
