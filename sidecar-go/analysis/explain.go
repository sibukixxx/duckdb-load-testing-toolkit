package analysis

import (
	"database/sql"
	"fmt"

	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis/queries"
)

// Explanation is a structured, deterministic "performance evidence
// summary" for one regressed endpoint: what regressed, by how much, which
// timing phase moved the most, which pods were affected, and how the error
// rate changed. It is built entirely from SQL/Go arithmetic — no LLM or
// heuristic scoring beyond the configured Thresholds.
type Explanation struct {
	RegressionDetected  bool         `json:"regression_detected"`
	Endpoint            string       `json:"endpoint"`
	P95BaselineMs       float64      `json:"p95_baseline_ms"`
	P95CurrentMs        float64      `json:"p95_current_ms"`
	P95ChangePercent    float64      `json:"p95_change_percent"`
	LargestTimingChange TimingChange `json:"largest_timing_change"`
	AffectedPods        []string     `json:"affected_pods"`
	ErrorRateBaseline   float64      `json:"error_rate_baseline"`
	ErrorRateCurrent    float64      `json:"error_rate_current"`
	Severity            string       `json:"severity"`
}

// podOutlierMultiplier is how far above an endpoint's current-run average
// p95 a pod's own p95 must be to count as "affected".
const podOutlierMultiplier = 1.3

// Explain finds every endpoint with a regression (per DetectEndpointRegressions)
// and builds one Explanation per regressed endpoint, adding bottleneck
// evidence and affected-pod detection on top of the raw regression finding.
func Explain(db *sql.DB, baselineRunID, currentRunID string, thresholds Thresholds) ([]Explanation, error) {
	analyzer := NewEndpointAnalyzer(db)

	comparisons, err := CompareEndpoints(analyzer, baselineRunID, currentRunID)
	if err != nil {
		return nil, err
	}
	bottlenecks, err := AnalyzeBottleneck(analyzer, baselineRunID, currentRunID)
	if err != nil {
		return nil, err
	}
	bottleneckByEndpoint := map[string]BottleneckEvidence{}
	for _, b := range bottlenecks {
		bottleneckByEndpoint[b.Endpoint] = b
	}

	var explanations []Explanation
	for _, c := range comparisons {
		regressedLatency := c.P95ChangePct > thresholds.P95RTTIncreasePercent
		regressedErrors := c.Current.ErrorRate > thresholds.ErrorRateAbsoluteMax
		if !regressedLatency && !regressedErrors {
			continue
		}

		exp := Explanation{
			RegressionDetected: true,
			Endpoint:           c.Endpoint,
			P95BaselineMs:      Round(c.Baseline.P95RTT, 2),
			P95CurrentMs:       Round(c.Current.P95RTT, 2),
			P95ChangePercent:   Round(c.P95ChangePct, 2),
			ErrorRateBaseline:  Round(c.Baseline.ErrorRate, 2),
			ErrorRateCurrent:   Round(c.Current.ErrorRate, 2),
		}

		if b, ok := bottleneckByEndpoint[c.Endpoint]; ok {
			exp.LargestTimingChange = TimingChange{
				Phase:   b.LargestContributor,
				DeltaMs: Round(b.LargestContributorMs, 2),
			}
		}

		pods, err := affectedPods(db, currentRunID, c.Endpoint)
		if err != nil {
			return nil, err
		}
		exp.AffectedPods = pods

		exp.Severity = "warning"
		if regressedErrors || c.P95ChangePct > thresholds.P95RTTIncreasePercent*2 {
			exp.Severity = "critical"
		}

		explanations = append(explanations, exp)
	}

	return explanations, nil
}

// affectedPods returns the pods whose p95 latency for endpoint in runID is
// more than podOutlierMultiplier times the average p95 across all pods
// serving that endpoint — i.e. pods that stand out, not just the slowest
// one relative to their peers.
func affectedPods(db *sql.DB, runID, endpoint string) ([]string, error) {
	q := queries.MustGet(queries.PodEndpointSummary)
	rows, err := db.Query(q.SQL, runID)
	if err != nil {
		return nil, fmt.Errorf("explain: %s failed: %w", q.ID, err)
	}
	defer rows.Close()

	type podStat struct {
		pod    string
		p95RTT float64
	}
	var stats []podStat
	var sum float64
	for rows.Next() {
		var ep, pod string
		var requestCount, errorCount int64
		var avgRTT, p95RTT float64
		if err := rows.Scan(&ep, &pod, &requestCount, &errorCount, &avgRTT, &p95RTT); err != nil {
			return nil, fmt.Errorf("explain: scan pod stats failed: %w", err)
		}
		if ep != endpoint {
			continue
		}
		stats = append(stats, podStat{pod: pod, p95RTT: p95RTT})
		sum += p95RTT
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(stats) == 0 {
		return nil, nil
	}

	avg := sum / float64(len(stats))
	var affected []string
	for _, s := range stats {
		if avg > 0 && s.p95RTT > avg*podOutlierMultiplier {
			affected = append(affected, s.pod)
		}
	}
	return affected, nil
}
