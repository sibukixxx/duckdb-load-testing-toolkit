# DuckDB Load Testing Toolkit

[![CI](https://github.com/sibukixxx/duckdb-load-testing-toolkit/actions/workflows/ci.yml/badge.svg)](https://github.com/sibukixxx/duckdb-load-testing-toolkit/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go)](sidecar-go/go.mod)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](#project-status-and-contributing)

A portable load-testing pipeline that captures request-level [k6](https://grafana.com/docs/k6/latest/) metrics in [DuckDB](https://duckdb.org/), uploads results to S3-compatible storage, and analyzes them without running a permanent observability stack.

> [!IMPORTANT]
> This project is under active development. Interfaces and deployment manifests may change before the first stable release.

If you've ever wanted to re-slice a load test after the fact — by endpoint, by pod, by percentile, by anything — instead of being stuck with whatever your dashboard pre-aggregated, this toolkit is for you. A ⭐ on the repo helps other people running k6 tests find it.

## Table of Contents

- [Why this project?](#why-this-project)
- [How it works](#how-it-works)
- [Components](#components)
- [Quick start](#quick-start)
- [Browser viewer](#browser-viewer)
- [Sidecar API](#sidecar-api)
- [Configuration](#configuration)
- [Development](#development)
- [Project status and contributing](#project-status-and-contributing)
- [License](#license)

## Why this project?

Most load-testing dashboards keep pre-aggregated metrics. That is useful during a run, but it limits the questions you can ask afterward. This toolkit stores one row per request so that you can:

- investigate a specific endpoint, status code, pod, or time window with SQL;
- inspect DNS, TCP, TLS, TTFB, and total request timing;
- combine results produced by multiple Kubernetes pods;
- keep and share a test run as a single `.duckdb` file;
- analyze results locally or in a browser with DuckDB-Wasm;
- compare a run with a baseline and detect performance regressions.

## How it works

```text
┌──────────────── Kubernetes pod ────────────────┐
│                                                │
│  k6 ── request metrics ──▶ Go sidecar ──▶ DuckDB
│                                                │
└────────────────────────────────────────────────┘
                                      │
                                      ▼
                            S3-compatible storage
                                      │
                         ┌────────────┴────────────┐
                         ▼                         ▼
                  aggregation job          browser frontend
```

The k6 script sends request metrics to the Go sidecar. The sidecar buffers them, periodically writes them to DuckDB, and can upload the database to RustFS, MinIO, or Amazon S3. An optional Kubernetes job merges databases from distributed workers, while the frontend opens results with DuckDB-Wasm.

## Components

| Path | Purpose |
| --- | --- |
| `sidecar-go/` | HTTP ingestion API, DuckDB storage, S3 upload, analysis, and live WebSocket updates |
| `k6/` | Basic and advanced k6 scenarios plus Kubernetes manifests |
| `aggregate-job/` | Kubernetes job for merging distributed DuckDB result files |
| `frontend/` | Browser-based DuckDB-Wasm and Chart.js result viewer |
| `docker-compose.yml` | Local RustFS and sidecar environment |

## Quick start

### Prerequisites

- Docker with Docker Compose v2
- k6 0.45 or later
- `curl`

Go 1.24 or later is only required for local sidecar development. Node.js 18 or later is only required for the optional frontend.

### 1. Start the local stack

```bash
docker compose up -d
docker compose ps
```

The sidecar listens on `http://localhost:8081`. RustFS exposes its S3 API on port `9000` and console on port `9001`.

Create the result bucket on the first run:

```bash
docker compose exec rustfs mc alias set local http://localhost:9000 rustfs-user rustfs-password
docker compose exec rustfs mc mb --ignore-existing local/loadtest
```

The credentials above are development defaults from `docker-compose.yml`; do not use them in a shared or production environment.

### 2. Run k6

In another terminal:

```bash
SIDECAR_BASE=http://localhost:8081 \
TARGET=https://test.k6.io \
RUN_ID=quickstart \
k6 run k6/k6-script.js
```

Only run load tests against systems you own or are authorized to test.

### 3. Save and query the result

```bash
curl -X POST http://localhost:8081/api/v1/flush-upload
curl http://localhost:8081/api/v1/download -o result.duckdb
duckdb result.duckdb \
  "SELECT status, count(*) AS requests, avg(rtt) AS avg_rtt FROM metrics GROUP BY status"
```

To stop the local stack while keeping its volumes:

```bash
docker compose stop
```

## Browser viewer

```bash
cd frontend
npm install
npm start
```

Open `http://localhost:8080` and select a DuckDB result file. Queries run locally in the browser with DuckDB-Wasm.

## Sidecar API

| Method | Endpoint | Description |
| --- | --- | --- |
| `GET` | `/api/v1/health` | Health check |
| `POST` | `/api/v1/ingest` | Ingest one request metric |
| `POST` | `/api/v1/ingest/batch` | Ingest a metric batch |
| `POST` | `/api/v1/flush` | Flush buffered metrics to DuckDB |
| `POST` | `/api/v1/flush-upload` | Flush and upload the database |
| `GET` | `/api/v1/download` | Download the current database |
| `GET` | `/api/v1/stats` | Read current run statistics |
| `POST` | `/api/v1/analysis/compare` | Compare runs |
| `GET` | `/api/v1/analysis/run-stats` | Read statistics for a run |
| `GET` | `/api/v1/analysis/trend` | Read a metric trend |
| `POST` | `/api/v1/analysis/baseline` | Calculate a baseline |
| `GET` | `/ws` | Stream live metrics over WebSocket |

## Configuration

The sidecar is configured through environment variables.

| Variable | Default | Description |
| --- | --- | --- |
| `PORT` | `8081` | HTTP server port |
| `DATA_DIR` | `/data` | Directory for DuckDB files |
| `RUN_ID` | `local-run` | Identifier attached to the test run |
| `POD_NAME` | `pod-local` | Worker or pod identifier |
| `S3_ENDPOINT` | unset | S3-compatible endpoint; leave unset to disable uploads |
| `S3_ACCESS_KEY` | unset | S3 access key |
| `S3_SECRET_KEY` | unset | S3 secret key |
| `S3_BUCKET` | unset | Destination bucket |
| `S3_REGION` | unset | S3 region when required by the provider |

See [`docker-compose.yml`](docker-compose.yml) for a complete local example and [`k6/k8s/`](k6/k8s/) for Kubernetes manifests.

## Development

```bash
make build-sidecar  # build the Go sidecar
make test-unit      # run unit tests with the race detector
make test-e2e       # run end-to-end tests
make vet            # run go vet
make fmt            # format Go source files
```

CI runs formatting, vet, build, unit tests, end-to-end tests, and a Docker image build.

## Project status and contributing

The project is currently being prepared for broader contributions. GitHub Issues are temporarily disabled while the core behavior and public roadmap settle. If you would like to contribute in the meantime, open a pull request with a focused change and a clear description of how it was tested.

If this toolkit is useful to you, consider starring the repo — it helps others discover the project.

[![Star History Chart](https://api.star-history.com/svg?repos=sibukixxx/duckdb-load-testing-toolkit&type=Date)](https://star-history.com/#sibukixxx/duckdb-load-testing-toolkit&Date)

## License

Distributed under the [MIT License](LICENSE).
