package tests

import (
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/example/duckdb-sidecar/handlers"
	"github.com/example/duckdb-sidecar/models"
	"github.com/example/duckdb-sidecar/test/e2e"
)

func TestE2EIngest_SingleEvent(t *testing.T) {
	t.Parallel()
	app := e2e.NewTestApp(t)
	client := e2e.NewClient(app.BaseURL())

	event := models.Event{
		RunID:  "run-001",
		Ts:     1704067200000,
		PodID:  "pod-1",
		URL:    "https://api.example.com/users",
		Method: "GET",
		Status: 200,
		RTT:    42.5,
	}

	resp := client.MustPostJSON(t, "/api/v1/ingest", event)
	defer resp.Body.Close()
	e2e.AssertAccepted(t, resp)
}

func TestE2EIngest_BatchEvents(t *testing.T) {
	t.Parallel()
	app := e2e.NewTestApp(t)
	client := e2e.NewClient(app.BaseURL())

	req := handlers.IngestRequest{
		Events: []models.Event{
			{RunID: "run-1", Ts: 1000, Status: 200, RTT: 10.0},
			{RunID: "run-1", Ts: 2000, Status: 201, RTT: 20.0},
			{RunID: "run-1", Ts: 3000, Status: 200, RTT: 30.0},
		},
	}

	resp := client.MustPostJSON(t, "/api/v1/ingest/batch", req)
	defer resp.Body.Close()
	e2e.AssertAccepted(t, resp)

	var got handlers.IngestResponse
	e2e.DecodeJSON(t, resp, &got)

	if got.Accepted != 3 {
		t.Errorf("accepted: got %d, want 3", got.Accepted)
	}
}

func TestE2EIngest_BatchEmpty_ReturnsBadRequest(t *testing.T) {
	t.Parallel()
	app := e2e.NewTestApp(t)
	client := e2e.NewClient(app.BaseURL())

	req := handlers.IngestRequest{Events: nil}

	resp := client.MustPostJSON(t, "/api/v1/ingest/batch", req)
	defer resp.Body.Close()
	e2e.AssertBadRequest(t, resp)
}

func TestE2EIngest_InvalidJSON_ReturnsBadRequest(t *testing.T) {
	t.Parallel()
	app := e2e.NewTestApp(t)
	client := e2e.NewClient(app.BaseURL())

	resp, err := client.PostRaw("/api/v1/ingest", "application/json", []byte("not json"))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	e2e.AssertBadRequest(t, resp)
}

// TestE2EIngest_FullFlow verifies the complete ingest → flush → stats pipeline.
func TestE2EIngest_FullFlow(t *testing.T) {
	t.Parallel()
	app := e2e.NewTestApp(t, e2e.WithRunID("flow-test"))
	client := e2e.NewClient(app.BaseURL())

	// 5 events: 4 success, 1 error
	batchReq := handlers.IngestRequest{
		Events: []models.Event{
			{RunID: "flow-test", Ts: 1000, Status: 200, RTT: 10.0},
			{RunID: "flow-test", Ts: 2000, Status: 200, RTT: 20.0},
			{RunID: "flow-test", Ts: 3000, Status: 200, RTT: 30.0},
			{RunID: "flow-test", Ts: 4000, Status: 500, RTT: 100.0, ErrorCode: "ERR"},
			{RunID: "flow-test", Ts: 5000, Status: 200, RTT: 40.0},
		},
	}

	batchResp := client.MustPostJSON(t, "/api/v1/ingest/batch", batchReq)
	defer batchResp.Body.Close()
	e2e.AssertAccepted(t, batchResp)

	var ingestResult handlers.IngestResponse
	e2e.DecodeJSON(t, batchResp, &ingestResult)
	if ingestResult.Accepted != 5 {
		t.Fatalf("accepted: got %d, want 5", ingestResult.Accepted)
	}

	// Flush buffer to DuckDB
	flushResp := client.MustPost(t, "/api/v1/flush")
	defer flushResp.Body.Close()
	e2e.AssertOK(t, flushResp)

	var flushResult handlers.FlushResponse
	e2e.DecodeJSON(t, flushResp, &flushResult)
	if flushResult.Flushed != 5 {
		t.Errorf("flushed: got %d, want 5", flushResult.Flushed)
	}

	// Get stats and verify
	statsResp := client.MustGet(t, "/api/v1/stats")
	defer statsResp.Body.Close()
	e2e.AssertOK(t, statsResp)

	var stats handlers.StatsResponse
	e2e.DecodeJSON(t, statsResp, &stats)

	if stats.TotalRequests != 5 {
		t.Errorf("total_requests: got %d, want 5", stats.TotalRequests)
	}
	if stats.ErrorCount != 1 {
		t.Errorf("error_count: got %d, want 1", stats.ErrorCount)
	}
	expectedErrorRate := 20.0
	if stats.ErrorRate != expectedErrorRate {
		t.Errorf("error_rate: got %v, want %v", stats.ErrorRate, expectedErrorRate)
	}
}
