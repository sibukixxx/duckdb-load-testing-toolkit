package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"

	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis/fixtures"
)

func TestHandleEndpoints(t *testing.T) {
	db, cleanup, err := fixtures.BuildDB(fixtures.Healthy)
	if err != nil {
		t.Fatalf("BuildDB failed: %v", err)
	}
	defer cleanup()

	ah := NewAnalysisHandlers(db)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analysis/endpoints?run_id="+fixtures.CurrentRunID, nil)
	w := httptest.NewRecorder()
	ah.HandleEndpoints(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp EndpointsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Endpoints) != 2 {
		t.Errorf("expected 2 endpoints, got %d", len(resp.Endpoints))
	}
}

func TestHandleEndpoints_MissingRunID(t *testing.T) {
	db, cleanup, err := fixtures.BuildDB(fixtures.Healthy)
	if err != nil {
		t.Fatalf("BuildDB failed: %v", err)
	}
	defer cleanup()

	ah := NewAnalysisHandlers(db)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analysis/endpoints", nil)
	w := httptest.NewRecorder()
	ah.HandleEndpoints(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandleDiagnose(t *testing.T) {
	db, cleanup, err := fixtures.BuildDB(fixtures.LatencyRegression)
	if err != nil {
		t.Fatalf("BuildDB failed: %v", err)
	}
	defer cleanup()

	ah := NewAnalysisHandlers(db)

	body, _ := json.Marshal(DiagnoseRequest{
		BaselineRunID: fixtures.BaselineRunID,
		CurrentRunID:  fixtures.CurrentRunID,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/analysis/diagnose", bytes.NewReader(body))
	w := httptest.NewRecorder()
	ah.HandleDiagnose(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp DiagnoseResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Explanations) == 0 {
		t.Error("expected at least one explanation for the latency-regression fixture")
	}
	foundOrders := false
	for _, e := range resp.Explanations {
		if e.Endpoint == "/api/orders" {
			foundOrders = true
			if e.LargestTimingChange.Phase == "" {
				t.Error("expected a non-empty largest timing change phase")
			}
		}
	}
	if !foundOrders {
		t.Errorf("expected an explanation for /api/orders, got %+v", resp.Explanations)
	}
}

func TestHandleDiagnose_MissingFields(t *testing.T) {
	db, cleanup, err := fixtures.BuildDB(fixtures.Healthy)
	if err != nil {
		t.Fatalf("BuildDB failed: %v", err)
	}
	defer cleanup()

	ah := NewAnalysisHandlers(db)
	body, _ := json.Marshal(DiagnoseRequest{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/analysis/diagnose", bytes.NewReader(body))
	w := httptest.NewRecorder()
	ah.HandleDiagnose(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}
