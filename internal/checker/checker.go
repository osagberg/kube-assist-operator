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

// Package checker provides a pluggable architecture for health checks.
package checker

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Severity levels for issues
const (
	SeverityCritical = "Critical"
	SeverityWarning  = "Warning"
	SeverityInfo     = "Info"
)

// Issue represents a single detected problem
type Issue struct {
	// Type categorizes the issue (e.g., "CrashLoopBackOff", "OOMKilled")
	Type string `json:"type"`

	// Severity indicates how serious the issue is (Critical, Warning, Info)
	Severity string `json:"severity"`

	// Resource identifies the affected resource (e.g., "deployment/api-server")
	Resource string `json:"resource"`

	// Namespace where the resource lives
	Namespace string `json:"namespace"`

	// Message is a human-readable description of the issue
	Message string `json:"message"`

	// Suggestion provides actionable advice to fix the issue
	Suggestion string `json:"suggestion,omitempty"`

	// Metadata contains additional context-specific information
	Metadata map[string]string `json:"metadata,omitempty"`
}

// CheckResult contains results from a single checker
type CheckResult struct {
	// CheckerName identifies which checker produced this result
	CheckerName string `json:"checkerName"`

	// Healthy is the count of healthy resources checked
	Healthy int `json:"healthy"`

	// Issues is the list of problems found
	Issues []Issue `json:"issues"`

	// Error is set if the checker encountered an error (not serialized to JSON)
	Error error `json:"-"`
}

// CheckContext provides context for checker execution
type CheckContext struct {
	// Client is the Kubernetes API client
	Client client.Client

	// Namespaces to check (empty means all accessible namespaces)
	Namespaces []string

	// Config contains checker-specific configuration
	Config map[string]interface{}
}

// Checker is the interface all health checkers must implement
type Checker interface {
	// Name returns the checker identifier (e.g., "workloads", "helmreleases")
	Name() string

	// Check performs the health check and returns results
	Check(ctx context.Context, checkCtx *CheckContext) (*CheckResult, error)

	// Supports returns true if this checker can run in the current environment
	// (e.g., HelmReleaseChecker returns false if Flux CRDs are not installed)
	Supports(ctx context.Context, client client.Client) bool
}

// CountBySeverity returns a count of issues grouped by severity
func (r *CheckResult) CountBySeverity() map[string]int {
	counts := make(map[string]int)
	for _, issue := range r.Issues {
		counts[issue.Severity]++
	}
	return counts
}

// HasCritical returns true if any critical issues were found
func (r *CheckResult) HasCritical() bool {
	for _, issue := range r.Issues {
		if issue.Severity == SeverityCritical {
			return true
		}
	}
	return false
}

// TotalIssues returns the total number of issues found
func (r *CheckResult) TotalIssues() int {
	return len(r.Issues)
}
