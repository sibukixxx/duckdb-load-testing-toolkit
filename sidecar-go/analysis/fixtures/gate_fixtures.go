package fixtures

import (
	"database/sql"
	"fmt"

	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/models"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/storage"
)

// GateScenario names a synthetic fixture built specifically to exercise the
// performance gate (analysis/gate) rather than endpoint analysis or
// explain. Each one isolates one gate behavior so a test can assert on it
// without a hand-built policy interacting with unrelated data.
type GateScenario string

const (
	// GatePass: baseline and current perform the same; every check passes.
	GatePass GateScenario = "gate-pass"
	// GateRelativeRegressionFail: current is well beyond a percentage
	// regression threshold on one endpoint.
	GateRelativeRegressionFail GateScenario = "gate-relative-regression-fail"
	// GateAbsoluteBudgetFail: current exceeds an absolute latency budget
	// even though it barely changed from baseline (a regression check
	// alone would not catch it).
	GateAbsoluteBudgetFail GateScenario = "gate-absolute-budget-fail"
	// GateErrorRateFail: current's error rate is far above baseline's.
	GateErrorRateFail GateScenario = "gate-error-rate-fail"
	// GateInsufficientSamples: both runs have far fewer requests than any
	// reasonable min_samples.
	GateInsufficientSamples GateScenario = "gate-insufficient-samples"
	// GateNewEndpoint: current has an endpoint the baseline never saw.
	GateNewEndpoint GateScenario = "gate-new-endpoint"
	// GateRemovedEndpoint: baseline has an endpoint the current run no
	// longer serves.
	GateRemovedEndpoint GateScenario = "gate-removed-endpoint"
	// GateMixedResult: three endpoints whose checks land on all three of
	// PASS, WARN, and FAIL at once, in a single evaluation.
	GateMixedResult GateScenario = "gate-mixed-result"
)

// AllGateScenarios lists every gate fixture, in a stable order. It does not
// include a "missing baseline" scenario: that is not a data shape but a
// call-time mode (Evaluate with baselineRunID == ""), exercised in
// analysis/gate's tests directly against GatePass's current run.
func AllGateScenarios() []GateScenario {
	return []GateScenario{
		GatePass, GateRelativeRegressionFail, GateAbsoluteBudgetFail, GateErrorRateFail,
		GateInsufficientSamples, GateNewEndpoint, GateRemovedEndpoint, GateMixedResult,
	}
}

const (
	gateEndpointOK     = "/api/ok"
	gateEndpointSlow   = "/api/slow"
	gateEndpointFlaky  = "/api/flaky"
	gateEndpointReport = "/api/report"
	gateEndpointErrors = "/api/payment"
	gateEndpointRare   = "/api/rare"
	gateEndpointNew    = "/api/new-feature"
	gateEndpointLegacy = "/api/legacy"
	gateEndpointUsers  = "/api/users"

	gateSampleSize       = 100
	gateInsufficientSize = 5
)

var gateHealthy = componentProfile{dns: 2, tcp: 3, tls: 5, ttfb: 30, transfer: 10}

// GenerateGate returns the deterministic event set for a gate fixture
// scenario, covering "baseline" and "current" run_ids (see BaselineRunID,
// CurrentRunID).
func GenerateGate(scenario GateScenario) []models.Event {
	switch scenario {
	case GatePass:
		var events []models.Event
		events = append(events, generate(genOptions{
			runID: BaselineRunID, endpoint: gateEndpointUsers, base: gateHealthy,
			errorRate: 0.01, seed: 10_000, n: gateSampleSize,
		})...)
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: gateEndpointUsers, base: gateHealthy,
			errorRate: 0.01, seed: 10_100, n: gateSampleSize,
		})...)
		return events

	case GateRelativeRegressionFail:
		regressed := componentProfile{dns: 2, tcp: 3, tls: 5, ttfb: 68, transfer: 10} // total ~+60% vs gateHealthy's ~50ms
		var events []models.Event
		events = append(events, generate(genOptions{
			runID: BaselineRunID, endpoint: gateEndpointSlow, base: gateHealthy,
			errorRate: 0.01, seed: 20_000, n: gateSampleSize,
		})...)
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: gateEndpointSlow, base: regressed,
			errorRate: 0.01, seed: 20_100, n: gateSampleSize,
		})...)
		return events

	case GateAbsoluteBudgetFail:
		// Both runs are consistent with each other (no regression), but
		// both sit above any reasonable absolute budget — this isolates
		// the budget check from the regression check.
		slow := componentProfile{dns: 5, tcp: 8, tls: 10, ttfb: 550, transfer: 30}
		var events []models.Event
		events = append(events, generate(genOptions{
			runID: BaselineRunID, endpoint: gateEndpointReport, base: slow,
			errorRate: 0.01, seed: 30_000, n: gateSampleSize,
		})...)
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: gateEndpointReport, base: slow,
			errorRate: 0.01, seed: 30_100, n: gateSampleSize,
		})...)
		return events

	case GateErrorRateFail:
		var events []models.Event
		events = append(events, generate(genOptions{
			runID: BaselineRunID, endpoint: gateEndpointErrors, base: gateHealthy,
			errorRate: 0.01, seed: 40_000, n: gateSampleSize,
		})...)
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: gateEndpointErrors, base: gateHealthy,
			errorRate: 0.15, seed: 40_100, n: gateSampleSize,
		})...)
		return events

	case GateInsufficientSamples:
		var events []models.Event
		events = append(events, generate(genOptions{
			runID: BaselineRunID, endpoint: gateEndpointRare, base: gateHealthy,
			errorRate: 0.0, seed: 50_000, n: gateInsufficientSize,
		})...)
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: gateEndpointRare, base: gateHealthy,
			errorRate: 0.0, seed: 50_100, n: gateInsufficientSize,
		})...)
		return events

	case GateNewEndpoint:
		var events []models.Event
		events = append(events, generate(genOptions{
			runID: BaselineRunID, endpoint: gateEndpointUsers, base: gateHealthy,
			errorRate: 0.01, seed: 60_000, n: gateSampleSize,
		})...)
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: gateEndpointUsers, base: gateHealthy,
			errorRate: 0.01, seed: 60_100, n: gateSampleSize,
		})...)
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: gateEndpointNew, base: gateHealthy,
			errorRate: 0.01, seed: 60_200, n: gateSampleSize,
		})...)
		return events

	case GateRemovedEndpoint:
		var events []models.Event
		events = append(events, generate(genOptions{
			runID: BaselineRunID, endpoint: gateEndpointUsers, base: gateHealthy,
			errorRate: 0.01, seed: 70_000, n: gateSampleSize,
		})...)
		events = append(events, generate(genOptions{
			runID: BaselineRunID, endpoint: gateEndpointLegacy, base: gateHealthy,
			errorRate: 0.01, seed: 70_100, n: gateSampleSize,
		})...)
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: gateEndpointUsers, base: gateHealthy,
			errorRate: 0.01, seed: 70_200, n: gateSampleSize,
		})...)
		return events

	case GateMixedResult:
		// Three endpoints, three different check types, three different
		// verdicts: /api/ok passes everything, /api/slow lands a p95
		// regression between the warn and fail thresholds, /api/flaky
		// spikes its error rate past the fail threshold while latency
		// stays flat. A single evaluation should therefore report PASS,
		// WARN, and FAIL findings all at once.
		warnRegressed := componentProfile{dns: 2, tcp: 3, tls: 5, ttfb: 37, transfer: 10} // ~+14%, between warn (10%) and fail (20%)
		var events []models.Event
		events = append(events, generate(genOptions{
			runID: BaselineRunID, endpoint: gateEndpointOK, base: gateHealthy,
			errorRate: 0.01, seed: 80_000, n: gateSampleSize,
		})...)
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: gateEndpointOK, base: gateHealthy,
			errorRate: 0.01, seed: 80_100, n: gateSampleSize,
		})...)
		events = append(events, generate(genOptions{
			runID: BaselineRunID, endpoint: gateEndpointSlow, base: gateHealthy,
			errorRate: 0.01, seed: 80_200, n: gateSampleSize,
		})...)
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: gateEndpointSlow, base: warnRegressed,
			errorRate: 0.01, seed: 80_300, n: gateSampleSize,
		})...)
		events = append(events, generate(genOptions{
			runID: BaselineRunID, endpoint: gateEndpointFlaky, base: gateHealthy,
			errorRate: 0.01, seed: 80_400, n: gateSampleSize,
		})...)
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: gateEndpointFlaky, base: gateHealthy,
			errorRate: 0.15, seed: 80_500, n: gateSampleSize,
		})...)
		return events

	default:
		panic(fmt.Sprintf("fixtures: unknown gate scenario %q", scenario))
	}
}

// BuildGateDB creates an in-memory DuckDB database with the standard
// `metrics` schema, loads a gate fixture's synthetic events into it, and
// returns the open connection plus a cleanup function.
func BuildGateDB(scenario GateScenario) (db *sql.DB, cleanup func(), err error) {
	db, err = sql.Open("duckdb", "")
	if err != nil {
		return nil, nil, fmt.Errorf("fixtures: open duckdb: %w", err)
	}
	cleanup = func() { db.Close() }

	if _, err = db.Exec(storage.MetricsTableSchema); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("fixtures: create schema: %w", err)
	}

	stmt, err := db.Prepare(`
		INSERT INTO metrics (
			ts, run_id, pod_id, vu, iter, method, url, name, status, body_len,
			rtt, dns_lookup, tcp_connect, tls_handshake, ttfb, content_transfer,
			request_size, response_size, error_code, error_msg, tags
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("fixtures: prepare insert: %w", err)
	}
	defer stmt.Close()

	for _, e := range GenerateGate(scenario) {
		_, err = stmt.Exec(
			e.Ts, e.RunID, e.PodID, e.VU, e.Iter, e.Method, e.URL, e.Name, e.Status, e.BodyLen,
			e.RTT, e.DNSLookup, e.TCPConnect, e.TLSHandshake, e.TTFB, e.ContentTransfer,
			e.RequestSize, e.ResponseSize, e.ErrorCode, e.ErrorMsg, "",
		)
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("fixtures: insert event: %w", err)
		}
	}

	return db, cleanup, nil
}
