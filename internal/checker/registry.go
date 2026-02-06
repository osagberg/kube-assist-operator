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
	"fmt"
	"sync"
)

// Registry manages checker registration and execution
type Registry struct {
	mu       sync.RWMutex
	checkers map[string]Checker
}

// NewRegistry creates a new checker registry
func NewRegistry() *Registry {
	return &Registry{
		checkers: make(map[string]Checker),
	}
}

// Register adds a checker to the registry
func (r *Registry) Register(c Checker) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := c.Name()
	if _, exists := r.checkers[name]; exists {
		return fmt.Errorf("checker %q already registered", name)
	}

	r.checkers[name] = c
	return nil
}

// MustRegister adds a checker to the registry and panics on error
func (r *Registry) MustRegister(c Checker) {
	if err := r.Register(c); err != nil {
		panic(err)
	}
}

// Unregister removes a checker from the registry by name.
// Returns true if the checker was found and removed, false if not found.
func (r *Registry) Unregister(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, exists := r.checkers[name]
	if exists {
		delete(r.checkers, name)
	}
	return exists
}

// Replace adds or replaces a checker in the registry.
// Unlike Register, this does not error if the name already exists.
func (r *Registry) Replace(c Checker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checkers[c.Name()] = c
}

// Get returns a checker by name
func (r *Registry) Get(name string) (Checker, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	c, ok := r.checkers[name]
	return c, ok
}

// List returns all registered checker names
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.checkers))
	for name := range r.checkers {
		names = append(names, name)
	}
	return names
}

// Run executes a single checker by name
func (r *Registry) Run(ctx context.Context, name string, checkCtx *CheckContext) (*CheckResult, error) {
	checker, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("checker %q not found", name)
	}

	if !checker.Supports(ctx, checkCtx.DataSource) {
		return &CheckResult{
			CheckerName: name,
			Error:       fmt.Errorf("checker %q is not supported in this environment", name),
		}, nil
	}

	return checker.Check(ctx, checkCtx)
}

// RunAll executes checkers concurrently and returns results keyed by checker name.
// If names is nil, all registered checkers are run.
func (r *Registry) RunAll(ctx context.Context, checkCtx *CheckContext, names []string) map[string]*CheckResult {
	if names == nil {
		r.mu.RLock()
		names = make([]string, 0, len(r.checkers))
		for name := range r.checkers {
			names = append(names, name)
		}
		r.mu.RUnlock()
	}

	results := make(map[string]*CheckResult)
	resultsChan := make(chan struct {
		name   string
		result *CheckResult
	}, len(names))

	var wg sync.WaitGroup

	for _, name := range names {
		wg.Add(1)
		go func(checkerName string) {
			defer wg.Done()

			result, err := r.Run(ctx, checkerName, checkCtx)
			if err != nil {
				result = &CheckResult{
					CheckerName: checkerName,
					Error:       err,
				}
			}

			resultsChan <- struct {
				name   string
				result *CheckResult
			}{checkerName, result}
		}(name)
	}

	// Close channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results
	for res := range resultsChan {
		results[res.name] = res.result
	}

	return results
}

// AggregateResults combines multiple CheckResults into summary statistics
type AggregateResults struct {
	TotalHealthy  int
	TotalIssues   int
	BySeverity    map[string]int
	ByChecker     map[string]*CheckResult
	CheckerErrors []string
}

// Aggregate combines all results into summary statistics
func (r *Registry) Aggregate(results map[string]*CheckResult) *AggregateResults {
	agg := &AggregateResults{
		BySeverity:    make(map[string]int),
		ByChecker:     results,
		CheckerErrors: make([]string, 0),
	}

	for name, result := range results {
		if result.Error != nil {
			agg.CheckerErrors = append(agg.CheckerErrors, fmt.Sprintf("%s: %v", name, result.Error))
			continue
		}

		agg.TotalHealthy += result.Healthy
		agg.TotalIssues += len(result.Issues)

		for _, issue := range result.Issues {
			agg.BySeverity[issue.Severity]++
		}
	}

	return agg
}
