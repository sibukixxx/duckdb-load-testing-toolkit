package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/models"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/realtime"
)

// Handlers contains HTTP handlers for the sidecar API
type Handlers struct {
	storage  Storage
	uploader Uploader
	runID    string
	hub      *realtime.Hub
}

// NewHandlers creates a new Handlers instance
func NewHandlers(storage Storage, uploader Uploader, runID string) *Handlers {
	return &Handlers{
		storage:  storage,
		uploader: uploader,
		runID:    runID,
	}
}

// SetRealtimeHub sets the realtime hub for live updates
func (h *Handlers) SetRealtimeHub(hub *realtime.Hub) {
	h.hub = hub
}

// IngestRequest represents the request body for batch ingest
type IngestRequest struct {
	Events []models.Event `json:"events"`
}

// IngestResponse represents the response for ingest operations
type IngestResponse struct {
	Accepted int    `json:"accepted"`
	Message  string `json:"message,omitempty"`
}

// HandleIngest handles single event ingestion
func (h *Handlers) HandleIngest(w http.ResponseWriter, r *http.Request) {
	var ev models.Event
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}

	h.storage.AddEvent(ev)

	// Record event for realtime monitoring
	if h.hub != nil {
		isError := ev.Status >= 400 || ev.ErrorCode != ""
		h.hub.RecordEvent(ev.RTT, isError, ev.VU)
		h.hub.BroadcastEvent(ev)
	}

	w.WriteHeader(http.StatusAccepted)
}

// HandleIngestBatch handles batch event ingestion
func (h *Handlers) HandleIngestBatch(w http.ResponseWriter, r *http.Request) {
	var req IngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}

	if len(req.Events) == 0 {
		writeJSON(w, http.StatusBadRequest, IngestResponse{
			Accepted: 0,
			Message:  "no events provided",
		})
		return
	}

	h.storage.AddEvents(req.Events)

	// Record events for realtime monitoring
	if h.hub != nil {
		for _, ev := range req.Events {
			isError := ev.Status >= 400 || ev.ErrorCode != ""
			h.hub.RecordEvent(ev.RTT, isError, ev.VU)
		}
		h.hub.UpdateBufferSize(h.storage.BufferSize())
	}

	writeJSON(w, http.StatusAccepted, IngestResponse{
		Accepted: len(req.Events),
	})
}

// FlushResponse represents the response for flush operations
type FlushResponse struct {
	Flushed  int    `json:"flushed"`
	Uploaded bool   `json:"uploaded"`
	Key      string `json:"key,omitempty"`
	Message  string `json:"message,omitempty"`
}

// HandleFlush flushes buffer to DuckDB without uploading
func (h *Handlers) HandleFlush(w http.ResponseWriter, r *http.Request) {
	count, err := h.storage.Flush()
	if err != nil {
		log.Printf("flush error: %v", err)
		http.Error(w, "flush failed", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, FlushResponse{
		Flushed: count,
	})
}

// HandleFlushUpload flushes buffer and uploads to S3
func (h *Handlers) HandleFlushUpload(w http.ResponseWriter, r *http.Request) {
	count, err := h.storage.Flush()
	if err != nil {
		log.Printf("flush error: %v", err)
		http.Error(w, "flush failed", http.StatusInternalServerError)
		return
	}

	if h.uploader == nil || !h.uploader.IsConfigured() {
		writeJSON(w, http.StatusOK, FlushResponse{
			Flushed:  count,
			Uploaded: false,
			Message:  "no S3 configured; flush done",
		})
		return
	}

	key := fmt.Sprintf("runs/%s/%s", h.runID, filepath.Base(h.storage.GetDBFile()))
	if err := h.uploader.UploadFile(r.Context(), key, h.storage.GetDBFile()); err != nil {
		log.Printf("upload error: %v", err)
		http.Error(w, "upload failed", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, FlushResponse{
		Flushed:  count,
		Uploaded: true,
		Key:      key,
	})
}

// HandleDownload serves the DuckDB file for download
func (h *Handlers) HandleDownload(w http.ResponseWriter, r *http.Request) {
	f, err := os.Open(h.storage.GetDBFile())
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(h.storage.GetDBFile())))
	http.ServeContent(w, r, filepath.Base(h.storage.GetDBFile()), time.Now(), f)
}

// StatsResponse represents the response for stats endpoint
type StatsResponse struct {
	TotalRequests int64   `json:"total_requests"`
	ErrorCount    int64   `json:"error_count"`
	ErrorRate     float64 `json:"error_rate"`
	AvgRTT        float64 `json:"avg_rtt_ms"`
	P50RTT        float64 `json:"p50_rtt_ms"`
	P95RTT        float64 `json:"p95_rtt_ms"`
	P99RTT        float64 `json:"p99_rtt_ms"`
	RPS           float64 `json:"rps"`
	DurationSec   float64 `json:"duration_sec"`
	BufferSize    int     `json:"buffer_size"`
}

// HandleStats returns current metrics statistics
func (h *Handlers) HandleStats(w http.ResponseWriter, r *http.Request) {
	// Flush first to get latest data
	h.storage.Flush()

	stats, err := h.storage.GetStats()
	if err != nil {
		log.Printf("stats error: %v", err)
		http.Error(w, "failed to get stats", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, StatsResponse{
		TotalRequests: stats.TotalRequests,
		ErrorCount:    stats.ErrorCount,
		ErrorRate:     stats.ErrorRate(),
		AvgRTT:        stats.AvgRTT,
		P50RTT:        stats.P50RTT,
		P95RTT:        stats.P95RTT,
		P99RTT:        stats.P99RTT,
		RPS:           stats.RPS(),
		DurationSec:   stats.DurationSeconds(),
		BufferSize:    h.storage.BufferSize(),
	})
}

// HealthResponse represents the response for health check
type HealthResponse struct {
	Status     string `json:"status"`
	BufferSize int    `json:"buffer_size"`
	DBFile     string `json:"db_file"`
	S3Enabled  bool   `json:"s3_enabled"`
}

// HandleHealth returns health status
func (h *Handlers) HandleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{
		Status:     "ok",
		BufferSize: h.storage.BufferSize(),
		DBFile:     h.storage.GetDBFile(),
		S3Enabled:  h.uploader != nil && h.uploader.IsConfigured(),
	})
}

// writeJSON writes a JSON response
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
