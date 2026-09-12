package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"

	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis/fixtures"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/storage"
)

// buildResultFile writes a scenario's fixture events to a real .duckdb file
// on disk, the way the sidecar would, so CLI commands can be exercised
// against a file path the same way a user would invoke them.
func buildResultFile(t *testing.T, scenario fixtures.Scenario) string {
	t.Helper()
	dbFile := filepath.Join(t.TempDir(), "result.duckdb")

	s, err := storage.NewDuckDBStorage(dbFile)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	s.AddEvents(fixtures.Generate(scenario))
	if _, err := s.Flush(); err != nil {
		t.Fatalf("failed to flush: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("failed to close: %v", err)
	}
	return dbFile
}

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	fnErr := fn()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("failed to read captured stdout: %v", err)
	}
	return buf.String(), fnErr
}

func TestRunSummary(t *testing.T) {
	dbFile := buildResultFile(t, fixtures.Healthy)

	out, err := captureStdout(t, func() error {
		return runSummary([]string{dbFile, "--run-id", fixtures.CurrentRunID})
	})
	if err != nil {
		t.Fatalf("runSummary failed: %v", err)
	}

	var stats map[string]any
	if err := json.Unmarshal([]byte(out), &stats); err != nil {
		t.Fatalf("failed to decode output: %v\noutput: %s", err, out)
	}
	if stats["TotalRequests"].(float64) == 0 {
		t.Error("expected non-zero TotalRequests")
	}
}

func TestRunEndpoints(t *testing.T) {
	dbFile := buildResultFile(t, fixtures.Healthy)

	out, err := captureStdout(t, func() error {
		return runEndpoints([]string{"--run-id", fixtures.CurrentRunID, dbFile})
	})
	if err != nil {
		t.Fatalf("runEndpoints failed: %v", err)
	}

	var stats []map[string]any
	if err := json.Unmarshal([]byte(out), &stats); err != nil {
		t.Fatalf("failed to decode output: %v\noutput: %s", err, out)
	}
	if len(stats) != 2 {
		t.Errorf("expected 2 endpoints, got %d", len(stats))
	}
}

func TestRunCompareAndDiagnose(t *testing.T) {
	dbFile := buildResultFile(t, fixtures.LatencyRegression)

	compareArgs := []string{dbFile, "--baseline", fixtures.BaselineRunID, "--current", fixtures.CurrentRunID}
	out, err := captureStdout(t, func() error { return runCompare(compareArgs) })
	if err != nil {
		t.Fatalf("runCompare failed: %v", err)
	}
	if out == "" {
		t.Error("expected non-empty compare output")
	}

	diagnoseArgs := []string{"--baseline", fixtures.BaselineRunID, "--current", fixtures.CurrentRunID, dbFile}
	out, err = captureStdout(t, func() error { return runDiagnose(diagnoseArgs) })
	if err != nil {
		t.Fatalf("runDiagnose failed: %v", err)
	}

	var diag struct {
		Explanations []map[string]any `json:"explanations"`
	}
	if err := json.Unmarshal([]byte(out), &diag); err != nil {
		t.Fatalf("failed to decode diagnose output: %v\noutput: %s", err, out)
	}
	if len(diag.Explanations) == 0 {
		t.Error("expected at least one explanation for a regressed fixture")
	}
}

func TestRunCheckAnalysis(t *testing.T) {
	if err := runCheckAnalysis(nil); err != nil {
		t.Fatalf("runCheckAnalysis failed: %v", err)
	}
}

func TestRunSummary_MissingArgs(t *testing.T) {
	if err := runSummary(nil); err == nil {
		t.Error("expected an error when no arguments are given")
	}
}
