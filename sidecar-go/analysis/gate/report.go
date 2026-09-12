package gate

import (
	"fmt"
	"strings"
)

// Exit code contract for CI integrations (see docs/performance-gate.md):
// a caller must be able to tell a real performance regression apart from a
// tool failure (bad flags, unreadable file, corrupt database) without
// parsing output.
const (
	ExitPass          = 0 // PASS, and WARN when WarnExitCode is left at its default.
	ExitPolicyFailure = 1 // FAIL, or WARN when the caller opted into failing on WARN.
	ExitToolError     = 2 // The gate could not run at all (bad input, not a verdict).
)

// ExitCode maps r.Status to a process exit code. warnExitCode is the code
// to return for WARN — pass ExitPass (the default) to let CI proceed on
// WARN, or ExitPolicyFailure to make WARN fail the build too.
func (r *Result) ExitCode(warnExitCode int) int {
	switch r.Status {
	case StatusFail:
		return ExitPolicyFailure
	case StatusWarn:
		return warnExitCode
	default:
		return ExitPass
	}
}

// FormatHuman renders r as a short, terminal-friendly report: one summary
// line plus one line per violation. It never lists PASS findings — those
// are only in the summary counts — so the output stays proportional to
// what needs attention, not to how much data was analyzed.
func FormatHuman(r *Result) string {
	var b strings.Builder

	fmt.Fprintf(&b, "PERFORMANCE GATE: %s\n", r.Status)
	fmt.Fprintf(&b, "%d passed, %d warned, %d failed, %d unknown\n",
		r.Summary.Passed, r.Summary.Warned, r.Summary.Failed, r.Summary.Unknown)

	if len(r.NewEndpoints) > 0 {
		fmt.Fprintf(&b, "new endpoints (no baseline): %s\n", strings.Join(r.NewEndpoints, ", "))
	}
	if len(r.RemovedEndpoints) > 0 {
		fmt.Fprintf(&b, "removed endpoints (baseline only): %s\n", strings.Join(r.RemovedEndpoints, ", "))
	}

	if len(r.Violations) == 0 {
		return b.String()
	}

	b.WriteString("\n")
	for _, f := range r.Violations {
		fmt.Fprintf(&b, "%-7s %-24s %s (%s)\n", f.Status, f.Scope, f.Metric, f.Check)

		switch {
		case f.Status == StatusUnknown:
			fmt.Fprintf(&b, "        reason: %s\n", f.Reason)
		case f.Check == CheckRegression && f.Baseline != nil && f.Current != nil:
			fmt.Fprintf(&b, "        %s -> %s", formatMs(*f.Baseline, f.Metric), formatMs(*f.Current, f.Metric))
			if f.ChangePercent != nil {
				fmt.Fprintf(&b, "  (%+.2f%%)", *f.ChangePercent)
			}
			b.WriteString("\n")
			if f.Threshold != nil {
				fmt.Fprintf(&b, "        allowed: %+.2f%%\n", *f.Threshold)
			}
			if f.PrimaryTimingChange != nil && f.PrimaryTimingChange.Phase != "" {
				fmt.Fprintf(&b, "        primary timing change: %s %+.2fms\n", strings.ToUpper(f.PrimaryTimingChange.Phase), f.PrimaryTimingChange.DeltaMs)
			}
		case f.Check == CheckBudget && f.Current != nil:
			fmt.Fprintf(&b, "        current: %s", formatMs(*f.Current, f.Metric))
			if f.Threshold != nil {
				fmt.Fprintf(&b, "  (budget: %s)", formatMs(*f.Threshold, f.Metric))
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}

func formatMs(v float64, metric string) string {
	if metric == "error_rate" {
		return fmt.Sprintf("%.2f%%", v)
	}
	return fmt.Sprintf("%.2fms", v)
}
