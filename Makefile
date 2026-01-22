.PHONY: build-sidecar zip

build-sidecar:
	cd sidecar-go && go build -o duckdb-sidecar

zip:
	cd /mnt/data && zip -r duckdb-load-testing-template.zip duckdb-load-testing-template
