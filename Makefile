# Victoria — developer task runner.
# `make check` runs everything CI runs.

BINARY        := victoria
PKG           := ./...
# A gateway token is required at startup; this default is for local runs only.
GATEWAY_TOKEN ?= dev-token

.PHONY: all build build-dev run test test-race test-integration \
        fmt fmt-check vet lint vuln tidy tools docker clean check help

all: build

## build: compile the production binary (no dev endpoints)
build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BINARY) ./cmd/victoria

## build-dev: compile with -tags dev (mounts /admin/dev/* demo routes)
build-dev:
	CGO_ENABLED=0 go build -tags dev -o $(BINARY)-dev ./cmd/victoria

## run: run locally with the in-memory store
run:
	VICTORIA_GATEWAY_INBOUND_TOKEN=$(GATEWAY_TOKEN) go run ./cmd/victoria

## test: unit + e2e tests (whatsmeow disabled, no external deps)
test:
	VICTORIA_WHATSAPP_DISABLED=1 go test $(PKG)

## test-race: same as test, with the race detector
test-race:
	VICTORIA_WHATSAPP_DISABLED=1 go test -race $(PKG)

## test-integration: Postgres-backed tests (needs VICTORIA_TEST_DATABASE_URL)
test-integration:
	go test -count=1 ./internal/store/postgres

## fmt: format all Go files
fmt:
	gofmt -w .

## fmt-check: fail if any file is not gofmt-clean
fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "These files need gofmt:"; echo "$$unformatted"; exit 1; \
	fi

## vet: go vet both the prod and dev builds
vet:
	go vet $(PKG)
	go vet -tags dev $(PKG)

## lint: run golangci-lint (install with `make tools`)
lint:
	golangci-lint run

## vuln: scan dependencies for known vulnerabilities
vuln:
	govulncheck ./...

## tidy: tidy go.mod / go.sum
tidy:
	go mod tidy

## tools: install dev tooling (golangci-lint, govulncheck)
tools:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest

## docker: build the container image
docker:
	docker build -t victoria:dev .

## clean: remove build artifacts
clean:
	rm -f $(BINARY) $(BINARY)-dev coverage.out

## check: everything CI runs (fmt, vet, lint, tests)
check: fmt-check vet lint test
	@echo "✓ all checks passed"

## help: list targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'
