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

package prediction

import (
	"testing"
	"time"

	"github.com/osagberg/kube-assist-operator/internal/history"
)

func TestAnalyze(t *testing.T) {
	tests := []struct {
		name    string
		input   []history.HealthSnapshot
		wantNil bool
		wantDir string // expected trend direction
	}{
		{
			name:    "nil with insufficient data",
			input:   makeSnapshots(3, 90, 0), // only 3 points
			wantNil: true,
		},
		{
			name:    "stable trend",
			input:   makeSnapshots(10, 95, 0), // flat at 95
			wantDir: "stable",
		},
		{
			name:    "improving trend",
			input:   makeSnapshots(10, 70, 3), // increasing by 3 per snapshot
			wantDir: "improving",
		},
		{
			name:    "degrading trend",
			input:   makeSnapshots(10, 95, -3), // decreasing by 3 per snapshot
			wantDir: "degrading",
		},
		{
			name:    "exactly 5 data points",
			input:   makeSnapshots(5, 80, 1),
			wantNil: false,
			wantDir: "improving",
		},
		{
			name:    "projected score clamped to 100",
			input:   makeSnapshots(10, 99, 1), // would project > 100
			wantDir: "improving",
		},
		{
			name:    "projected score clamped to 0",
			input:   makeSnapshots(10, 5, -2), // would project < 0
			wantDir: "degrading",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Analyze(tt.input)
			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil, got %+v", result)
				}
				return
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}
			if result.TrendDirection != tt.wantDir {
				t.Errorf("expected direction %q, got %q (velocity=%.2f)", tt.wantDir, result.TrendDirection, result.Velocity)
			}
			if result.ProjectedScore < 0 || result.ProjectedScore > 100 {
				t.Errorf("projected score out of range: %.1f", result.ProjectedScore)
			}
			if result.DataPoints != len(tt.input) {
				t.Errorf("expected %d data points, got %d", len(tt.input), result.DataPoints)
			}
		})
	}
}

func TestAnalyze_RSquared(t *testing.T) {
	// Perfect linear data should have R-squared close to 1
	snapshots := makeSnapshots(10, 50, 5) // perfectly linear
	result := Analyze(snapshots)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.RSquared < 0.99 {
		t.Errorf("expected R-squared close to 1 for linear data, got %.4f", result.RSquared)
	}
}

func TestAnalyze_ConfidenceInterval(t *testing.T) {
	snapshots := makeSnapshots(10, 80, -1)
	result := Analyze(snapshots)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.ConfidenceInterval[0] > result.ProjectedScore {
		t.Errorf("lower CI bound (%.1f) should be <= projected score (%.1f)",
			result.ConfidenceInterval[0], result.ProjectedScore)
	}
	if result.ConfidenceInterval[1] < result.ProjectedScore {
		t.Errorf("upper CI bound (%.1f) should be >= projected score (%.1f)",
			result.ConfidenceInterval[1], result.ProjectedScore)
	}
}

// makeSnapshots creates n snapshots starting at the given score,
// incrementing by delta per snapshot, spaced 30 seconds apart.
func makeSnapshots(n int, startScore, delta float64) []history.HealthSnapshot {
	base := time.Now().Add(-time.Duration(n) * 30 * time.Second)
	snapshots := make([]history.HealthSnapshot, n)
	for i := range n {
		score := startScore + float64(i)*delta
		if score < 0 {
			score = 0
		}
		if score > 100 {
			score = 100
		}
		healthy := int(score)
		issues := 100 - healthy
		snapshots[i] = history.HealthSnapshot{
			Timestamp:    base.Add(time.Duration(i) * 30 * time.Second),
			TotalHealthy: healthy,
			TotalIssues:  issues,
			BySeverity:   map[string]int{"Critical": issues / 3, "Warning": issues / 3, "Info": issues / 3},
			ByChecker:    map[string]int{"workloads": issues / 2, "helmreleases": issues / 2},
			HealthScore:  score,
		}
	}
	return snapshots
}
