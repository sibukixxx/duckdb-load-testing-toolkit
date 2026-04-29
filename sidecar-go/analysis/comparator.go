package analysis

import (
	"database/sql"
	"fmt"
	"math"
)

// RunStats holds statistics for a single run
type RunStats struct {
	RunID           string
	TotalRequests   int64
	ErrorCount      int64
	ErrorRate       float64
	AvgRTT          float64
	P50RTT          float64
	P95RTT          float64
	P99RTT          float64
	MinRTT          float64
	MaxRTT          float64
	RPS             float64
	DurationSeconds float64
}

// ComparisonResult holds the comparison between baseline and current run
type ComparisonResult struct {
	Baseline    *RunStats
	Current     *RunStats
	Regressions []Regression
	IsPassing   bool
}

// Regression represents a detected performance regression
type Regression struct {
	Metric        string
	BaselineValue float64
	CurrentValue  float64
	ChangePercent float64
	Threshold     float64
	Severity      string // "warning", "critical"
}

// Thresholds defines acceptable changes for each metric
type Thresholds struct {
	P50RTTIncreasePercent    float64 // e.g., 10.0 means 10% increase is allowed
	P95RTTIncreasePercent    float64
	P99RTTIncreasePercent    float64
	ErrorRateIncreasePercent float64
	ErrorRateAbsoluteMax     float64 // Maximum absolute error rate
	RPSDecreasePercent       float64
}

// DefaultThresholds returns sensible default thresholds
func DefaultThresholds() Thresholds {
	return Thresholds{
		P50RTTIncreasePercent:    10.0,
		P95RTTIncreasePercent:    15.0,
		P99RTTIncreasePercent:    20.0,
		ErrorRateIncreasePercent: 50.0,
		ErrorRateAbsoluteMax:     5.0,
		RPSDecreasePercent:       10.0,
	}
}

// Comparator compares test runs for regression detection
type Comparator struct {
	db         *sql.DB
	thresholds Thresholds
}

// NewComparator creates a new Comparator
func NewComparator(db *sql.DB, thresholds Thresholds) *Comparator {
	return &Comparator{
		db:         db,
		thresholds: thresholds,
	}
}

// GetRunStats calculates statistics for a specific run
func (c *Comparator) GetRunStats(runID string) (*RunStats, error) {
	query := `
	SELECT
		COUNT(*) as total_requests,
		COUNT(CASE WHEN status >= 400 OR error_code != '' AND error_code IS NOT NULL THEN 1 END) as error_count,
		AVG(rtt) as avg_rtt,
		PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY rtt) as p50_rtt,
		PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY rtt) as p95_rtt,
		PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY rtt) as p99_rtt,
		MIN(rtt) as min_rtt,
		MAX(rtt) as max_rtt,
		MIN(ts) as start_ts,
		MAX(ts) as end_ts
	FROM metrics
	WHERE run_id = ?
	`

	row := c.db.QueryRow(query, runID)

	stats := &RunStats{RunID: runID}
	var startTs, endTs int64

	err := row.Scan(
		&stats.TotalRequests,
		&stats.ErrorCount,
		&stats.AvgRTT,
		&stats.P50RTT,
		&stats.P95RTT,
		&stats.P99RTT,
		&stats.MinRTT,
		&stats.MaxRTT,
		&startTs,
		&endTs,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get run stats: %w", err)
	}

	// Calculate derived metrics
	if stats.TotalRequests > 0 {
		stats.ErrorRate = float64(stats.ErrorCount) / float64(stats.TotalRequests) * 100
	}
	stats.DurationSeconds = float64(endTs-startTs) / 1000.0
	if stats.DurationSeconds > 0 {
		stats.RPS = float64(stats.TotalRequests) / stats.DurationSeconds
	}

	return stats, nil
}

// Compare compares two runs and detects regressions
func (c *Comparator) Compare(baselineID, currentID string) (*ComparisonResult, error) {
	baseline, err := c.GetRunStats(baselineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get baseline stats: %w", err)
	}

	current, err := c.GetRunStats(currentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get current stats: %w", err)
	}

	result := &ComparisonResult{
		Baseline:    baseline,
		Current:     current,
		Regressions: []Regression{},
		IsPassing:   true,
	}

	// Check P50 RTT
	if reg := c.checkRegression("P50_RTT", baseline.P50RTT, current.P50RTT, c.thresholds.P50RTTIncreasePercent); reg != nil {
		result.Regressions = append(result.Regressions, *reg)
		if reg.Severity == "critical" {
			result.IsPassing = false
		}
	}

	// Check P95 RTT
	if reg := c.checkRegression("P95_RTT", baseline.P95RTT, current.P95RTT, c.thresholds.P95RTTIncreasePercent); reg != nil {
		result.Regressions = append(result.Regressions, *reg)
		if reg.Severity == "critical" {
			result.IsPassing = false
		}
	}

	// Check P99 RTT
	if reg := c.checkRegression("P99_RTT", baseline.P99RTT, current.P99RTT, c.thresholds.P99RTTIncreasePercent); reg != nil {
		result.Regressions = append(result.Regressions, *reg)
		if reg.Severity == "critical" {
			result.IsPassing = false
		}
	}

	// Check Error Rate (increase)
	if reg := c.checkRegression("ERROR_RATE", baseline.ErrorRate, current.ErrorRate, c.thresholds.ErrorRateIncreasePercent); reg != nil {
		result.Regressions = append(result.Regressions, *reg)
		if reg.Severity == "critical" {
			result.IsPassing = false
		}
	}

	// Check absolute error rate
	if current.ErrorRate > c.thresholds.ErrorRateAbsoluteMax {
		result.Regressions = append(result.Regressions, Regression{
			Metric:        "ERROR_RATE_ABSOLUTE",
			BaselineValue: baseline.ErrorRate,
			CurrentValue:  current.ErrorRate,
			ChangePercent: 0,
			Threshold:     c.thresholds.ErrorRateAbsoluteMax,
			Severity:      "critical",
		})
		result.IsPassing = false
	}

	// Check RPS (decrease)
	if reg := c.checkRPSRegression("RPS", baseline.RPS, current.RPS, c.thresholds.RPSDecreasePercent); reg != nil {
		result.Regressions = append(result.Regressions, *reg)
		if reg.Severity == "critical" {
			result.IsPassing = false
		}
	}

	return result, nil
}

// checkRegression checks if a metric has increased beyond threshold
func (c *Comparator) checkRegression(metric string, baseline, current, threshold float64) *Regression {
	if baseline == 0 {
		return nil
	}

	changePercent := ((current - baseline) / baseline) * 100

	if changePercent > threshold {
		severity := "warning"
		if changePercent > threshold*2 {
			severity = "critical"
		}

		return &Regression{
			Metric:        metric,
			BaselineValue: baseline,
			CurrentValue:  current,
			ChangePercent: changePercent,
			Threshold:     threshold,
			Severity:      severity,
		}
	}

	return nil
}

// checkRPSRegression checks if RPS has decreased beyond threshold
func (c *Comparator) checkRPSRegression(metric string, baseline, current, threshold float64) *Regression {
	if baseline == 0 {
		return nil
	}

	// For RPS, we check for decrease
	changePercent := ((baseline - current) / baseline) * 100

	if changePercent > threshold {
		severity := "warning"
		if changePercent > threshold*2 {
			severity = "critical"
		}

		return &Regression{
			Metric:        metric,
			BaselineValue: baseline,
			CurrentValue:  current,
			ChangePercent: -changePercent, // Negative to indicate decrease
			Threshold:     threshold,
			Severity:      severity,
		}
	}

	return nil
}

// GetHistoricalTrend returns statistics for multiple runs over time
func (c *Comparator) GetHistoricalTrend(limit int) ([]RunStats, error) {
	query := `
	SELECT
		run_id,
		COUNT(*) as total_requests,
		COUNT(CASE WHEN status >= 400 OR (error_code != '' AND error_code IS NOT NULL) THEN 1 END) as error_count,
		AVG(rtt) as avg_rtt,
		PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY rtt) as p50_rtt,
		PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY rtt) as p95_rtt,
		PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY rtt) as p99_rtt,
		MIN(rtt) as min_rtt,
		MAX(rtt) as max_rtt,
		MIN(ts) as start_ts,
		MAX(ts) as end_ts
	FROM metrics
	GROUP BY run_id
	ORDER BY MIN(ts) DESC
	LIMIT ?
	`

	rows, err := c.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get historical trend: %w", err)
	}
	defer rows.Close()

	var results []RunStats
	for rows.Next() {
		var stats RunStats
		var startTs, endTs int64

		err := rows.Scan(
			&stats.RunID,
			&stats.TotalRequests,
			&stats.ErrorCount,
			&stats.AvgRTT,
			&stats.P50RTT,
			&stats.P95RTT,
			&stats.P99RTT,
			&stats.MinRTT,
			&stats.MaxRTT,
			&startTs,
			&endTs,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		if stats.TotalRequests > 0 {
			stats.ErrorRate = float64(stats.ErrorCount) / float64(stats.TotalRequests) * 100
		}
		stats.DurationSeconds = float64(endTs-startTs) / 1000.0
		if stats.DurationSeconds > 0 {
			stats.RPS = float64(stats.TotalRequests) / stats.DurationSeconds
		}

		results = append(results, stats)
	}

	return results, nil
}

// CalculateBaseline calculates a baseline from multiple runs
func (c *Comparator) CalculateBaseline(runIDs []string) (*RunStats, error) {
	if len(runIDs) == 0 {
		return nil, fmt.Errorf("no run IDs provided")
	}

	var allStats []RunStats
	for _, runID := range runIDs {
		stats, err := c.GetRunStats(runID)
		if err != nil {
			continue // Skip failed runs
		}
		allStats = append(allStats, *stats)
	}

	if len(allStats) == 0 {
		return nil, fmt.Errorf("no valid runs found")
	}

	// Calculate averages
	baseline := &RunStats{RunID: "baseline"}
	for _, s := range allStats {
		baseline.TotalRequests += s.TotalRequests
		baseline.ErrorCount += s.ErrorCount
		baseline.AvgRTT += s.AvgRTT
		baseline.P50RTT += s.P50RTT
		baseline.P95RTT += s.P95RTT
		baseline.P99RTT += s.P99RTT
		baseline.RPS += s.RPS
	}

	n := float64(len(allStats))
	baseline.AvgRTT /= n
	baseline.P50RTT /= n
	baseline.P95RTT /= n
	baseline.P99RTT /= n
	baseline.RPS /= n

	if baseline.TotalRequests > 0 {
		baseline.ErrorRate = float64(baseline.ErrorCount) / float64(baseline.TotalRequests) * 100
	}

	return baseline, nil
}

// Round rounds a float to specified decimal places
func Round(val float64, precision int) float64 {
	ratio := math.Pow(10, float64(precision))
	return math.Round(val*ratio) / ratio
}
