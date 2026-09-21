# DuckDB Load Testing Toolkit

Go sidecar/CLI for collecting load-test metrics, storing them in DuckDB/object storage, and running reproducible analysis/gates.

## Commands
- `make build-sidecar`
- `make build-cli`
- `make test-unit`
- `make test-e2e`
- `make test-analysis`
- `make test-gate`
- `make ci`
- `make bench-gate` — explicit benchmark, not normal test gate

## Shared rules
- `storage.MetricsTableSchema` is the canonical metrics schema; fixtures/queries must not maintain divergent copies.
- Endpoint grouping uses request names / normalized fallback; do not reintroduce URL query/path cardinality explosion.
- Keep analysis/gate evaluation deterministic from stored metrics and policy inputs.
- For AWS S3, absence of `S3_ENDPOINT` is intentional; custom endpoints are for S3-compatible/local storage.
- Load tests must not target production/third parties without explicit authorization.
- Generated benchmark/results data is output, not hand-edited source.

## Change-dependent checks
- Go sidecar/CLI: `make ci`.
- Analysis SQL/schema: `make test-analysis`.
- Gate/policy: `make test-gate`; benchmark only when performance is in scope.
- End-to-end storage: `make test-e2e` when Docker/dependencies are available.

## Done
- Relevant deterministic gates pass.
- Metrics/schema/query changes remain mutually compatible.
- Load target/environment-dependent checks are reported explicitly.
