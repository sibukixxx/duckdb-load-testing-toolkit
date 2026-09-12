package analysis

import "fmt"

// RegressionFinding is one machine-checkable regression: a metric, the
// scope it was measured over (an endpoint, or "overall"), the baseline and
// current values, the percentage change, and a severity derived from
// Thresholds. It mirrors the run-level Regression type but adds a Scope so
// findings can be attributed to a single endpoint.
type RegressionFinding struct {
	Metric        string  `json:"metric"`
	Scope         string  `json:"scope"`
	Baseline      float64 `json:"baseline"`
	Current       float64 `json:"current"`
	ChangePercent float64 `json:"change_percent"`
	Severity      string  `json:"severity"`
}

// EndpointComparison holds baseline vs. current deltas for one endpoint,
// covering the percentile and error-rate metrics load-test comparisons
// care about.
type EndpointComparison struct {
	Endpoint       string
	Baseline       EndpointStats
	Current        EndpointStats
	P50DeltaMs     float64
	P50ChangePct   float64
	P95DeltaMs     float64
	P95ChangePct   float64
	P99DeltaMs     float64
	P99ChangePct   float64
	ErrorRateDelta float64 // percentage points
}

// CompareEndpoints computes an EndpointComparison for every endpoint
// present in both the baseline and current run.
func CompareEndpoints(analyzer *EndpointAnalyzer, baselineRunID, currentRunID string) ([]EndpointComparison, error) {
	baseline, err := analyzer.GetEndpointStats(baselineRunID)
	if err != nil {
		return nil, fmt.Errorf("endpoint comparison: baseline: %w", err)
	}
	current, err := analyzer.GetEndpointStats(currentRunID)
	if err != nil {
		return nil, fmt.Errorf("endpoint comparison: current: %w", err)
	}

	baselineByEndpoint := map[string]EndpointStats{}
	for _, s := range baseline {
		baselineByEndpoint[s.Endpoint] = s
	}

	var out []EndpointComparison
	for _, cur := range current {
		base, ok := baselineByEndpoint[cur.Endpoint]
		if !ok {
			continue
		}
		out = append(out, EndpointComparison{
			Endpoint:       cur.Endpoint,
			Baseline:       base,
			Current:        cur,
			P50DeltaMs:     cur.P50RTT - base.P50RTT,
			P50ChangePct:   percentChange(base.P50RTT, cur.P50RTT),
			P95DeltaMs:     cur.P95RTT - base.P95RTT,
			P95ChangePct:   percentChange(base.P95RTT, cur.P95RTT),
			P99DeltaMs:     cur.P99RTT - base.P99RTT,
			P99ChangePct:   percentChange(base.P99RTT, cur.P99RTT),
			ErrorRateDelta: cur.ErrorRate - base.ErrorRate,
		})
	}
	return out, nil
}

func percentChange(baseline, current float64) float64 {
	if baseline == 0 {
		return 0
	}
	return ((current - baseline) / baseline) * 100
}

// DetectEndpointRegressions applies Thresholds to each endpoint's P95
// latency and error rate, the two signals most indicative of a real
// regression, and returns one RegressionFinding per breach. Severity
// follows the same rule as the run-level Comparator: "critical" once the
// change exceeds twice the configured threshold, "warning" otherwise.
func DetectEndpointRegressions(analyzer *EndpointAnalyzer, baselineRunID, currentRunID string, thresholds Thresholds) ([]RegressionFinding, error) {
	comparisons, err := CompareEndpoints(analyzer, baselineRunID, currentRunID)
	if err != nil {
		return nil, err
	}

	var findings []RegressionFinding
	for _, c := range comparisons {
		if c.P95ChangePct > thresholds.P95RTTIncreasePercent {
			findings = append(findings, RegressionFinding{
				Metric: "p95_rtt", Scope: c.Endpoint,
				Baseline: Round(c.Baseline.P95RTT, 2), Current: Round(c.Current.P95RTT, 2),
				ChangePercent: Round(c.P95ChangePct, 2),
				Severity:      severity(c.P95ChangePct, thresholds.P95RTTIncreasePercent),
			})
		}
		if c.Current.ErrorRate > thresholds.ErrorRateAbsoluteMax {
			findings = append(findings, RegressionFinding{
				Metric: "error_rate", Scope: c.Endpoint,
				Baseline: Round(c.Baseline.ErrorRate, 2), Current: Round(c.Current.ErrorRate, 2),
				ChangePercent: Round(percentChange(c.Baseline.ErrorRate, c.Current.ErrorRate), 2),
				Severity:      "critical",
			})
		}
	}
	return findings, nil
}

func severity(changePercent, threshold float64) string {
	if changePercent > threshold*2 {
		return "critical"
	}
	return "warning"
}
