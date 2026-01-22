package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/example/duckdb-sidecar/handlers"
	"github.com/example/duckdb-sidecar/realtime"
	"github.com/example/duckdb-sidecar/storage"
	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/gorilla/mux"
)

func main() {
	runID := getEnvOrDefault("RUN_ID", "local-run")
	podName := getEnvOrDefault("POD_NAME", "pod-local")
	dataDir := getEnvOrDefault("DATA_DIR", "/data")
	port := getEnvOrDefault("PORT", "8081")

	// Create data directory
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("failed to create data dir: %v", err)
	}

	dbFile := filepath.Join(dataDir, fmt.Sprintf("%s-%s.duckdb", runID, podName))

	// Initialize storage
	store, err := storage.NewDuckDBStorage(dbFile)
	if err != nil {
		log.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	// Initialize S3 uploader (optional)
	uploader, err := storage.NewS3UploaderFromEnv()
	if err != nil {
		log.Printf("warning: S3 uploader initialization failed: %v", err)
	}

	// Create handlers
	h := handlers.NewHandlers(store, uploader, runID)

	// Create analysis handlers
	analysisHandlers := handlers.NewAnalysisHandlers(store.GetDB())

	// Create realtime hub for WebSocket streaming
	hub := realtime.NewHub()
	go hub.Run()

	// Set hub on handlers for realtime updates
	h.SetRealtimeHub(hub)

	// Background flusher
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		for range ticker.C {
			if count, err := store.Flush(); err != nil {
				log.Printf("background flush error: %v", err)
			} else if count > 0 {
				log.Printf("background flush: %d events", count)
			}
			// Update buffer size in hub
			hub.UpdateBufferSize(store.BufferSize())
		}
	}()

	// Setup router
	r := mux.NewRouter()

	// API v1 routes
	api := r.PathPrefix("/api/v1").Subrouter()

	// Ingest endpoints
	api.HandleFunc("/ingest", h.HandleIngest).Methods("POST")
	api.HandleFunc("/ingest/batch", h.HandleIngestBatch).Methods("POST")

	// Flush endpoints
	api.HandleFunc("/flush", h.HandleFlush).Methods("POST")
	api.HandleFunc("/flush-upload", h.HandleFlushUpload).Methods("POST")

	// Data endpoints
	api.HandleFunc("/download", h.HandleDownload).Methods("GET")
	api.HandleFunc("/stats", h.HandleStats).Methods("GET")

	// Health check
	api.HandleFunc("/health", h.HandleHealth).Methods("GET")
	r.HandleFunc("/health", h.HandleHealth).Methods("GET")

	// Analysis endpoints
	api.HandleFunc("/analysis/compare", analysisHandlers.HandleCompare).Methods("POST")
	api.HandleFunc("/analysis/run-stats", analysisHandlers.HandleRunStats).Methods("GET")
	api.HandleFunc("/analysis/trend", analysisHandlers.HandleTrend).Methods("GET")
	api.HandleFunc("/analysis/baseline", analysisHandlers.HandleCalculateBaseline).Methods("POST")

	// WebSocket endpoint for realtime monitoring
	r.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		realtime.ServeWs(hub, w, r)
	})

	// Server
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("sidecar listening :%s, db=%s, s3=%v", port, dbFile, uploader != nil)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
