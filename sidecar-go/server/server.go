package server

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/handlers"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/realtime"
)

// NewRouter builds the application router shared by the production server and E2E tests.
func NewRouter(h *handlers.Handlers, ah *handlers.AnalysisHandlers, hub *realtime.Hub) http.Handler {
	r := mux.NewRouter()
	api := r.PathPrefix("/api/v1").Subrouter()

	api.HandleFunc("/ingest", h.HandleIngest).Methods("POST")
	api.HandleFunc("/ingest/batch", h.HandleIngestBatch).Methods("POST")

	api.HandleFunc("/flush", h.HandleFlush).Methods("POST")
	api.HandleFunc("/flush-upload", h.HandleFlushUpload).Methods("POST")

	api.HandleFunc("/download", h.HandleDownload).Methods("GET")
	api.HandleFunc("/stats", h.HandleStats).Methods("GET")

	api.HandleFunc("/health", h.HandleHealth).Methods("GET")
	r.HandleFunc("/health", h.HandleHealth).Methods("GET")

	api.HandleFunc("/analysis/compare", ah.HandleCompare).Methods("POST")
	api.HandleFunc("/analysis/run-stats", ah.HandleRunStats).Methods("GET")
	api.HandleFunc("/analysis/trend", ah.HandleTrend).Methods("GET")
	api.HandleFunc("/analysis/baseline", ah.HandleCalculateBaseline).Methods("POST")

	r.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		realtime.ServeWs(hub, w, r)
	})

	return r
}
