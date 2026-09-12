package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis/gate"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis/policy"
)

// maxAnalysisRequestBody caps the size of a decoded JSON request body for
// every analysis endpoint. These requests are small control-plane payloads
// (run_id strings, optional thresholds, an embedded policy) — 1 MiB is
// generous headroom, not a real payload size — and capping it stops an
// oversized or malformed body from being buffered into memory in full
// before json.Decode has a chance to reject it.
const maxAnalysisRequestBody = 1 << 20 // 1 MiB

// AnalysisHandlers contains handlers for analysis endpoints
type AnalysisHandlers struct {
	db         *sql.DB
	comparator *analysis.Comparator
	endpoints  *analysis.EndpointAnalyzer
}

// NewAnalysisHandlers creates new analysis handlers
func NewAnalysisHandlers(db *sql.DB) *AnalysisHandlers {
	return &AnalysisHandlers{
		db:         db,
		comparator: analysis.NewComparator(db, analysis.DefaultThresholds()),
		endpoints:  analysis.NewEndpointAnalyzer(db),
	}
}

// CompareRequest represents a comparison request
type CompareRequest struct {
	BaselineRunID string               `json:"baseline_run_id"`
	CurrentRunID  string               `json:"current_run_id"`
	Thresholds    *analysis.Thresholds `json:"thresholds,omitempty"`
}

// CompareResponse represents a comparison response
type CompareResponse struct {
	IsPassing   bool                 `json:"is_passing"`
	Baseline    *analysis.RunStats   `json:"baseline"`
	Current     *analysis.RunStats   `json:"current"`
	Regressions []RegressionResponse `json:"regressions"`
	Summary     string               `json:"summary"`
}

// RegressionResponse represents a regression in the response
type RegressionResponse struct {
	Metric        string  `json:"metric"`
	BaselineValue float64 `json:"baseline_value"`
	CurrentValue  float64 `json:"current_value"`
	ChangePercent float64 `json:"change_percent"`
	Threshold     float64 `json:"threshold"`
	Severity      string  `json:"severity"`
}

// HandleCompare compares two test runs
func (h *AnalysisHandlers) HandleCompare(w http.ResponseWriter, r *http.Request) {
	var req CompareRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxAnalysisRequestBody)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.BaselineRunID == "" || req.CurrentRunID == "" {
		http.Error(w, "baseline_run_id and current_run_id are required", http.StatusBadRequest)
		return
	}

	// Use custom thresholds if provided
	comparator := h.comparator
	if req.Thresholds != nil {
		comparator = analysis.NewComparator(h.db, *req.Thresholds)
	}

	result, err := comparator.Compare(req.BaselineRunID, req.CurrentRunID)
	if err != nil {
		log.Printf("comparison error: %v", err)
		http.Error(w, "comparison failed", http.StatusInternalServerError)
		return
	}

	// Build response
	resp := CompareResponse{
		IsPassing: result.IsPassing,
		Baseline:  result.Baseline,
		Current:   result.Current,
	}

	for _, reg := range result.Regressions {
		resp.Regressions = append(resp.Regressions, RegressionResponse{
			Metric:        reg.Metric,
			BaselineValue: analysis.Round(reg.BaselineValue, 2),
			CurrentValue:  analysis.Round(reg.CurrentValue, 2),
			ChangePercent: analysis.Round(reg.ChangePercent, 2),
			Threshold:     reg.Threshold,
			Severity:      reg.Severity,
		})
	}

	// Generate summary
	if result.IsPassing {
		resp.Summary = "No regressions detected. Performance is within acceptable thresholds."
	} else {
		resp.Summary = "Performance regressions detected. Review the regressions list for details."
	}

	writeJSON(w, http.StatusOK, resp)
}

// RunStatsResponse represents run statistics response
type RunStatsResponse struct {
	RunID           string  `json:"run_id"`
	TotalRequests   int64   `json:"total_requests"`
	ErrorCount      int64   `json:"error_count"`
	ErrorRate       float64 `json:"error_rate"`
	AvgRTT          float64 `json:"avg_rtt_ms"`
	P50RTT          float64 `json:"p50_rtt_ms"`
	P95RTT          float64 `json:"p95_rtt_ms"`
	P99RTT          float64 `json:"p99_rtt_ms"`
	MinRTT          float64 `json:"min_rtt_ms"`
	MaxRTT          float64 `json:"max_rtt_ms"`
	RPS             float64 `json:"rps"`
	DurationSeconds float64 `json:"duration_seconds"`
}

// HandleRunStats returns statistics for a specific run
func (h *AnalysisHandlers) HandleRunStats(w http.ResponseWriter, r *http.Request) {
	runID := r.URL.Query().Get("run_id")
	if runID == "" {
		http.Error(w, "run_id is required", http.StatusBadRequest)
		return
	}

	stats, err := h.comparator.GetRunStats(runID)
	if err != nil {
		log.Printf("get run stats error: %v", err)
		http.Error(w, "failed to get run stats", http.StatusInternalServerError)
		return
	}

	resp := RunStatsResponse{
		RunID:           stats.RunID,
		TotalRequests:   stats.TotalRequests,
		ErrorCount:      stats.ErrorCount,
		ErrorRate:       analysis.Round(stats.ErrorRate, 2),
		AvgRTT:          analysis.Round(stats.AvgRTT, 2),
		P50RTT:          analysis.Round(stats.P50RTT, 2),
		P95RTT:          analysis.Round(stats.P95RTT, 2),
		P99RTT:          analysis.Round(stats.P99RTT, 2),
		MinRTT:          analysis.Round(stats.MinRTT, 2),
		MaxRTT:          analysis.Round(stats.MaxRTT, 2),
		RPS:             analysis.Round(stats.RPS, 2),
		DurationSeconds: analysis.Round(stats.DurationSeconds, 2),
	}

	writeJSON(w, http.StatusOK, resp)
}

// EndpointStatsResponse represents endpoint-level statistics in the API response.
type EndpointStatsResponse struct {
	Endpoint           string           `json:"endpoint"`
	RequestCount       int64            `json:"request_count"`
	ErrorCount         int64            `json:"error_count"`
	ErrorRate          float64          `json:"error_rate"`
	AvgRTT             float64          `json:"avg_rtt_ms"`
	P50RTT             float64          `json:"p50_rtt_ms"`
	P90RTT             float64          `json:"p90_rtt_ms"`
	P95RTT             float64          `json:"p95_rtt_ms"`
	P99RTT             float64          `json:"p99_rtt_ms"`
	MaxRTT             float64          `json:"max_rtt_ms"`
	AvgDNS             float64          `json:"avg_dns_ms"`
	AvgTCP             float64          `json:"avg_tcp_ms"`
	AvgTLS             float64          `json:"avg_tls_ms"`
	AvgTTFB            float64          `json:"avg_ttfb_ms"`
	AvgTransfer        float64          `json:"avg_transfer_ms"`
	StatusDistribution map[string]int64 `json:"status_distribution"`
}

// EndpointsResponse represents the response for endpoint analysis.
type EndpointsResponse struct {
	RunID     string                  `json:"run_id"`
	Endpoints []EndpointStatsResponse `json:"endpoints"`
}

// HandleEndpoints returns per-endpoint statistics for a run.
func (h *AnalysisHandlers) HandleEndpoints(w http.ResponseWriter, r *http.Request) {
	runID := r.URL.Query().Get("run_id")
	if runID == "" {
		http.Error(w, "run_id is required", http.StatusBadRequest)
		return
	}

	stats, err := h.endpoints.GetEndpointStats(runID)
	if err != nil {
		log.Printf("endpoint analysis error: %v", err)
		http.Error(w, "failed to analyze endpoints", http.StatusInternalServerError)
		return
	}

	resp := EndpointsResponse{RunID: runID, Endpoints: []EndpointStatsResponse{}}
	for _, s := range stats {
		resp.Endpoints = append(resp.Endpoints, EndpointStatsResponse{
			Endpoint:           s.Endpoint,
			RequestCount:       s.RequestCount,
			ErrorCount:         s.ErrorCount,
			ErrorRate:          analysis.Round(s.ErrorRate, 2),
			AvgRTT:             analysis.Round(s.AvgRTT, 2),
			P50RTT:             analysis.Round(s.P50RTT, 2),
			P90RTT:             analysis.Round(s.P90RTT, 2),
			P95RTT:             analysis.Round(s.P95RTT, 2),
			P99RTT:             analysis.Round(s.P99RTT, 2),
			MaxRTT:             analysis.Round(s.MaxRTT, 2),
			AvgDNS:             analysis.Round(s.AvgDNS, 2),
			AvgTCP:             analysis.Round(s.AvgTCP, 2),
			AvgTLS:             analysis.Round(s.AvgTLS, 2),
			AvgTTFB:            analysis.Round(s.AvgTTFB, 2),
			AvgTransfer:        analysis.Round(s.AvgTransfer, 2),
			StatusDistribution: s.StatusDistribution,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

// DiagnoseRequest represents a diagnose (baseline vs. current) request.
type DiagnoseRequest struct {
	BaselineRunID string               `json:"baseline_run_id"`
	CurrentRunID  string               `json:"current_run_id"`
	Thresholds    *analysis.Thresholds `json:"thresholds,omitempty"`
}

// TimingChangeResponse represents the largest timing phase change for an endpoint.
type TimingChangeResponse struct {
	Phase   string  `json:"phase"`
	DeltaMs float64 `json:"delta_ms"`
}

// ExplanationResponse represents one structured performance-regression explanation.
type ExplanationResponse struct {
	Endpoint            string               `json:"endpoint"`
	P95BaselineMs       float64              `json:"p95_baseline_ms"`
	P95CurrentMs        float64              `json:"p95_current_ms"`
	P95ChangePercent    float64              `json:"p95_change_percent"`
	LargestTimingChange TimingChangeResponse `json:"largest_timing_change"`
	AffectedPods        []string             `json:"affected_pods"`
	ErrorRateBaseline   float64              `json:"error_rate_baseline"`
	ErrorRateCurrent    float64              `json:"error_rate_current"`
	Severity            string               `json:"severity"`
}

// DiagnoseResponse represents the response for the diagnose endpoint:
// machine-checkable regression findings plus a structured explanation per
// regressed endpoint.
type DiagnoseResponse struct {
	Regressions  []analysis.RegressionFinding `json:"regressions"`
	Explanations []ExplanationResponse        `json:"explanations"`
}

// HandleDiagnose compares two runs at the endpoint level and returns
// regression findings plus a structured, deterministic explanation of what
// changed for each regressed endpoint (which timing phase moved the most,
// which pods were affected, how the error rate changed).
func (h *AnalysisHandlers) HandleDiagnose(w http.ResponseWriter, r *http.Request) {
	var req DiagnoseRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxAnalysisRequestBody)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.BaselineRunID == "" || req.CurrentRunID == "" {
		http.Error(w, "baseline_run_id and current_run_id are required", http.StatusBadRequest)
		return
	}

	thresholds := analysis.DefaultThresholds()
	if req.Thresholds != nil {
		thresholds = *req.Thresholds
	}

	regressions, err := analysis.DetectEndpointRegressions(h.endpoints, req.BaselineRunID, req.CurrentRunID, thresholds)
	if err != nil {
		log.Printf("diagnose error: %v", err)
		http.Error(w, "diagnose failed", http.StatusInternalServerError)
		return
	}

	explanations, err := analysis.Explain(h.db, req.BaselineRunID, req.CurrentRunID, thresholds)
	if err != nil {
		log.Printf("diagnose error: %v", err)
		http.Error(w, "diagnose failed", http.StatusInternalServerError)
		return
	}

	resp := DiagnoseResponse{
		Regressions:  regressions,
		Explanations: []ExplanationResponse{},
	}
	if resp.Regressions == nil {
		resp.Regressions = []analysis.RegressionFinding{}
	}
	for _, e := range explanations {
		resp.Explanations = append(resp.Explanations, ExplanationResponse{
			Endpoint:         e.Endpoint,
			P95BaselineMs:    e.P95BaselineMs,
			P95CurrentMs:     e.P95CurrentMs,
			P95ChangePercent: e.P95ChangePercent,
			LargestTimingChange: TimingChangeResponse{
				Phase:   e.LargestTimingChange.Phase,
				DeltaMs: e.LargestTimingChange.DeltaMs,
			},
			AffectedPods:      e.AffectedPods,
			ErrorRateBaseline: e.ErrorRateBaseline,
			ErrorRateCurrent:  e.ErrorRateCurrent,
			Severity:          e.Severity,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

// GateRequest represents a performance gate evaluation request. Policy is
// the same schema `duckload gate --policy` reads from a file (see
// analysis/policy), embedded directly as JSON — policy.Parse accepts it
// unchanged since JSON is valid YAML. CurrentRunID and BaselineRunID are
// resolved against this sidecar's own database; evaluating a baseline
// held in a separate file is a CLI-only capability (see `duckload gate
// --baseline <file>`), since an HTTP request has only one database to
// query. Omit BaselineRunID to run in Performance-Budget-only mode.
type GateRequest struct {
	CurrentRunID  string          `json:"current_run_id"`
	BaselineRunID string          `json:"baseline_run_id,omitempty"`
	Policy        json.RawMessage `json:"policy"`
}

// HandleGate evaluates the performance gate for one run (and, optionally,
// a baseline comparison) against an embedded policy, returning the same
// gate.Result shape `duckload gate --format json` prints. This is the same
// evaluation engine, not a reimplementation: see analysis/gate.
func (h *AnalysisHandlers) HandleGate(w http.ResponseWriter, r *http.Request) {
	var req GateRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxAnalysisRequestBody)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.CurrentRunID == "" {
		http.Error(w, "current_run_id is required", http.StatusBadRequest)
		return
	}
	if len(req.Policy) == 0 {
		http.Error(w, "policy is required", http.StatusBadRequest)
		return
	}

	pol, err := policy.Parse(req.Policy)
	if err != nil {
		http.Error(w, "invalid policy: "+err.Error(), http.StatusBadRequest)
		return
	}

	current, err := h.endpoints.GetEndpointStats(req.CurrentRunID)
	if err != nil {
		log.Printf("gate error: %v", err)
		http.Error(w, "failed to analyze current run", http.StatusInternalServerError)
		return
	}

	var baseline []analysis.EndpointStats
	if req.BaselineRunID != "" {
		baseline, err = h.endpoints.GetEndpointStats(req.BaselineRunID)
		if err != nil {
			log.Printf("gate error: %v", err)
			http.Error(w, "failed to analyze baseline run", http.StatusInternalServerError)
			return
		}
	}

	result := gate.Evaluate(current, baseline, pol)
	writeJSON(w, http.StatusOK, result)
}

// TrendResponse represents historical trend response
type TrendResponse struct {
	Runs []RunStatsResponse `json:"runs"`
}

// HandleTrend returns historical trend data
func (h *AnalysisHandlers) HandleTrend(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 10
	if limitStr != "" {
		if _, err := json.Number(limitStr).Int64(); err == nil {
			if l, _ := json.Number(limitStr).Int64(); l > 0 && l <= 100 {
				limit = int(l)
			}
		}
	}

	trend, err := h.comparator.GetHistoricalTrend(limit)
	if err != nil {
		log.Printf("get trend error: %v", err)
		http.Error(w, "failed to get trend", http.StatusInternalServerError)
		return
	}

	resp := TrendResponse{Runs: []RunStatsResponse{}}
	for _, stats := range trend {
		resp.Runs = append(resp.Runs, RunStatsResponse{
			RunID:           stats.RunID,
			TotalRequests:   stats.TotalRequests,
			ErrorCount:      stats.ErrorCount,
			ErrorRate:       analysis.Round(stats.ErrorRate, 2),
			AvgRTT:          analysis.Round(stats.AvgRTT, 2),
			P50RTT:          analysis.Round(stats.P50RTT, 2),
			P95RTT:          analysis.Round(stats.P95RTT, 2),
			P99RTT:          analysis.Round(stats.P99RTT, 2),
			MinRTT:          analysis.Round(stats.MinRTT, 2),
			MaxRTT:          analysis.Round(stats.MaxRTT, 2),
			RPS:             analysis.Round(stats.RPS, 2),
			DurationSeconds: analysis.Round(stats.DurationSeconds, 2),
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

// BaselineRequest represents a baseline calculation request
type BaselineRequest struct {
	RunIDs []string `json:"run_ids"`
}

// HandleCalculateBaseline calculates baseline from multiple runs
func (h *AnalysisHandlers) HandleCalculateBaseline(w http.ResponseWriter, r *http.Request) {
	var req BaselineRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxAnalysisRequestBody)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.RunIDs) == 0 {
		http.Error(w, "run_ids are required", http.StatusBadRequest)
		return
	}

	baseline, err := h.comparator.CalculateBaseline(req.RunIDs)
	if err != nil {
		log.Printf("calculate baseline error: %v", err)
		http.Error(w, "failed to calculate baseline", http.StatusInternalServerError)
		return
	}

	resp := RunStatsResponse{
		RunID:         baseline.RunID,
		TotalRequests: baseline.TotalRequests,
		ErrorCount:    baseline.ErrorCount,
		ErrorRate:     analysis.Round(baseline.ErrorRate, 2),
		AvgRTT:        analysis.Round(baseline.AvgRTT, 2),
		P50RTT:        analysis.Round(baseline.P50RTT, 2),
		P95RTT:        analysis.Round(baseline.P95RTT, 2),
		P99RTT:        analysis.Round(baseline.P99RTT, 2),
		RPS:           analysis.Round(baseline.RPS, 2),
	}

	writeJSON(w, http.StatusOK, resp)
}
