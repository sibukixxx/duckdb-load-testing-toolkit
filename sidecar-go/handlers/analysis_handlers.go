package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis"
)

// AnalysisHandlers contains handlers for analysis endpoints
type AnalysisHandlers struct {
	db         *sql.DB
	comparator *analysis.Comparator
}

// NewAnalysisHandlers creates new analysis handlers
func NewAnalysisHandlers(db *sql.DB) *AnalysisHandlers {
	return &AnalysisHandlers{
		db:         db,
		comparator: analysis.NewComparator(db, analysis.DefaultThresholds()),
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
