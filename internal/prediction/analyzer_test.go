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
	"math"
	"testing"
	"time"

	"github.com/osagberg/kube-assist-operator/internal/history"
)

const trendStable = "stable"

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
			wantDir: trendStable,
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
		return
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
		return
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

func TestAnalyze_IdenticalScores(t *testing.T) {
	snapshots := makeSnapshots(10, 80, 0) // all scores = 80
	result := Analyze(snapshots)
	if result == nil {
		t.Fatal("expected non-nil result")
		return
	}
	if result.TrendDirection != trendStable {
		t.Errorf("expected stable, got %q", result.TrendDirection)
	}
	if result.RSquared != 1.0 {
		t.Errorf("expected R-squared = 1.0 for identical scores, got %.4f", result.RSquared)
	}
	if result.ProjectedScore != 80 {
		t.Errorf("expected projected score = 80, got %.1f", result.ProjectedScore)
	}
	// CI should be tight around 80
	if result.ConfidenceInterval[0] < 79.9 || result.ConfidenceInterval[1] > 80.1 {
		t.Errorf("expected tight CI around 80, got [%.2f, %.2f]",
			result.ConfidenceInterval[0], result.ConfidenceInterval[1])
	}
}

func TestAnalyze_IdenticalTimestamps(t *testing.T) {
	base := time.Now()
	snapshots := make([]history.HealthSnapshot, 6)
	for i := range snapshots {
		snapshots[i] = history.HealthSnapshot{
			Timestamp:   base, // all same timestamp
			HealthScore: 50 + float64(i),
			BySeverity:  map[string]int{},
			ByChecker:   map[string]int{},
		}
	}
	result := Analyze(snapshots)
	if result == nil {
		t.Fatal("expected non-nil result")
		return
	}
	if result.TrendDirection != trendStable {
		t.Errorf("expected stable, got %q", result.TrendDirection)
	}
	// Should return last score
	if result.ProjectedScore != 55 {
		t.Errorf("expected projected score = 55 (last score), got %.1f", result.ProjectedScore)
	}
}

func TestAnalyze_CIInvariant(t *testing.T) {
	datasets := []struct {
		name  string
		snaps []history.HealthSnapshot
	}{
		{"improving", makeSnapshots(10, 50, 2)},
		{"degrading", makeSnapshots(10, 90, -2)},
		{trendStable, makeSnapshots(10, 70, 0)},
		{"noisy", makeNoisySnapshots(20, 60, 1, 5)},
	}
	for _, ds := range datasets {
		t.Run(ds.name, func(t *testing.T) {
			result := Analyze(ds.snaps)
			if result == nil {
				t.Fatal("expected non-nil result")
				return
			}
			if result.ConfidenceInterval[0] > result.ProjectedScore {
				t.Errorf("CI lower (%.2f) > projected (%.2f)",
					result.ConfidenceInterval[0], result.ProjectedScore)
			}
			if result.ConfidenceInterval[1] < result.ProjectedScore {
				t.Errorf("CI upper (%.2f) < projected (%.2f)",
					result.ConfidenceInterval[1], result.ProjectedScore)
			}
		})
	}
}

func TestAnalyze_CIWidensForExtrapolation(t *testing.T) {
	// More spread in the data means wider CI for the same 1hr projection.
	// Use delta=0 so projected score stays near startScore (50), avoiding clamping.
	tight := makeNoisySnapshots(20, 50, 0, 0.1) // tiny noise
	wide := makeNoisySnapshots(20, 50, 0, 8)    // large noise

	resultTight := Analyze(tight)
	resultWide := Analyze(wide)
	if resultTight == nil || resultWide == nil {
		t.Fatal("expected non-nil results")
	}

	ciWidthTight := resultTight.ConfidenceInterval[1] - resultTight.ConfidenceInterval[0]
	ciWidthWide := resultWide.ConfidenceInterval[1] - resultWide.ConfidenceInterval[0]

	if ciWidthWide <= ciWidthTight {
		t.Errorf("expected wider CI for noisier data: tight=%.2f, wide=%.2f",
			ciWidthTight, ciWidthWide)
	}
}

func TestFindRiskyCheckers(t *testing.T) {
	t.Run("checker trending up", func(t *testing.T) {
		base := time.Now().Add(-10 * 30 * time.Second)
		snapshots := make([]history.HealthSnapshot, 10)
		for i := range snapshots {
			issues := 1
			if i >= 5 {
				issues = 5 // second half has significantly more issues
			}
			snapshots[i] = history.HealthSnapshot{
				Timestamp:   base.Add(time.Duration(i) * 30 * time.Second),
				HealthScore: 80,
				BySeverity:  map[string]int{"Critical": 0, "Warning": 0, "Info": 0},
				ByChecker:   map[string]int{"bad-checker": issues},
			}
		}
		risky := findRiskyCheckers(snapshots)
		found := false
		for _, c := range risky {
			if c == "bad-checker" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected 'bad-checker' in risky list, got %v", risky)
		}
	})

	t.Run("checker stable", func(t *testing.T) {
		snapshots := makeSnapshots(10, 80, 0) // stable, same checker counts
		risky := findRiskyCheckers(snapshots)
		if len(risky) != 0 {
			t.Errorf("expected no risky checkers for stable data, got %v", risky)
		}
	})

	t.Run("empty checkers", func(t *testing.T) {
		base := time.Now().Add(-10 * 30 * time.Second)
		snapshots := make([]history.HealthSnapshot, 6)
		for i := range snapshots {
			snapshots[i] = history.HealthSnapshot{
				Timestamp:   base.Add(time.Duration(i) * 30 * time.Second),
				HealthScore: 90,
				BySeverity:  map[string]int{},
				ByChecker:   map[string]int{},
			}
		}
		risky := findRiskyCheckers(snapshots)
		if len(risky) != 0 {
			t.Errorf("expected no risky checkers for empty data, got %v", risky)
		}
	})

	t.Run("insufficient data", func(t *testing.T) {
		snapshots := makeSnapshots(3, 80, 0)
		risky := findRiskyCheckers(snapshots)
		if risky != nil {
			t.Errorf("expected nil for insufficient data, got %v", risky)
		}
	})
}

func TestComputeSeverityTrajectories(t *testing.T) {
	t.Run("increasing severity", func(t *testing.T) {
		base := time.Now().Add(-10 * 30 * time.Second)
		snapshots := make([]history.HealthSnapshot, 10)
		for i := range snapshots {
			crit := 1
			if i >= 5 {
				crit = 5
			}
			snapshots[i] = history.HealthSnapshot{
				Timestamp:   base.Add(time.Duration(i) * 30 * time.Second),
				HealthScore: 80,
				BySeverity:  map[string]int{"Critical": crit, "Warning": 2, "Info": 2},
				ByChecker:   map[string]int{},
			}
		}
		traj := computeSeverityTrajectories(snapshots)
		if traj["Critical"] != "increasing" {
			t.Errorf("expected Critical=increasing, got %q", traj["Critical"])
		}
		if traj["Warning"] != trendStable {
			t.Errorf("expected Warning=stable, got %q", traj["Warning"])
		}
	})

	t.Run("decreasing severity", func(t *testing.T) {
		base := time.Now().Add(-10 * 30 * time.Second)
		snapshots := make([]history.HealthSnapshot, 10)
		for i := range snapshots {
			warn := 5
			if i >= 5 {
				warn = 1
			}
			snapshots[i] = history.HealthSnapshot{
				Timestamp:   base.Add(time.Duration(i) * 30 * time.Second),
				HealthScore: 80,
				BySeverity:  map[string]int{"Critical": 1, "Warning": warn, "Info": 1},
				ByChecker:   map[string]int{},
			}
		}
		traj := computeSeverityTrajectories(snapshots)
		if traj["Warning"] != "decreasing" {
			t.Errorf("expected Warning=decreasing, got %q", traj["Warning"])
		}
	})

	t.Run("stable severity", func(t *testing.T) {
		snapshots := makeSnapshots(10, 80, 0)
		traj := computeSeverityTrajectories(snapshots)
		for _, sev := range []string{"Critical", "Warning", "Info"} {
			if traj[sev] != trendStable {
				t.Errorf("expected %s=stable, got %q", sev, traj[sev])
			}
		}
	})

	t.Run("insufficient data", func(t *testing.T) {
		snapshots := makeSnapshots(3, 80, 0)
		traj := computeSeverityTrajectories(snapshots)
		if traj != nil {
			t.Errorf("expected nil for insufficient data, got %v", traj)
		}
	})
}

func TestAnalyze_NaNInput(t *testing.T) {
	base := time.Now().Add(-10 * 30 * time.Second)
	snapshots := make([]history.HealthSnapshot, 6)
	for i := range snapshots {
		score := float64(80)
		if i == 3 {
			score = math.NaN()
		}
		snapshots[i] = history.HealthSnapshot{
			Timestamp:   base.Add(time.Duration(i) * 30 * time.Second),
			HealthScore: score,
			BySeverity:  map[string]int{"Critical": 0, "Warning": 0, "Info": 0},
			ByChecker:   map[string]int{},
		}
	}
	// Should not panic
	result := Analyze(snapshots)
	// result may be non-nil with NaN values, but it must not panic
	_ = result
}

// makeNoisySnapshots creates snapshots with a linear trend plus deterministic noise.
func makeNoisySnapshots(n int, startScore, delta, noiseAmp float64) []history.HealthSnapshot {
	base := time.Now().Add(-time.Duration(n) * 30 * time.Second)
	snapshots := make([]history.HealthSnapshot, n)
	for i := range n {
		// Deterministic "noise" using a simple pattern
		noise := noiseAmp * math.Sin(float64(i)*2.5)
		score := startScore + float64(i)*delta + noise
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
