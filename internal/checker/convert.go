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

package checker

import (
	assistv1alpha1 "github.com/osagberg/kube-assist-operator/api/v1alpha1"
)

// ToAPIIssue converts a checker.Issue to assistv1alpha1.DiagnosticIssue
func ToAPIIssue(issue Issue) assistv1alpha1.DiagnosticIssue {
	return assistv1alpha1.DiagnosticIssue{
		Type:       issue.Type,
		Severity:   issue.Severity,
		Message:    issue.Message,
		Suggestion: issue.Suggestion,
	}
}

// ToAPIIssues converts a slice of checker.Issue to assistv1alpha1.DiagnosticIssue
func ToAPIIssues(issues []Issue) []assistv1alpha1.DiagnosticIssue {
	result := make([]assistv1alpha1.DiagnosticIssue, len(issues))
	for i, issue := range issues {
		result[i] = ToAPIIssue(issue)
	}
	return result
}

// FromAPIIssue converts an assistv1alpha1.DiagnosticIssue to checker.Issue
func FromAPIIssue(issue assistv1alpha1.DiagnosticIssue) Issue {
	return Issue{
		Type:       issue.Type,
		Severity:   issue.Severity,
		Message:    issue.Message,
		Suggestion: issue.Suggestion,
	}
}

// FromAPIIssues converts a slice of assistv1alpha1.DiagnosticIssue to checker.Issue
func FromAPIIssues(issues []assistv1alpha1.DiagnosticIssue) []Issue {
	result := make([]Issue, len(issues))
	for i, issue := range issues {
		result[i] = FromAPIIssue(issue)
	}
	return result
}

// MergeResults combines multiple CheckResults into a single list of API issues
func MergeResults(results map[string]*CheckResult) []assistv1alpha1.DiagnosticIssue {
	var allIssues []assistv1alpha1.DiagnosticIssue
	for _, result := range results {
		if result != nil && result.Error == nil {
			allIssues = append(allIssues, ToAPIIssues(result.Issues)...)
		}
	}
	return allIssues
}
