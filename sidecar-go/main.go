package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/handlers"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/realtime"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/server"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/storage"
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
	s3Uploader, err := storage.NewS3UploaderFromEnv()
	if err != nil {
		log.Printf("warning: S3 uploader initialization failed: %v", err)
	}
	// Avoid a typed-nil interface value when S3 is not configured.
	var uploader handlers.Uploader
	if s3Uploader != nil {
		uploader = s3Uploader
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

	r := server.NewRouter(h, analysisHandlers, hub)

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
