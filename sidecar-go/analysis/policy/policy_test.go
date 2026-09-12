package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func writePolicy(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.yml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write policy file: %v", err)
	}
	return path
}

func TestLoad_ValidPolicy(t *testing.T) {
	path := writePolicy(t, `
version: 1
unknown_treatment: FAIL
defaults:
  min_samples: 50
  p95:
    max_ms: 500
    warn_max_ms: 400
    max_regression_percent: 20
    warn_regression_percent: 10
    min_absolute_regression_ms: 20
  error_rate:
    max_absolute: 1.0
    max_absolute_increase: 0.5
endpoints:
  /api/login:
    p95:
      max_regression_percent: 10
`)

	p, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if p.Version != 1 {
		t.Errorf("Version = %d, expected 1", p.Version)
	}
	if p.UnknownTreatment != StatusFail {
		t.Errorf("UnknownTreatment = %q, expected FAIL", p.UnknownTreatment)
	}
	if p.Defaults.P95 == nil || *p.Defaults.P95.MaxMs != 500 {
		t.Fatalf("Defaults.P95.MaxMs not loaded correctly: %+v", p.Defaults.P95)
	}
}

func TestLoad_DefaultUnknownTreatment(t *testing.T) {
	path := writePolicy(t, `
version: 1
defaults:
  p95:
    max_ms: 500
`)
	p, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if p.UnknownTreatment != StatusWarn {
		t.Errorf("expected default UnknownTreatment WARN, got %q", p.UnknownTreatment)
	}
}

func TestLoad_JSONIsValidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, []byte(`{"version": 1, "defaults": {"p95": {"max_ms": 500}}}`), 0o600); err != nil {
		t.Fatalf("failed to write policy file: %v", err)
	}

	p, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed for JSON policy: %v", err)
	}
	if p.Defaults.P95 == nil || *p.Defaults.P95.MaxMs != 500 {
		t.Fatalf("JSON policy not loaded correctly: %+v", p.Defaults.P95)
	}
}

func TestLoad_RejectsUnsupportedVersion(t *testing.T) {
	path := writePolicy(t, `version: 2`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error for an unsupported policy version")
	}
}

func TestLoad_RejectsInvalidUnknownTreatment(t *testing.T) {
	path := writePolicy(t, `
version: 1
unknown_treatment: MAYBE
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error for an invalid unknown_treatment")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yml")); err == nil {
		t.Fatal("expected an error for a missing policy file")
	}
}

func TestEffectiveMetricPolicy_MergesFieldByField(t *testing.T) {
	path := writePolicy(t, `
version: 1
defaults:
  min_samples: 50
  p95:
    max_ms: 500
    max_regression_percent: 20
  error_rate:
    max_absolute: 1.0
endpoints:
  /api/login:
    p95:
      max_regression_percent: 10
`)
	p, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Overridden endpoint: p95.max_regression_percent changes, but
	// p95.max_ms, min_samples, and error_rate still inherit from defaults.
	effective := p.EffectiveMetricPolicy("/api/login")
	if effective.MinSamplesOrDefault() != 50 {
		t.Errorf("MinSamples = %d, expected inherited 50", effective.MinSamplesOrDefault())
	}
	if effective.P95 == nil || *effective.P95.MaxRegressionPercent != 10 {
		t.Fatalf("expected overridden max_regression_percent=10, got %+v", effective.P95)
	}
	if effective.P95.MaxMs == nil || *effective.P95.MaxMs != 500 {
		t.Fatalf("expected inherited max_ms=500, got %+v", effective.P95)
	}
	if effective.ErrorRate == nil || *effective.ErrorRate.MaxAbsolute != 1.0 {
		t.Fatalf("expected inherited error_rate, got %+v", effective.ErrorRate)
	}

	// Unlisted endpoint: falls back to defaults entirely.
	fallback := p.EffectiveMetricPolicy("/api/unrelated")
	if fallback.P95 == nil || *fallback.P95.MaxRegressionPercent != 20 {
		t.Fatalf("expected default max_regression_percent=20, got %+v", fallback.P95)
	}
}

func TestMinSamplesOrDefault(t *testing.T) {
	m := MetricPolicy{}
	if m.MinSamplesOrDefault() != DefaultMinSamples {
		t.Errorf("expected DefaultMinSamples, got %d", m.MinSamplesOrDefault())
	}

	n := 5
	m.MinSamples = &n
	if m.MinSamplesOrDefault() != 5 {
		t.Errorf("expected 5, got %d", m.MinSamplesOrDefault())
	}
}
