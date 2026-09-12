# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Purpose

**問題**: 従来の負荷テストツールは集計済みメトリクスしか保存せず、テスト後に別の切り口で分析することが困難。

**解決策**: k6の各リクエストを生データとしてDuckDBに保存し、テスト後に自由なSQLクエリで分析可能にする。

**主な特徴**:
- **生データ保存**: 集計前の個別リクエストデータをすべて記録
- **ポータブル**: `.duckdb`ファイル1つで結果を共有・分析可能
- **軽量**: InfluxDB/Grafanaなどの重厚なスタック不要
- **ブラウザ分析**: duckdb-wasmでサーバーなしでも分析可能
- **エンドポイント分析・回帰検知・Explain**: baselineとの自動比較、ボトルネックの根拠、決定論的な原因説明
- **Performance Gate**: YAML/JSONポリシーで設定するPASS/WARN/FAIL判定。CI provider非依存

## Build Commands

### Go Sidecar
```bash
make build-sidecar              # Build sidecar binary
make build-cli                  # Build the duckload CLI (offline analysis, gate)
cd sidecar-go && go build -o duckdb-sidecar  # Direct build

# Run tests
cd sidecar-go && go test ./...                   # All tests
cd sidecar-go && go test ./storage/...           # Single package
cd sidecar-go && go test -v -run TestDuckDBStorage_Flush ./storage/...  # Single test

make test-unit                  # Race-detector unit tests (analysis, cmd, handlers, models, orchestrator, realtime, storage)
make test-e2e                   # End-to-end tests (test/e2e/tests)
make test-analysis              # Validate every analysis SQL query against every synthetic fixture
make test-gate                  # Policy schema + gate evaluation engine, against every synthetic gate fixture
make bench-gate                 # Synthetic cost check for the gate at 100k/1M requests (not part of `make test`)
make ci                         # fmt-check + vet + test-unit (fast local gate)
```

### Docker Compose (RustFS + Sidecar)
```bash
docker compose up -d            # Start RustFS and Sidecar
docker compose down             # Stop all services
```

### Docker (Sidecar only)
```bash
cd sidecar-go && docker build -t duckdb-sidecar .
IMAGE_NAME=yourrepo/duckdb-sidecar:latest ./scripts/build_and_push_sidecar.sh
```

### Frontend
```bash
cd frontend && npm install
npm start                       # Dev server on :8080
npm run build                   # Production build (parcel)
```

## Architecture

This is a load testing pipeline template using k6 + Kubernetes + DuckDB + RustFS:

```
┌─────────────────────────────────────────────────────────────┐
│                     Kubernetes Pod                          │
│  ┌──────────────┐         ┌──────────────────────────────┐ │
│  │     k6       │ HTTP    │     sidecar-go               │ │
│  │   (load      │────────>│  - /api/v1/ingest (events)   │ │
│  │   generator) │         │  - /api/v1/flush-upload      │ │
│  └──────────────┘         │  - /api/v1/download (.duckdb)│ │
│                           │                              │ │
│                           │  Buffers events → DuckDB     │ │
│                           └──────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
                                     │
                                     │ S3-compatible API
                                     ▼
                         ┌──────────────────────┐
                         │  RustFS / AWS S3     │
                         │  (S3互換ストレージ)   │
                         └──────────────────────┘
                                     │
                                     ▼
                            ┌───────────────┐
                            │ Aggregate Job │  (Python + boto3)
                            │ → combined.db │
                            └───────────────┘
                                     │
                                     ▼
                            ┌───────────────┐
                            │   Frontend    │  (duckdb-wasm)
                            │  Visualize    │
                            └───────────────┘
```

### Data Flow
1. **k6** sends HTTP requests to target, captures metrics (status, RTT, body_len)
2. **Sidecar** receives events via POST to `/api/v1/ingest`, buffers in memory
3. Every 5s, sidecar flushes buffer to DuckDB via CSV COPY
4. On `/api/v1/flush-upload`, uploads `.duckdb` file to RustFS/S3
5. **Aggregate job** downloads all `.duckdb` files for a run, merges into `combined.duckdb`
6. **Frontend** loads `.duckdb` via duckdb-wasm for browser-based analysis

### Key Components

**sidecar-go/**: Go service (requires Go 1.24+) with modular structure:
- `main.go`: HTTP server entry point using gorilla/mux
- `models/`: Event data model with detailed timing fields
- `storage/`: DuckDB storage (duckdb-go/v2) and S3-compatible uploader (AWS SDK v2). Exports `MetricsTableSchema`, the canonical DDL, so fixtures/tests never duplicate it.
- `handlers/`: HTTP handlers for ingest, flush, download, and analysis endpoints (`analysis_handlers.go`)
- `analysis/`: Endpoint analysis, bottleneck evidence, baseline comparison, regression detection, and Explain (deterministic, no LLM)
  - `queries/`: The analysis SQL catalog — each query is a `.sql` file under `sql/` with a metadata header (id, purpose, required columns, parameters, expected output), not a string embedded in Go
  - `validator/`: Validates a catalog query via DuckDB itself (schema introspection, `EXPLAIN`, execution) rather than a custom SQL linter
  - `fixtures/`: Deterministic synthetic performance fixtures (see `fixtures.go` and `gate_fixtures.go`) so analysis/gate logic is tested without a real load test
  - `gate/`: The performance gate's evaluation engine (`Evaluate(current, baseline, policy) *Result` — pure function, no I/O, no CI-provider awareness)
  - `policy/`: The performance policy schema (YAML/JSON) and loader
- `cmd/duckload/`: The `duckload` CLI — offline analysis of a `.duckdb` file (`summary`, `endpoints`, `compare`, `diagnose`, `check-analysis`, `gate`)
- `orchestrator/`: Distributed pod orchestration controller
- `realtime/`: WebSocket hub for live metrics streaming

**k6/**: Load test scripts
- `k6-script.js`: Basic script posting events to sidecar
- `scenarios/`: Advanced scenarios (auth-flow, multi-endpoint, data-driven) — each request is tagged with a `name` (e.g. `login`, `get_profile`); the analysis layer groups by `COALESCE(NULLIF(name, ''), url)` so query/path parameters in `url` never explode endpoint cardinality

**aggregate-job/**: Python script using boto3. Downloads per-pod `.duckdb` files from S3-compatible storage, attaches each, and merges into `metrics_all` table.

**frontend/**: duckdb-wasm + Chart.js. Loads a local `.duckdb` file and runs Summary/Endpoints/Regression/Timing Breakdown/Gate views entirely in the browser — no server-side analysis required.

**docker-compose.yml**: Local development setup with RustFS and Sidecar.

**docs/**: `performance-gate.md`, `performance-policy.md`, `ci-integration.md`, plus a ready-to-copy example policy and GitHub Actions workflow under `docs/examples/`.

### DuckDB Table Schema

The actual schema (`storage.MetricsTableSchema` in `sidecar-go/storage/duckdb.go`) is considerably wider than a minimal example — it carries per-request identification (`run_id`, `pod_id`, `vu`, `iter`), request info (`method`, `url`, `name`), response info (`status`, `body_len`), detailed timings (`rtt`, `dns_lookup`, `tcp_connect`, `tls_handshake`, `ttfb`, `content_transfer`), size metrics, error fields, and a `tags` JSON column. Read that constant directly rather than relying on a copy here — it is the compatibility boundary (see the note in each README) and this file is not kept in sync with it automatically.

### Environment Variables

**Sidecar**:
- `RUN_ID`, `POD_NAME`: Test run identification
- `S3_BUCKET`: Target bucket name
- `S3_ENDPOINT`: S3-compatible endpoint (e.g., `http://rustfs:9000`)
- `S3_ACCESS_KEY`, `S3_SECRET_KEY`: Credentials for RustFS/MinIO
- `S3_REGION`: Region (default: `us-east-1`)
- `PORT`: Listen port (default: `8081`)

**k6**: `SIDECAR_BASE`, `TARGET`, `RUN_ID`, `POD_NAME`

**Aggregate**: `S3_BUCKET`, `S3_ENDPOINT`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `RUN_ID`

### Kubernetes Resources
- `k6/k8s/deployment-k6-sidecar.yaml`: Pod with k6 + sidecar containers
- `k6/k8s/configmap-k6-scripts.yaml`: k6 script ConfigMap
- `k6/k8s/hpa-k6.yaml`: HorizontalPodAutoscaler
- `aggregate-job/job-aggregate.yaml`: Aggregation Job

### Storage Options
- **RustFS**: `S3_ENDPOINT=http://rustfs:9000` (local/on-prem)
- **MinIO**: `S3_ENDPOINT=http://minio:9000` (local/on-prem)
- **AWS S3**: Do not set `S3_ENDPOINT` (uses IAM roles)

## Local Development Quickstart

```bash
# Start RustFS + Sidecar
docker compose up -d

# Create bucket (first time only)
docker compose exec rustfs mc alias set local http://localhost:9000 rustfs-user rustfs-password
docker compose exec rustfs mc mb local/loadtest

# Run k6 test
SIDECAR_BASE=http://localhost:8081 TARGET=https://httpbin.org/get RUN_ID=test-run k6 run k6/k6-script.js

# Upload results to RustFS
curl -X POST http://localhost:8081/api/v1/flush-upload

# Download results
curl http://localhost:8081/api/v1/download -o result.duckdb

# Query with DuckDB CLI
duckdb result.duckdb "SELECT status, COUNT(*), AVG(rtt) FROM metrics GROUP BY status"

# Offline analysis with duckload (build once with `make build-cli`)
sidecar-go/duckload endpoints result.duckdb --run-id test-run
sidecar-go/duckload gate --current result.duckdb --run-id test-run --policy docs/examples/performance-policy.example.yml
```

See `docs/performance-gate.md`, `docs/performance-policy.md`, and `docs/ci-integration.md` for the performance gate's PASS/WARN/FAIL contract, its policy schema, and wiring it into CI.
