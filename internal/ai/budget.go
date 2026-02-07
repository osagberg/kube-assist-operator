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
	"fmt"
	"sync"
	"time"
)

// BudgetWindow defines a time-bounded token limit.
type BudgetWindow struct {
	Name     string
	Duration time.Duration
	Limit    int
}

type windowUsage struct {
	tokens    int
	startedAt time.Time
}

// Budget tracks token usage across multiple time windows.
type Budget struct {
	mu      sync.RWMutex
	windows []BudgetWindow
	usage   []windowUsage
}

// NewBudget creates a Budget from the given windows. Pass nil/empty for no limits.
func NewBudget(windows []BudgetWindow) *Budget {
	if len(windows) == 0 {
		return nil
	}
	usage := make([]windowUsage, len(windows))
	now := time.Now()
	for i := range usage {
		usage[i].startedAt = now
	}
	return &Budget{windows: windows, usage: usage}
}

// ErrBudgetExceeded is returned when a token budget window would be exceeded.
var ErrBudgetExceeded = fmt.Errorf("token budget exceeded")

// CheckAllowance returns an error if estimated tokens would exceed any window.
func (b *Budget) CheckAllowance(estimatedTokens int) error {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	now := time.Now()
	for i, w := range b.windows {
		u := b.usage[i]
		if now.Sub(u.startedAt) >= w.Duration {
			continue // window expired, will reset on next RecordUsage
		}
		if u.tokens+estimatedTokens > w.Limit {
			return fmt.Errorf("%w: %s window (%d/%d tokens)", ErrBudgetExceeded, w.Name, u.tokens, w.Limit)
		}
	}
	return nil
}

// RecordUsage records token consumption, resetting expired windows.
func (b *Budget) RecordUsage(tokens int) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for i, w := range b.windows {
		if now.Sub(b.usage[i].startedAt) >= w.Duration {
			b.usage[i] = windowUsage{startedAt: now}
		}
		b.usage[i].tokens += tokens
	}
}

// GetUsage returns current usage for each window (for metrics).
func (b *Budget) GetUsage() []WindowUsage {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	now := time.Now()
	result := make([]WindowUsage, len(b.windows))
	for i, w := range b.windows {
		u := b.usage[i]
		tokens := u.tokens
		if now.Sub(u.startedAt) >= w.Duration {
			tokens = 0
		}
		result[i] = WindowUsage{
			Name:  w.Name,
			Limit: w.Limit,
			Used:  tokens,
		}
	}
	return result
}

// WindowUsage reports current usage for a single budget window.
type WindowUsage struct {
	Name  string
	Limit int
	Used  int
}
