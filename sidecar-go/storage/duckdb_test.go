package storage

import (
	"os"
	"path/filepath"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/models"
)

func TestNewDuckDBStorage(t *testing.T) {
	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, "test.duckdb")

	storage, err := NewDuckDBStorage(dbFile)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	// Verify file was created
	if _, err := os.Stat(dbFile); os.IsNotExist(err) {
		t.Error("Database file was not created")
	}
}

func TestDuckDBStorage_AddEvent(t *testing.T) {
	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, "test.duckdb")

	storage, err := NewDuckDBStorage(dbFile)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	event := models.Event{
		RunID:   "test-run",
		Ts:      1704067200000,
		PodID:   "pod-1",
		URL:     "https://example.com",
		Status:  200,
		RTT:     50.0,
		BodyLen: 100,
	}

	storage.AddEvent(event)

	if storage.BufferSize() != 1 {
		t.Errorf("BufferSize() = %d, expected 1", storage.BufferSize())
	}
}

func TestDuckDBStorage_AddEvents(t *testing.T) {
	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, "test.duckdb")

	storage, err := NewDuckDBStorage(dbFile)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	events := []models.Event{
		{RunID: "test", Ts: 1000, Status: 200, RTT: 10.0},
		{RunID: "test", Ts: 2000, Status: 201, RTT: 20.0},
		{RunID: "test", Ts: 3000, Status: 500, RTT: 30.0},
	}

	storage.AddEvents(events)

	if storage.BufferSize() != 3 {
		t.Errorf("BufferSize() = %d, expected 3", storage.BufferSize())
	}
}

func TestDuckDBStorage_Flush(t *testing.T) {
	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, "test.duckdb")

	storage, err := NewDuckDBStorage(dbFile)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	events := []models.Event{
		{
			RunID:           "test-run",
			Ts:              1704067200000,
			PodID:           "pod-1",
			VU:              1,
			Iter:            1,
			Method:          "GET",
			URL:             "https://example.com/api",
			Name:            "api-call",
			Status:          200,
			BodyLen:         1024,
			RTT:             50.0,
			DNSLookup:       5.0,
			TCPConnect:      10.0,
			TLSHandshake:    15.0,
			TTFB:            40.0,
			ContentTransfer: 10.0,
			RequestSize:     256,
			ResponseSize:    1280,
			Tags:            map[string]string{"region": "us-east-1"},
		},
		{
			RunID:     "test-run",
			Ts:        1704067201000,
			PodID:     "pod-1",
			Status:    500,
			RTT:       100.0,
			ErrorCode: "SERVER_ERROR",
			ErrorMsg:  "Internal Server Error",
		},
	}

	storage.AddEvents(events)

	count, err := storage.Flush()
	if err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	if count != 2 {
		t.Errorf("Flush count = %d, expected 2", count)
	}

	if storage.BufferSize() != 0 {
		t.Errorf("BufferSize after flush = %d, expected 0", storage.BufferSize())
	}

	// Verify data was written
	rows, err := storage.Query("SELECT COUNT(*) FROM metrics")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	defer rows.Close()

	var rowCount int
	if rows.Next() {
		rows.Scan(&rowCount)
	}
	if rowCount != 2 {
		t.Errorf("Row count in DB = %d, expected 2", rowCount)
	}
}

func TestDuckDBStorage_FlushEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, "test.duckdb")

	storage, err := NewDuckDBStorage(dbFile)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	count, err := storage.Flush()
	if err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	if count != 0 {
		t.Errorf("Flush count = %d, expected 0", count)
	}
}

func TestDuckDBStorage_GetStats(t *testing.T) {
	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, "test.duckdb")

	storage, err := NewDuckDBStorage(dbFile)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	events := []models.Event{
		{Ts: 1000, Status: 200, RTT: 10.0},
		{Ts: 2000, Status: 200, RTT: 20.0},
		{Ts: 3000, Status: 200, RTT: 30.0},
		{Ts: 4000, Status: 500, RTT: 40.0, ErrorCode: "SERVER_ERROR"},
		{Ts: 5000, Status: 200, RTT: 50.0},
	}

	storage.AddEvents(events)
	if _, err := storage.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	stats, err := storage.GetStats()
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if stats.TotalRequests != 5 {
		t.Errorf("TotalRequests = %d, expected 5", stats.TotalRequests)
	}

	if stats.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, expected 1", stats.ErrorCount)
	}

	expectedAvg := 30.0
	if stats.AvgRTT != expectedAvg {
		t.Errorf("AvgRTT = %f, expected %f", stats.AvgRTT, expectedAvg)
	}

	expectedErrorRate := 20.0
	if stats.ErrorRate() != expectedErrorRate {
		t.Errorf("ErrorRate = %f, expected %f", stats.ErrorRate(), expectedErrorRate)
	}
}

func TestMetricsStats_RPS(t *testing.T) {
	stats := &MetricsStats{
		TotalRequests: 1000,
		StartTs:       0,
		EndTs:         10000, // 10 seconds
	}

	expectedRPS := 100.0
	if stats.RPS() != expectedRPS {
		t.Errorf("RPS = %f, expected %f", stats.RPS(), expectedRPS)
	}
}

func TestMetricsStats_ZeroDuration(t *testing.T) {
	stats := &MetricsStats{
		TotalRequests: 100,
		StartTs:       1000,
		EndTs:         1000,
	}

	if stats.RPS() != 0 {
		t.Errorf("RPS with zero duration = %f, expected 0", stats.RPS())
	}
}

func TestDuckDBStorage_DetailedTimings(t *testing.T) {
	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, "test.duckdb")

	storage, err := NewDuckDBStorage(dbFile)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	event := models.Event{
		RunID:           "test",
		Ts:              1000,
		PodID:           "pod-1",
		URL:             "https://example.com",
		Status:          200,
		RTT:             100.0,
		DNSLookup:       5.5,
		TCPConnect:      10.2,
		TLSHandshake:    25.3,
		TTFB:            80.0,
		ContentTransfer: 19.0,
	}

	storage.AddEvent(event)
	if _, err := storage.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Query and verify detailed timings
	rows, err := storage.Query(`
		SELECT dns_lookup, tcp_connect, tls_handshake, ttfb, content_transfer
		FROM metrics
	`)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("No rows returned")
	}

	var dns, tcp, tls, ttfb, content float64
	if err := rows.Scan(&dns, &tcp, &tls, &ttfb, &content); err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if dns != 5.5 {
		t.Errorf("DNSLookup = %f, expected 5.5", dns)
	}
	if tcp != 10.2 {
		t.Errorf("TCPConnect = %f, expected 10.2", tcp)
	}
	if tls != 25.3 {
		t.Errorf("TLSHandshake = %f, expected 25.3", tls)
	}
	if ttfb != 80.0 {
		t.Errorf("TTFB = %f, expected 80.0", ttfb)
	}
	if content != 19.0 {
		t.Errorf("ContentTransfer = %f, expected 19.0", content)
	}
}

func TestDuckDBStorage_Tags(t *testing.T) {
	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, "test.duckdb")

	storage, err := NewDuckDBStorage(dbFile)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	event := models.Event{
		RunID:  "test",
		Ts:     1000,
		Status: 200,
		RTT:    50.0,
		Tags: map[string]string{
			"region":   "us-east-1",
			"user_id":  "user123",
			"scenario": "login",
		},
	}

	storage.AddEvent(event)
	if _, err := storage.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Query and verify tags
	rows, err := storage.Query("SELECT tags FROM metrics")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("No rows returned")
	}

	var tagsJSON string
	if err := rows.Scan(&tagsJSON); err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// Verify JSON contains expected values
	if tagsJSON == "" {
		t.Error("Tags JSON is empty")
	}
}
