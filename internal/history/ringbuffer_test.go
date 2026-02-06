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

package history

import (
	"sync"
	"testing"
	"time"
)

func TestRingBuffer_AddAndLatest(t *testing.T) {
	tests := []struct {
		name      string
		capacity  int
		add       []HealthSnapshot
		wantOk    bool
		wantScore float64
	}{
		{
			name:     "empty buffer",
			capacity: 5,
			add:      nil,
			wantOk:   false,
		},
		{
			name:     "single item",
			capacity: 5,
			add: []HealthSnapshot{
				{HealthScore: 90.0},
			},
			wantOk:    true,
			wantScore: 90.0,
		},
		{
			name:     "multiple items returns latest",
			capacity: 5,
			add: []HealthSnapshot{
				{HealthScore: 80.0},
				{HealthScore: 90.0},
				{HealthScore: 95.0},
			},
			wantOk:    true,
			wantScore: 95.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := New(tt.capacity)
			for _, s := range tt.add {
				rb.Add(s)
			}
			got, ok := rb.Latest()
			if ok != tt.wantOk {
				t.Errorf("Latest() ok = %v, want %v", ok, tt.wantOk)
			}
			if ok && got.HealthScore != tt.wantScore {
				t.Errorf("Latest() score = %v, want %v", got.HealthScore, tt.wantScore)
			}
		})
	}
}

func TestRingBuffer_Last(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
		add      []float64
		lastN    int
		want     []float64
	}{
		{
			name:     "empty buffer",
			capacity: 5,
			add:      nil,
			lastN:    3,
			want:     nil,
		},
		{
			name:     "fewer than n items",
			capacity: 5,
			add:      []float64{1, 2},
			lastN:    5,
			want:     []float64{1, 2},
		},
		{
			name:     "exact n items",
			capacity: 5,
			add:      []float64{1, 2, 3},
			lastN:    3,
			want:     []float64{1, 2, 3},
		},
		{
			name:     "more items than n",
			capacity: 5,
			add:      []float64{1, 2, 3, 4, 5},
			lastN:    3,
			want:     []float64{3, 4, 5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := New(tt.capacity)
			for _, score := range tt.add {
				rb.Add(HealthSnapshot{HealthScore: score})
			}
			got := rb.Last(tt.lastN)
			if len(got) != len(tt.want) {
				t.Fatalf("Last(%d) len = %d, want %d", tt.lastN, len(got), len(tt.want))
			}
			for i, s := range got {
				if s.HealthScore != tt.want[i] {
					t.Errorf("Last(%d)[%d] score = %v, want %v", tt.lastN, i, s.HealthScore, tt.want[i])
				}
			}
		})
	}
}

func TestRingBuffer_Since(t *testing.T) {
	now := time.Now()

	rb := New(10)
	rb.Add(HealthSnapshot{Timestamp: now.Add(-3 * time.Minute), HealthScore: 80})
	rb.Add(HealthSnapshot{Timestamp: now.Add(-2 * time.Minute), HealthScore: 85})
	rb.Add(HealthSnapshot{Timestamp: now.Add(-1 * time.Minute), HealthScore: 90})
	rb.Add(HealthSnapshot{Timestamp: now, HealthScore: 95})

	tests := []struct {
		name  string
		since time.Time
		want  int
	}{
		{"all", now.Add(-5 * time.Minute), 4},
		{"last two", now.Add(-1 * time.Minute), 2},
		{"exact match", now, 1},
		{"none", now.Add(1 * time.Minute), 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rb.Since(tt.since)
			if len(got) != tt.want {
				t.Errorf("Since() len = %d, want %d", len(got), tt.want)
			}
		})
	}
}

func TestRingBuffer_Wraparound(t *testing.T) {
	rb := New(3) // capacity 3

	// Add 5 items - should wrap and only keep last 3
	for i := 1; i <= 5; i++ {
		rb.Add(HealthSnapshot{HealthScore: float64(i * 10)})
	}

	if rb.Len() != 3 {
		t.Errorf("Len() = %d, want 3", rb.Len())
	}

	got := rb.Last(3)
	if len(got) != 3 {
		t.Fatalf("Last(3) len = %d, want 3", len(got))
	}

	// Should have scores 30, 40, 50 in chronological order
	expected := []float64{30, 40, 50}
	for i, s := range got {
		if s.HealthScore != expected[i] {
			t.Errorf("Last(3)[%d] score = %v, want %v", i, s.HealthScore, expected[i])
		}
	}

	latest, ok := rb.Latest()
	if !ok || latest.HealthScore != 50 {
		t.Errorf("Latest() score = %v, want 50", latest.HealthScore)
	}
}

func TestRingBuffer_ThreadSafety(t *testing.T) {
	rb := New(100)

	var wg sync.WaitGroup

	// Concurrent writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				rb.Add(HealthSnapshot{HealthScore: float64(id*100 + j)})
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = rb.Last(10)
				_, _ = rb.Latest()
				_ = rb.Len()
			}
		}()
	}

	wg.Wait()

	// Should not have panicked and buffer should be valid
	if rb.Len() > 100 {
		t.Errorf("Len() = %d, exceeds capacity 100", rb.Len())
	}
}

func TestComputeScore(t *testing.T) {
	tests := []struct {
		name    string
		healthy int
		issues  int
		want    float64
	}{
		{"both zero", 0, 0, 100},
		{"all healthy", 10, 0, 100},
		{"equal", 5, 5, 50},
		{"all issues", 0, 10, 0},
		{"mostly healthy", 9, 1, 90},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeScore(tt.healthy, tt.issues)
			if got != tt.want {
				t.Errorf("ComputeScore(%d, %d) = %v, want %v", tt.healthy, tt.issues, got, tt.want)
			}
		})
	}
}
