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

---

## 初期セットアップ

### 前提条件

| ツール | バージョン | 用途 | インストール確認 |
|--------|----------|------|-----------------|
| **Docker** | 20.10+ | コンテナ実行 | `docker --version` |
| **Docker Compose** | v2.0+ | ローカル環境構築 | `docker compose version` |
| **Go** | 1.24+ | Sidecarビルド | `go version` |
| **k6** | 0.45+ | 負荷テスト実行 | `k6 version` |
| **Node.js** | 18+ | フロントエンド | `node --version` |
| **kubectl** | 1.25+ | K8sデプロイ（オプション） | `kubectl version --client` |
| **DuckDB CLI** | 0.9+ | 結果分析（オプション） | `duckdb --version` |

### ツールのインストール

```bash
# macOS (Homebrew)
brew install go k6 node duckdb

# k6のみ（Linux）
sudo gpg -k
sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69
echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" | sudo tee /etc/apt/sources.list.d/k6.list
sudo apt-get update && sudo apt-get install k6

# DuckDB CLI（Linux）
wget https://github.com/duckdb/duckdb/releases/download/v0.9.2/duckdb_cli-linux-amd64.zip
unzip duckdb_cli-linux-amd64.zip && sudo mv duckdb /usr/local/bin/
```

### リポジトリのクローンと確認

```bash
git clone <repository-url>
cd duckdb-load-testing-template

# ディレクトリ構成の確認
ls -la
# → CLAUDE.md, README.md, docker-compose.yml, sidecar-go/, k6/, frontend/, ...
```

---

## デプロイ方法

このツールキットは3つのデプロイ方法をサポートしています：

| 方法 | 用途 | 複雑さ | スケーラビリティ |
|------|------|--------|-----------------|
| **Docker Compose** | ローカル開発・検証 | 低 | 単一マシン |
| **Kubernetes (RustFS/MinIO)** | オンプレミス・プライベートクラウド | 中 | 高 |
| **Kubernetes (AWS S3)** | AWS本番環境 | 中 | 高 |

---

## 方法1: Docker Compose（ローカル開発）

最も簡単にローカルで試す方法です。RustFS（S3互換ストレージ）とSidecarがすべてコンテナで起動します。

### Step 1: コンテナの起動

```bash
# RustFS + Sidecar を起動
docker compose up -d

# 起動確認（両方が running になるまで待機）
docker compose ps
# NAME             STATUS
# duckdb-sidecar   Up (healthy)
# rustfs           Up (healthy)

# ログ確認（問題がある場合）
docker compose logs -f
```

### Step 2: S3バケットの作成（初回のみ）

```bash
# RustFSコンテナ内でバケットを作成
docker compose exec rustfs mc alias set local http://localhost:9000 rustfs-user rustfs-password
docker compose exec rustfs mc mb local/loadtest

# バケット作成の確認
docker compose exec rustfs mc ls local/
# → loadtest/ が表示される
```

### Step 3: 負荷テストの実行

```bash
# k6でテスト実行（別ターミナルで）
cd k6
SIDECAR_BASE=http://localhost:8081 \
TARGET=https://httpbin.org/get \
RUN_ID=test-$(date +%Y%m%d-%H%M%S) \
k6 run k6-script.js

# テスト中のメトリクス確認
curl http://localhost:8081/api/v1/stats | jq
```

### Step 4: 結果の保存と確認

```bash
# DuckDBファイルをS3にアップロード
curl -X POST http://localhost:8081/api/v1/flush-upload

# ローカルにダウンロード
curl http://localhost:8081/api/v1/download -o result.duckdb

# DuckDB CLIで確認
duckdb result.duckdb "SELECT status, COUNT(*) as cnt, AVG(rtt) as avg_rtt FROM metrics GROUP BY status"

# または RustFS Console で確認
# URL: http://localhost:9001
# ユーザー: rustfs-user
# パスワード: rustfs-password
```

### Step 5: フロントエンドで可視化（オプション）

```bash
cd frontend
npm install
npm run dev
# → http://localhost:8080 を開き、Admin ページから result.duckdb を選択
```

フロントエンドは Vite + Preact + TypeScript + `preact-iso` 構成です。
ルート構成:

- `/` ホーム
- `/offers/:id` Offer 詳細（LP/Offer 検証用の雛形）
- `/cart` 擬似カート
- `/thanks` リード送信後
- `/admin` DuckDB ファイルビューア（旧 `app.js` の機能）

```bash
npm run build      # tsc -b && vite build（preact-iso prerender 付き）
npm run preview    # ビルド成果物をローカルでプレビュー
```

### クリーンアップ

```bash
# コンテナ停止（データは保持）
docker compose stop

# コンテナとボリュームを完全削除
docker compose down -v
```

---

## 方法2: ローカルビルド（手動セットアップ）

Sidecarをローカルでビルドして実行する方法です。デバッグや開発時に有用です。

### Step 1: RustFSの起動

```bash
# RustFSコンテナを起動
docker run -d \
  --name rustfs \
  -p 9000:9000 \
  -p 9001:9001 \
  -e RUSTFS_ROOT_USER=rustfs-user \
  -e RUSTFS_ROOT_PASSWORD=rustfs-password \
  rustfs/rustfs server /data --console-address ":9001"

# 起動確認
docker ps | grep rustfs

# バケット作成
docker exec rustfs mc alias set local http://localhost:9000 rustfs-user rustfs-password
docker exec rustfs mc mb local/loadtest
```

### Step 2: Sidecarのビルドと起動

```bash
cd sidecar-go

# ビルド
go build -o duckdb-sidecar

# テスト実行（オプション）
go test ./...

# Sidecar起動
S3_ENDPOINT=http://localhost:9000 \
S3_ACCESS_KEY=rustfs-user \
S3_SECRET_KEY=rustfs-password \
S3_BUCKET=loadtest \
RUN_ID=test-run \
POD_NAME=local-pod \
./duckdb-sidecar

# 別ターミナルでヘルスチェック
curl http://localhost:8081/api/v1/health
# → {"status":"ok"}
```

### Step 3: k6でテスト実行

```bash
# 別ターミナルで
cd k6
SIDECAR_BASE=http://localhost:8081 \
TARGET=https://httpbin.org/get \
RUN_ID=test-run \
POD_NAME=local-k6 \
k6 run k6-script.js
```

### Step 4: 結果の確認

```bash
# アップロード
curl -X POST http://localhost:8081/api/v1/flush-upload

# ダウンロード
curl http://localhost:8081/api/v1/download -o result.duckdb

# 分析
duckdb result.duckdb
```

---

## 方法3: Kubernetes + RustFS/MinIO（オンプレミス）

Kubernetes環境でRustFS（またはMinIO）をS3互換ストレージとして使用する方法です。

### Step 1: Sidecar Dockerイメージのビルドとプッシュ

```bash
# イメージ名を設定（自分のレジストリに変更）
export IMAGE_NAME=your-registry/duckdb-sidecar:latest

# ビルドとプッシュ
./scripts/build_and_push_sidecar.sh

# または手動で
cd sidecar-go
docker build -t $IMAGE_NAME .
docker push $IMAGE_NAME
```

### Step 2: RustFS/MinIOのデプロイ（クラスタ内に未構築の場合）

```bash
# MinIOをHelmでデプロイする例
helm repo add minio https://charts.min.io/
helm install minio minio/minio \
  --set rootUser=minio-user \
  --set rootPassword=minio-password \
  --set persistence.size=10Gi

# または、既存のS3互換ストレージのエンドポイントを使用
```

### Step 3: シークレットの作成

```bash
# S3認証情報のシークレット作成
kubectl create secret generic s3-credentials \
  --from-literal=access-key=minio-user \
  --from-literal=secret-key=minio-password

# 確認
kubectl get secret s3-credentials -o yaml
```

### Step 4: マニフェストの編集

`k6/k8s/deployment-k6-sidecar.yaml` を編集：

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: k6-load
  labels:
    app: k6-load
spec:
  replicas: 3  # 負荷に応じて調整
  selector:
    matchLabels:
      app: k6-load
  template:
    metadata:
      labels:
        app: k6-load
    spec:
      containers:
      - name: k6
        image: grafana/k6:latest
        args: ["run", "/scripts/k6-script.js"]
        env:
        - name: SIDECAR_BASE
          value: "http://localhost:8081"
        - name: TARGET
          value: "https://your-target-service.com"
        - name: RUN_ID
          value: "run-20240101-001"  # テスト実行ごとに変更
        - name: POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        volumeMounts:
        - name: scripts
          mountPath: /scripts
        resources:
          requests:
            cpu: "500m"
            memory: "256Mi"
          limits:
            cpu: "1000m"
            memory: "512Mi"

      - name: sidecar
        image: your-registry/duckdb-sidecar:latest  # Step 1でプッシュしたイメージ
        env:
        - name: RUN_ID
          value: "run-20240101-001"
        - name: POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: S3_ENDPOINT
          value: "http://minio.default.svc:9000"  # クラスタ内のMinIO
        - name: S3_BUCKET
          value: "loadtest"
        - name: S3_ACCESS_KEY
          valueFrom:
            secretKeyRef:
              name: s3-credentials
              key: access-key
        - name: S3_SECRET_KEY
          valueFrom:
            secretKeyRef:
              name: s3-credentials
              key: secret-key
        ports:
        - containerPort: 8081
        volumeMounts:
        - name: data
          mountPath: /data
        resources:
          requests:
            cpu: "100m"
            memory: "128Mi"
          limits:
            cpu: "500m"
            memory: "512Mi"
        livenessProbe:
          httpGet:
            path: /api/v1/health
            port: 8081
          initialDelaySeconds: 5
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /api/v1/health
            port: 8081
          initialDelaySeconds: 3
          periodSeconds: 5

      volumes:
      - name: scripts
        configMap:
          name: k6-scripts
      - name: data
        emptyDir: {}
```

### Step 5: デプロイ

```bash
# k6スクリプトのConfigMap作成
kubectl apply -f k6/k8s/configmap-k6-scripts.yaml

# Deployment作成
kubectl apply -f k6/k8s/deployment-k6-sidecar.yaml

# 起動確認
kubectl get pods -l app=k6-load -w
# → すべてのPodが Running になるまで待機

# ログ確認
kubectl logs -l app=k6-load -c k6 -f
kubectl logs -l app=k6-load -c sidecar -f
```

### Step 6: テスト完了後のアップロード

```bash
# 各Podのsidecarにflush-uploadを実行
for pod in $(kubectl get pods -l app=k6-load -o jsonpath='{.items[*].metadata.name}'); do
  echo "Uploading from $pod..."
  kubectl exec $pod -c sidecar -- curl -X POST http://localhost:8081/api/v1/flush-upload
done
```

### Step 7: 結果の統合

```bash
# Aggregate Jobを実行して複数のDuckDBファイルを統合
kubectl apply -f aggregate-job/aggregate-scripts-configmap.yaml
kubectl apply -f aggregate-job/job-aggregate.yaml

# Job完了待機
kubectl wait --for=condition=complete job/aggregate-job --timeout=300s

# 結果の確認
kubectl logs job/aggregate-job
# → combined.duckdb が S3 にアップロードされる
```

---

## 方法4: Kubernetes + AWS S3（本番環境）

AWS EKS環境でS3を使用する方法です。IAMロールによる認証を推奨します。

### Step 1: IAMロールの設定（IRSA）

```bash
# EKSクラスタでOIDCプロバイダーを有効化（未設定の場合）
eksctl utils associate-iam-oidc-provider --cluster your-cluster --approve

# S3アクセス用のIAMポリシー作成
cat > s3-policy.json << 'EOF'
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:PutObject",
        "s3:GetObject",
        "s3:ListBucket"
      ],
      "Resource": [
        "arn:aws:s3:::your-loadtest-bucket",
        "arn:aws:s3:::your-loadtest-bucket/*"
      ]
    }
  ]
}
EOF

aws iam create-policy \
  --policy-name LoadTestS3Policy \
  --policy-document file://s3-policy.json

# ServiceAccount用のIAMロール作成
eksctl create iamserviceaccount \
  --name loadtest-sa \
  --namespace default \
  --cluster your-cluster \
  --attach-policy-arn arn:aws:iam::ACCOUNT_ID:policy/LoadTestS3Policy \
  --approve
```

### Step 2: S3バケットの作成

```bash
# バケット作成
aws s3 mb s3://your-loadtest-bucket --region ap-northeast-1

# バケットポリシー確認
aws s3api get-bucket-policy --bucket your-loadtest-bucket
```

### Step 3: マニフェストの編集（AWS S3用）

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: k6-load
spec:
  replicas: 5
  selector:
    matchLabels:
      app: k6-load
  template:
    metadata:
      labels:
        app: k6-load
    spec:
      serviceAccountName: loadtest-sa  # IRSAで作成したServiceAccount
      containers:
      - name: k6
        image: grafana/k6:latest
        args: ["run", "/scripts/k6-script.js"]
        env:
        - name: SIDECAR_BASE
          value: "http://localhost:8081"
        - name: TARGET
          value: "https://your-production-api.com"
        - name: RUN_ID
          value: "prod-run-20240101"
        - name: POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        volumeMounts:
        - name: scripts
          mountPath: /scripts

      - name: sidecar
        image: your-ecr-registry/duckdb-sidecar:latest
        env:
        - name: RUN_ID
          value: "prod-run-20240101"
        - name: POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: S3_BUCKET
          value: "your-loadtest-bucket"
        - name: S3_REGION
          value: "ap-northeast-1"
        # S3_ENDPOINT を設定しない → AWS S3を使用
        # S3_ACCESS_KEY, S3_SECRET_KEY を設定しない → IRSAを使用
        ports:
        - containerPort: 8081
        volumeMounts:
        - name: data
          mountPath: /data

      volumes:
      - name: scripts
        configMap:
          name: k6-scripts
      - name: data
        emptyDir: {}
```

### Step 4: デプロイと実行

```bash
# デプロイ
kubectl apply -f k6/k8s/deployment-k6-sidecar-aws.yaml

# テスト実行を監視
kubectl logs -l app=k6-load -c k6 -f

# 完了後、アップロード
for pod in $(kubectl get pods -l app=k6-load -o jsonpath='{.items[*].metadata.name}'); do
  kubectl exec $pod -c sidecar -- curl -X POST http://localhost:8081/api/v1/flush-upload
done

# S3で結果を確認
aws s3 ls s3://your-loadtest-bucket/prod-run-20240101/
```

---

## 本番環境での考慮事項

### セキュリティ

| 項目 | 推奨事項 |
|------|---------|
| **認証情報** | Kubernetes Secretsまたは外部シークレット管理（AWS Secrets Manager、HashiCorp Vault）を使用 |
| **ネットワーク** | NetworkPolicyでSidecarへのアクセスを制限 |
| **イメージ** | プライベートレジストリからプル、イメージスキャンを実施 |
| **RBAC** | 最小権限の原則に従ったServiceAccount設定 |

### スケーリング

```yaml
# HorizontalPodAutoscaler（オプション）
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: k6-load-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: k6-load
  minReplicas: 1
  maxReplicas: 10
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
```

### リソース見積もり

| コンポーネント | CPU | メモリ | ディスク |
|--------------|-----|--------|---------|
| k6 (per pod) | 500m-1000m | 256Mi-512Mi | - |
| Sidecar (per pod) | 100m-500m | 128Mi-512Mi | 100Mi-1Gi (DuckDB) |
| Aggregate Job | 500m | 1Gi | 10Gi (統合時) |

### モニタリング

```bash
# Sidecarの統計情報を定期取得
watch -n 5 'curl -s http://localhost:8081/api/v1/stats | jq'

# WebSocketでリアルタイム監視（開発時）
websocat ws://localhost:8081/ws
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
