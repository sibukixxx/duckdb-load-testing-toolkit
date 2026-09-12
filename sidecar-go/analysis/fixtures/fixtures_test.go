package fixtures

import (
	"reflect"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
)

func TestGenerate_Deterministic(t *testing.T) {
	for _, sc := range All() {
		a := Generate(sc)
		b := Generate(sc)
		if len(a) != len(b) {
			t.Fatalf("%s: non-deterministic length: %d vs %d", sc, len(a), len(b))
		}
		for i := range a {
			if !reflect.DeepEqual(a[i], b[i]) {
				t.Fatalf("%s: event %d differs between runs", sc, i)
			}
		}
		if len(a) == 0 {
			t.Fatalf("%s: generated no events", sc)
		}
	}
}

func TestGenerate_HasBothRuns(t *testing.T) {
	for _, sc := range All() {
		events := Generate(sc)
		var hasBaseline, hasCurrent bool
		for _, e := range events {
			switch e.RunID {
			case BaselineRunID:
				hasBaseline = true
			case CurrentRunID:
				hasCurrent = true
			}
			if e.RTT <= 0 {
				t.Errorf("%s: expected positive RTT, got %f", sc, e.RTT)
			}
		}
		if !hasBaseline || !hasCurrent {
			t.Errorf("%s: expected both baseline and current run_id, got baseline=%v current=%v", sc, hasBaseline, hasCurrent)
		}
	}
}

func TestBuildDB(t *testing.T) {
	for _, sc := range All() {
		db, cleanup, err := BuildDB(sc)
		if err != nil {
			t.Fatalf("%s: BuildDB failed: %v", sc, err)
		}

		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM metrics").Scan(&count); err != nil {
			cleanup()
			t.Fatalf("%s: query failed: %v", sc, err)
		}
		if count != len(Generate(sc)) {
			t.Errorf("%s: row count = %d, expected %d", sc, count, len(Generate(sc)))
		}
		cleanup()
	}
}

// TestLatencyRegression_ObservablyRegresses sanity-checks that the
// latency-regression fixture actually regresses on /api/orders and leaves
// /api/users alone, since downstream analysis tests rely on this shape.
func TestLatencyRegression_ObservablyRegresses(t *testing.T) {
	db, cleanup, err := BuildDB(LatencyRegression)
	if err != nil {
		t.Fatalf("BuildDB failed: %v", err)
	}
	defer cleanup()

	avgRTT := func(runID, endpoint string) float64 {
		var v float64
		err := db.QueryRow(
			"SELECT AVG(rtt) FROM metrics WHERE run_id = ? AND url = ?", runID, endpoint,
		).Scan(&v)
		if err != nil {
			t.Fatalf("query failed: %v", err)
		}
		return v
	}

	baselineOrders := avgRTT(BaselineRunID, endpointOrders)
	currentOrders := avgRTT(CurrentRunID, endpointOrders)
	if currentOrders < baselineOrders*1.5 {
		t.Errorf("expected /api/orders to regress: baseline=%.2f current=%.2f", baselineOrders, currentOrders)
	}

	baselineUsers := avgRTT(BaselineRunID, endpointUsers)
	currentUsers := avgRTT(CurrentRunID, endpointUsers)
	if currentUsers > baselineUsers*1.2 {
		t.Errorf("expected /api/users to stay stable: baseline=%.2f current=%.2f", baselineUsers, currentUsers)
	}
}
