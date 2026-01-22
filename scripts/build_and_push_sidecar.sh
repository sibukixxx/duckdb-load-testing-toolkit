#!/bin/bash
set -e
IMAGE_NAME=${IMAGE_NAME:-yourrepo/duckdb-sidecar:latest}
docker build -t $IMAGE_NAME ./sidecar-go
docker push $IMAGE_NAME
echo "Built and pushed $IMAGE_NAME"
