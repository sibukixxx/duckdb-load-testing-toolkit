package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"

	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis/fixtures"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis/gate"
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

const testGatePolicyJSON = `{
	"version": 1,
	"defaults": {
		"min_samples": 30,
		"p95": {"max_ms": 500, "max_regression_percent": 20, "warn_regression_percent": 10},
		"error_rate": {"max_absolute": 5, "max_absolute_increase": 5}
	}
}`

func TestHandleGate_Fail(t *testing.T) {
	db, cleanup, err := fixtures.BuildDB(fixtures.LatencyRegression)
	if err != nil {
		t.Fatalf("BuildDB failed: %v", err)
	}
	defer cleanup()

	ah := NewAnalysisHandlers(db)

	body, _ := json.Marshal(GateRequest{
		CurrentRunID:  fixtures.CurrentRunID,
		BaselineRunID: fixtures.BaselineRunID,
		Policy:        json.RawMessage(testGatePolicyJSON),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/analysis/gate", bytes.NewReader(body))
	w := httptest.NewRecorder()
	ah.HandleGate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var res gate.Result
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if res.Status != gate.StatusFail {
		t.Errorf("expected status FAIL, got %s: %+v", res.Status, res.Violations)
	}
}

func TestHandleGate_BudgetOnlyMode(t *testing.T) {
	db, cleanup, err := fixtures.BuildDB(fixtures.Healthy)
	if err != nil {
		t.Fatalf("BuildDB failed: %v", err)
	}
	defer cleanup()

	ah := NewAnalysisHandlers(db)

	body, _ := json.Marshal(GateRequest{
		CurrentRunID: fixtures.CurrentRunID,
		Policy:       json.RawMessage(testGatePolicyJSON),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/analysis/gate", bytes.NewReader(body))
	w := httptest.NewRecorder()
	ah.HandleGate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var res gate.Result
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if res.Status != gate.StatusPass {
		t.Errorf("expected status PASS, got %s: %+v", res.Status, res.Violations)
	}
	for _, v := range res.Violations {
		if v.Check == gate.CheckRegression {
			t.Errorf("did not expect a regression check without a baseline: %+v", v)
		}
	}
}

func TestHandleGate_MissingFields(t *testing.T) {
	db, cleanup, err := fixtures.BuildDB(fixtures.Healthy)
	if err != nil {
		t.Fatalf("BuildDB failed: %v", err)
	}
	defer cleanup()

	ah := NewAnalysisHandlers(db)

	tests := []GateRequest{
		{Policy: json.RawMessage(testGatePolicyJSON)},               // missing current_run_id
		{CurrentRunID: fixtures.CurrentRunID},                       // missing policy
		{CurrentRunID: fixtures.CurrentRunID, Policy: []byte(`{}`)}, // policy missing version
	}
	for _, tt := range tests {
		body, _ := json.Marshal(tt)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/analysis/gate", bytes.NewReader(body))
		w := httptest.NewRecorder()
		ah.HandleGate(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("request %+v: expected status 400, got %d: %s", tt, w.Code, w.Body.String())
		}
	}
}

// TestHandleGate_RejectsOversizedBody guards against unbounded memory use
// from a malicious or malformed request: every analysis endpoint that
// decodes a JSON body wraps r.Body in http.MaxBytesReader before decoding.
func TestHandleGate_RejectsOversizedBody(t *testing.T) {
	db, cleanup, err := fixtures.BuildDB(fixtures.Healthy)
	if err != nil {
		t.Fatalf("BuildDB failed: %v", err)
	}
	defer cleanup()

	ah := NewAnalysisHandlers(db)

	oversizedPolicy := make([]byte, maxAnalysisRequestBody+1)
	for i := range oversizedPolicy {
		oversizedPolicy[i] = 'a'
	}
	body, _ := json.Marshal(GateRequest{
		CurrentRunID: fixtures.CurrentRunID,
		Policy:       json.RawMessage(oversizedPolicy),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/analysis/gate", bytes.NewReader(body))
	w := httptest.NewRecorder()
	ah.HandleGate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for an oversized body, got %d: %s", w.Code, w.Body.String())
	}
}
