# DuckDB 负载测试工具包

[English](README.md) | 简体中文

[![CI](https://github.com/sibukixxx/duckdb-load-testing-toolkit/actions/workflows/ci.yml/badge.svg)](https://github.com/sibukixxx/duckdb-load-testing-toolkit/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go)](sidecar-go/go.mod)

这是一个便携式负载测试流水线：使用 [k6](https://grafana.com/docs/k6/latest/) 采集每个请求的指标，将数据保存到 [DuckDB](https://duckdb.org/)，上传至兼容 S3 的对象存储，并且无需长期运行监控平台即可进行分析。

> [!NOTE]
> `/api/v1` 下的 Sidecar API 和 `metrics` 表结构属于兼容性边界。对这两者的不兼容变更必须使用新的 API 版本，或提供迁移说明。仓库中的部署清单是可定制的示例；用于生产环境前，请检查镜像标签、凭据和资源限制。

## 为什么使用本项目？

许多负载测试仪表盘只保留预聚合指标，测试结束后便难以从新的维度重新分析。本工具包为每个请求保存一行原始数据，因此可以：

- 使用 SQL 按接口、状态码、Pod 或时间范围进行调查；
- 分析 DNS、TCP、TLS、TTFB 和请求总耗时；
- 合并多个 Kubernetes Pod 生成的测试结果；
- 将一次测试保存为单个 `.duckdb` 文件，便于共享；
- 在本地或浏览器中使用 DuckDB-Wasm 分析结果；
- 与基线对比并检测性能回归。

## 工作原理

```text
┌──────────────── Kubernetes Pod ────────────────┐
│                                                │
│  k6 ── 请求指标 ──▶ Go Sidecar ──▶ DuckDB      │
│                                                │
└────────────────────────────────────────────────┘
                                      │
                                      ▼
                              S3 兼容对象存储
                                      │
                         ┌────────────┴────────────┐
                         ▼                         ▼
                      聚合作业                  浏览器前端
```

k6 脚本将请求指标发送给 Go Sidecar。Sidecar 在内存中缓冲数据，定期写入 DuckDB，并可将数据库上传到 RustFS、MinIO 或 Amazon S3。可选的 Kubernetes Job 能够合并分布式工作节点生成的数据库，前端则通过 DuckDB-Wasm 在浏览器中打开测试结果。

## 目录结构

| 路径 | 用途 |
| --- | --- |
| `sidecar-go/` | HTTP 数据接收 API、DuckDB 存储、S3 上传、结果分析和 WebSocket 实时更新 |
| `k6/` | 基础及高级 k6 场景，以及 Kubernetes 部署清单 |
| `aggregate-job/` | 合并分布式 DuckDB 结果文件的 Kubernetes Job |
| `frontend/` | 基于 DuckDB-Wasm 和 Chart.js 的浏览器结果查看器 |
| `docker-compose.yml` | 本地 RustFS 与 Sidecar 环境 |

## 快速开始

### 前置条件

- 支持 Compose v2 的 Docker
- k6 0.45 或更高版本
- `curl`

只有在本地开发 Sidecar 时才需要 Go 1.24 或更高版本。可选前端需要 Node.js 18 或更高版本。

### 1. 启动本地环境

```bash
docker compose up -d
docker compose ps
```

Sidecar 地址为 `http://localhost:8081`。RustFS 的 S3 API 使用端口 `9000`，管理控制台使用端口 `9001`。

首次运行时创建结果存储桶：

```bash
docker compose exec rustfs mc alias set local http://localhost:9000 rustfs-user rustfs-password
docker compose exec rustfs mc mb --ignore-existing local/loadtest
```

以上凭据来自 `docker-compose.yml`，仅用于本地开发，请勿在共享或生产环境中使用。

### 2. 运行 k6

在另一个终端中运行：

```bash
SIDECAR_BASE=http://localhost:8081 \
TARGET=https://test.k6.io \
RUN_ID=quickstart \
k6 run k6/k6-script.js
```

只能对自己拥有或已获授权的系统执行负载测试。

### 3. 保存并查询结果

```bash
curl -X POST http://localhost:8081/api/v1/flush-upload
curl http://localhost:8081/api/v1/download -o result.duckdb
duckdb result.duckdb \
  "SELECT status, count(*) AS requests, avg(rtt) AS avg_rtt FROM metrics GROUP BY status"
```

停止本地环境并保留数据卷：

```bash
docker compose stop
```

## 浏览器结果查看器

```bash
cd frontend
npm install
npm start
```

打开 `http://localhost:8080` 并选择 DuckDB 结果文件。所有查询都在浏览器本地执行。

## Sidecar API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/v1/health` | 健康检查 |
| `POST` | `/api/v1/ingest` | 写入一个请求指标 |
| `POST` | `/api/v1/ingest/batch` | 批量写入请求指标 |
| `POST` | `/api/v1/flush` | 将缓冲数据写入 DuckDB |
| `POST` | `/api/v1/flush-upload` | 写入缓冲数据并上传数据库 |
| `GET` | `/api/v1/download` | 下载当前数据库 |
| `GET` | `/api/v1/stats` | 获取当前测试统计信息 |
| `POST` | `/api/v1/analysis/compare` | 对比测试结果 |
| `GET` | `/api/v1/analysis/run-stats` | 获取一次测试的统计信息 |
| `GET` | `/api/v1/analysis/trend` | 获取指标趋势 |
| `POST` | `/api/v1/analysis/baseline` | 计算基线 |
| `GET` | `/ws` | 通过 WebSocket 接收实时指标 |

## 配置

Sidecar 通过环境变量进行配置。

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `PORT` | `8081` | HTTP 服务端口 |
| `DATA_DIR` | `/data` | DuckDB 文件目录 |
| `RUN_ID` | `local-run` | 测试运行标识符 |
| `POD_NAME` | `pod-local` | 工作节点或 Pod 标识符 |
| `S3_ENDPOINT` | 未设置 | S3 兼容端点；留空则使用 AWS 默认端点 |
| `S3_ACCESS_KEY` | 未设置 | S3 Access Key |
| `S3_SECRET_KEY` | 未设置 | S3 Secret Key |
| `S3_BUCKET` | 未设置 | 目标存储桶；留空则禁用上传 |
| `S3_REGION` | 未设置 | 服务商要求的 S3 区域，默认使用 `us-east-1` |

完整的本地示例请参阅 [`docker-compose.yml`](docker-compose.yml)，Kubernetes 清单位于 [`k6/k8s/`](k6/k8s/)。

## 开发

```bash
make build-sidecar  # 构建 Go Sidecar
make test-unit      # 使用 race detector 运行单元测试
make test-e2e       # 运行端到端测试
make vet            # 运行 go vet
make fmt            # 格式化 Go 源代码
make fmt-check      # 检查格式但不修改文件
```

CI 会检查格式、运行 `go vet`、构建程序、执行单元测试和端到端测试，并验证 Docker 镜像能够成功构建。

## 项目状态与贡献

本工具包可在上述兼容性保证下使用。项目会继续通过向后兼容的改进和带迁移说明的变更持续开发。GitHub Issues 当前未开放；如需贡献，请提交范围明确的 Pull Request，并说明测试方式。

如果本工具包对你有帮助，欢迎为仓库点亮 Star，让更多使用 k6 的用户发现它。

## 许可证

本项目使用 [MIT License](LICENSE)。
