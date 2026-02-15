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
	"github.com/osagberg/kube-assist-operator/internal/datasource"
)

const (
	// QuotaCheckerName is the identifier for this checker
	QuotaCheckerName = "quotas"

	// Default warning threshold percentage
	DefaultQuotaWarningPercent = 80

	// Issue types
	issueTypeQuotaExceeded  = "QuotaExceeded"
	issueTypeQuotaNearLimit = "QuotaNearLimit"
)

// QuotaChecker analyzes ResourceQuotas for health issues
type QuotaChecker struct {
	warningPercent int
}

// NewQuotaChecker creates a new ResourceQuota checker
func NewQuotaChecker() *QuotaChecker {
	return &QuotaChecker{
		warningPercent: DefaultQuotaWarningPercent,
	}
}

// WithWarningPercent sets the warning threshold percentage
func (c *QuotaChecker) WithWarningPercent(percent int) *QuotaChecker {
	c.warningPercent = percent
	return c
}

// Name returns the checker identifier
func (c *QuotaChecker) Name() string {
	return QuotaCheckerName
}

// Supports always returns true since ResourceQuotas are core resources
func (c *QuotaChecker) Supports(ctx context.Context, ds datasource.DataSource) bool {
	return true
}

// Check performs health checks on ResourceQuotas
func (c *QuotaChecker) Check(ctx context.Context, checkCtx *checker.CheckContext) (*checker.CheckResult, error) {
	result := &checker.CheckResult{
		CheckerName: QuotaCheckerName,
		Issues:      []checker.Issue{},
	}

	// Read config into local variable to avoid mutating shared state
	warningPercent := c.warningPercent
	if checkCtx.Config != nil {
		if percent, ok := checkCtx.Config["usageWarningPercent"].(int); ok {
			warningPercent = percent
		}
	}

	for _, ns := range checkCtx.Namespaces {
		var quotaList corev1.ResourceQuotaList
		if err := checkCtx.DataSource.List(ctx, &quotaList, client.InNamespace(ns)); err != nil {
			continue
		}

		for _, quota := range quotaList.Items {
			issues := c.checkQuotaWith(&quota, warningPercent)
			if len(issues) == 0 {
				result.Healthy++
			} else {
				result.Issues = append(result.Issues, issues...)
			}
		}
	}

	return result, nil
}

// checkQuotaWith analyzes a single ResourceQuota using the provided warning threshold
func (c *QuotaChecker) checkQuotaWith(quota *corev1.ResourceQuota, warningPercent int) []checker.Issue {
	var issues []checker.Issue
	resourceRef := fmt.Sprintf("resourcequota/%s", quota.Name)

	// Check each resource in the quota
	for resourceName, hardLimit := range quota.Status.Hard {
		used, hasUsed := quota.Status.Used[resourceName]
		if !hasUsed {
			continue
		}

		// Calculate usage percentage.
		// Use approximate float conversion to preserve fractional quantities
		// (e.g. CPU millicores) instead of truncating via Value().
		hardValue := hardLimit.AsApproximateFloat64()
		usedValue := used.AsApproximateFloat64()

		if hardValue <= 0 {
			continue
		}

		usagePercent := usedValue / hardValue * 100

		if usagePercent >= 100 {
			// Quota exceeded
			issues = append(issues, checker.Issue{
				Type:      issueTypeQuotaExceeded,
				Severity:  checker.SeverityCritical,
				Resource:  resourceRef,
				Namespace: quota.Namespace,
				Message:   fmt.Sprintf("ResourceQuota %s: %s is at %.0f%% (%s/%s)", quota.Name, resourceName, usagePercent, used.String(), hardLimit.String()),
				Suggestion: "Quota is fully consumed. New pods requesting this resource will fail to schedule. " +
					"Check current usage: kubectl describe resourcequota " + quota.Name + " -n " + quota.Namespace + ". " +
					"Options: request a quota increase from your platform team, " +
					"or reduce usage by scaling down or optimizing resource requests.",
				Metadata: map[string]string{
					"quota":        quota.Name,
					"resource":     string(resourceName),
					"used":         used.String(),
					"hard":         hardLimit.String(),
					"usagePercent": fmt.Sprintf("%.1f", usagePercent),
				},
			})
		} else if usagePercent >= float64(warningPercent) {
			// Near quota limit
			issues = append(issues, checker.Issue{
				Type:      issueTypeQuotaNearLimit,
				Severity:  checker.SeverityWarning,
				Resource:  resourceRef,
				Namespace: quota.Namespace,
				Message:   fmt.Sprintf("ResourceQuota %s: %s is at %.0f%% (%s/%s)", quota.Name, resourceName, usagePercent, used.String(), hardLimit.String()),
				Suggestion: fmt.Sprintf("Quota usage is approaching the limit (warning at %d%%). "+
					"Review usage: kubectl describe resourcequota %s -n %s. "+
					"Plan ahead: request a quota increase from your platform team before deployments fail.",
					warningPercent, quota.Name, quota.Namespace),
				Metadata: map[string]string{
					"quota":        quota.Name,
					"resource":     string(resourceName),
					"used":         used.String(),
					"hard":         hardLimit.String(),
					"usagePercent": fmt.Sprintf("%.1f", usagePercent),
				},
			})
		}
	}

	return issues
}
