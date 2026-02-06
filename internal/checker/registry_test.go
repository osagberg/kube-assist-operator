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
	"context"
	"errors"
	"testing"

	"github.com/osagberg/kube-assist-operator/internal/datasource"
)

// mockChecker is a simple checker for testing
type mockChecker struct {
	name      string
	supported bool
	issues    []Issue
	err       error
}

func (m *mockChecker) Name() string {
	return m.name
}

func (m *mockChecker) Supports(ctx context.Context, ds datasource.DataSource) bool {
	return m.supported
}

func (m *mockChecker) Check(ctx context.Context, checkCtx *CheckContext) (*CheckResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &CheckResult{
		CheckerName: m.name,
		Healthy:     1,
		Issues:      m.issues,
	}, nil
}

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry()

	checker1 := &mockChecker{name: "test1", supported: true}
	checker2 := &mockChecker{name: "test2", supported: true}
	duplicate := &mockChecker{name: "test1", supported: true}

	// First registration should succeed
	if err := r.Register(checker1); err != nil {
		t.Errorf("Register() error = %v, want nil", err)
	}

	// Second registration should succeed
	if err := r.Register(checker2); err != nil {
		t.Errorf("Register() error = %v, want nil", err)
	}

	// Duplicate registration should fail
	if err := r.Register(duplicate); err == nil {
		t.Error("Register() expected error for duplicate, got nil")
	}
}

func TestRegistry_MustRegister(t *testing.T) {
	r := NewRegistry()

	checker1 := &mockChecker{name: "test1", supported: true}

	// Should not panic
	r.MustRegister(checker1)

	// Should panic on duplicate
	defer func() {
		if recover() == nil {
			t.Error("MustRegister() expected panic for duplicate")
		}
	}()

	r.MustRegister(checker1)
}

func TestRegistry_Get(t *testing.T) {
	r := NewRegistry()

	checker := &mockChecker{name: "test", supported: true}
	r.MustRegister(checker)

	// Should find registered checker
	got, ok := r.Get("test")
	if !ok {
		t.Error("Get() expected to find checker")
	}
	if got.Name() != "test" {
		t.Errorf("Get() name = %s, want test", got.Name())
	}

	// Should not find unregistered checker
	_, ok = r.Get("nonexistent")
	if ok {
		t.Error("Get() expected not to find nonexistent checker")
	}
}

func TestRegistry_List(t *testing.T) {
	r := NewRegistry()

	// Empty registry
	if len(r.List()) != 0 {
		t.Errorf("List() = %v, want empty", r.List())
	}

	// Add checkers
	r.MustRegister(&mockChecker{name: "a", supported: true})
	r.MustRegister(&mockChecker{name: "b", supported: true})
	r.MustRegister(&mockChecker{name: "c", supported: true})

	list := r.List()
	if len(list) != 3 {
		t.Errorf("List() length = %d, want 3", len(list))
	}
}

func TestRegistry_Run(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()
	checkCtx := &CheckContext{}

	// Checker that returns issues
	issueChecker := &mockChecker{
		name:      "issues",
		supported: true,
		issues: []Issue{
			{Type: "TestIssue", Severity: SeverityWarning},
		},
	}

	// Checker that returns error
	errorChecker := &mockChecker{
		name:      "error",
		supported: true,
		err:       errors.New("check failed"),
	}

	// Unsupported checker
	unsupportedChecker := &mockChecker{
		name:      "unsupported",
		supported: false,
	}

	r.MustRegister(issueChecker)
	r.MustRegister(errorChecker)
	r.MustRegister(unsupportedChecker)

	// Test successful run
	result, err := r.Run(ctx, "issues", checkCtx)
	if err != nil {
		t.Errorf("Run() error = %v, want nil", err)
	}
	if len(result.Issues) != 1 {
		t.Errorf("Run() issues = %d, want 1", len(result.Issues))
	}

	// Test error run
	_, err = r.Run(ctx, "error", checkCtx)
	if err == nil {
		t.Error("Run() expected error, got nil")
	}

	// Test unsupported run
	result, err = r.Run(ctx, "unsupported", checkCtx)
	if err != nil {
		t.Errorf("Run() error = %v, want nil for unsupported", err)
	}
	if result.Error == nil {
		t.Error("Run() expected Error in result for unsupported")
	}

	// Test nonexistent checker
	_, err = r.Run(ctx, "nonexistent", checkCtx)
	if err == nil {
		t.Error("Run() expected error for nonexistent checker")
	}
}

func TestRegistry_RunAll(t *testing.T) {
	t.Run("nil names runs all registered", func(t *testing.T) {
		r := NewRegistry()
		ctx := context.Background()
		checkCtx := &CheckContext{}

		r.MustRegister(&mockChecker{name: "a", supported: true, issues: []Issue{{Type: "A"}}})
		r.MustRegister(&mockChecker{name: "b", supported: true, issues: []Issue{{Type: "B"}}})
		r.MustRegister(&mockChecker{name: "c", supported: false})

		results := r.RunAll(ctx, checkCtx, nil)

		if len(results) != 3 {
			t.Errorf("RunAll(nil) results = %d, want 3", len(results))
		}

		if results["a"] == nil || len(results["a"].Issues) != 1 {
			t.Error("RunAll(nil) expected result for 'a' with 1 issue")
		}
		if results["b"] == nil || len(results["b"].Issues) != 1 {
			t.Error("RunAll(nil) expected result for 'b' with 1 issue")
		}
		if results["c"] == nil || results["c"].Error == nil {
			t.Error("RunAll(nil) expected error result for unsupported 'c'")
		}
	})

	t.Run("explicit names runs only specified", func(t *testing.T) {
		r := NewRegistry()
		ctx := context.Background()
		checkCtx := &CheckContext{}

		r.MustRegister(&mockChecker{name: "a", supported: true, issues: []Issue{{Type: "A"}}})
		r.MustRegister(&mockChecker{name: "b", supported: true, issues: []Issue{{Type: "B"}}})
		r.MustRegister(&mockChecker{name: "c", supported: false})

		results := r.RunAll(ctx, checkCtx, []string{"a", "c"})

		if len(results) != 2 {
			t.Errorf("RunAll(explicit) results = %d, want 2", len(results))
		}

		if results["a"] == nil || len(results["a"].Issues) != 1 {
			t.Error("RunAll(explicit) expected result for 'a' with 1 issue")
		}
		if results["b"] != nil {
			t.Error("RunAll(explicit) should not contain 'b'")
		}
		if results["c"] == nil || results["c"].Error == nil {
			t.Error("RunAll(explicit) expected error result for unsupported 'c'")
		}
	})
}

func TestRegistry_Aggregate(t *testing.T) {
	r := NewRegistry()

	results := map[string]*CheckResult{
		"a": {
			CheckerName: "a",
			Healthy:     5,
			Issues: []Issue{
				{Severity: SeverityCritical},
				{Severity: SeverityWarning},
			},
		},
		"b": {
			CheckerName: "b",
			Healthy:     3,
			Issues: []Issue{
				{Severity: SeverityWarning},
				{Severity: SeverityInfo},
			},
		},
		"c": {
			CheckerName: "c",
			Error:       errors.New("failed"),
		},
	}

	agg := r.Aggregate(results)

	if agg.TotalHealthy != 8 {
		t.Errorf("Aggregate() TotalHealthy = %d, want 8", agg.TotalHealthy)
	}

	if agg.TotalIssues != 4 {
		t.Errorf("Aggregate() TotalIssues = %d, want 4", agg.TotalIssues)
	}

	if agg.BySeverity[SeverityCritical] != 1 {
		t.Errorf("Aggregate() Critical = %d, want 1", agg.BySeverity[SeverityCritical])
	}

	if agg.BySeverity[SeverityWarning] != 2 {
		t.Errorf("Aggregate() Warning = %d, want 2", agg.BySeverity[SeverityWarning])
	}

	if len(agg.CheckerErrors) != 1 {
		t.Errorf("Aggregate() CheckerErrors = %d, want 1", len(agg.CheckerErrors))
	}
}
