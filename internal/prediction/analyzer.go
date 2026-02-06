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

	"github.com/osagberg/kube-assist-operator/internal/history"
)

// MinDataPoints is the minimum number of snapshots required for prediction
const MinDataPoints = 5

// PredictionResult contains the trend analysis output
type PredictionResult struct {
	// TrendDirection is "improving", "stable", or "degrading"
	TrendDirection string `json:"trendDirection"`

	// Velocity is the rate of change in health score per hour
	// Positive = improving, negative = degrading
	Velocity float64 `json:"velocity"`

	// ProjectedScore is the predicted health score 1 hour from now, clamped to [0, 100]
	ProjectedScore float64 `json:"projectedScore"`

	// ConfidenceInterval is the 95% prediction interval [lower, upper]
	ConfidenceInterval [2]float64 `json:"confidenceInterval"`

	// RSquared is the coefficient of determination (goodness of fit), 0-1
	RSquared float64 `json:"rSquared"`

	// DataPoints is the number of snapshots used for the prediction
	DataPoints int `json:"dataPoints"`

	// RiskyCheckers lists checker names with increasing issue counts
	RiskyCheckers []string `json:"riskyCheckers,omitempty"`

	// SeverityTrajectories shows the trend for each severity level
	SeverityTrajectories map[string]string `json:"severityTrajectories,omitempty"`
}

// Analyze performs linear regression on health score history.
// Returns nil if fewer than MinDataPoints snapshots are available.
func Analyze(snapshots []history.HealthSnapshot) *PredictionResult {
	if len(snapshots) < MinDataPoints {
		return nil
	}

	// Convert timestamps to seconds from first snapshot (x values)
	// and health scores as y values
	baseTime := snapshots[0].Timestamp
	n := float64(len(snapshots))

	var sumX, sumY, sumXY, sumX2 float64
	xs := make([]float64, len(snapshots))
	ys := make([]float64, len(snapshots))

	for i, snap := range snapshots {
		x := snap.Timestamp.Sub(baseTime).Seconds()
		y := snap.HealthScore
		xs[i] = x
		ys[i] = y
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	// OLS: slope (b1) and intercept (b0)
	denom := n*sumX2 - sumX*sumX
	if denom == 0 {
		// All timestamps are the same — can't compute slope
		return &PredictionResult{
			TrendDirection: "stable",
			Velocity:       0,
			ProjectedScore: snapshots[len(snapshots)-1].HealthScore,
			DataPoints:     len(snapshots),
		}
	}

	slope := (n*sumXY - sumX*sumY) / denom
	intercept := (sumY - slope*sumX) / n

	// R-squared
	meanY := sumY / n
	var ssRes, ssTot float64
	for i := range xs {
		predicted := intercept + slope*xs[i]
		ssRes += (ys[i] - predicted) * (ys[i] - predicted)
		ssTot += (ys[i] - meanY) * (ys[i] - meanY)
	}
	rSquared := 0.0
	if ssTot > 0 {
		rSquared = 1 - ssRes/ssTot
	}
	if rSquared < 0 {
		rSquared = 0
	}

	// Velocity: slope is score/second, convert to score/hour
	velocity := slope * 3600

	// Projected score: 1 hour from the latest snapshot
	latestX := xs[len(xs)-1]
	projectionX := latestX + 3600 // 1 hour ahead
	projectedScore := intercept + slope*projectionX

	// Clamp to [0, 100]
	projectedScore = math.Max(0, math.Min(100, projectedScore))

	// 95% confidence interval (using standard error of estimate)
	var mse float64
	if n > 2 {
		mse = ssRes / (n - 2)
	}
	se := math.Sqrt(mse)
	// Simplified 95% CI: +/-1.96 * se (approximate, assumes normal residuals)
	ci := [2]float64{
		math.Max(0, projectedScore-1.96*se),
		math.Min(100, projectedScore+1.96*se),
	}

	// Determine trend direction
	var trendDirection string
	absVelocity := math.Abs(velocity)
	switch {
	case absVelocity < 0.5: // less than 0.5 points/hour
		trendDirection = "stable"
	case velocity > 0:
		trendDirection = "improving"
	default:
		trendDirection = "degrading"
	}

	// Find risky checkers (increasing issue counts over time)
	riskyCheckers := findRiskyCheckers(snapshots)

	// Severity trajectories
	severityTrajectories := computeSeverityTrajectories(snapshots)

	return &PredictionResult{
		TrendDirection:       trendDirection,
		Velocity:             math.Round(velocity*100) / 100,     // 2 decimal places
		ProjectedScore:       math.Round(projectedScore*10) / 10, // 1 decimal place
		ConfidenceInterval:   ci,
		RSquared:             math.Round(rSquared*1000) / 1000, // 3 decimal places
		DataPoints:           len(snapshots),
		RiskyCheckers:        riskyCheckers,
		SeverityTrajectories: severityTrajectories,
	}
}

// findRiskyCheckers returns checker names whose issue counts are trending upward.
func findRiskyCheckers(snapshots []history.HealthSnapshot) []string {
	if len(snapshots) < MinDataPoints {
		return nil
	}

	// Compare average issue count in first half vs second half
	mid := len(snapshots) / 2
	firstHalf := snapshots[:mid]
	secondHalf := snapshots[mid:]

	checkerTotals := make(map[string][2]float64) // [firstHalfSum, secondHalfSum]

	for _, snap := range firstHalf {
		for checker, count := range snap.ByChecker {
			v := checkerTotals[checker]
			v[0] += float64(count)
			checkerTotals[checker] = v
		}
	}
	for _, snap := range secondHalf {
		for checker, count := range snap.ByChecker {
			v := checkerTotals[checker]
			v[1] += float64(count)
			checkerTotals[checker] = v
		}
	}

	var risky []string
	firstLen := float64(len(firstHalf))
	secondLen := float64(len(secondHalf))
	for checker, totals := range checkerTotals {
		avgFirst := totals[0] / firstLen
		avgSecond := totals[1] / secondLen
		if avgSecond > avgFirst+0.5 { // at least 0.5 more issues on average
			risky = append(risky, checker)
		}
	}
	return risky
}

// computeSeverityTrajectories returns the trend for each severity level.
func computeSeverityTrajectories(snapshots []history.HealthSnapshot) map[string]string {
	if len(snapshots) < MinDataPoints {
		return nil
	}

	mid := len(snapshots) / 2
	firstHalf := snapshots[:mid]
	secondHalf := snapshots[mid:]

	severities := []string{"Critical", "Warning", "Info"}
	trajectories := make(map[string]string)

	for _, sev := range severities {
		var firstSum, secondSum float64
		for _, snap := range firstHalf {
			firstSum += float64(snap.BySeverity[sev])
		}
		for _, snap := range secondHalf {
			secondSum += float64(snap.BySeverity[sev])
		}
		avgFirst := firstSum / float64(len(firstHalf))
		avgSecond := secondSum / float64(len(secondHalf))

		diff := avgSecond - avgFirst
		switch {
		case diff > 0.5:
			trajectories[sev] = "increasing"
		case diff < -0.5:
			trajectories[sev] = "decreasing"
		default:
			trajectories[sev] = "stable"
		}
	}

	return trajectories
}
