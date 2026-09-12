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
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis/gate"
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

// buildGateResultFile writes a gate fixture's events to a real .duckdb
// file, optionally filtered to a single run_id (empty keeps every run_id
// the scenario generates in one file, e.g. for same-file "current"/
// "baseline" usage).
func buildGateResultFile(t *testing.T, scenario fixtures.GateScenario, onlyRunID string) string {
	t.Helper()
	dbFile := filepath.Join(t.TempDir(), "result.duckdb")

	s, err := storage.NewDuckDBStorage(dbFile)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	events := fixtures.GenerateGate(scenario)
	if onlyRunID != "" {
		filtered := events[:0:0]
		for _, e := range events {
			if e.RunID == onlyRunID {
				filtered = append(filtered, e)
			}
		}
		events = filtered
	}
	s.AddEvents(events)
	if _, err := s.Flush(); err != nil {
		t.Fatalf("failed to flush: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("failed to close: %v", err)
	}
	return dbFile
}

func writeGatePolicy(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.yml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write policy file: %v", err)
	}
	return path
}

const standardGatePolicyYAML = `
version: 1
defaults:
  min_samples: 30
  p95:
    max_ms: 500
    warn_max_ms: 400
    max_regression_percent: 20
    warn_regression_percent: 10
  error_rate:
    max_absolute: 5
    max_absolute_increase: 5
`

func TestRunGate_SameFileExplicitRunIDs_Fail(t *testing.T) {
	dbFile := buildGateResultFile(t, fixtures.GateRelativeRegressionFail, "")
	policyPath := writeGatePolicy(t, standardGatePolicyYAML)

	out, code := captureStdoutInt(t, func() int {
		return runGate([]string{
			"--current", dbFile, "--run-id", fixtures.CurrentRunID,
			"--baseline", dbFile, "--baseline-run-id", fixtures.BaselineRunID,
			"--policy", policyPath, "--format", "json",
		})
	})
	if code != gate.ExitPolicyFailure {
		t.Fatalf("expected exit code %d, got %d (output: %s)", gate.ExitPolicyFailure, code, out)
	}

	var res gate.Result
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("failed to decode gate JSON output: %v\noutput: %s", err, out)
	}
	if res.Status != gate.StatusFail {
		t.Errorf("expected status FAIL, got %s", res.Status)
	}
}

func TestRunGate_SeparateFilesAutoDetectRunID(t *testing.T) {
	currentFile := buildGateResultFile(t, fixtures.GateRelativeRegressionFail, fixtures.CurrentRunID)
	baselineFile := buildGateResultFile(t, fixtures.GateRelativeRegressionFail, fixtures.BaselineRunID)
	policyPath := writeGatePolicy(t, standardGatePolicyYAML)

	_, code := captureStdoutInt(t, func() int {
		return runGate([]string{
			"--current", currentFile, "--baseline", baselineFile, "--policy", policyPath,
		})
	})
	if code != gate.ExitPolicyFailure {
		t.Fatalf("expected exit code %d for a real regression, got %d", gate.ExitPolicyFailure, code)
	}
}

func TestRunGate_BudgetOnlyMode_Pass(t *testing.T) {
	dbFile := buildGateResultFile(t, fixtures.GatePass, fixtures.CurrentRunID)
	policyPath := writeGatePolicy(t, standardGatePolicyYAML)

	_, code := captureStdoutInt(t, func() int {
		return runGate([]string{"--current", dbFile, "--policy", policyPath})
	})
	if code != gate.ExitPass {
		t.Fatalf("expected exit code %d, got %d", gate.ExitPass, code)
	}
}

func TestRunGate_ToolErrors(t *testing.T) {
	dbFile := buildGateResultFile(t, fixtures.GatePass, fixtures.CurrentRunID)
	policyPath := writeGatePolicy(t, standardGatePolicyYAML)

	tests := []struct {
		name string
		args []string
	}{
		{"missing current/policy", []string{}},
		{"missing file", []string{"--current", "/does/not/exist.duckdb", "--policy", policyPath}},
		{"bad format flag", []string{"--current", dbFile, "--policy", policyPath, "--format", "xml"}},
		{"ambiguous run_id", []string{"--current", buildGateResultFile(t, fixtures.GatePass, ""), "--policy", policyPath}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, code := captureStdoutInt(t, func() int { return runGate(tt.args) })
			if code != gate.ExitToolError {
				t.Errorf("expected exit code %d, got %d", gate.ExitToolError, code)
			}
		})
	}
}

func TestRunGate_WarnExitCode(t *testing.T) {
	dbFile := buildGateResultFile(t, fixtures.GateRelativeRegressionFail, "")
	// A very loose fail threshold but a warn threshold the fixture still
	// exceeds, so the gate lands on WARN rather than FAIL or PASS.
	policyPath := writeGatePolicy(t, `
version: 1
defaults:
  min_samples: 30
  p95:
    max_regression_percent: 200
    warn_regression_percent: 20
`)

	baseArgs := []string{
		"--current", dbFile, "--run-id", fixtures.CurrentRunID,
		"--baseline", dbFile, "--baseline-run-id", fixtures.BaselineRunID,
		"--policy", policyPath,
	}

	_, code := captureStdoutInt(t, func() int { return runGate(baseArgs) })
	if code != gate.ExitPass {
		t.Errorf("expected default warn exit code %d, got %d", gate.ExitPass, code)
	}

	_, code = captureStdoutInt(t, func() int {
		return runGate(append(append([]string{}, baseArgs...), "--warn-exit-code", "1"))
	})
	if code != gate.ExitPolicyFailure {
		t.Errorf("expected --warn-exit-code 1 to produce exit code %d, got %d", gate.ExitPolicyFailure, code)
	}
}

func captureStdoutInt(t *testing.T, fn func() int) (string, int) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	code := fn()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("failed to read captured stdout: %v", err)
	}
	return buf.String(), code
}
