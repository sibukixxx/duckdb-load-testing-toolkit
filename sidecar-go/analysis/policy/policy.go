// Package policy defines the performance gate's configuration schema and
// loads it from a YAML (or JSON, which is valid YAML) file. It intentionally
// stays a plain data format — a handful of named, documented fields — not a
// custom DSL: the gate engine (analysis/gate) is the only place decisions
// get made.
package policy

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// SchemaVersion is the current policy file format version. A policy file
// must declare a version the loader recognizes so a future incompatible
// format change fails loudly instead of silently misinterpreting a file.
const SchemaVersion = 1

// Status is a gate verdict. Policy files reference it only to configure how
// UNKNOWN findings should be treated (UnknownTreatment); the full set of
// values is defined here so both packages agree on the vocabulary.
type Status string

const (
	StatusPass    Status = "PASS"
	StatusWarn    Status = "WARN"
	StatusFail    Status = "FAIL"
	StatusUnknown Status = "UNKNOWN"
)

// DefaultMinSamples is used for a metric group when min_samples is not set
// anywhere in the policy (neither defaults nor an endpoint override).
const DefaultMinSamples = 30

// DefaultUnknownTreatment is used when a policy file does not set
// unknown_treatment: findings the gate cannot evaluate (insufficient
// samples, no matching baseline endpoint, ...) become WARN rather than
// silently passing or failing the build.
const DefaultUnknownTreatment = StatusWarn

// Policy is the root of a performance policy file.
type Policy struct {
	// Version must equal SchemaVersion.
	Version int `yaml:"version"`

	// UnknownTreatment says how a Finding the gate could not evaluate
	// (INSUFFICIENT_DATA / UNKNOWN) should count toward the overall
	// status: PASS, WARN, or FAIL. Defaults to WARN.
	UnknownTreatment Status `yaml:"unknown_treatment"`

	// Defaults applies to every endpoint unless overridden below.
	Defaults MetricPolicy `yaml:"defaults"`

	// Endpoints holds per-endpoint overrides, keyed by the same endpoint
	// identity the analysis layer uses (COALESCE(NULLIF(name,''), url) —
	// see analysis/queries/sql/endpoint_summary.sql). Unset fields in an
	// override fall back to Defaults field by field, not as a whole
	// struct, so a policy can tighten one metric for one endpoint without
	// restating the rest.
	Endpoints map[string]MetricPolicy `yaml:"endpoints"`
}

// MetricPolicy groups every check the gate can run for one endpoint (or
// the defaults applied to all of them): latency budgets/regressions for
// three percentiles, an error-rate budget/regression, and the minimum
// sample size that makes any of the above trustworthy.
type MetricPolicy struct {
	// MinSamples is the minimum current-run (and, for regression checks,
	// baseline-run) request count an endpoint needs before its latency or
	// error-rate findings are trusted. Below it, findings are UNKNOWN
	// rather than PASS or FAIL — see analysis/gate.
	MinSamples *int `yaml:"min_samples"`

	P50       *LatencyPolicy   `yaml:"p50"`
	P95       *LatencyPolicy   `yaml:"p95"`
	P99       *LatencyPolicy   `yaml:"p99"`
	ErrorRate *ErrorRatePolicy `yaml:"error_rate"`
}

// LatencyPolicy configures both an absolute Performance Budget and a
// relative regression check for one percentile. Either half can be used
// alone: a budget needs no baseline, and a regression check needs no
// absolute ceiling.
//
// Both halves optionally carry a softer "warn" threshold in addition to
// the hard "fail" one, so a gate can flag a smaller drift as WARN while
// only failing the build on a larger one — this is what lets the gate's
// summary show passed/warned/failed instead of a single boolean.
type LatencyPolicy struct {
	// MaxMs / WarnMaxMs: absolute Performance Budget, in milliseconds.
	MaxMs     *float64 `yaml:"max_ms"`
	WarnMaxMs *float64 `yaml:"warn_max_ms"`

	// MaxRegressionPercent / WarnRegressionPercent: how much the current
	// run's percentile may exceed the baseline's before this counts as a
	// regression, as a percentage (20 means +20%).
	MaxRegressionPercent  *float64 `yaml:"max_regression_percent"`
	WarnRegressionPercent *float64 `yaml:"warn_regression_percent"`

	// MinAbsoluteRegressionMs is a noise-resistance floor: a regression
	// only counts once the absolute change also exceeds this many
	// milliseconds, so a run that is both "+25%" and "+2ms" on an
	// already-fast endpoint does not fail the build on jitter alone.
	// Zero (the default) disables the floor.
	MinAbsoluteRegressionMs *float64 `yaml:"min_absolute_regression_ms"`
}

// ErrorRatePolicy is the same budget/regression/warn shape as
// LatencyPolicy, but for the error rate. Every value is a percentage
// (0-100), matching how the rest of the analysis layer already reports
// error rates (see analysis.EndpointStats.ErrorRate) — e.g. 1.0 means 1%,
// not 0.01.
type ErrorRatePolicy struct {
	MaxAbsolute     *float64 `yaml:"max_absolute"`
	WarnMaxAbsolute *float64 `yaml:"warn_max_absolute"`

	MaxAbsoluteIncrease  *float64 `yaml:"max_absolute_increase"`
	WarnAbsoluteIncrease *float64 `yaml:"warn_absolute_increase"`
}

// Load reads and validates a policy file. YAML is the primary format;
// since JSON is valid YAML, a .json policy file loads through the same
// path without a second parser.
func Load(path string) (*Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("policy: failed to read %s: %w", path, err)
	}

	p, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("policy: %s: %w", path, err)
	}
	return p, nil
}

// Parse validates and returns the policy encoded in data. data may be YAML
// or JSON (JSON is valid YAML, so no second parser is needed) — this is
// what lets the HTTP API accept a policy embedded in a JSON request body
// using the exact same rules Load applies to a file.
func Parse(data []byte) (*Policy, error) {
	var p Policy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("failed to parse policy: %w", err)
	}

	if err := p.Validate(); err != nil {
		return nil, err
	}
	if p.UnknownTreatment == "" {
		p.UnknownTreatment = DefaultUnknownTreatment
	}
	return &p, nil
}

// Validate checks structural invariants Load cannot rely on the zero value
// for: the schema version, and that UnknownTreatment (when set) is one of
// the recognized statuses.
func (p *Policy) Validate() error {
	if p.Version != SchemaVersion {
		return fmt.Errorf("unsupported policy version %d (expected %d)", p.Version, SchemaVersion)
	}
	switch p.UnknownTreatment {
	case "", StatusPass, StatusWarn, StatusFail:
	default:
		return fmt.Errorf("unknown_treatment must be PASS, WARN, or FAIL, got %q", p.UnknownTreatment)
	}
	return nil
}

// EffectiveMetricPolicy resolves the policy that applies to endpoint,
// merging Defaults with any endpoint-specific override field by field:
// an override that sets only p95.max_ms still inherits every other
// default (min_samples, p50, error_rate, ...) rather than discarding them.
func (p *Policy) EffectiveMetricPolicy(endpoint string) MetricPolicy {
	effective := p.Defaults
	override, ok := p.Endpoints[endpoint]
	if !ok {
		return effective
	}

	if override.MinSamples != nil {
		effective.MinSamples = override.MinSamples
	}
	if override.P50 != nil {
		effective.P50 = mergeLatency(effective.P50, override.P50)
	}
	if override.P95 != nil {
		effective.P95 = mergeLatency(effective.P95, override.P95)
	}
	if override.P99 != nil {
		effective.P99 = mergeLatency(effective.P99, override.P99)
	}
	if override.ErrorRate != nil {
		effective.ErrorRate = mergeErrorRate(effective.ErrorRate, override.ErrorRate)
	}
	return effective
}

// MinSamplesOrDefault returns m.MinSamples, or DefaultMinSamples if unset.
func (m MetricPolicy) MinSamplesOrDefault() int {
	if m.MinSamples != nil {
		return *m.MinSamples
	}
	return DefaultMinSamples
}

func mergeLatency(base, override *LatencyPolicy) *LatencyPolicy {
	if base == nil {
		return override
	}
	merged := *base
	if override.MaxMs != nil {
		merged.MaxMs = override.MaxMs
	}
	if override.WarnMaxMs != nil {
		merged.WarnMaxMs = override.WarnMaxMs
	}
	if override.MaxRegressionPercent != nil {
		merged.MaxRegressionPercent = override.MaxRegressionPercent
	}
	if override.WarnRegressionPercent != nil {
		merged.WarnRegressionPercent = override.WarnRegressionPercent
	}
	if override.MinAbsoluteRegressionMs != nil {
		merged.MinAbsoluteRegressionMs = override.MinAbsoluteRegressionMs
	}
	return &merged
}

func mergeErrorRate(base, override *ErrorRatePolicy) *ErrorRatePolicy {
	if base == nil {
		return override
	}
	merged := *base
	if override.MaxAbsolute != nil {
		merged.MaxAbsolute = override.MaxAbsolute
	}
	if override.WarnMaxAbsolute != nil {
		merged.WarnMaxAbsolute = override.WarnMaxAbsolute
	}
	if override.MaxAbsoluteIncrease != nil {
		merged.MaxAbsoluteIncrease = override.MaxAbsoluteIncrease
	}
	if override.WarnAbsoluteIncrease != nil {
		merged.WarnAbsoluteIncrease = override.WarnAbsoluteIncrease
	}
	return &merged
}
