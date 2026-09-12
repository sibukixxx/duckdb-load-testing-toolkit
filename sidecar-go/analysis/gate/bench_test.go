package gate_test

// This file is a synthetic cost check for the performance gate at
// 100k and 1M requests (section 23 of the P1 spec): endpoint analysis and
// gate evaluation must stay DuckDB-side aggregation, not a Go-memory scan
// of every row, so cost should grow with the number of endpoints, not the
// number of requests. It is a benchmark, not a test — run explicitly with
// `make bench-gate` — precisely so a slow bulk load never taxes the
// regular CI unit-test run.

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"

	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis/gate"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis/policy"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/storage"
)

// buildBenchDB bulk-loads n synthetic requests, spread across a handful of
// endpoints, into an in-memory DuckDB database via CSV COPY — the same
// bulk-load path the sidecar itself uses (see storage.DuckDBStorage) —
// rather than a per-row INSERT loop, so building a 1M-row fixture does not
// itself dominate the benchmark.
func buildBenchDB(b *testing.B, n int) *sql.DB {
	b.Helper()

	db, err := sql.Open("duckdb", "")
	if err != nil {
		b.Fatalf("open duckdb: %v", err)
	}
	b.Cleanup(func() { db.Close() })

	if _, err := db.Exec(storage.MetricsTableSchema); err != nil {
		b.Fatalf("create schema: %v", err)
	}

	tmp, err := os.CreateTemp(b.TempDir(), "bench-*.csv")
	if err != nil {
		b.Fatalf("create temp csv: %v", err)
	}
	defer tmp.Close()

	w := csv.NewWriter(tmp)
	header := []string{
		"ts", "run_id", "pod_id", "vu", "iter", "method", "url", "name", "status", "body_len",
		"rtt", "dns_lookup", "tcp_connect", "tls_handshake", "ttfb", "content_transfer",
		"request_size", "response_size", "error_code", "error_msg", "tags",
	}
	if err := w.Write(header); err != nil {
		b.Fatalf("write csv header: %v", err)
	}

	endpoints := []string{"/api/users", "/api/orders", "/api/search", "/api/checkout", "/api/health"}
	pods := []string{"pod-0", "pod-1", "pod-2"}
	for i := 0; i < n; i++ {
		endpoint := endpoints[i%len(endpoints)]
		pod := pods[i%len(pods)]
		runID := "current"
		if i%2 == 0 {
			runID = "baseline"
		}
		rtt := 50.0 + float64(i%37)

		row := []string{
			strconv.Itoa(1_700_000_000_000 + i), runID, pod, strconv.Itoa(i % 10), strconv.Itoa(i),
			"GET", endpoint, endpoint, "200", "512",
			fmt.Sprintf("%f", rtt), "2.0", "3.0", "5.0", "30.0", "10.0",
			"256", "768", "", "", "",
		}
		if err := w.Write(row); err != nil {
			b.Fatalf("write csv row: %v", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		b.Fatalf("flush csv: %v", err)
	}

	if _, err := db.Exec(fmt.Sprintf("COPY metrics FROM '%s' (HEADER true, AUTO_DETECT true)", tmp.Name())); err != nil {
		b.Fatalf("copy into duckdb: %v", err)
	}

	return db
}

func benchmarkGateEvaluate(b *testing.B, n int) {
	db := buildBenchDB(b, n)
	analyzer := analysis.NewEndpointAnalyzer(db)

	maxMs, warnMs, maxPct, warnPct := 500.0, 400.0, 20.0, 10.0
	pol := &policy.Policy{
		Version:          policy.SchemaVersion,
		UnknownTreatment: policy.StatusWarn,
		Defaults: policy.MetricPolicy{
			P95: &policy.LatencyPolicy{MaxMs: &maxMs, WarnMaxMs: &warnMs, MaxRegressionPercent: &maxPct, WarnRegressionPercent: &warnPct},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		current, err := analyzer.GetEndpointStats("current")
		if err != nil {
			b.Fatalf("GetEndpointStats(current): %v", err)
		}
		baseline, err := analyzer.GetEndpointStats("baseline")
		if err != nil {
			b.Fatalf("GetEndpointStats(baseline): %v", err)
		}
		if gate.Evaluate(current, baseline, pol) == nil {
			b.Fatal("Evaluate returned nil")
		}
	}
}

func BenchmarkEvaluate_100kRequests(b *testing.B) { benchmarkGateEvaluate(b, 100_000) }
func BenchmarkEvaluate_1MRequests(b *testing.B)   { benchmarkGateEvaluate(b, 1_000_000) }
