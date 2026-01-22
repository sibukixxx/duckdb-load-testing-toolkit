package analysis

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
)

func setupTestDB(t *testing.T) (*sql.DB, func()) {
	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, "test.duckdb")

	db, err := sql.Open("duckdb", dbFile)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	// Create schema
	_, err = db.Exec(`
		CREATE TABLE metrics (
			ts BIGINT,
			run_id VARCHAR,
			pod_id VARCHAR,
			vu INTEGER,
			iter INTEGER,
			method VARCHAR,
			url VARCHAR,
			name VARCHAR,
			status INTEGER,
			body_len INTEGER,
			rtt DOUBLE,
			dns_lookup DOUBLE,
			tcp_connect DOUBLE,
			tls_handshake DOUBLE,
			ttfb DOUBLE,
			content_transfer DOUBLE,
			request_size INTEGER,
			response_size INTEGER,
			error_code VARCHAR,
			error_msg VARCHAR,
			tags VARCHAR
		)
	`)
	if err != nil {
		db.Close()
		t.Fatalf("Failed to create schema: %v", err)
	}

	cleanup := func() {
		db.Close()
	}

	return db, cleanup
}

func insertTestData(t *testing.T, db *sql.DB, runID string, events []struct {
	ts        int64
	status    int
	rtt       float64
	errorCode string
}) {
	for _, e := range events {
		_, err := db.Exec(`
			INSERT INTO metrics (ts, run_id, status, rtt, error_code)
			VALUES (?, ?, ?, ?, ?)
		`, e.ts, runID, e.status, e.rtt, e.errorCode)
		if err != nil {
			t.Fatalf("Failed to insert test data: %v", err)
		}
	}
}

func TestComparator_GetRunStats(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Insert test data
	events := []struct {
		ts        int64
		status    int
		rtt       float64
		errorCode string
	}{
		{1000, 200, 10.0, ""},
		{2000, 200, 20.0, ""},
		{3000, 200, 30.0, ""},
		{4000, 500, 40.0, "SERVER_ERROR"},
		{5000, 200, 50.0, ""},
	}
	insertTestData(t, db, "test-run", events)

	comparator := NewComparator(db, DefaultThresholds())
	stats, err := comparator.GetRunStats("test-run")
	if err != nil {
		t.Fatalf("GetRunStats failed: %v", err)
	}

	if stats.TotalRequests != 5 {
		t.Errorf("TotalRequests = %d, expected 5", stats.TotalRequests)
	}

	if stats.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, expected 1", stats.ErrorCount)
	}

	expectedErrorRate := 20.0
	if stats.ErrorRate != expectedErrorRate {
		t.Errorf("ErrorRate = %f, expected %f", stats.ErrorRate, expectedErrorRate)
	}

	expectedAvg := 30.0
	if stats.AvgRTT != expectedAvg {
		t.Errorf("AvgRTT = %f, expected %f", stats.AvgRTT, expectedAvg)
	}
}

func TestComparator_Compare_NoPregression(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Baseline run
	baselineEvents := []struct {
		ts        int64
		status    int
		rtt       float64
		errorCode string
	}{
		{1000, 200, 100.0, ""},
		{2000, 200, 110.0, ""},
		{3000, 200, 105.0, ""},
	}
	insertTestData(t, db, "baseline", baselineEvents)

	// Current run (similar performance)
	currentEvents := []struct {
		ts        int64
		status    int
		rtt       float64
		errorCode string
	}{
		{1000, 200, 102.0, ""},
		{2000, 200, 108.0, ""},
		{3000, 200, 106.0, ""},
	}
	insertTestData(t, db, "current", currentEvents)

	comparator := NewComparator(db, DefaultThresholds())
	result, err := comparator.Compare("baseline", "current")
	if err != nil {
		t.Fatalf("Compare failed: %v", err)
	}

	if !result.IsPassing {
		t.Error("Expected test to pass with similar performance")
	}

	if len(result.Regressions) > 0 {
		t.Errorf("Expected no regressions, got %d", len(result.Regressions))
	}
}

func TestComparator_Compare_WithRegression(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Baseline run (fast)
	baselineEvents := []struct {
		ts        int64
		status    int
		rtt       float64
		errorCode string
	}{
		{1000, 200, 50.0, ""},
		{2000, 200, 55.0, ""},
		{3000, 200, 52.0, ""},
	}
	insertTestData(t, db, "baseline", baselineEvents)

	// Current run (50% slower - should trigger regression)
	currentEvents := []struct {
		ts        int64
		status    int
		rtt       float64
		errorCode string
	}{
		{1000, 200, 80.0, ""},
		{2000, 200, 85.0, ""},
		{3000, 200, 82.0, ""},
	}
	insertTestData(t, db, "current", currentEvents)

	comparator := NewComparator(db, DefaultThresholds())
	result, err := comparator.Compare("baseline", "current")
	if err != nil {
		t.Fatalf("Compare failed: %v", err)
	}

	if result.IsPassing {
		t.Error("Expected test to fail with 50% slower performance")
	}

	if len(result.Regressions) == 0 {
		t.Error("Expected regressions to be detected")
	}

	// Check that P95 regression was detected
	found := false
	for _, reg := range result.Regressions {
		if reg.Metric == "P95_RTT" || reg.Metric == "P50_RTT" {
			found = true
			if reg.ChangePercent <= 0 {
				t.Errorf("Expected positive change percent, got %f", reg.ChangePercent)
			}
		}
	}
	if !found {
		t.Error("Expected P50 or P95 RTT regression to be detected")
	}
}

func TestComparator_Compare_ErrorRateRegression(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Baseline run (low errors)
	baselineEvents := []struct {
		ts        int64
		status    int
		rtt       float64
		errorCode string
	}{
		{1000, 200, 50.0, ""},
		{2000, 200, 50.0, ""},
		{3000, 200, 50.0, ""},
		{4000, 200, 50.0, ""},
		{5000, 500, 50.0, "ERROR"}, // 1 error = 20%
	}
	insertTestData(t, db, "baseline", baselineEvents)

	// Current run (high errors)
	currentEvents := []struct {
		ts        int64
		status    int
		rtt       float64
		errorCode string
	}{
		{1000, 200, 50.0, ""},
		{2000, 500, 50.0, "ERROR"},
		{3000, 500, 50.0, "ERROR"},
		{4000, 500, 50.0, "ERROR"}, // 3 errors = 75%
	}
	insertTestData(t, db, "current", currentEvents)

	thresholds := DefaultThresholds()
	thresholds.ErrorRateAbsoluteMax = 50.0 // Set high to focus on relative change

	comparator := NewComparator(db, thresholds)
	result, err := comparator.Compare("baseline", "current")
	if err != nil {
		t.Fatalf("Compare failed: %v", err)
	}

	// Should fail due to error rate increase
	foundErrorRegression := false
	for _, reg := range result.Regressions {
		if reg.Metric == "ERROR_RATE" {
			foundErrorRegression = true
		}
	}

	if !foundErrorRegression {
		t.Error("Expected error rate regression to be detected")
	}
}

func TestComparator_GetHistoricalTrend(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Insert multiple runs
	runs := []string{"run-001", "run-002", "run-003"}
	for i, runID := range runs {
		events := []struct {
			ts        int64
			status    int
			rtt       float64
			errorCode string
		}{
			{int64(i*10000 + 1000), 200, float64(50 + i*10), ""},
			{int64(i*10000 + 2000), 200, float64(55 + i*10), ""},
			{int64(i*10000 + 3000), 200, float64(52 + i*10), ""},
		}
		insertTestData(t, db, runID, events)
	}

	comparator := NewComparator(db, DefaultThresholds())
	trend, err := comparator.GetHistoricalTrend(10)
	if err != nil {
		t.Fatalf("GetHistoricalTrend failed: %v", err)
	}

	if len(trend) != 3 {
		t.Errorf("Expected 3 runs in trend, got %d", len(trend))
	}
}

func TestComparator_CalculateBaseline(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Insert multiple baseline runs
	runs := []string{"baseline-001", "baseline-002", "baseline-003"}
	for i, runID := range runs {
		events := []struct {
			ts        int64
			status    int
			rtt       float64
			errorCode string
		}{
			{int64(i*10000 + 1000), 200, 100.0, ""},
			{int64(i*10000 + 2000), 200, 100.0, ""},
			{int64(i*10000 + 3000), 200, 100.0, ""},
		}
		insertTestData(t, db, runID, events)
	}

	comparator := NewComparator(db, DefaultThresholds())
	baseline, err := comparator.CalculateBaseline(runs)
	if err != nil {
		t.Fatalf("CalculateBaseline failed: %v", err)
	}

	if baseline.RunID != "baseline" {
		t.Errorf("Expected RunID 'baseline', got '%s'", baseline.RunID)
	}

	// All runs have same RTT, so average should be 100
	if baseline.AvgRTT != 100.0 {
		t.Errorf("Expected AvgRTT 100.0, got %f", baseline.AvgRTT)
	}
}

func TestDefaultThresholds(t *testing.T) {
	thresholds := DefaultThresholds()

	if thresholds.P50RTTIncreasePercent <= 0 {
		t.Error("P50RTTIncreasePercent should be positive")
	}

	if thresholds.P95RTTIncreasePercent <= 0 {
		t.Error("P95RTTIncreasePercent should be positive")
	}

	if thresholds.ErrorRateAbsoluteMax <= 0 {
		t.Error("ErrorRateAbsoluteMax should be positive")
	}
}

func TestRound(t *testing.T) {
	tests := []struct {
		val       float64
		precision int
		expected  float64
	}{
		{1.2345, 2, 1.23},
		{1.2355, 2, 1.24},
		{1.5, 0, 2.0},
		{1.4, 0, 1.0},
	}

	for _, tt := range tests {
		result := Round(tt.val, tt.precision)
		if result != tt.expected {
			t.Errorf("Round(%f, %d) = %f, expected %f", tt.val, tt.precision, result, tt.expected)
		}
	}
}
