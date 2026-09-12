# Performance Policy

A performance policy is a YAML (or JSON — JSON is valid YAML, so it loads
through the same parser) file that configures the [performance
gate](performance-gate.md) declaratively. It is data, not code: the gate
engine (`sidecar-go/analysis/gate`) is the only place a decision actually
gets made, and it is unaffected by anything you can write in this file
beyond the schema below.

A ready-to-copy starting point lives at
[`docs/examples/performance-policy.example.yml`](examples/performance-policy.example.yml).
Copy it and adjust the numbers to your service — the values in it are a
reasonable default, not a strict recommendation.

## Schema (version 1)

```yaml
version: 1                    # required; must be 1
unknown_treatment: WARN       # optional; PASS | WARN | FAIL (default WARN)

defaults:                     # applies to every endpoint unless overridden
  min_samples: 30
  p50: { ... }                # LatencyPolicy, optional
  p95: { ... }                # LatencyPolicy, optional
  p99: { ... }                # LatencyPolicy, optional
  error_rate: { ... }         # ErrorRatePolicy, optional

endpoints:                    # optional per-endpoint overrides
  /api/login:
    p95: { max_regression_percent: 15 }
  /api/report:
    p95: { max_regression_percent: 30 }
```

Every field is optional except `version`. A metric group you don't
configure (no `p95`, no `error_rate`, ...) is simply never checked — the
gate does not assume a default budget or threshold you never asked for.

### `min_samples`

The minimum request count an endpoint needs, in the current run (and, for
a regression check, in the baseline run too), before any of that
endpoint's findings are trusted. Below it, every configured check for
that endpoint becomes `UNKNOWN` with reason `insufficient_samples` rather
than PASS or FAIL — see
["Unknown / insufficient evidence"](performance-gate.md#unknown--insufficient-evidence).
Defaults to 30 when unset anywhere.

**Why**: a p95 computed from 3 requests is not a p95 you can act on. This
is what keeps the gate from crying regression over noise on a
low-traffic endpoint.

### `p50` / `p95` / `p99` (LatencyPolicy)

```yaml
p95:
  max_ms: 500                      # absolute Performance Budget (fails above this)
  warn_max_ms: 400                 # softer budget (warns above this)
  max_regression_percent: 20       # regression check: fails above +20% vs. baseline
  warn_regression_percent: 10      # softer regression threshold
  min_absolute_regression_ms: 20   # noise floor: only counts once the ms delta also clears this
```

| Field | Unit | What | Default behavior if unset |
| --- | --- | --- | --- |
| `max_ms` | milliseconds | Absolute ceiling for this percentile, checked on the current run alone (no baseline needed). | The budget check does not run. |
| `warn_max_ms` | milliseconds | Softer ceiling; exceeding it (but not `max_ms`) produces a WARN instead of FAIL. | No WARN tier for the budget check. |
| `max_regression_percent` | percent | How much this percentile may increase vs. baseline before it's a regression. | The regression check does not run. |
| `warn_regression_percent` | percent | Softer regression threshold. | No WARN tier for the regression check. |
| `min_absolute_regression_ms` | milliseconds | A regression only counts once the absolute change also exceeds this floor — see [noise resistance](performance-gate.md#noise-resistance). | `0` (no floor). |

You can set only the budget half, only the regression half, or both —
they are independent checks and both get reported.

### `error_rate` (ErrorRatePolicy)

```yaml
error_rate:
  max_absolute: 1.0             # budget: fails if current error rate > 1%
  warn_max_absolute: 0.5        # softer budget
  max_absolute_increase: 1.0    # regression: fails if current - baseline > 1 percentage point
  warn_absolute_increase: 0.5   # softer regression threshold
```

**Unit**: every error-rate value is a **percentage (0-100)**, matching how
the rest of the analysis layer already reports error rates — `1.0` means
1%, not 0.01. This keeps the policy consistent with the endpoint analysis
and diagnose JSON responses (`analysis.EndpointStats.ErrorRate`), which
also use 0-100.

`max_absolute_increase` is a difference in **percentage points**, not a
relative percent-of-percent change — a baseline of 1% and a current of
3% is a 2-point increase, not "200%".

### `endpoints` overrides

Each entry under `endpoints` merges into `defaults` **field by field**,
not as a whole block: overriding only `p95.max_regression_percent` for
one endpoint still inherits `min_samples`, `p50`, `error_rate`, and every
other `p95` field from `defaults`. You never need to restate the rest of
the policy to tighten or loosen one number for one endpoint.

The endpoint key must match the endpoint identity the analysis layer
already uses: `COALESCE(NULLIF(name, ''), url)` — the k6 request `name`
tag (e.g. `login`, `get_profile`) when set, falling back to the raw
`url`. See [Endpoint identity](#endpoint-identity) below.

## Endpoint identity

Grouping by the raw request `url` would explode cardinality on any
endpoint with query parameters or path parameters (`/users?id=1`,
`/users?id=2`, ... would each become their own "endpoint"). The analysis
layer and the gate both already avoid this: they group by
`COALESCE(NULLIF(name, ''), url)`, reusing the k6 `name` tag every
scenario in `k6/scenarios/` already sets (`login`, `get_profile`,
`get_data`, ...) instead of guessing at a normalization from the URL
itself. If your own k6 script never sets `name`, the raw `url` is used —
so this only helps once you start tagging requests, and never rewrites a
URL you didn't ask it to.

Use whichever value shows up in `duckload endpoints`' or the
`/api/v1/analysis/endpoints` response's `"endpoint"` field as the key
under `policy.endpoints`.

## Two independent gates, one model

A policy with only `p95.max_ms` set (no `max_regression_percent`) is a
pure **Performance Budget Gate** — it needs no baseline at all and works
in `duckload gate`'s Performance-Budget-only mode (omit `--baseline`
entirely). A policy with only `max_regression_percent` set is a pure
**Regression Gate**. Most real policies use both, and the gate evaluates
them independently per endpoint per metric — an endpoint can fail its
budget while passing its regression check, or the reverse, and both show
up as separate findings.
