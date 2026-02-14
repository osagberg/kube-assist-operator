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
	"sync/atomic"
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

// TryConsume atomically checks allowance and reserves tokens under a single
// write lock, eliminating the check-then-act race in CheckAllowance+RecordUsage.
func (b *Budget) TryConsume(estimatedTokens int) error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	// First pass: check all windows
	for i, w := range b.windows {
		u := b.usage[i]
		if now.Sub(u.startedAt) >= w.Duration {
			continue
		}
		if u.tokens+estimatedTokens > w.Limit {
			return fmt.Errorf("%w: %s window (%d/%d tokens)", ErrBudgetExceeded, w.Name, u.tokens, w.Limit)
		}
	}
	// Second pass: reserve tokens, resetting expired windows
	for i, w := range b.windows {
		if now.Sub(b.usage[i].startedAt) >= w.Duration {
			b.usage[i] = windowUsage{startedAt: now}
		}
		b.usage[i].tokens += estimatedTokens
	}
	return nil
}

// ReleaseUnused returns unused reserved tokens back to the budget.
func (b *Budget) ReleaseUnused(tokens int) {
	if b == nil || tokens <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.usage {
		b.usage[i].tokens -= tokens
		if b.usage[i].tokens < 0 {
			b.usage[i].tokens = 0
		}
	}
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

// ChatBudget tracks aggregate token consumption across all chat sessions.
// It uses an atomic counter for lock-free reads and a configurable maximum.
type ChatBudget struct {
	used atomic.Int64
	max  int64
}

// NewChatBudget creates an aggregate chat token budget. Pass 0 for unlimited.
func NewChatBudget(maxTokens int64) *ChatBudget {
	if maxTokens <= 0 {
		return nil
	}
	return &ChatBudget{max: maxTokens}
}

// TryChatConsume checks the aggregate chat budget and reserves tokens.
// Returns an error if the aggregate limit would be exceeded.
func (cb *ChatBudget) TryChatConsume(tokens int) error {
	if cb == nil {
		return nil
	}
	for {
		cur := cb.used.Load()
		next := cur + int64(tokens)
		if next > cb.max {
			return fmt.Errorf("%w: chat aggregate (%d/%d tokens)", ErrBudgetExceeded, cur, cb.max)
		}
		if cb.used.CompareAndSwap(cur, next) {
			return nil
		}
	}
}

// ChatUsed returns the current aggregate chat token usage.
func (cb *ChatBudget) ChatUsed() int64 {
	if cb == nil {
		return 0
	}
	return cb.used.Load()
}
