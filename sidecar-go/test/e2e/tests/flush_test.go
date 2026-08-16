package tests

import (
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/handlers"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/models"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/test/e2e"
)

func TestE2EFlush_EmptyBuffer(t *testing.T) {
	t.Parallel()
	app := e2e.NewTestApp(t)
	client := e2e.NewClient(app.BaseURL())

	resp := client.MustPost(t, "/api/v1/flush")
	defer resp.Body.Close()
	e2e.AssertOK(t, resp)

	var got handlers.FlushResponse
	e2e.DecodeJSON(t, resp, &got)
	if got.Flushed != 0 {
		t.Errorf("flushed: got %d, want 0", got.Flushed)
	}
}

func TestE2EFlush_WithEvents(t *testing.T) {
	t.Parallel()
	app := e2e.NewTestApp(t)
	client := e2e.NewClient(app.BaseURL())

	// Ingest 3 events
	for i := range 3 {
		ev := models.Event{Ts: int64(i+1) * 1000, Status: 200, RTT: float64(i+1) * 10}
		r := client.MustPostJSON(t, "/api/v1/ingest", ev)
		r.Body.Close()
	}

	resp := client.MustPost(t, "/api/v1/flush")
	defer resp.Body.Close()
	e2e.AssertOK(t, resp)

	var got handlers.FlushResponse
	e2e.DecodeJSON(t, resp, &got)
	if got.Flushed != 3 {
		t.Errorf("flushed: got %d, want 3", got.Flushed)
	}
}

func TestE2EFlushUpload_WithStubUploader(t *testing.T) {
	t.Parallel()
	// Default TestApp includes a StubUploader (IsConfigured=true)
	app := e2e.NewTestApp(t)
	client := e2e.NewClient(app.BaseURL())

	// Ingest one event so there is something to flush
	ev := models.Event{Ts: 1000, Status: 200, RTT: 50.0}
	ingestResp := client.MustPostJSON(t, "/api/v1/ingest", ev)
	ingestResp.Body.Close()

	resp := client.MustPost(t, "/api/v1/flush-upload")
	defer resp.Body.Close()
	e2e.AssertOK(t, resp)

	var got handlers.FlushResponse
	e2e.DecodeJSON(t, resp, &got)

	if got.Flushed != 1 {
		t.Errorf("flushed: got %d, want 1", got.Flushed)
	}
	if !got.Uploaded {
		t.Error("uploaded: got false, want true")
	}
	if got.Key == "" {
		t.Error("key: expected non-empty S3 key")
	}

	// Verify the stub recorded exactly one upload
	uploads := app.Uploader.Uploads()
	if len(uploads) != 1 {
		t.Fatalf("stub uploads: got %d, want 1", len(uploads))
	}
	if uploads[0].Key != got.Key {
		t.Errorf("upload key: got %q, want %q", uploads[0].Key, got.Key)
	}
}

func TestE2EFlushUpload_WithoutUploader(t *testing.T) {
	t.Parallel()
	// WithNoUploader disables S3 — flush-upload should succeed but not upload
	app := e2e.NewTestApp(t, e2e.WithNoUploader())
	client := e2e.NewClient(app.BaseURL())

	ev := models.Event{Ts: 1000, Status: 200, RTT: 10.0}
	ingestResp := client.MustPostJSON(t, "/api/v1/ingest", ev)
	ingestResp.Body.Close()

	resp := client.MustPost(t, "/api/v1/flush-upload")
	defer resp.Body.Close()
	e2e.AssertOK(t, resp)

	var got handlers.FlushResponse
	e2e.DecodeJSON(t, resp, &got)

	if got.Uploaded {
		t.Error("uploaded: got true, want false (no S3 configured)")
	}
	if got.Message == "" {
		t.Error("message: expected explanation when S3 not configured")
	}

	// Stub uploader should not have received any calls
	if len(app.Uploader.Uploads()) != 0 {
		t.Errorf("stub uploads: got %d, want 0", len(app.Uploader.Uploads()))
	}
}
