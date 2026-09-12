# CI Integration

The [performance gate](performance-gate.md) is a CLI (`duckload gate`) and
an HTTP endpoint (`POST /api/v1/analysis/gate`) — not a GitHub Action, a
GitHub App, or a Marketplace listing. Every decision (PASS/WARN/FAIL) is
made by `sidecar-go/analysis/gate`, the same package the CLI and the HTTP
handler both call. A CI provider's job is only to:

```
k6
 ↓
result.duckdb
 ↓
duckload gate
 ↓
exit code
 ↓
your CI provider
```

This keeps the gate portable: the exact same command run on a laptop
reproduces the exact same verdict a CI job produced (see
["Local reproduction"](performance-gate.md#local-reproduction)), and a
different CI provider (GitLab CI, CircleCI, a plain shell script in a
pre-merge hook) needs nothing beyond "run this binary, check its exit
code."

## The three steps

Any CI integration boils down to the same three steps, regardless of
provider:

1. **Build `duckload`** (or use a pre-built binary — it's a static Go binary with no runtime dependency beyond the DuckDB library it statically links).
2. **Produce two `.duckdb` files**: a `current` result (from the code under review) and a `baseline` result (a known-good prior run). How you get these is entirely up to your pipeline — see "Baseline management" below.
3. **Run `duckload gate`** and act on its exit code — see the
   [exit code contract](performance-gate.md#exit-code-contract).

```bash
duckload gate \
  --current current.duckdb \
  --baseline baseline.duckdb \
  --policy performance-policy.yml
echo "exit code: $?"
```

## GitHub Actions example

A worked example lives at
[`docs/examples/github-actions-performance-gate.yml`](examples/github-actions-performance-gate.yml).
It is not under `.github/workflows/` — copy it there yourself
(e.g. `.github/workflows/performance-gate.yml`) and fill in the two
placeholder steps for however your project produces its two `.duckdb`
files. The workflow itself does nothing but call `duckload gate` and
report its result; no gating logic lives in the YAML.

It demonstrates the two optional pieces below: writing a
`GITHUB_STEP_SUMMARY` and uploading the result files as build artifacts.
Both are GitHub-specific *reporting*, kept in the workflow adapter layer —
`sidecar-go/analysis/gate` has no knowledge that `GITHUB_STEP_SUMMARY`
exists.

### GitHub Step Summary

`duckload gate --format human`'s output is plain text meant for a
terminal, and doubles as Markdown-safe content for a step summary:

```bash
./duckload gate --current current.duckdb --baseline baseline.duckdb \
  --policy performance-policy.yml --format human | tee performance-report.md
echo '```' | cat - performance-report.md > /tmp/x && mv /tmp/x performance-report.md # optional: wrap as a code block
cat performance-report.md >> "$GITHUB_STEP_SUMMARY"
```

### CI artifacts

Uploading the two `.duckdb` files alongside the JSON and human reports
means "why did this PR fail?" can be answered after the fact — including
loading `current.duckdb` in [the browser viewer](../README.md#browser-viewer)
or re-running `duckload diagnose` against it — without re-running the load
test. This is the same "keep the result as a portable file" idea the rest
of the project already relies on, applied to CI artifacts instead of
S3-compatible storage.

## Other CI providers

Nothing above is GitHub-specific except the two optional reporting steps.
For GitLab CI, CircleCI, Jenkins, or a plain shell script:

```sh
cd sidecar-go && go build -o duckload ./cmd/duckload
# ... produce current.duckdb and baseline.duckdb however your pipeline does ...
./duckload gate --current current.duckdb --baseline baseline.duckdb \
  --policy performance-policy.yml --format json > performance-report.json
code=$?
cat performance-report.json # or upload it as your provider's artifact mechanism
exit $code
```

## Baseline management

P1 requires supporting an **explicit baseline** — a `.duckdb` file (or a
`run_id` within one) you name directly — and that is what
`duckload gate --baseline` does. Two ways to get that file into a CI job
without building a new artifact-management system:

- **A separate result file per run** (the common case): each sidecar
  already uploads its `.duckdb` file to S3-compatible storage keyed by
  `runs/<run_id>/...` (see the top-level README's Sidecar API section).
  Download last night's known-good run's file as your `baseline.duckdb`
  and this PR's run as `current.duckdb`.
- **A previous CI run's artifact**: if your provider supports fetching a
  previous job's uploaded artifact (GitHub Actions'
  `actions/download-artifact` with `run-id`, for example), download the
  `current.duckdb` a prior known-good run uploaded and use it as this
  run's `--baseline`.

"Automatically pick the latest successful baseline" (rather than naming
one explicitly) is intentionally **not** implemented in P1: it needs a
notion of "which prior run was actually successful" that varies per CI
provider and per team's release process, and forcing one convention into
this project's own storage layer risked exactly the kind of
provider-specific coupling this feature is trying to avoid. See the P2
candidates in the project's task history for this.
