# DuckDB Load Testing Toolkit

k6 + Kubernetes + DuckDB を組み合わせた負荷テストパイプラインのテンプレートです。

---

## なぜこのツールキットが必要か？

### 従来の負荷テストの課題

負荷テストツール（k6、JMeter、Gatlingなど）は優れたメトリクス収集機能を持っていますが、以下の課題があります：

1. **集計済みデータしか残らない**: 平均、P95、P99などの集計値は得られるが、個々のリクエストデータは失われる
2. **後から別の切り口で分析できない**: テスト実行後に「特定のエンドポイントだけ」「特定の時間帯だけ」といった分析が困難
3. **分散環境でのデータ統合が面倒**: 複数のk6インスタンスからのデータを統合するには追加のインフラ（InfluxDB、Grafanaなど）が必要
4. **軽量な可視化手段がない**: 結果を確認するためだけに重厚な監視スタックを構築する必要がある

### このツールキットのアプローチ

```
従来: k6 → 集計メトリクス → InfluxDB → Grafana（重厚）
本ツールキット: k6 → 生データ → DuckDB → ブラウザで分析（軽量）
```

**すべてのリクエストを生データとしてDuckDBに保存**することで：

- テスト後に自由なSQLクエリで分析可能
- 単一の`.duckdb`ファイルとして持ち運び可能
- duckdb-wasmでブラウザ上でも分析可能（サーバー不要）
- 過去のテスト結果との比較・回帰検知が容易

### ユースケース

| シナリオ | このツールキットの利点 |
|---------|----------------------|
| **本番リリース前の性能検証** | ベースラインと比較して性能劣化を自動検出 |
| **ボトルネック調査** | DNS/TCP/TLS/TTFBの詳細タイミングで原因特定 |
| **負荷テスト結果の共有** | `.duckdb`ファイル1つを渡すだけで分析環境を再現 |
| **CI/CDへの組み込み** | 軽量なサイドカーパターンで既存パイプラインに統合 |
| **オフライン環境での分析** | インターネット接続不要でブラウザ分析可能 |

---

## 設計思想

### 1. サイドカーパターン

k6とメトリクス収集を同一Pod内で動作させることで：
- ネットワークレイテンシの影響を最小化
- k6の実行に影響を与えずにメトリクスを収集
- Podごとに独立したDuckDBファイルを生成

### 2. DuckDBの選択理由

| 特性 | メリット |
|------|---------|
| **組み込み型** | 外部DBサーバー不要、単一バイナリで動作 |
| **列指向** | 分析クエリ（集計、フィルタ）が高速 |
| **SQLインターフェース** | 学習コストが低い、既存の知識を活用 |
| **WASM対応** | ブラウザ上でもネイティブ並みの速度で動作 |
| **ファイルベース** | 結果の保存・共有・バージョン管理が容易 |

### 3. S3互換ストレージ

RustFS/MinIO/AWS S3のいずれにも対応することで：
- ローカル開発: RustFS/MinIOで手軽に検証
- 本番環境: AWS S3で堅牢なストレージ
- オンプレミス: MinIOで自前運用も可能

---

## アーキテクチャ概要

```
┌─────────────────────────────────────────────────────────────┐
│                     Kubernetes Pod                          │
│  ┌──────────────┐         ┌──────────────────────────────┐ │
│  │     k6       │  HTTP   │       sidecar-go             │ │
│  │  (負荷生成)   │────────>│  メトリクス収集 → DuckDB保存  │ │
│  └──────────────┘         └──────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
                                     │
                                     │ S3互換API
                                     ▼
                         ┌──────────────────────┐
                         │  RustFS / AWS S3     │
                         │  (S3互換ストレージ)   │
                         └──────────────────────┘
                                     │
                                     ▼
                            ┌───────────────┐
                            │ Aggregate Job │  (複数DBを統合)
                            └───────────────┘
                                     │
                                     ▼
                            ┌───────────────┐
                            │   Frontend    │  (duckdb-wasmで
                            │               │   ブラウザ分析)
                            └───────────────┘
```

### データフロー詳細

1. **k6** が対象サーバーにリクエストを送信
2. 各リクエストの詳細メトリクス（RTT, DNS, TCP, TLS, TTFB等）を **Sidecar** に POST
3. Sidecar がメモリバッファに蓄積、5秒ごとに **DuckDB** にフラッシュ
4. テスト完了後、`.duckdb` ファイルを **S3互換ストレージ** にアップロード
5. **Aggregate Job** が複数Podの `.duckdb` を1つに統合
6. **Frontend** でブラウザ上からSQLクエリによる分析

### 主な機能

- **k6**: 負荷テストの実行とメトリクス生成
- **Sidecar (Go)**: リクエストごとのメトリクスをDuckDBに保存、S3互換ストレージへアップロード
- **Aggregate Job**: 複数Podから生成された`.duckdb`ファイルを1つに統合
- **Frontend**: duckdb-wasmを使ってブラウザ上で結果を可視化
- **RustFS対応**: S3互換APIによりRustFS、MinIO、AWS S3いずれも利用可能

### 高度な機能

- **詳細メトリクス**: DNS解決、TCP接続、TLSハンドシェイク、TTFB等の詳細タイミング
- **シナリオテスト**: 認証フロー、複数エンドポイント、データ駆動テスト対応
- **回帰検知**: ベースラインとの自動比較、性能劣化の検出
- **分散オーケストレーション**: 複数Podの一括管理、統合flush
- **リアルタイム監視**: WebSocketによるライブメトリクスストリーミング

## ディレクトリ構成

```
├── sidecar-go/              # Go製サイドカー
│   ├── models/              # データモデル
│   ├── storage/             # DuckDB・S3ストレージ
│   ├── handlers/            # HTTPハンドラー
│   ├── analysis/            # 比較分析・回帰検知
│   ├── orchestrator/        # 分散オーケストレーション
│   └── realtime/            # WebSocketリアルタイム監視
├── k6/                      # k6スクリプト
│   ├── k6-script.js         # 基本スクリプト
│   ├── scenarios/           # 高度なシナリオ
│   │   ├── base.js          # 共通モジュール
│   │   ├── auth-flow.js     # 認証フローシナリオ
│   │   ├── multi-endpoint.js # 複数エンドポイント
│   │   └── data-driven.js   # データ駆動テスト
│   └── k8s/                 # Kubernetesマニフェスト
├── frontend/                # duckdb-wasmフロントエンド
├── aggregate-job/           # 複数DBファイル統合ジョブ
├── docker-compose.yml       # ローカル開発用
└── scripts/                 # ヘルパースクリプト
```

## 前提条件

- Go 1.24以上
- Node.js 18以上
- Docker / Docker Compose
- k6（`brew install k6` でインストール）
- kubectl（Kubernetes使用時）

## クイックスタート（Docker Compose + RustFS）

最も簡単にローカルで試す方法です。

```bash
# RustFS + Sidecar を起動
docker compose up -d

# バケットを作成（初回のみ）
docker compose exec rustfs mc alias set local http://localhost:9000 rustfs-user rustfs-password
docker compose exec rustfs mc mb local/loadtest

# k6でテスト実行
cd k6
SIDECAR_BASE=http://localhost:8081 \
TARGET=https://httpbin.org/get \
RUN_ID=test-run \
k6 run k6-script.js

# アップロード実行
curl -X POST http://localhost:8081/api/v1/flush-upload

# RustFS Console で確認: http://localhost:9001
# ユーザー: rustfs-user / パスワード: rustfs-password
```

## ローカル開発（手動セットアップ）

### 1. RustFSの起動

```bash
docker run -d \
  --name rustfs \
  -p 9000:9000 \
  -p 9001:9001 \
  -e RUSTFS_ROOT_USER=rustfs-user \
  -e RUSTFS_ROOT_PASSWORD=rustfs-password \
  rustfs/rustfs server /data --console-address ":9001"

# バケット作成
docker exec rustfs mc alias set local http://localhost:9000 rustfs-user rustfs-password
docker exec rustfs mc mb local/loadtest
```

### 2. Sidecarのビルドと起動

```bash
cd sidecar-go
go build -o duckdb-sidecar

# RustFS接続設定付きで起動
S3_ENDPOINT=http://localhost:9000 \
S3_ACCESS_KEY=rustfs-user \
S3_SECRET_KEY=rustfs-password \
S3_BUCKET=loadtest \
RUN_ID=test-run \
POD_NAME=local-pod \
./duckdb-sidecar
```

### 3. k6でテスト実行

```bash
# k6をインストール（未インストールの場合）
brew install k6  # macOS

# テスト実行
cd k6
SIDECAR_BASE=http://localhost:8081 \
TARGET=https://httpbin.org/get \
RUN_ID=test-run \
POD_NAME=local-k6 \
k6 run k6-script.js
```

### 4. 結果の確認

```bash
# アップロード実行
curl -X POST http://localhost:8081/api/v1/flush-upload

# RustFS Consoleで確認: http://localhost:9001
# または直接ダウンロード
curl http://localhost:8081/api/v1/download -o result.duckdb

# DuckDB CLIで確認
duckdb result.duckdb
SELECT status, COUNT(*) as cnt, AVG(rtt) as avg_rtt FROM metrics GROUP BY status;
```

### 5. フロントエンドで可視化

```bash
cd frontend
npm install
npm start
# → http://localhost:8080 でブラウザを開き、.duckdbファイルを選択
```

## Kubernetes環境での使用

### 1. Sidecar Dockerイメージのビルド

```bash
export IMAGE_NAME=your-registry/duckdb-sidecar:latest
./scripts/build_and_push_sidecar.sh
```

### 2. Kubernetesマニフェストの設定

`k6/k8s/deployment-k6-sidecar.yaml` を編集:

```yaml
- name: sidecar
  image: your-registry/duckdb-sidecar:latest
  env:
  # RustFS設定
  - name: S3_ENDPOINT
    value: "http://rustfs.default.svc:9000"
  - name: S3_ACCESS_KEY
    valueFrom:
      secretKeyRef:
        name: rustfs-credentials
        key: access-key
  - name: S3_SECRET_KEY
    valueFrom:
      secretKeyRef:
        name: rustfs-credentials
        key: secret-key
  - name: S3_BUCKET
    value: "loadtest"
  - name: RUN_ID
    value: "run-20240101-001"
```

### 3. デプロイ

```bash
# シークレット作成
kubectl create secret generic rustfs-credentials \
  --from-literal=access-key=rustfs-user \
  --from-literal=secret-key=rustfs-password

# ConfigMapを作成
kubectl apply -f k6/k8s/configmap-k6-scripts.yaml

# Deploymentを作成
kubectl apply -f k6/k8s/deployment-k6-sidecar.yaml
```

### 4. 複数ファイルの統合

```bash
kubectl apply -f aggregate-job/aggregate-scripts-configmap.yaml
kubectl apply -f aggregate-job/job-aggregate.yaml
# → RustFSに combined.duckdb が作成される
```

## Sidecar API

### 基本エンドポイント

| エンドポイント | メソッド | 説明 |
|---------------|---------|------|
| `/api/v1/ingest` | POST | 単一イベントを受信 |
| `/api/v1/ingest/batch` | POST | バッチイベントを受信 |
| `/api/v1/flush` | POST | バッファをDuckDBにフラッシュ |
| `/api/v1/flush-upload` | POST | フラッシュ後にS3にアップロード |
| `/api/v1/download` | GET | DuckDBファイルをダウンロード |
| `/api/v1/stats` | GET | 現在の統計情報を取得 |
| `/api/v1/health` | GET | ヘルスチェック |

### 分析エンドポイント

| エンドポイント | メソッド | 説明 |
|---------------|---------|------|
| `/api/v1/analysis/compare` | POST | 2つのランを比較して回帰を検出 |
| `/api/v1/analysis/run-stats` | GET | 特定ランの統計情報を取得 |
| `/api/v1/analysis/trend` | GET | 履歴トレンドを取得 |
| `/api/v1/analysis/baseline` | POST | 複数ランからベースラインを計算 |

### リアルタイムエンドポイント

| エンドポイント | プロトコル | 説明 |
|---------------|----------|------|
| `/ws` | WebSocket | リアルタイムメトリクスストリーム |

### 詳細イベントJSON形式

```json
{
  "run_id": "test-run",
  "ts": 1704067200000,
  "pod_id": "k6-pod-1",
  "vu": 1,
  "iter": 100,
  "method": "POST",
  "url": "https://example.com/api",
  "name": "login",
  "status": 200,
  "body_len": 1024,
  "rtt": 123.45,
  "dns_lookup": 5.0,
  "tcp_connect": 10.0,
  "tls_handshake": 20.0,
  "ttfb": 80.0,
  "content_transfer": 8.45,
  "request_size": 256,
  "response_size": 1280,
  "error_code": "",
  "error_msg": "",
  "tags": {
    "region": "us-east-1",
    "scenario": "auth_flow"
  }
}
```

## DuckDBスキーマ（拡張版）

```sql
CREATE TABLE metrics (
    -- 識別情報
    ts BIGINT,              -- タイムスタンプ（ミリ秒）
    run_id VARCHAR,         -- テスト実行ID
    pod_id VARCHAR,         -- Pod名
    vu INTEGER,             -- Virtual User ID
    iter INTEGER,           -- イテレーション番号

    -- リクエスト情報
    method VARCHAR,         -- HTTPメソッド
    url VARCHAR,            -- リクエストURL
    name VARCHAR,           -- シナリオ/リクエスト名

    -- レスポンス情報
    status INTEGER,         -- HTTPステータスコード
    body_len INTEGER,       -- レスポンスボディサイズ

    -- 詳細タイミング（ミリ秒）
    rtt DOUBLE,             -- トータルRTT
    dns_lookup DOUBLE,      -- DNS解決時間
    tcp_connect DOUBLE,     -- TCP接続時間
    tls_handshake DOUBLE,   -- TLSハンドシェイク時間
    ttfb DOUBLE,            -- Time to First Byte
    content_transfer DOUBLE, -- コンテンツ転送時間

    -- サイズメトリクス
    request_size INTEGER,   -- リクエストサイズ
    response_size INTEGER,  -- レスポンスサイズ

    -- エラー情報
    error_code VARCHAR,     -- エラーコード
    error_msg VARCHAR,      -- エラーメッセージ

    -- カスタムタグ（JSON）
    tags VARCHAR
);
```

## 環境変数

### Sidecar

| 変数名 | デフォルト | 説明 |
|--------|----------|------|
| `RUN_ID` | `local-run` | テスト実行ID |
| `POD_NAME` | `pod-local` | Pod識別子 |
| `S3_BUCKET` | - | アップロード先バケット名 |
| `S3_ENDPOINT` | - | S3互換エンドポイント（RustFS: `http://host:9000`） |
| `S3_ACCESS_KEY` | - | アクセスキー |
| `S3_SECRET_KEY` | - | シークレットキー |
| `S3_REGION` | `us-east-1` | リージョン |
| `PORT` | `8081` | リッスンポート |

### k6

| 変数名 | デフォルト | 説明 |
|--------|----------|------|
| `SIDECAR_BASE` | `http://localhost:8081` | Sidecar URL |
| `TARGET` | `https://httpbin.org/get` | テスト対象URL |
| `RUN_ID` | `local-run` | テスト実行ID |
| `POD_NAME` | `k6-vu` | Pod識別子 |

## 分析クエリ例

```sql
-- ステータスコード別の集計
SELECT
    status,
    COUNT(*) as request_count,
    AVG(rtt) as avg_rtt_ms,
    PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY rtt) as p95_rtt_ms,
    PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY rtt) as p99_rtt_ms
FROM metrics
GROUP BY status
ORDER BY status;

-- 時系列でのスループット（1秒ごと）
SELECT
    (ts / 1000) * 1000 as time_bucket,
    COUNT(*) as rps,
    AVG(rtt) as avg_rtt
FROM metrics
GROUP BY time_bucket
ORDER BY time_bucket;

-- Pod別のパフォーマンス比較
SELECT
    pod_id,
    COUNT(*) as total_requests,
    AVG(rtt) as avg_rtt,
    MAX(rtt) as max_rtt
FROM metrics
GROUP BY pod_id;
```

## ストレージの選択

| ストレージ | 用途 | 設定 |
|-----------|------|------|
| **RustFS** | ローカル開発、オンプレ | `S3_ENDPOINT=http://rustfs:9000` |
| **MinIO** | ローカル開発、オンプレ | `S3_ENDPOINT=http://minio:9000` |
| **AWS S3** | 本番環境 | `S3_ENDPOINT` を設定しない（IAMロール使用） |

---

## 典型的なワークフロー

### 1. ベースライン計測

```bash
# 安定した状態でベースラインを計測
RUN_ID=baseline-v1.0 k6 run k6-script.js
curl -X POST http://localhost:8081/api/v1/flush-upload
```

### 2. 変更後の計測

```bash
# コード変更後に再計測
RUN_ID=after-optimization k6 run k6-script.js
curl -X POST http://localhost:8081/api/v1/flush-upload
```

### 3. 比較分析

```bash
# API経由で回帰検出
curl -X POST http://localhost:8081/api/v1/analysis/compare \
  -H "Content-Type: application/json" \
  -d '{"baseline_run_id": "baseline-v1.0", "current_run_id": "after-optimization"}'
```

または、DuckDB CLIで直接比較：

```sql
-- 2つのランを比較
SELECT
    run_id,
    COUNT(*) as requests,
    AVG(rtt) as avg_rtt,
    PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY rtt) as p95
FROM metrics
WHERE run_id IN ('baseline-v1.0', 'after-optimization')
GROUP BY run_id;
```

---

## トラブルシューティング

| 症状 | 原因 | 対処 |
|------|------|------|
| Sidecarに接続できない | ポートが開いていない | `docker compose ps` でコンテナ状態を確認 |
| S3アップロード失敗 | バケットが存在しない | `mc mb local/loadtest` でバケット作成 |
| DuckDBファイルが空 | flushされていない | `/api/v1/flush` を明示的に呼び出す |
| フロントエンドでファイルが読めない | CORSまたはファイル形式 | ブラウザのコンソールでエラーを確認 |

---

## 発展的な使い方

### CI/CDパイプラインへの統合

GitHub Actionsの例：

```yaml
- name: Run load test
  run: |
    docker compose up -d
    k6 run k6/k6-script.js
    curl -X POST http://localhost:8081/api/v1/flush-upload

- name: Check for regression
  run: |
    result=$(curl -s -X POST http://localhost:8081/api/v1/analysis/compare ...)
    if echo "$result" | jq -e '.has_regression'; then
      echo "Performance regression detected!"
      exit 1
    fi
```

### カスタムメトリクスの追加

k6スクリプトで `tags` フィールドを活用：

```javascript
http.post(SIDECAR_BASE + '/api/v1/ingest', JSON.stringify({
  // ... 基本フィールド
  tags: {
    scenario: 'checkout',
    user_type: 'premium',
    region: 'ap-northeast-1'
  }
}));
```

後からタグでフィルタして分析：

```sql
SELECT AVG(rtt) FROM metrics
WHERE json_extract_string(tags, '$.user_type') = 'premium';
```

---

## 注意事項

- 本テンプレートはローカル開発・テスト用です
- 本番環境では認証情報をSecretで管理してください
- RustFSのデフォルト認証情報は必ず変更してください
- 大量のリクエスト（数百万以上）を記録する場合はディスク容量に注意

---

## 関連リソース

- [k6 Documentation](https://k6.io/docs/)
- [DuckDB Documentation](https://duckdb.org/docs/)
- [duckdb-wasm](https://github.com/duckdb/duckdb-wasm)

## ライセンス

MIT License
