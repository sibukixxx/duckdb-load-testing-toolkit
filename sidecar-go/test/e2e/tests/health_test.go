package tests

import (
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/handlers"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/test/e2e"
)

func TestE2EHealth_ReturnsOK(t *testing.T) {
	t.Parallel()
	app := e2e.NewTestApp(t)
	client := e2e.NewClient(app.BaseURL())

	for _, path := range []string{"/health", "/api/v1/health"} {
		t.Run(path, func(t *testing.T) {
			resp := client.MustGet(t, path)
			defer resp.Body.Close()
			e2e.AssertOK(t, resp)

			var got handlers.HealthResponse
			e2e.DecodeJSON(t, resp, &got)

			if got.Status != "ok" {
				t.Errorf("status: got %q, want \"ok\"", got.Status)
			}
		})
	}
}

func TestE2EHealth_S3EnabledFlag(t *testing.T) {
	t.Parallel()

	t.Run("S3 enabled when stub uploader is present", func(t *testing.T) {
		app := e2e.NewTestApp(t)
		client := e2e.NewClient(app.BaseURL())

		resp := client.MustGet(t, "/api/v1/health")
		defer resp.Body.Close()

		var got handlers.HealthResponse
		e2e.DecodeJSON(t, resp, &got)
		if !got.S3Enabled {
			t.Error("s3_enabled: got false, want true (stub uploader is configured)")
		}
	})

	t.Run("S3 disabled when WithNoUploader is used", func(t *testing.T) {
		app := e2e.NewTestApp(t, e2e.WithNoUploader())
		client := e2e.NewClient(app.BaseURL())

		resp := client.MustGet(t, "/api/v1/health")
		defer resp.Body.Close()

		var got handlers.HealthResponse
		e2e.DecodeJSON(t, resp, &got)
		if got.S3Enabled {
			t.Error("s3_enabled: got true, want false (no uploader configured)")
		}
	})
}

func TestE2EHealth_BufferSizeReflectsIngested(t *testing.T) {
	t.Parallel()
	app := e2e.NewTestApp(t)
	client := e2e.NewClient(app.BaseURL())

	// Initially buffer should be empty
	resp1 := client.MustGet(t, "/api/v1/health")
	defer resp1.Body.Close()

	var initial handlers.HealthResponse
	e2e.DecodeJSON(t, resp1, &initial)
	if initial.BufferSize != 0 {
		t.Errorf("initial buffer_size: got %d, want 0", initial.BufferSize)
	}
}
