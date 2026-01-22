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
- **回帰検知**: ベースラインとの自動比較機能

## Build Commands

### Go Sidecar
```bash
make build-sidecar              # Build sidecar binary
cd sidecar-go && go build -o duckdb-sidecar  # Direct build

# Run tests
cd sidecar-go && go test ./...                   # All tests
cd sidecar-go && go test ./storage/...           # Single package
cd sidecar-go && go test -v -run TestDuckDBStorage_Flush ./storage/...  # Single test
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
- `storage/`: DuckDB storage (duckdb-go/v2) and S3-compatible uploader (AWS SDK v2)
- `handlers/`: HTTP handlers for ingest, flush, download, and analysis endpoints
- `analysis/`: Baseline comparison and regression detection logic
- `orchestrator/`: Distributed pod orchestration controller
- `realtime/`: WebSocket hub for live metrics streaming

**k6/**: Load test scripts
- `k6-script.js`: Basic script posting events to sidecar
- `scenarios/`: Advanced scenarios (auth-flow, multi-endpoint, data-driven)

**aggregate-job/**: Python script using boto3. Downloads per-pod `.duckdb` files from S3-compatible storage, attaches each, and merges into `metrics_all` table.

**frontend/**: duckdb-wasm + Chart.js. Loads local `.duckdb` file and runs analytics queries in browser.

**docker-compose.yml**: Local development setup with RustFS and Sidecar.

### DuckDB Table Schema
```sql
CREATE TABLE metrics (
    ts BIGINT,
    pod_id VARCHAR,
    url VARCHAR,
    status INTEGER,
    rtt DOUBLE,
    body_len INTEGER
);
```

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
```
