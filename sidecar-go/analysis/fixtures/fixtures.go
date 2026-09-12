// Package fixtures builds small, deterministic DuckDB databases that stand
// in for a real k6 load-test result. They let the analysis layer (endpoint
// analysis, bottleneck evidence, regression detection, explain) be tested
// in CI without running an actual load test against a live service.
//
// Every scenario is generated from fixed inputs and a tiny linear
// congruential generator (not math/rand) so results are bit-identical run
// to run and across Go versions/platforms.
package fixtures

import (
	"database/sql"
	"fmt"

	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/models"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/storage"
)

// Scenario names a synthetic performance fixture. Each one models a
// specific investigation the analysis layer needs to support.
type Scenario string

const (
	// Healthy has matching baseline/current performance and no regressions.
	Healthy Scenario = "healthy"
	// LatencyRegression worsens total latency on a single endpoint.
	LatencyRegression Scenario = "latency-regression"
	// ErrorSpike raises the 5xx rate on a single endpoint.
	ErrorSpike Scenario = "error-spike"
	// NetworkRegression worsens DNS/TCP/TLS timing on a single endpoint.
	NetworkRegression Scenario = "network-regression"
	// BackendRegression worsens TTFB (server-side processing) on a single endpoint.
	BackendRegression Scenario = "backend-regression"
	// PodOutlier makes a single pod much slower than its peers.
	PodOutlier Scenario = "pod-outlier"
	// MixedRegression combines latency, error, and pod-outlier regressions.
	MixedRegression Scenario = "mixed-regression"
)

// All lists every fixture scenario, in a stable order.
func All() []Scenario {
	return []Scenario{
		Healthy, LatencyRegression, ErrorSpike, NetworkRegression,
		BackendRegression, PodOutlier, MixedRegression,
	}
}

const (
	BaselineRunID = "baseline"
	CurrentRunID  = "current"

	endpointUsers  = "/api/users"
	endpointOrders = "/api/orders"

	requestsPerEndpoint = 60
)

var pods = []string{"pod-0", "pod-1", "pod-2"}

// componentProfile is a request's timing breakdown, in milliseconds. RTT is
// always the sum of its components so bottleneck evidence stays internally
// consistent.
type componentProfile struct {
	dns, tcp, tls, ttfb, transfer float64
}

func (c componentProfile) scaled(m componentProfile, jitter float64) componentProfile {
	return componentProfile{
		dns:      c.dns * m.dns * jitter,
		tcp:      c.tcp * m.tcp * jitter,
		tls:      c.tls * m.tls * jitter,
		ttfb:     c.ttfb * m.ttfb * jitter,
		transfer: c.transfer * m.transfer * jitter,
	}
}

func (c componentProfile) rtt() float64 {
	return c.dns + c.tcp + c.tls + c.ttfb + c.transfer
}

func identity() componentProfile {
	return componentProfile{dns: 1, tcp: 1, tls: 1, ttfb: 1, transfer: 1}
}

var healthyUsers = componentProfile{dns: 2, tcp: 3, tls: 5, ttfb: 30, transfer: 10}
var healthyOrders = componentProfile{dns: 2, tcp: 3, tls: 5, ttfb: 35, transfer: 12}

// lcg is a minimal linear congruential generator used instead of math/rand
// so fixture output is guaranteed identical across Go versions.
type lcg struct{ state uint64 }

func newLCG(seed uint64) *lcg { return &lcg{state: seed} }

// next returns a pseudo-random float64 in [0, 1).
func (l *lcg) next() float64 {
	l.state = l.state*6364136223846793005 + 1442695040888963407
	return float64(l.state>>11) / float64(1<<53)
}

type genOptions struct {
	runID         string
	endpoint      string
	base          componentProfile
	errorRate     float64
	seed          uint64
	podMultiplier map[string]componentProfile // per-pod override of the identity multiplier
}

func generate(opt genOptions) []models.Event {
	rng := newLCG(opt.seed)
	events := make([]models.Event, 0, requestsPerEndpoint)

	for i := 0; i < requestsPerEndpoint; i++ {
		pod := pods[i%len(pods)]
		mult := identity()
		if opt.podMultiplier != nil {
			if m, ok := opt.podMultiplier[pod]; ok {
				mult = m
			}
		}

		jitter := 1.0 + (rng.next()-0.5)*0.2 // +/-10%
		comp := opt.base.scaled(mult, jitter)

		status := 200
		errCode := ""
		if rng.next() < opt.errorRate {
			status = 500
			errCode = "SERVER_ERROR"
		}

		events = append(events, models.Event{
			RunID:           opt.runID,
			Ts:              int64(1_700_000_000_000 + i*100),
			PodID:           pod,
			VU:              i % 10,
			Iter:            i,
			Method:          "GET",
			URL:             opt.endpoint,
			Name:            opt.endpoint,
			Status:          status,
			BodyLen:         512,
			RTT:             comp.rtt(),
			DNSLookup:       comp.dns,
			TCPConnect:      comp.tcp,
			TLSHandshake:    comp.tls,
			TTFB:            comp.ttfb,
			ContentTransfer: comp.transfer,
			RequestSize:     256,
			ResponseSize:    768,
			ErrorCode:       errCode,
		})
	}
	return events
}

// Generate returns the deterministic event set for a scenario, covering
// both a "baseline" and a "current" run_id so comparison/regression/explain
// analysis can run against it directly.
func Generate(scenario Scenario) []models.Event {
	var events []models.Event

	baseline := func(seedOffset uint64) {
		events = append(events, generate(genOptions{
			runID: BaselineRunID, endpoint: endpointUsers, base: healthyUsers,
			errorRate: 0.01, seed: 1000 + seedOffset,
		})...)
		events = append(events, generate(genOptions{
			runID: BaselineRunID, endpoint: endpointOrders, base: healthyOrders,
			errorRate: 0.01, seed: 2000 + seedOffset,
		})...)
	}

	switch scenario {
	case Healthy:
		baseline(0)
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: endpointUsers, base: healthyUsers,
			errorRate: 0.01, seed: 3000,
		})...)
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: endpointOrders, base: healthyOrders,
			errorRate: 0.01, seed: 4000,
		})...)

	case LatencyRegression:
		baseline(0)
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: endpointUsers, base: healthyUsers,
			errorRate: 0.01, seed: 3000,
		})...)
		regressed := componentProfile{dns: 2, tcp: 3, tls: 5, ttfb: 90, transfer: 30}
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: endpointOrders, base: regressed,
			errorRate: 0.01, seed: 4000,
		})...)

	case ErrorSpike:
		baseline(0)
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: endpointUsers, base: healthyUsers,
			errorRate: 0.18, seed: 3000,
		})...)
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: endpointOrders, base: healthyOrders,
			errorRate: 0.01, seed: 4000,
		})...)

	case NetworkRegression:
		baseline(0)
		regressed := componentProfile{dns: 22, tcp: 25, tls: 28, ttfb: 30, transfer: 10}
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: endpointUsers, base: regressed,
			errorRate: 0.01, seed: 3000,
		})...)
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: endpointOrders, base: healthyOrders,
			errorRate: 0.01, seed: 4000,
		})...)

	case BackendRegression:
		baseline(0)
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: endpointUsers, base: healthyUsers,
			errorRate: 0.01, seed: 3000,
		})...)
		regressed := componentProfile{dns: 2, tcp: 3, tls: 5, ttfb: 120, transfer: 12}
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: endpointOrders, base: regressed,
			errorRate: 0.01, seed: 4000,
		})...)

	case PodOutlier:
		baseline(0)
		outlierPod := map[string]componentProfile{
			"pod-2": {dns: 1, tcp: 1, tls: 1, ttfb: 4, transfer: 1},
		}
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: endpointUsers, base: healthyUsers,
			errorRate: 0.01, seed: 3000,
		})...)
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: endpointOrders, base: healthyOrders,
			errorRate: 0.01, seed: 4000, podMultiplier: outlierPod,
		})...)

	case MixedRegression:
		baseline(0)
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: endpointUsers, base: healthyUsers,
			errorRate: 0.20, seed: 3000,
		})...)
		regressed := componentProfile{dns: 2, tcp: 3, tls: 5, ttfb: 95, transfer: 30}
		outlierPod := map[string]componentProfile{
			"pod-2": {dns: 1, tcp: 1, tls: 1, ttfb: 2.5, transfer: 1},
		}
		events = append(events, generate(genOptions{
			runID: CurrentRunID, endpoint: endpointOrders, base: regressed,
			errorRate: 0.01, seed: 4000, podMultiplier: outlierPod,
		})...)

	default:
		panic(fmt.Sprintf("fixtures: unknown scenario %q", scenario))
	}

	return events
}

// BuildDB creates an in-memory DuckDB database with the standard `metrics`
// schema, loads the scenario's synthetic events into it, and returns the
// open connection plus a cleanup function.
func BuildDB(scenario Scenario) (db *sql.DB, cleanup func(), err error) {
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

	for _, e := range Generate(scenario) {
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
