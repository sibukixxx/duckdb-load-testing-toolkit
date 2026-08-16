// Package e2e provides the E2E test infrastructure for the sidecar HTTP API.
// Tests bring up a real httptest.Server backed by a temp DuckDB file.
package e2e

import (
	"net/http/httptest"
	"path/filepath"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/handlers"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/realtime"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/server"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/storage"
)

// TestApp is a fully initialized sidecar HTTP server for E2E testing.
// Each test gets its own isolated instance via NewTestApp.
type TestApp struct {
	Server   *httptest.Server
	Storage  *storage.DuckDBStorage
	Uploader *StubUploader
	t        *testing.T
}

// TestAppOption configures a TestApp before it starts.
type TestAppOption func(*testAppConfig)

type testAppConfig struct {
	uploader   handlers.Uploader
	noUploader bool
	runID      string
}

// WithRunID overrides the default run ID ("e2e-test-run").
func WithRunID(id string) TestAppOption {
	return func(c *testAppConfig) { c.runID = id }
}

// WithUploader replaces the default StubUploader with a custom implementation.
func WithUploader(u handlers.Uploader) TestAppOption {
	return func(c *testAppConfig) { c.uploader = u }
}

// WithNoUploader configures the server without an S3 uploader (S3Enabled=false).
func WithNoUploader() TestAppOption {
	return func(c *testAppConfig) { c.noUploader = true }
}

// NewTestApp creates a real httptest.Server backed by a temporary DuckDB file.
// The server and all resources are cleaned up automatically when the test ends.
func NewTestApp(t *testing.T, opts ...TestAppOption) *TestApp {
	t.Helper()

	cfg := &testAppConfig{
		runID: "e2e-test-run",
	}
	for _, opt := range opts {
		opt(cfg)
	}

	dbFile := filepath.Join(t.TempDir(), "e2e-test.duckdb")
	store, err := storage.NewDuckDBStorage(dbFile)
	if err != nil {
		t.Fatalf("e2e: create DuckDB storage: %v", err)
	}

	hub := realtime.NewHub()
	go hub.Run()

	stubUploader := NewStubUploader()
	var uploader handlers.Uploader
	switch {
	case cfg.noUploader:
		uploader = nil
	case cfg.uploader != nil:
		uploader = cfg.uploader
	default:
		uploader = stubUploader
	}

	h := handlers.NewHandlers(store, uploader, cfg.runID)
	h.SetRealtimeHub(hub)

	ah := handlers.NewAnalysisHandlers(store.GetDB())
	router := server.NewRouter(h, ah, hub)
	srv := httptest.NewServer(router)

	app := &TestApp{
		Server:   srv,
		Storage:  store,
		Uploader: stubUploader,
		t:        t,
	}

	t.Cleanup(func() {
		srv.Close()
		store.Close()
	})

	return app
}

// BaseURL returns the base URL of the test server (e.g. "http://127.0.0.1:PORT").
func (a *TestApp) BaseURL() string {
	return a.Server.URL
}
