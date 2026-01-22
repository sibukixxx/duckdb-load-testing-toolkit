package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/example/duckdb-sidecar/models"
	"github.com/example/duckdb-sidecar/storage"
	_ "github.com/duckdb/duckdb-go/v2"
)

func setupTestHandler(t *testing.T) (*Handlers, func()) {
	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, "test.duckdb")

	store, err := storage.NewDuckDBStorage(dbFile)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	h := NewHandlers(store, nil, "test-run")

	cleanup := func() {
		store.Close()
	}

	return h, cleanup
}

func TestHandleIngest(t *testing.T) {
	h, cleanup := setupTestHandler(t)
	defer cleanup()

	event := models.Event{
		RunID:   "test-run",
		Ts:      1704067200000,
		PodID:   "pod-1",
		URL:     "https://example.com",
		Status:  200,
		RTT:     50.0,
		BodyLen: 100,
	}

	body, _ := json.Marshal(event)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleIngest(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("Expected status %d, got %d", http.StatusAccepted, w.Code)
	}

	if h.storage.BufferSize() != 1 {
		t.Errorf("Expected buffer size 1, got %d", h.storage.BufferSize())
	}
}

func TestHandleIngest_InvalidJSON(t *testing.T) {
	h, cleanup := setupTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleIngest(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleIngestBatch(t *testing.T) {
	h, cleanup := setupTestHandler(t)
	defer cleanup()

	events := IngestRequest{
		Events: []models.Event{
			{RunID: "test", Ts: 1000, Status: 200, RTT: 10.0},
			{RunID: "test", Ts: 2000, Status: 201, RTT: 20.0},
			{RunID: "test", Ts: 3000, Status: 200, RTT: 30.0},
		},
	}

	body, _ := json.Marshal(events)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleIngestBatch(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("Expected status %d, got %d", http.StatusAccepted, w.Code)
	}

	var resp IngestResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Accepted != 3 {
		t.Errorf("Expected 3 accepted, got %d", resp.Accepted)
	}

	if h.storage.BufferSize() != 3 {
		t.Errorf("Expected buffer size 3, got %d", h.storage.BufferSize())
	}
}

func TestHandleIngestBatch_Empty(t *testing.T) {
	h, cleanup := setupTestHandler(t)
	defer cleanup()

	events := IngestRequest{Events: []models.Event{}}

	body, _ := json.Marshal(events)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleIngestBatch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleFlush(t *testing.T) {
	h, cleanup := setupTestHandler(t)
	defer cleanup()

	// Add some events
	for i := 0; i < 5; i++ {
		h.storage.AddEvent(models.Event{
			RunID:  "test",
			Ts:     int64(i * 1000),
			Status: 200,
			RTT:    float64(i * 10),
		})
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/flush", nil)
	w := httptest.NewRecorder()

	h.HandleFlush(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp FlushResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Flushed != 5 {
		t.Errorf("Expected 5 flushed, got %d", resp.Flushed)
	}

	if h.storage.BufferSize() != 0 {
		t.Errorf("Expected buffer size 0, got %d", h.storage.BufferSize())
	}
}

func TestHandleFlushUpload_NoS3(t *testing.T) {
	h, cleanup := setupTestHandler(t)
	defer cleanup()

	h.storage.AddEvent(models.Event{Ts: 1000, Status: 200, RTT: 10.0})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/flush-upload", nil)
	w := httptest.NewRecorder()

	h.HandleFlushUpload(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp FlushResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Uploaded {
		t.Error("Expected uploaded to be false")
	}

	if resp.Message == "" {
		t.Error("Expected message about no S3 configured")
	}
}

func TestHandleStats(t *testing.T) {
	h, cleanup := setupTestHandler(t)
	defer cleanup()

	// Add events with varying statuses
	events := []models.Event{
		{Ts: 1000, Status: 200, RTT: 10.0},
		{Ts: 2000, Status: 200, RTT: 20.0},
		{Ts: 3000, Status: 200, RTT: 30.0},
		{Ts: 4000, Status: 500, RTT: 40.0, ErrorCode: "ERROR"},
		{Ts: 5000, Status: 200, RTT: 50.0},
	}
	for _, e := range events {
		h.storage.AddEvent(e)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	w := httptest.NewRecorder()

	h.HandleStats(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp StatsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.TotalRequests != 5 {
		t.Errorf("Expected 5 total requests, got %d", resp.TotalRequests)
	}

	if resp.ErrorCount != 1 {
		t.Errorf("Expected 1 error, got %d", resp.ErrorCount)
	}

	expectedErrorRate := 20.0
	if resp.ErrorRate != expectedErrorRate {
		t.Errorf("Expected error rate %f, got %f", expectedErrorRate, resp.ErrorRate)
	}
}

func TestHandleHealth(t *testing.T) {
	h, cleanup := setupTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()

	h.HandleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Status != "ok" {
		t.Errorf("Expected status 'ok', got '%s'", resp.Status)
	}

	if resp.S3Enabled {
		t.Error("Expected S3 to be disabled")
	}
}

func TestHandleDownload(t *testing.T) {
	h, cleanup := setupTestHandler(t)
	defer cleanup()

	// Add and flush an event to create data
	h.storage.AddEvent(models.Event{Ts: 1000, Status: 200, RTT: 10.0})
	h.storage.Flush()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/download", nil)
	w := httptest.NewRecorder()

	h.HandleDownload(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	if w.Header().Get("Content-Type") != "application/octet-stream" {
		t.Errorf("Expected Content-Type 'application/octet-stream', got '%s'", w.Header().Get("Content-Type"))
	}

	if w.Body.Len() == 0 {
		t.Error("Expected non-empty response body")
	}
}

func TestHandleIngestWithDetailedTimings(t *testing.T) {
	h, cleanup := setupTestHandler(t)
	defer cleanup()

	event := models.Event{
		RunID:          "test-run",
		Ts:             1704067200000,
		PodID:          "pod-1",
		VU:             1,
		Iter:           10,
		Method:         "POST",
		URL:            "https://example.com/api",
		Name:           "login",
		Status:         200,
		BodyLen:        1024,
		RTT:            100.0,
		DNSLookup:      5.0,
		TCPConnect:     10.0,
		TLSHandshake:   20.0,
		TTFB:           80.0,
		ContentTransfer: 15.0,
		RequestSize:    256,
		ResponseSize:   1280,
		Tags: map[string]string{
			"region": "us-east-1",
		},
	}

	body, _ := json.Marshal(event)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleIngest(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("Expected status %d, got %d", http.StatusAccepted, w.Code)
	}

	// Flush and verify
	h.storage.Flush()

	rows, err := h.storage.Query("SELECT dns_lookup, tcp_connect, tls_handshake FROM metrics")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("No rows returned")
	}

	var dns, tcp, tls float64
	if err := rows.Scan(&dns, &tcp, &tls); err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if dns != 5.0 || tcp != 10.0 || tls != 20.0 {
		t.Errorf("Timing values mismatch: dns=%f, tcp=%f, tls=%f", dns, tcp, tls)
	}
}
