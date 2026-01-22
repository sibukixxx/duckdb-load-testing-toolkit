FROM golang:1.23-bookworm AS builder

WORKDIR /app

# 依存関係のダウンロード
COPY go.mod go.sum ./
RUN go mod download

# ソースコードのコピーとビルド
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o duckdb-sidecar

# 実行用イメージ
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /app/duckdb-sidecar .

# データディレクトリ作成
RUN mkdir -p /data

EXPOSE 8081

CMD ["./duckdb-sidecar"]
