package gate

import (
	"strings"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"

	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis/fixtures"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis/policy"
)

func ptrF(v float64) *float64 { return &v }
func ptrI(v int) *int         { return &v }

// standardPolicy is the policy every scenario below is evaluated against
// unless a test needs to vary it: a 500ms p95 budget with a softer 400ms
// warning, a 20% p95 regression threshold with a 10% warning, a 5%
// absolute error-rate budget with a softer 2% warning, and a 5-percentage-
// point error-rate regression threshold, all gated on at least 30 samples.
func standardPolicy() *policy.Policy {
	return &policy.Policy{
		Version:          policy.SchemaVersion,
		UnknownTreatment: policy.StatusWarn,
		Defaults: policy.MetricPolicy{
			MinSamples: ptrI(30),
			P95: &policy.LatencyPolicy{
				MaxMs:                 ptrF(500),
				WarnMaxMs:             ptrF(400),
				MaxRegressionPercent:  ptrF(20),
				WarnRegressionPercent: ptrF(10),
			},
			ErrorRate: &policy.ErrorRatePolicy{
				MaxAbsolute:          ptrF(5),
				WarnMaxAbsolute:      ptrF(2),
				MaxAbsoluteIncrease:  ptrF(5),
				WarnAbsoluteIncrease: ptrF(2),
			},
		},
	}
}

// evaluateFixture builds scenario's fixture database and evaluates it.
// withBaseline controls whether the "baseline" run_id is loaded at all —
// false exercises Performance-Budget-only mode (no baseline argument to
// Evaluate whatsoever), matching how the CLI omits --baseline entirely.
func evaluateFixture(t *testing.T, scenario fixtures.GateScenario, pol *policy.Policy, withBaseline bool) *Result {
	t.Helper()
	db, cleanup, err := fixtures.BuildGateDB(scenario)
	if err != nil {
		t.Fatalf("BuildGateDB(%s) failed: %v", scenario, err)
	}
	t.Cleanup(cleanup)

	analyzer := analysis.NewEndpointAnalyzer(db)
	current, err := analyzer.GetEndpointStats(fixtures.CurrentRunID)
	if err != nil {
		t.Fatalf("GetEndpointStats(current) failed: %v", err)
	}

	var baseline []analysis.EndpointStats
	if withBaseline {
		baseline, err = analyzer.GetEndpointStats(fixtures.BaselineRunID)
		if err != nil {
			t.Fatalf("GetEndpointStats(baseline) failed: %v", err)
		}
	}

	return Evaluate(current, baseline, pol)
}

func TestGate_Pass(t *testing.T) {
	res := evaluateFixture(t, fixtures.GatePass, standardPolicy(), true)
	if res.Status != StatusPass {
		t.Fatalf("expected PASS, got %s: %+v", res.Status, res.Violations)
	}
	if res.Summary.Failed != 0 || res.Summary.Warned != 0 || res.Summary.Unknown != 0 {
		t.Errorf("expected a clean summary, got %+v", res.Summary)
	}
	if res.Summary.Passed == 0 {
		t.Error("expected at least one passed finding")
	}
}

func TestGate_RelativeRegressionFail(t *testing.T) {
	res := evaluateFixture(t, fixtures.GateRelativeRegressionFail, standardPolicy(), true)
	if res.Status != StatusFail {
		t.Fatalf("expected FAIL, got %s", res.Status)
	}

	found := false
	for _, v := range res.Violations {
		if v.Scope == "/api/slow" && v.Metric == "p95" && v.Check == CheckRegression {
			found = true
			if v.Status != StatusFail {
				t.Errorf("expected FAIL finding, got %s", v.Status)
			}
			if v.ChangePercent == nil || *v.ChangePercent < 20 {
				t.Errorf("expected change_percent > 20, got %+v", v.ChangePercent)
			}
			if v.PrimaryTimingChange == nil || v.PrimaryTimingChange.Phase != "ttfb" {
				t.Errorf("expected TTFB to be the primary timing change, got %+v", v.PrimaryTimingChange)
			}
		}
	}
	if !found {
		t.Fatalf("expected a p95 regression violation for /api/slow, got %+v", res.Violations)
	}
}

func TestGate_AbsoluteBudgetFail(t *testing.T) {
	res := evaluateFixture(t, fixtures.GateAbsoluteBudgetFail, standardPolicy(), true)
	if res.Status != StatusFail {
		t.Fatalf("expected FAIL, got %s", res.Status)
	}

	found := false
	for _, v := range res.Violations {
		if v.Scope == "/api/report" && v.Metric == "p95" && v.Check == CheckBudget {
			found = true
			if v.Status != StatusFail {
				t.Errorf("expected FAIL finding, got %s", v.Status)
			}
			if v.Current == nil || *v.Current < 500 {
				t.Errorf("expected current p95 to exceed the 500ms budget, got %+v", v.Current)
			}
		}
	}
	if !found {
		t.Fatalf("expected a p95 budget violation for /api/report, got %+v", res.Violations)
	}
}

func TestGate_ErrorRateFail(t *testing.T) {
	res := evaluateFixture(t, fixtures.GateErrorRateFail, standardPolicy(), true)
	if res.Status != StatusFail {
		t.Fatalf("expected FAIL, got %s", res.Status)
	}

	found := false
	for _, v := range res.Violations {
		if v.Scope == "/api/payment" && v.Metric == "error_rate" {
			found = true
			if v.Status != StatusFail {
				t.Errorf("expected FAIL finding, got %s", v.Status)
			}
		}
	}
	if !found {
		t.Fatalf("expected an error_rate violation for /api/payment, got %+v", res.Violations)
	}
}

func TestGate_InsufficientSamples(t *testing.T) {
	res := evaluateFixture(t, fixtures.GateInsufficientSamples, standardPolicy(), true)

	if res.Summary.Unknown == 0 {
		t.Fatalf("expected UNKNOWN findings for a run with too few samples, got %+v", res.Summary)
	}
	if res.Summary.Failed != 0 || res.Summary.Warned != 0 {
		t.Errorf("expected no PASS/FAIL/WARN findings, only UNKNOWN, got %+v", res.Summary)
	}
	// Default UnknownTreatment is WARN.
	if res.Status != StatusWarn {
		t.Errorf("expected overall status WARN (default unknown_treatment), got %s", res.Status)
	}

	for _, v := range res.Violations {
		if v.Reason != ReasonInsufficientSamples {
			t.Errorf("expected reason %q, got %q", ReasonInsufficientSamples, v.Reason)
		}
	}
}

func TestGate_InsufficientSamples_UnknownTreatmentFail(t *testing.T) {
	pol := standardPolicy()
	pol.UnknownTreatment = policy.StatusFail

	res := evaluateFixture(t, fixtures.GateInsufficientSamples, pol, true)
	if res.Status != StatusFail {
		t.Errorf("expected overall status FAIL when unknown_treatment=FAIL, got %s", res.Status)
	}
}

func TestGate_NewEndpoint(t *testing.T) {
	res := evaluateFixture(t, fixtures.GateNewEndpoint, standardPolicy(), true)

	if len(res.NewEndpoints) != 1 || res.NewEndpoints[0] != "/api/new-feature" {
		t.Fatalf("expected NewEndpoints = [/api/new-feature], got %v", res.NewEndpoints)
	}

	foundUnknown := false
	for _, v := range res.Violations {
		if v.Scope == "/api/new-feature" && v.Check == CheckRegression {
			foundUnknown = true
			if v.Status != StatusUnknown || v.Reason != ReasonNoBaselineEndpoint {
				t.Errorf("expected UNKNOWN/no_baseline_for_endpoint, got status=%s reason=%s", v.Status, v.Reason)
			}
		}
		if v.Scope == "/api/new-feature" && v.Check == CheckBudget {
			t.Errorf("budget checks should evaluate normally for a new endpoint, got a violation: %+v", v)
		}
	}
	if !foundUnknown {
		t.Fatalf("expected an UNKNOWN regression finding for /api/new-feature, got %+v", res.Violations)
	}
}

func TestGate_RemovedEndpoint(t *testing.T) {
	res := evaluateFixture(t, fixtures.GateRemovedEndpoint, standardPolicy(), true)

	if len(res.RemovedEndpoints) != 1 || res.RemovedEndpoints[0] != "/api/legacy" {
		t.Fatalf("expected RemovedEndpoints = [/api/legacy], got %v", res.RemovedEndpoints)
	}
	for _, v := range res.Violations {
		if v.Scope == "/api/legacy" {
			t.Errorf("did not expect any finding for a removed endpoint, got %+v", v)
		}
	}
	if res.Status != StatusPass {
		t.Errorf("expected overall PASS (the surviving endpoint is healthy), got %s: %+v", res.Status, res.Violations)
	}
}

func TestGate_MixedResult(t *testing.T) {
	res := evaluateFixture(t, fixtures.GateMixedResult, standardPolicy(), true)

	if res.Status != StatusFail {
		t.Fatalf("expected overall FAIL (worst-case wins), got %s", res.Status)
	}
	if res.Summary.Passed == 0 || res.Summary.Warned == 0 || res.Summary.Failed == 0 {
		t.Fatalf("expected a genuine mix of passed/warned/failed, got %+v", res.Summary)
	}

	var sawWarnSlow, sawFailFlaky bool
	for _, v := range res.Violations {
		if v.Scope == "/api/slow" && v.Status == StatusWarn {
			sawWarnSlow = true
		}
		if v.Scope == "/api/flaky" && v.Status == StatusFail {
			sawFailFlaky = true
		}
		if v.Scope == "/api/ok" {
			t.Errorf("did not expect a violation for /api/ok, got %+v", v)
		}
	}
	if !sawWarnSlow {
		t.Error("expected a WARN finding for /api/slow")
	}
	if !sawFailFlaky {
		t.Error("expected a FAIL finding for /api/flaky")
	}
}

// TestGate_MissingBaseline exercises the Performance-Budget-only mode
// (section 15 of the P1 spec): passing an empty baselineRunID should skip
// every regression check silently — no UNKNOWN findings — while budget
// checks still run.
func TestGate_MissingBaseline(t *testing.T) {
	res := evaluateFixture(t, fixtures.GatePass, standardPolicy(), false)

	if res.Summary.Unknown != 0 {
		t.Errorf("expected no UNKNOWN findings in budget-only mode, got %+v", res.Summary)
	}
	for _, v := range res.Violations {
		if v.Check == CheckRegression {
			t.Errorf("did not expect any regression check to run without a baseline, got %+v", v)
		}
	}
	if res.Status != StatusPass {
		t.Errorf("expected PASS, got %s: %+v", res.Status, res.Violations)
	}
}

func TestGate_MissingBaseline_BudgetStillFails(t *testing.T) {
	res := evaluateFixture(t, fixtures.GateAbsoluteBudgetFail, standardPolicy(), false)
	if res.Status != StatusFail {
		t.Fatalf("expected budget check to still fail without a baseline, got %s", res.Status)
	}
	for _, v := range res.Violations {
		if v.Check == CheckRegression {
			t.Errorf("did not expect any regression check to run without a baseline, got %+v", v)
		}
	}
}

func TestDeterminism_SameInputsSameOutput(t *testing.T) {
	pol := standardPolicy()
	a := evaluateFixture(t, fixtures.GateMixedResult, pol, true)
	b := evaluateFixture(t, fixtures.GateMixedResult, standardPolicy(), true)

	if a.Status != b.Status || a.Summary != b.Summary {
		t.Fatalf("expected identical results for identical inputs: %+v vs %+v", a, b)
	}
	if len(a.Violations) != len(b.Violations) {
		t.Fatalf("violation count differs: %d vs %d", len(a.Violations), len(b.Violations))
	}
	for i := range a.Violations {
		if a.Violations[i].Scope != b.Violations[i].Scope || a.Violations[i].Metric != b.Violations[i].Metric {
			t.Fatalf("violation order/content differs at index %d: %+v vs %+v", i, a.Violations[i], b.Violations[i])
		}
	}
}

func TestExitCode(t *testing.T) {
	tests := []struct {
		status       Status
		warnExitCode int
		want         int
	}{
		{StatusPass, ExitPass, ExitPass},
		{StatusWarn, ExitPass, ExitPass},
		{StatusWarn, ExitPolicyFailure, ExitPolicyFailure},
		{StatusFail, ExitPass, ExitPolicyFailure},
		{StatusFail, ExitPolicyFailure, ExitPolicyFailure},
	}
	for _, tt := range tests {
		r := &Result{Status: tt.status}
		if got := r.ExitCode(tt.warnExitCode); got != tt.want {
			t.Errorf("Result{Status:%s}.ExitCode(%d) = %d, expected %d", tt.status, tt.warnExitCode, got, tt.want)
		}
	}
}

func TestFormatHuman_ProducesOutput(t *testing.T) {
	res := evaluateFixture(t, fixtures.GateRelativeRegressionFail, standardPolicy(), true)
	out := FormatHuman(res)
	if out == "" {
		t.Fatal("expected non-empty human-readable report")
	}
	if !strings.Contains(out, "PERFORMANCE GATE: FAIL") {
		t.Errorf("expected report to state the overall status, got:\n%s", out)
	}
}
