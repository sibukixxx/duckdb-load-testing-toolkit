package analysis

import "fmt"

// TimingBreakdown is the average network/backend timing phases for a set
// of requests, in milliseconds.
type TimingBreakdown struct {
	DNS      float64
	TCP      float64
	TLS      float64
	TTFB     float64
	Transfer float64
}

// TimingChange names one phase and how much it moved, in milliseconds.
type TimingChange struct {
	Phase   string  `json:"phase"`
	DeltaMs float64 `json:"delta_ms"`
}

// BottleneckEvidence structures the answer to "what part of the request
// got slower" for one endpoint: the baseline and current timing
// breakdowns, their per-phase deltas, and which phase changed the most.
// It intentionally stops at evidence — no automated root-cause diagnosis.
type BottleneckEvidence struct {
	Endpoint             string
	Baseline             TimingBreakdown
	Current              TimingBreakdown
	Delta                TimingBreakdown
	LargestContributor   string
	LargestContributorMs float64
}

// AnalyzeBottleneck compares the per-endpoint timing breakdown between a
// baseline and current run and ranks, per endpoint, which timing phase
// (DNS/TCP/TLS/TTFB/transfer) contributed most to any change in total
// latency.
func AnalyzeBottleneck(analyzer *EndpointAnalyzer, baselineRunID, currentRunID string) ([]BottleneckEvidence, error) {
	baseline, err := analyzer.GetEndpointStats(baselineRunID)
	if err != nil {
		return nil, fmt.Errorf("bottleneck analysis: baseline: %w", err)
	}
	current, err := analyzer.GetEndpointStats(currentRunID)
	if err != nil {
		return nil, fmt.Errorf("bottleneck analysis: current: %w", err)
	}

	baselineByEndpoint := map[string]EndpointStats{}
	for _, s := range baseline {
		baselineByEndpoint[s.Endpoint] = s
	}

	var evidence []BottleneckEvidence
	for _, cur := range current {
		base, ok := baselineByEndpoint[cur.Endpoint]
		if !ok {
			continue // Endpoint only present in current run; nothing to compare against.
		}
		evidence = append(evidence, TimingDelta(base, cur))
	}

	return evidence, nil
}

// TimingDelta computes the timing-phase breakdown, per-phase deltas, and
// largest contributor between an endpoint's baseline and current stats. It
// does no I/O — callers that already have both EndpointStats in hand (the
// performance gate, in particular) can use it directly instead of paying
// for AnalyzeBottleneck's own queries a second time.
func TimingDelta(baseline, current EndpointStats) BottleneckEvidence {
	e := BottleneckEvidence{
		Endpoint: current.Endpoint,
		Baseline: TimingBreakdown{DNS: baseline.AvgDNS, TCP: baseline.AvgTCP, TLS: baseline.AvgTLS, TTFB: baseline.AvgTTFB, Transfer: baseline.AvgTransfer},
		Current:  TimingBreakdown{DNS: current.AvgDNS, TCP: current.AvgTCP, TLS: current.AvgTLS, TTFB: current.AvgTTFB, Transfer: current.AvgTransfer},
	}
	e.Delta = TimingBreakdown{
		DNS:      e.Current.DNS - e.Baseline.DNS,
		TCP:      e.Current.TCP - e.Baseline.TCP,
		TLS:      e.Current.TLS - e.Baseline.TLS,
		TTFB:     e.Current.TTFB - e.Baseline.TTFB,
		Transfer: e.Current.Transfer - e.Baseline.Transfer,
	}
	e.LargestContributor, e.LargestContributorMs = largestPhase(e.Delta)
	return e
}

// largestPhase returns the phase name with the largest absolute delta, and
// that delta (signed, so a caller can tell improvement from regression).
func largestPhase(d TimingBreakdown) (string, float64) {
	phases := []TimingChange{
		{"dns", d.DNS},
		{"tcp", d.TCP},
		{"tls", d.TLS},
		{"ttfb", d.TTFB},
		{"transfer", d.Transfer},
	}
	best := phases[0]
	for _, p := range phases[1:] {
		if abs(p.DeltaMs) > abs(best.DeltaMs) {
			best = p
		}
	}
	return best.Phase, best.DeltaMs
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
