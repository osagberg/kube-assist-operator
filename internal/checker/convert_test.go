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
	"errors"
	"testing"

	assistv1alpha1 "github.com/osagberg/kube-assist-operator/api/v1alpha1"
)

func TestToAPIIssue(t *testing.T) {
	issue := Issue{
		Type:       "CrashLoopBackOff",
		Severity:   SeverityCritical,
		Resource:   "deployment/test",
		Namespace:  "default",
		Message:    "Container is crashing",
		Suggestion: "Check logs",
	}

	apiIssue := ToAPIIssue(issue)

	if apiIssue.Type != issue.Type {
		t.Errorf("Type = %s, want %s", apiIssue.Type, issue.Type)
	}
	if apiIssue.Severity != issue.Severity {
		t.Errorf("Severity = %s, want %s", apiIssue.Severity, issue.Severity)
	}
	if apiIssue.Message != issue.Message {
		t.Errorf("Message = %s, want %s", apiIssue.Message, issue.Message)
	}
	if apiIssue.Suggestion != issue.Suggestion {
		t.Errorf("Suggestion = %s, want %s", apiIssue.Suggestion, issue.Suggestion)
	}
}

func TestToAPIIssues(t *testing.T) {
	issues := []Issue{
		{Type: "A", Severity: SeverityCritical, Message: "Issue A"},
		{Type: "B", Severity: SeverityWarning, Message: "Issue B"},
	}

	apiIssues := ToAPIIssues(issues)

	if len(apiIssues) != len(issues) {
		t.Errorf("len(apiIssues) = %d, want %d", len(apiIssues), len(issues))
	}

	for i, apiIssue := range apiIssues {
		if apiIssue.Type != issues[i].Type {
			t.Errorf("apiIssues[%d].Type = %s, want %s", i, apiIssue.Type, issues[i].Type)
		}
	}
}

func TestFromAPIIssue(t *testing.T) {
	apiIssue := assistv1alpha1.DiagnosticIssue{
		Type:       "OOMKilled",
		Severity:   "Critical",
		Message:    "Container out of memory",
		Suggestion: "Increase memory limit",
	}

	issue := FromAPIIssue(apiIssue)

	if issue.Type != apiIssue.Type {
		t.Errorf("Type = %s, want %s", issue.Type, apiIssue.Type)
	}
	if issue.Severity != apiIssue.Severity {
		t.Errorf("Severity = %s, want %s", issue.Severity, apiIssue.Severity)
	}
	if issue.Message != apiIssue.Message {
		t.Errorf("Message = %s, want %s", issue.Message, apiIssue.Message)
	}
	if issue.Suggestion != apiIssue.Suggestion {
		t.Errorf("Suggestion = %s, want %s", issue.Suggestion, apiIssue.Suggestion)
	}
}

func TestFromAPIIssues(t *testing.T) {
	apiIssues := []assistv1alpha1.DiagnosticIssue{
		{Type: "X", Severity: "Warning"},
		{Type: "Y", Severity: "Info"},
	}

	issues := FromAPIIssues(apiIssues)

	if len(issues) != len(apiIssues) {
		t.Errorf("len(issues) = %d, want %d", len(issues), len(apiIssues))
	}
}

func TestMergeResults(t *testing.T) {
	results := map[string]*CheckResult{
		"checker1": {
			CheckerName: "checker1",
			Issues: []Issue{
				{Type: "A", Severity: SeverityCritical},
				{Type: "B", Severity: SeverityWarning},
			},
		},
		"checker2": {
			CheckerName: "checker2",
			Issues: []Issue{
				{Type: "C", Severity: SeverityInfo},
			},
		},
		"checker3": {
			CheckerName: "checker3",
			Error:       errors.New("failed"),
		},
		"checker4": nil,
	}

	merged := MergeResults(results)

	// Should have 3 issues (checker3 error and checker4 nil are skipped)
	if len(merged) != 3 {
		t.Errorf("MergeResults() = %d issues, want 3", len(merged))
	}
}

func TestMergeResults_Empty(t *testing.T) {
	results := map[string]*CheckResult{}
	merged := MergeResults(results)

	if len(merged) != 0 {
		t.Errorf("MergeResults(empty) = %d issues, want 0", len(merged))
	}
}
