# Performance Gate

The performance gate turns a P0 analysis (endpoint statistics, baseline
comparison, regression detection) into a single machine-checkable verdict —
PASS, WARN, or FAIL — that CI can act on. It is a plain Go package
(`sidecar-go/analysis/gate`) with no dependency on any CI provider: the
same evaluation engine backs the `duckload gate` CLI command, the
`POST /api/v1/analysis/gate` HTTP endpoint, and this package's own tests.
A CI provider only ever calls one of those two entry points and reads an
exit code or a JSON body — it never re-implements a check.

## Concepts

- **Performance Budget** — an absolute ceiling on one metric for one
  endpoint (e.g. "p95 must stay under 500ms"), evaluated on the current
  run alone. No baseline is required.
- **Regression** — how much a metric moved between a baseline run and the
  current run, as a percentage. A regression check requires both a
  baseline and a current run to share the same endpoint.
- **Check** — the gate always runs a budget check and a regression check
  independently for latency percentiles and for the error rate; a single
  endpoint can fail one and pass the other, and both are reported.
- **Finding** — the result of one check for one endpoint and one metric.
  Every finding has a `status` (`PASS`, `WARN`, `FAIL`, or `UNKNOWN`).
- **Result** — the outcome of one evaluation: an overall `status`, a
  `summary` (counts by status), and a `violations` list containing every
  non-`PASS` finding. `PASS` findings are counted but not listed
  individually, so the report stays proportional to what needs attention.

See [`docs/performance-policy.md`](performance-policy.md) for how budgets
and regression thresholds are configured, and
[`docs/ci-integration.md`](ci-integration.md) for wiring this into a
pipeline.

## PASS / WARN / FAIL / UNKNOWN

Each finding's status:

| Status | Meaning |
| --- | --- |
| `PASS` | The check ran and stayed within policy. |
| `WARN` | The check exceeded a softer "warn" threshold, but not the "fail" one. |
| `FAIL` | The check exceeded its "fail" threshold. |
| `UNKNOWN` | The gate could not evaluate the check at all — see below. |

The overall `Result.status` is the worst status among every finding, where
`UNKNOWN` counts as whatever `unknown_treatment` says in the policy
(`WARN` by default) — see "Unknown / insufficient evidence" below. FAIL
always wins over WARN, which always wins over PASS/resolved-UNKNOWN.

## Unknown / insufficient evidence

The gate never forces a PASS or FAIL out of data it cannot trust. A
finding becomes `UNKNOWN` (never silently PASS, never silently FAIL) when:

| Reason | When it happens |
| --- | --- |
| `insufficient_samples` | The current run (or, for a regression check, the baseline run) has fewer requests for that endpoint than the policy's `min_samples`. |
| `no_baseline_for_endpoint` | A regression check is configured, but the current run has an endpoint the baseline never saw. |

Two related cases are **not** `UNKNOWN` findings:

- **No baseline supplied at all** (`--baseline` omitted, or the HTTP
  request has no `baseline_run_id`) runs the gate in
  **Performance-Budget-only mode**: every absolute budget still applies
  normally, but no regression check runs and nothing is reported as
  unknown, since none was requested.
- **An endpoint the baseline had but the current run no longer serves**
  produces no finding at all — there is nothing to gate in the current
  run — but is listed in `Result.removed_endpoints` for visibility.
- Conversely, an endpoint only the current run has is listed in
  `Result.new_endpoints`, in addition to its `UNKNOWN` regression finding
  (its budget check, if configured, still runs and can PASS/WARN/FAIL
  normally).

`unknown_treatment` in the policy (`PASS`, `WARN`, or `FAIL`; default
`WARN`) controls how much weight an `UNKNOWN` finding carries toward the
overall `Result.status`. It never rewrites the finding's own `status` —
that always stays `UNKNOWN` in the output — it only decides how the
overall verdict treats it.

## Noise resistance

A load test's numbers move on their own between runs. The gate avoids
treating that as a regression through three independent controls,
combined:

1. **A relative threshold** (`max_regression_percent` / `warn_regression_percent`) — the percentage change that counts as a regression.
2. **An absolute floor** (`min_absolute_regression_ms`) — a regression only counts once the absolute change also exceeds this many milliseconds, so "+25%, +2ms" on an already-fast endpoint does not fail a build over jitter.
3. **A minimum sample size** (`min_samples`) — see "Unknown" above.

## Machine-readable report (`--format json`)

```json
{
  "schema_version": 1,
  "status": "FAIL",
  "summary": { "passed": 14, "warned": 2, "failed": 1, "unknown": 0 },
  "violations": [
    {
      "scope": "/api/users",
      "metric": "p95",
      "check": "regression",
      "status": "FAIL",
      "baseline": 210.0,
      "current": 340.0,
      "delta": 130.0,
      "change_percent": 61.9,
      "threshold": 20.0,
      "primary_timing_change": { "phase": "ttfb", "delta_ms": 96.0 }
    }
  ],
  "new_endpoints": [],
  "removed_endpoints": []
}
```

`schema_version` is the compatibility boundary for anything that parses
this output: it only changes for a breaking change to this shape, and a
CI integration should check it before trusting the rest of the fields.

## Human-readable report (default)

```
PERFORMANCE GATE: FAIL
14 passed, 2 warned, 1 failed, 0 unknown

FAIL    /api/users               p95 (regression)
        210.00ms -> 340.00ms  (+61.90%)
        allowed: +20.00%
        primary timing change: TTFB +96.00ms
```

Only violations are listed — PASS findings are summarized as a count, not
printed individually — so the report stays short regardless of how many
endpoints were analyzed.

## Exit code contract

`duckload gate` (and any CI step that calls it) uses exit codes to let CI
distinguish "the build regressed" from "the tool couldn't run":

| Exit code | Meaning |
| --- | --- |
| `0` | `PASS`, or `WARN` when `--warn-exit-code` is left at its default. |
| `1` | `FAIL`, or `WARN` when `--warn-exit-code 1` was passed. |
| `2` | The gate did not produce a verdict at all: a missing/unreadable `.duckdb` file, an invalid or unreadable policy file, a bad flag, an ambiguous run_id needing `--run-id`/`--baseline-run-id` to disambiguate, and so on. |

Exit code `2` is never a performance verdict — treat it the same way a
compiler crash differs from a failing test. A CI step that only checks
"exit code is non-zero" cannot tell a real regression from a broken
pipeline; check for `2` specifically if that distinction matters to your
pipeline (for example, to retry once on a transient file-download failure
without treating it as a regression).

`--warn-exit-code` (default `0`) lets a pipeline decide whether WARN
should block a merge; there is no equivalent flag for FAIL, which always
maps to `1`.

## Local reproduction

A CI failure must be exactly reproducible on a laptop: the gate has no
CI-only code path. Download the same two `.duckdb` files the CI job used
(or reuse the ones a CI step uploaded as an artifact — see
[`docs/ci-integration.md`](ci-integration.md)) and the same policy file,
then run:

```bash
duckload gate --current current.duckdb --baseline baseline.duckdb --policy performance-policy.yml
```
