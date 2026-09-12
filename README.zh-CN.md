# DuckDB 负载测试工具包

[English](README.md) | [日本語](README.ja.md) | 简体中文

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
- 与基线对比并检测性能回归；
- 获得结构化、确定性的回归原因说明——具体是哪个接口、哪个耗时阶段、哪些 Pod；
- 用一份纯 YAML 策略文件驱动 PASS/WARN/FAIL 判定，为 CI 流水线设置性能门禁。

**基于请求级负载测试数据的可移植性能分析。** 无需常设的可观测性平台即可完成运行、保存、共享、查询、对比与诊断。

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
| `sidecar-go/analysis/` | 接口级分析、瓶颈证据、回归检测、原因说明，以及 SQL 查询目录 |
| `sidecar-go/analysis/gate/` | 性能门禁的判定引擎（PASS/WARN/FAIL），不依赖任何 CI 提供方 |
| `sidecar-go/analysis/policy/` | 性能策略的 Schema（YAML/JSON）与加载器 |
| `sidecar-go/analysis/fixtures/` | 确定性的合成性能夹具，无需真实负载测试即可验证分析与门禁功能 |
| `sidecar-go/cmd/duckload/` | `duckload`——直接分析 `.duckdb` 结果文件的离线命令行工具 |
| `docs/` | 性能门禁、策略 Schema、CI 集成参考文档；可直接复制使用的示例策略与 GitHub Actions 工作流 |
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

### 4. 分析与对比

用不同的 `RUN_ID`（例如 `after-change`）再次运行测试，下载该结果（由于 Sidecar 进程在两次运行之间未重启，两个 run_id 的数据都会写入同一个数据库文件），然后对比两次测试：

```bash
curl -X POST http://localhost:8081/api/v1/analysis/compare \
  -H 'Content-Type: application/json' \
  -d '{"baseline_run_id": "quickstart", "current_run_id": "after-change"}'
```

### 5. 诊断回归原因

```bash
curl -X POST http://localhost:8081/api/v1/analysis/diagnose \
  -H 'Content-Type: application/json' \
  -d '{"baseline_run_id": "quickstart", "current_run_id": "after-change"}'
```

### 6. 用性能门禁判定本次构建

```bash
cd sidecar-go && go build -o duckload ./cmd/duckload
./duckload gate \
  --current result.duckdb --run-id after-change \
  --baseline result.duckdb --baseline-run-id quickstart \
  --policy ../docs/examples/performance-policy.example.yml
echo "exit code: $?"
```

停止本地环境并保留数据卷：

```bash
docker compose stop
```

## 分析（Analysis）

除了运行级别的 `compare`/`run-stats`/`trend`/`baseline` 接口外，Sidecar 还能按接口分析一次测试并说明回归原因：

- **接口分析**——每个接口的请求/错误数、延迟百分位数（p50/p90/p95/p99）、状态码分布，以及 DNS/TCP/TLS/TTFB/传输的平均耗时。
- **瓶颈证据**——对比基线与当前测试时，找出每个接口耗时变化最大的阶段（DNS、TCP、TLS、TTFB 或传输）。这里只提供证据，不猜测根本原因。
- **回归检测**——在接口级别生成可供程序判断的结果（`{metric, scope, baseline, current, change_percent, severity}`），使用与运行级对比相同的可配置阈值。
- **原因说明（Explain）**——为每个发生回归的接口生成结构化、确定性的"性能证据摘要"：p95 变化、变化最大的耗时阶段、明显偏离同伴的 Pod，以及错误率变化。这些都是纯 SQL/Go 计算，不涉及大语言模型。

每条分析 SQL 都保存在 `sidecar-go/analysis/queries/sql/` 中，作为带有说明（用途、所需列、参数、预期输出）的独立文档化产物，而不是嵌在 handler 代码里的字符串。轻量级验证器（`sidecar-go/analysis/validator/`）会检查每条查询所需的列、绑定参数、执行 `EXPLAIN`、运行查询并比对结果列——针对七种确定性的合成夹具（`sidecar-go/analysis/fixtures/`：healthy、latency-regression、error-spike、network-regression、backend-regression、pod-outlier、mixed-regression），从而无需真实负载测试即可在 CI 中发现损坏的分析查询。

接口身份统一使用 `COALESCE(NULLIF(name, ''), url)`：k6 已经为每个请求打上了不受基数爆炸影响的 `name` 标签（例如 `login`、`get_profile`，参见 `k6/scenarios/`），若直接按原始 `url` 分组，带查询参数或路径参数的请求会被误判为各不相同的接口。

## 性能门禁（Performance Gate）

性能门禁（`sidecar-go/analysis/gate`）把一次接口分析转化为单一的、可供机器判断的结论——**PASS**、**WARN** 或 **FAIL**——由一份纯 YAML/JSON 策略文件驱动，因此 CI 无需任何厂商特定的判定逻辑即可在真正发生性能回归时阻止合并：

```bash
duckload gate \
  --current current.duckdb --baseline baseline.duckdb \
  --policy performance-policy.yml
```

```
PERFORMANCE GATE: FAIL
14 passed, 2 warned, 1 failed, 0 unknown

FAIL    /api/users               p95 (regression)
        210.00ms -> 340.00ms  (+61.90%)
        allowed: +20.00%
        primary timing change: TTFB +96.00ms
```

- **每个指标两项独立检查**：绝对的 **Performance Budget**（例如"p95 必须低于 500ms"，无需基线）与相对基线的**回归**检查——同一接口可以一项失败、另一项通过。
- **天生抗噪声**：相对阈值、绝对毫秒级下限、最小样本量三者必须同时满足才会被判定为回归——详见 [`docs/performance-gate.md`](docs/performance-gate.md#noise-resistance)。
- **绝不用不足的证据妄下判断**：样本不足，或基线中不存在的接口，都会得到 `UNKNOWN` 结论（可通过 `unknown_treatment` 配置），而不是误判为 PASS 或 FAIL。
- **一套判定引擎，三个入口**：`duckload gate`、`POST /api/v1/analysis/gate` 以及该包自身的测试，调用的都是同一个 `gate.Evaluate`——CI 中的失败结果永远可以用同一条命令在本地复现。
- **退出码约定**：`0` = PASS（或默认情况下的 WARN）、`1` = FAIL（或使用 `--warn-exit-code 1` 时的 WARN）、`2` = 工具/输入错误（绝不代表性能判定）——详见 [`docs/performance-gate.md`](docs/performance-gate.md#exit-code-contract)。

完整约定见 [`docs/performance-gate.md`](docs/performance-gate.md)，策略 Schema 见 [`docs/performance-policy.md`](docs/performance-policy.md)（附带可直接复制的[示例](docs/examples/performance-policy.example.yml)），CI 接入方式见 [`docs/ci-integration.md`](docs/ci-integration.md)（其中包含一份完整的 [GitHub Actions 示例](docs/examples/github-actions-performance-gate.yml)——请自行复制到 `.github/workflows/` 下）。

## CLI（`duckload`）

`duckload` 可以直接分析 `.duckdb` 结果文件，无需运行 Sidecar——适合文件已下载或共享之后使用。

```bash
cd sidecar-go
go build -o duckload ./cmd/duckload

./duckload summary result.duckdb --run-id quickstart
./duckload endpoints result.duckdb --run-id quickstart
./duckload compare result.duckdb --baseline quickstart --current after-change
./duckload diagnose result.duckdb --baseline quickstart --current after-change
./duckload check-analysis   # 针对每个合成夹具验证所有分析查询

# 性能门禁——详见上方"性能门禁"一节。
# --baseline/--current 既可以指向各自独立的 .duckdb 文件（若文件中只有一个
# run_id 会自动识别），也可以配合 --baseline-run-id/--run-id 指向同一个
# 文件中的某个 run_id。
./duckload gate --current current.duckdb --baseline baseline.duckdb --policy performance-policy.yml
./duckload gate --current result.duckdb --run-id after-change --baseline result.duckdb --baseline-run-id quickstart --policy performance-policy.yml --format json
```

## 浏览器结果查看器

```bash
cd frontend
npm install
npm start
```

打开 `http://localhost:8080` 并选择 DuckDB 结果文件。所有查询都在浏览器本地执行，无需服务端分析。提供四个视图：

- **Summary**——一次测试的整体请求数、错误率和延迟百分位数，附状态码图表。
- **Endpoints**——每个接口的请求/错误数和延迟百分位数。
- **Regression**——基线与当前测试之间每个接口的 p95 与错误率差值。
- **Timing Breakdown**——单个接口的 DNS/TCP/TLS/TTFB/传输平均耗时，基线与当前对比。
- **Gate**——一个最简化的客户端 PASS/WARN/FAIL 检查（一个 p95 回归阈值、一个错误率预算），无需离开浏览器即可对结果做初步判断；完整策略 Schema 请使用 `duckload gate` 或 HTTP 接口。

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
| `POST` | `/api/v1/analysis/compare` | 在整体层面对比两次测试并检测回归 |
| `GET` | `/api/v1/analysis/run-stats` | 获取一次测试的统计信息 |
| `GET` | `/api/v1/analysis/trend` | 获取指标趋势 |
| `POST` | `/api/v1/analysis/baseline` | 从多次测试计算基线 |
| `GET` | `/api/v1/analysis/endpoints` | 获取一次测试的接口级统计信息 |
| `POST` | `/api/v1/analysis/diagnose` | 获取接口级回归结果及结构化原因说明 |
| `POST` | `/api/v1/analysis/gate` | 使用内嵌策略评估[性能门禁](docs/performance-gate.md) |
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
make build-cli      # 构建 duckload 命令行工具
make test-unit      # 使用 race detector 运行单元测试
make test-e2e       # 运行端到端测试
make test-analysis  # 针对每个合成夹具验证所有分析 SQL 查询
make test-gate      # 针对每个合成门禁夹具验证策略 Schema 与判定引擎
make bench-gate     # 100k/1M 请求规模下门禁性能的合成基准测试（不包含在 `make test` 中）
make vet            # 运行 go vet
make fmt            # 格式化 Go 源代码
make fmt-check      # 检查格式但不修改文件
```

CI 会检查格式、运行 `go vet`、构建程序、执行单元测试、分析 SQL 验证和端到端测试，并验证 Docker 镜像能够成功构建。（`test-unit` 使用的 `./analysis/...` 已经涵盖 gate 与 policy 包；`test-gate` 只是方便单独运行这部分的快捷方式。）

## 项目状态与贡献

本工具包可在上述兼容性保证下使用。项目会继续通过向后兼容的改进和带迁移说明的变更持续开发。GitHub Issues 当前未开放；如需贡献，请提交范围明确的 Pull Request，并说明测试方式。

如果本工具包对你有帮助，欢迎为仓库点亮 Star，让更多使用 k6 的用户发现它。

## 许可证

本项目使用 [MIT License](LICENSE)。
