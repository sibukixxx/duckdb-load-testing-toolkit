package analysis

import (
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"

	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis/fixtures"
)

func TestEndpointAnalyzer_GetEndpointStats(t *testing.T) {
	db, cleanup, err := fixtures.BuildDB(fixtures.Healthy)
	if err != nil {
		t.Fatalf("BuildDB failed: %v", err)
	}
	defer cleanup()

	analyzer := NewEndpointAnalyzer(db)
	stats, err := analyzer.GetEndpointStats(fixtures.CurrentRunID)
	if err != nil {
		t.Fatalf("GetEndpointStats failed: %v", err)
	}

	if len(stats) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(stats))
	}
	for _, s := range stats {
		if s.RequestCount == 0 {
			t.Errorf("%s: expected non-zero request count", s.Endpoint)
		}
		if s.P95RTT < s.P50RTT {
			t.Errorf("%s: p95 (%f) should be >= p50 (%f)", s.Endpoint, s.P95RTT, s.P50RTT)
		}
		if len(s.StatusDistribution) == 0 {
			t.Errorf("%s: expected non-empty status distribution", s.Endpoint)
		}
	}
}

func TestAnalyzeBottleneck_BackendRegression(t *testing.T) {
	db, cleanup, err := fixtures.BuildDB(fixtures.BackendRegression)
	if err != nil {
		t.Fatalf("BuildDB failed: %v", err)
	}
	defer cleanup()

	analyzer := NewEndpointAnalyzer(db)
	evidence, err := AnalyzeBottleneck(analyzer, fixtures.BaselineRunID, fixtures.CurrentRunID)
	if err != nil {
		t.Fatalf("AnalyzeBottleneck failed: %v", err)
	}

	found := false
	for _, e := range evidence {
		if e.Endpoint != "/api/orders" {
			continue
		}
		found = true
		if e.LargestContributor != "ttfb" {
			t.Errorf("expected ttfb to be the largest contributor for /api/orders, got %s (delta=%f)", e.LargestContributor, e.LargestContributorMs)
		}
		if e.LargestContributorMs <= 0 {
			t.Errorf("expected a positive ttfb regression, got %f", e.LargestContributorMs)
		}
	}
	if !found {
		t.Fatal("expected bottleneck evidence for /api/orders")
	}
}

func TestAnalyzeBottleneck_NetworkRegression(t *testing.T) {
	db, cleanup, err := fixtures.BuildDB(fixtures.NetworkRegression)
	if err != nil {
		t.Fatalf("BuildDB failed: %v", err)
	}
	defer cleanup()

	analyzer := NewEndpointAnalyzer(db)
	evidence, err := AnalyzeBottleneck(analyzer, fixtures.BaselineRunID, fixtures.CurrentRunID)
	if err != nil {
		t.Fatalf("AnalyzeBottleneck failed: %v", err)
	}

	for _, e := range evidence {
		if e.Endpoint != "/api/users" {
			continue
		}
		if e.LargestContributor != "dns" && e.LargestContributor != "tcp" && e.LargestContributor != "tls" {
			t.Errorf("expected a network phase to dominate for /api/users, got %s", e.LargestContributor)
		}
	}
}

func TestDetectEndpointRegressions(t *testing.T) {
	db, cleanup, err := fixtures.BuildDB(fixtures.LatencyRegression)
	if err != nil {
		t.Fatalf("BuildDB failed: %v", err)
	}
	defer cleanup()

	analyzer := NewEndpointAnalyzer(db)
	findings, err := DetectEndpointRegressions(analyzer, fixtures.BaselineRunID, fixtures.CurrentRunID, DefaultThresholds())
	if err != nil {
		t.Fatalf("DetectEndpointRegressions failed: %v", err)
	}

	found := false
	for _, f := range findings {
		if f.Scope == "/api/orders" && f.Metric == "p95_rtt" {
			found = true
			if f.ChangePercent <= 0 {
				t.Errorf("expected positive change_percent, got %f", f.ChangePercent)
			}
		}
		if f.Scope == "/api/users" {
			t.Errorf("did not expect a regression on /api/users, got %+v", f)
		}
	}
	if !found {
		t.Error("expected a p95_rtt regression finding for /api/orders")
	}
}

func TestDetectEndpointRegressions_ErrorSpike(t *testing.T) {
	db, cleanup, err := fixtures.BuildDB(fixtures.ErrorSpike)
	if err != nil {
		t.Fatalf("BuildDB failed: %v", err)
	}
	defer cleanup()

	analyzer := NewEndpointAnalyzer(db)
	findings, err := DetectEndpointRegressions(analyzer, fixtures.BaselineRunID, fixtures.CurrentRunID, DefaultThresholds())
	if err != nil {
		t.Fatalf("DetectEndpointRegressions failed: %v", err)
	}

	found := false
	for _, f := range findings {
		if f.Scope == "/api/users" && f.Metric == "error_rate" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an error_rate regression finding for /api/users, got %+v", findings)
	}
}

func TestExplain_LatencyRegression(t *testing.T) {
	db, cleanup, err := fixtures.BuildDB(fixtures.LatencyRegression)
	if err != nil {
		t.Fatalf("BuildDB failed: %v", err)
	}
	defer cleanup()

	explanations, err := Explain(db, fixtures.BaselineRunID, fixtures.CurrentRunID, DefaultThresholds())
	if err != nil {
		t.Fatalf("Explain failed: %v", err)
	}

	found := false
	for _, e := range explanations {
		if e.Endpoint != "/api/orders" {
			continue
		}
		found = true
		if !e.RegressionDetected {
			t.Error("expected RegressionDetected to be true")
		}
		if e.P95CurrentMs <= e.P95BaselineMs {
			t.Errorf("expected current p95 (%f) > baseline p95 (%f)", e.P95CurrentMs, e.P95BaselineMs)
		}
		if e.LargestTimingChange.Phase == "" {
			t.Error("expected a non-empty largest timing change phase")
		}
	}
	if !found {
		t.Fatal("expected an explanation for /api/orders")
	}
}

func TestExplain_PodOutlier(t *testing.T) {
	db, cleanup, err := fixtures.BuildDB(fixtures.PodOutlier)
	if err != nil {
		t.Fatalf("BuildDB failed: %v", err)
	}
	defer cleanup()

	explanations, err := Explain(db, fixtures.BaselineRunID, fixtures.CurrentRunID, DefaultThresholds())
	if err != nil {
		t.Fatalf("Explain failed: %v", err)
	}

	found := false
	for _, e := range explanations {
		if e.Endpoint != "/api/orders" {
			continue
		}
		found = true
		hasPod2 := false
		for _, p := range e.AffectedPods {
			if p == "pod-2" {
				hasPod2 = true
			}
		}
		if !hasPod2 {
			t.Errorf("expected pod-2 to be listed as affected, got %v", e.AffectedPods)
		}
	}
	if !found {
		t.Fatal("expected an explanation for /api/orders in the pod-outlier fixture")
	}
}

func TestExplain_Healthy_NoRegressions(t *testing.T) {
	db, cleanup, err := fixtures.BuildDB(fixtures.Healthy)
	if err != nil {
		t.Fatalf("BuildDB failed: %v", err)
	}
	defer cleanup()

	explanations, err := Explain(db, fixtures.BaselineRunID, fixtures.CurrentRunID, DefaultThresholds())
	if err != nil {
		t.Fatalf("Explain failed: %v", err)
	}
	if len(explanations) != 0 {
		t.Errorf("expected no explanations for the healthy fixture, got %+v", explanations)
	}
}
