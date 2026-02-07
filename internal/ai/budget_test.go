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

package ai

import (
	"errors"
	"testing"
	"time"
)

func TestBudget_NilSafe(t *testing.T) {
	var b *Budget

	// All methods should be safe on nil
	if err := b.CheckAllowance(1000); err != nil {
		t.Errorf("nil budget CheckAllowance() = %v, want nil", err)
	}

	// Should not panic
	b.RecordUsage(500)

	usage := b.GetUsage()
	if usage != nil {
		t.Errorf("nil budget GetUsage() = %v, want nil", usage)
	}
}

func TestBudget_WithinBudget(t *testing.T) {
	b := NewBudget([]BudgetWindow{
		{Name: "daily", Duration: 24 * time.Hour, Limit: 10000},
	})

	if err := b.CheckAllowance(5000); err != nil {
		t.Errorf("CheckAllowance(5000) under limit = %v, want nil", err)
	}

	b.RecordUsage(3000)

	if err := b.CheckAllowance(5000); err != nil {
		t.Errorf("CheckAllowance(5000) with 3000 used of 10000 = %v, want nil", err)
	}

	usage := b.GetUsage()
	if len(usage) != 1 {
		t.Fatalf("GetUsage() len = %d, want 1", len(usage))
	}
	if usage[0].Used != 3000 {
		t.Errorf("GetUsage()[0].Used = %d, want 3000", usage[0].Used)
	}
}

func TestBudget_ExceedsBudget(t *testing.T) {
	b := NewBudget([]BudgetWindow{
		{Name: "daily", Duration: 24 * time.Hour, Limit: 5000},
	})

	b.RecordUsage(4000)

	err := b.CheckAllowance(2000)
	if err == nil {
		t.Fatal("CheckAllowance(2000) with 4000/5000 used should return error")
	}
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Errorf("error should wrap ErrBudgetExceeded, got: %v", err)
	}
}

func TestBudget_WindowReset(t *testing.T) {
	b := NewBudget([]BudgetWindow{
		{Name: "short", Duration: 1 * time.Millisecond, Limit: 1000},
	})

	b.RecordUsage(900)

	// Should fail before window expires
	if err := b.CheckAllowance(200); err == nil {
		t.Error("CheckAllowance should fail before window expires")
	}

	// Wait for window to expire
	time.Sleep(2 * time.Millisecond)

	// After expiry, CheckAllowance should pass (window will reset on next RecordUsage)
	if err := b.CheckAllowance(200); err != nil {
		t.Errorf("CheckAllowance after window expiry = %v, want nil", err)
	}

	// RecordUsage should reset the expired window
	b.RecordUsage(100)

	usage := b.GetUsage()
	if len(usage) != 1 {
		t.Fatalf("GetUsage() len = %d, want 1", len(usage))
	}
	if usage[0].Used != 100 {
		t.Errorf("after reset, Used = %d, want 100", usage[0].Used)
	}
}

func TestBudget_MultiWindow(t *testing.T) {
	b := NewBudget([]BudgetWindow{
		{Name: "daily", Duration: 24 * time.Hour, Limit: 5000},
		{Name: "monthly", Duration: 30 * 24 * time.Hour, Limit: 50000},
	})

	b.RecordUsage(3000)

	// Within both windows
	if err := b.CheckAllowance(1000); err != nil {
		t.Errorf("within both windows: %v", err)
	}

	// Exceeds daily but not monthly
	err := b.CheckAllowance(3000)
	if err == nil {
		t.Fatal("should exceed daily window")
	}
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Errorf("error should wrap ErrBudgetExceeded, got: %v", err)
	}

	usage := b.GetUsage()
	if len(usage) != 2 {
		t.Fatalf("GetUsage() len = %d, want 2", len(usage))
	}
	if usage[0].Name != "daily" || usage[0].Used != 3000 {
		t.Errorf("daily usage: name=%s, used=%d", usage[0].Name, usage[0].Used)
	}
	if usage[1].Name != "monthly" || usage[1].Used != 3000 {
		t.Errorf("monthly usage: name=%s, used=%d", usage[1].Name, usage[1].Used)
	}
}
