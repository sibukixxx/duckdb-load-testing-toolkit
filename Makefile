SIDECAR_DIR := sidecar-go
BINARY      := $(SIDECAR_DIR)/duckdb-sidecar

.PHONY: build-sidecar test test-unit test-e2e lint fmt fmt-check vet clean docker-build ci zip

# ── Build ──────────────────────────────────────────────────────────────────────

build-sidecar:
	cd $(SIDECAR_DIR) && go build -o duckdb-sidecar

# ── Test ───────────────────────────────────────────────────────────────────────

# Run all tests (unit + E2E)
test: test-unit test-e2e

# Unit tests only (excludes E2E directory)
test-unit:
	cd $(SIDECAR_DIR) && go test \
	  ./analysis/... ./handlers/... ./models/... \
	  ./orchestrator/... ./realtime/... ./storage/... \
	  -v -race -count=1

# E2E tests — set E2E_RUN=TestName to filter (e.g. make test-e2e E2E_RUN=TestE2EFlush)
test-e2e:
	cd $(SIDECAR_DIR) && go test ./test/e2e/tests/... \
	  -v -race -count=1 $(if $(E2E_RUN),-run $(E2E_RUN),)

# ── Code quality ───────────────────────────────────────────────────────────────

vet:
	cd $(SIDECAR_DIR) && go vet ./...

fmt:
	cd $(SIDECAR_DIR) && gofmt -w -l .

fmt-check:
	@files="$$(cd $(SIDECAR_DIR) && gofmt -l .)"; \
	if [ -n "$$files" ]; then \
	  echo "The following files need gofmt:"; \
	  echo "$$files"; \
	  exit 1; \
	fi

# Requires golangci-lint: https://golangci-lint.run/usage/install/
lint:
	cd $(SIDECAR_DIR) && golangci-lint run ./...

# Run vet + unit tests (fast CI gate)
ci: fmt-check vet test-unit

# ── Maintenance ────────────────────────────────────────────────────────────────

clean:
	rm -f $(BINARY)
	rm -rf $(SIDECAR_DIR)/data/*.duckdb $(SIDECAR_DIR)/data/*.duckdb.wal

docker-build:
	cd $(SIDECAR_DIR) && docker build -t duckdb-sidecar .

zip:
	cd /mnt/data && zip -r duckdb-load-testing-toolkit.zip duckdb-load-testing-toolkit
