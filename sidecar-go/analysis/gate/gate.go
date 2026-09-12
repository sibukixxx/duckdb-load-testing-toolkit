// Package gate implements the performance gate's evaluation engine: given
// per-endpoint statistics for a current run (and, optionally, a baseline
// run) plus a policy.Policy, it decides PASS/WARN/FAIL/UNKNOWN.
//
// This package has no knowledge of any CI provider. It is a pure function
// of (current stats, baseline stats, policy) so the same result is
// reachable from the CLI, the HTTP API, and a test — see policy.md and
// performance-gate.md for the contract this package implements.
package gate

import (
	"sort"

	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis/policy"
)

// SchemaVersion is the evaluation result's JSON schema version — the
// compatibility boundary for CI integrations that parse `duckload gate
// --format json` output. Bump it only for a breaking change to Result's
// shape.
const SchemaVersion = 1

// Status mirrors policy.Status; it is redeclared here so callers that only
// import gate (not policy) still get the type.
type Status = policy.Status

const (
	StatusPass    = policy.StatusPass
	StatusWarn    = policy.StatusWarn
	StatusFail    = policy.StatusFail
	StatusUnknown = policy.StatusUnknown
)

// Check names which half of a LatencyPolicy/ErrorRatePolicy produced a
// Finding: an absolute Performance Budget, or a baseline-relative
// regression check.
type Check string

const (
	CheckBudget     Check = "budget"
	CheckRegression Check = "regression"
)

// Finding is one evaluated check for one endpoint and metric.
type Finding struct {
	Scope         string   `json:"scope"`
	Metric        string   `json:"metric"` // "p50", "p95", "p99", or "error_rate"
	Check         Check    `json:"check"`
	Status        Status   `json:"status"`
	Baseline      *float64 `json:"baseline,omitempty"`
	Current       *float64 `json:"current,omitempty"`
	Delta         *float64 `json:"delta,omitempty"`
	ChangePercent *float64 `json:"change_percent,omitempty"`
	Threshold     *float64 `json:"threshold,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	// PrimaryTimingChange is bottleneck evidence (see analysis.TimingDelta)
	// carried onto a latency regression finding: which network/backend
	// phase moved the most between baseline and current. Set only for
	// regression findings on p50/p95/p99 where a baseline was available.
	PrimaryTimingChange *analysis.TimingChange `json:"primary_timing_change,omitempty"`
}

// Summary tallies Findings by status.
type Summary struct {
	Passed  int `json:"passed"`
	Warned  int `json:"warned"`
	Failed  int `json:"failed"`
	Unknown int `json:"unknown"`
}

// Result is the complete, structured outcome of one gate evaluation. The
// same Result drives both the JSON report and the human-readable one — see
// report.go — so the two never disagree about what happened.
type Result struct {
	SchemaVersion int     `json:"schema_version"`
	Status        Status  `json:"status"`
	Summary       Summary `json:"summary"`
	// Violations holds every non-PASS Finding (WARN, FAIL, and UNKNOWN).
	// PASS findings are counted in Summary but not listed individually,
	// keeping the report focused on what needs attention.
	Violations []Finding `json:"violations"`
	// NewEndpoints lists endpoints present in the current run but absent
	// from the baseline (when a baseline was evaluated). Their budget
	// checks still run normally; only their regression checks become
	// UNKNOWN (see Violations), so they are called out here too for
	// visibility.
	NewEndpoints []string `json:"new_endpoints,omitempty"`
	// RemovedEndpoints lists endpoints present in the baseline but absent
	// from the current run. Nothing to gate exists for them in the
	// current run, so they never produce a Finding; they are reported
	// here purely for visibility (e.g. "was this removal intentional?").
	RemovedEndpoints []string `json:"removed_endpoints,omitempty"`
}

// reasons used for UNKNOWN findings, kept as constants so callers can
// switch on them without string literals scattered around.
const (
	ReasonInsufficientSamples  = "insufficient_samples"
	ReasonNoBaselineEndpoint   = "no_baseline_for_endpoint"
	ReasonInsufficientBaseline = "insufficient_baseline_samples"
)

// Evaluate runs every check in pol against current's endpoint statistics,
// and — when baseline is non-nil — against a regression comparison with
// it. It is a pure function of its three arguments: no I/O, no clock, no
// CI-provider awareness. That is what lets current and baseline come from
// the same run_id-partitioned database, from two entirely separate
// .duckdb files, or from any other source a caller assembles
// []analysis.EndpointStats from.
//
// Passing a nil baseline runs the gate in Performance-Budget-only mode
// (section 15 of the P1 spec): every absolute budget still applies, but no
// regression check runs and no UNKNOWN findings are produced for missing
// baseline data, since none was requested.
func Evaluate(current, baseline []analysis.EndpointStats, pol *policy.Policy) *Result {
	var baselineByEndpoint map[string]analysis.EndpointStats
	var currentEndpointSet map[string]bool
	if baseline != nil {
		baselineByEndpoint = make(map[string]analysis.EndpointStats, len(baseline))
		for _, s := range baseline {
			baselineByEndpoint[s.Endpoint] = s
		}
		currentEndpointSet = make(map[string]bool, len(current))
	}

	var findings []Finding
	var newEndpoints []string

	for _, cur := range current {
		if currentEndpointSet != nil {
			currentEndpointSet[cur.Endpoint] = true
		}

		mp := pol.EffectiveMetricPolicy(cur.Endpoint)
		minSamples := mp.MinSamplesOrDefault()

		var base *analysis.EndpointStats
		var baselineMissing bool
		if baseline != nil {
			if b, ok := baselineByEndpoint[cur.Endpoint]; ok {
				base = &b
			} else {
				baselineMissing = true
				newEndpoints = append(newEndpoints, cur.Endpoint)
			}
		}

		insufficientCurrent := cur.RequestCount < int64(minSamples)
		insufficientBaseline := base != nil && base.RequestCount < int64(minSamples)

		var timingChange *analysis.TimingChange
		if base != nil && !insufficientCurrent && !insufficientBaseline {
			evidence := analysis.TimingDelta(*base, cur)
			timingChange = &analysis.TimingChange{Phase: evidence.LargestContributor, DeltaMs: evidence.LargestContributorMs}
		}

		findings = append(findings, evaluateLatency("p50", cur.Endpoint, mp.P50, cur.P50RTT, base, baselineMissing, insufficientCurrent, insufficientBaseline, timingChange, func(s *analysis.EndpointStats) float64 { return s.P50RTT })...)
		findings = append(findings, evaluateLatency("p95", cur.Endpoint, mp.P95, cur.P95RTT, base, baselineMissing, insufficientCurrent, insufficientBaseline, timingChange, func(s *analysis.EndpointStats) float64 { return s.P95RTT })...)
		findings = append(findings, evaluateLatency("p99", cur.Endpoint, mp.P99, cur.P99RTT, base, baselineMissing, insufficientCurrent, insufficientBaseline, timingChange, func(s *analysis.EndpointStats) float64 { return s.P99RTT })...)
		findings = append(findings, evaluateErrorRate(cur.Endpoint, mp.ErrorRate, cur.ErrorRate, base, baselineMissing, insufficientCurrent, insufficientBaseline)...)
	}

	var removedEndpoints []string
	if currentEndpointSet != nil {
		for endpoint := range baselineByEndpoint {
			if !currentEndpointSet[endpoint] {
				removedEndpoints = append(removedEndpoints, endpoint)
			}
		}
		sort.Strings(removedEndpoints)
	}
	sort.Strings(newEndpoints)

	return build(pol, findings, newEndpoints, removedEndpoints)
}

// evaluateLatency runs the budget and (if a baseline is in play) the
// regression check for one percentile of one endpoint.
func evaluateLatency(
	metric, endpoint string,
	lp *policy.LatencyPolicy,
	currentValue float64,
	base *analysis.EndpointStats,
	baselineMissing, insufficientCurrent, insufficientBaseline bool,
	timingChange *analysis.TimingChange,
	extract func(*analysis.EndpointStats) float64,
) []Finding {
	if lp == nil {
		return nil
	}

	var findings []Finding

	if lp.MaxMs != nil || lp.WarnMaxMs != nil {
		if insufficientCurrent {
			findings = append(findings, unknownFinding(endpoint, metric, CheckBudget, ReasonInsufficientSamples))
		} else {
			findings = append(findings, budgetFinding(endpoint, metric, currentValue, lp))
		}
	}

	regressionConfigured := lp.MaxRegressionPercent != nil || lp.WarnRegressionPercent != nil
	if regressionConfigured && (base != nil || baselineMissing) {
		switch {
		case baselineMissing:
			findings = append(findings, unknownFinding(endpoint, metric, CheckRegression, ReasonNoBaselineEndpoint))
		case insufficientCurrent:
			findings = append(findings, unknownFinding(endpoint, metric, CheckRegression, ReasonInsufficientSamples))
		case insufficientBaseline:
			findings = append(findings, unknownFinding(endpoint, metric, CheckRegression, ReasonInsufficientBaseline))
		default:
			f := regressionFinding(endpoint, metric, extract(base), currentValue, lp)
			if f.Status != StatusPass {
				f.PrimaryTimingChange = timingChange
			}
			findings = append(findings, f)
		}
	}

	return findings
}

func budgetFinding(endpoint, metric string, current float64, lp *policy.LatencyPolicy) Finding {
	f := Finding{Scope: endpoint, Metric: metric, Check: CheckBudget, Status: StatusPass, Current: ptr(current)}

	switch {
	case lp.MaxMs != nil && current > *lp.MaxMs:
		f.Status = StatusFail
		f.Threshold = lp.MaxMs
	case lp.WarnMaxMs != nil && current > *lp.WarnMaxMs:
		f.Status = StatusWarn
		f.Threshold = lp.WarnMaxMs
	case lp.MaxMs != nil:
		f.Threshold = lp.MaxMs
	case lp.WarnMaxMs != nil:
		f.Threshold = lp.WarnMaxMs
	}
	return f
}

func regressionFinding(endpoint, metric string, baseline, current float64, lp *policy.LatencyPolicy) Finding {
	delta := current - baseline
	changePercent := percentChange(baseline, current)
	minAbsolute := 0.0
	if lp.MinAbsoluteRegressionMs != nil {
		minAbsolute = *lp.MinAbsoluteRegressionMs
	}
	exceedsFloor := delta >= minAbsolute

	f := Finding{
		Scope: endpoint, Metric: metric, Check: CheckRegression, Status: StatusPass,
		Baseline: ptr(baseline), Current: ptr(current), Delta: ptr(delta), ChangePercent: ptr(changePercent),
	}

	switch {
	case lp.MaxRegressionPercent != nil && changePercent > *lp.MaxRegressionPercent && exceedsFloor:
		f.Status = StatusFail
		f.Threshold = lp.MaxRegressionPercent
	case lp.WarnRegressionPercent != nil && changePercent > *lp.WarnRegressionPercent && exceedsFloor:
		f.Status = StatusWarn
		f.Threshold = lp.WarnRegressionPercent
	case lp.MaxRegressionPercent != nil:
		f.Threshold = lp.MaxRegressionPercent
	case lp.WarnRegressionPercent != nil:
		f.Threshold = lp.WarnRegressionPercent
	}
	return f
}

func evaluateErrorRate(
	endpoint string,
	ep *policy.ErrorRatePolicy,
	currentValue float64,
	base *analysis.EndpointStats,
	baselineMissing, insufficientCurrent, insufficientBaseline bool,
) []Finding {
	if ep == nil {
		return nil
	}

	var findings []Finding
	const metric = "error_rate"

	if ep.MaxAbsolute != nil || ep.WarnMaxAbsolute != nil {
		if insufficientCurrent {
			findings = append(findings, unknownFinding(endpoint, metric, CheckBudget, ReasonInsufficientSamples))
		} else {
			f := Finding{Scope: endpoint, Metric: metric, Check: CheckBudget, Status: StatusPass, Current: ptr(currentValue)}
			switch {
			case ep.MaxAbsolute != nil && currentValue > *ep.MaxAbsolute:
				f.Status = StatusFail
				f.Threshold = ep.MaxAbsolute
			case ep.WarnMaxAbsolute != nil && currentValue > *ep.WarnMaxAbsolute:
				f.Status = StatusWarn
				f.Threshold = ep.WarnMaxAbsolute
			case ep.MaxAbsolute != nil:
				f.Threshold = ep.MaxAbsolute
			case ep.WarnMaxAbsolute != nil:
				f.Threshold = ep.WarnMaxAbsolute
			}
			findings = append(findings, f)
		}
	}

	regressionConfigured := ep.MaxAbsoluteIncrease != nil || ep.WarnAbsoluteIncrease != nil
	if regressionConfigured && (base != nil || baselineMissing) {
		switch {
		case baselineMissing:
			findings = append(findings, unknownFinding(endpoint, metric, CheckRegression, ReasonNoBaselineEndpoint))
		case insufficientCurrent:
			findings = append(findings, unknownFinding(endpoint, metric, CheckRegression, ReasonInsufficientSamples))
		case insufficientBaseline:
			findings = append(findings, unknownFinding(endpoint, metric, CheckRegression, ReasonInsufficientBaseline))
		default:
			delta := currentValue - base.ErrorRate
			f := Finding{
				Scope: endpoint, Metric: metric, Check: CheckRegression, Status: StatusPass,
				Baseline: ptr(base.ErrorRate), Current: ptr(currentValue), Delta: ptr(delta),
			}
			switch {
			case ep.MaxAbsoluteIncrease != nil && delta > *ep.MaxAbsoluteIncrease:
				f.Status = StatusFail
				f.Threshold = ep.MaxAbsoluteIncrease
			case ep.WarnAbsoluteIncrease != nil && delta > *ep.WarnAbsoluteIncrease:
				f.Status = StatusWarn
				f.Threshold = ep.WarnAbsoluteIncrease
			case ep.MaxAbsoluteIncrease != nil:
				f.Threshold = ep.MaxAbsoluteIncrease
			case ep.WarnAbsoluteIncrease != nil:
				f.Threshold = ep.WarnAbsoluteIncrease
			}
			findings = append(findings, f)
		}
	}

	return findings
}

func unknownFinding(endpoint, metric string, check Check, reason string) Finding {
	return Finding{Scope: endpoint, Metric: metric, Check: check, Status: StatusUnknown, Reason: reason}
}

// build assembles the Result: it tallies every finding, treating an
// UNKNOWN finding as pol.UnknownTreatment for the purpose of the overall
// Status (but always reporting its true status in Summary.Unknown and in
// the finding itself — the treatment never rewrites the evidence, only
// how much weight the gate gives it).
func build(pol *policy.Policy, findings []Finding, newEndpoints, removedEndpoints []string) *Result {
	res := &Result{
		SchemaVersion:    SchemaVersion,
		Status:           StatusPass,
		Violations:       []Finding{},
		NewEndpoints:     newEndpoints,
		RemovedEndpoints: removedEndpoints,
	}

	worst := StatusPass
	for _, f := range findings {
		switch f.Status {
		case StatusPass:
			res.Summary.Passed++
			continue
		case StatusWarn:
			res.Summary.Warned++
		case StatusFail:
			res.Summary.Failed++
		case StatusUnknown:
			res.Summary.Unknown++
		}
		res.Violations = append(res.Violations, f)

		effective := f.Status
		if f.Status == StatusUnknown {
			effective = pol.UnknownTreatment
		}
		worst = worse(worst, effective)
	}

	sort.Slice(res.Violations, func(i, j int) bool {
		a, b := res.Violations[i], res.Violations[j]
		if a.Scope != b.Scope {
			return a.Scope < b.Scope
		}
		if a.Metric != b.Metric {
			return a.Metric < b.Metric
		}
		return a.Check < b.Check
	})

	res.Status = worst
	return res
}

func worse(a, b Status) Status {
	rank := map[Status]int{StatusPass: 0, StatusUnknown: 1, StatusWarn: 2, StatusFail: 3}
	// StatusUnknown only reaches here if UnknownTreatment resolves to it,
	// which Policy.Validate never allows — this rank exists so worse()
	// stays total even if that ever changes.
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func percentChange(baseline, current float64) float64 {
	if baseline == 0 {
		return 0
	}
	return ((current - baseline) / baseline) * 100
}

func ptr[T any](v T) *T { return &v }
