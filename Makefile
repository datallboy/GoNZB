BINARY_NAME=gonzb
VERSION=$(shell git describe --tags --always --dirty)
BUILD_TIME=$(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
GOOS=$(shell go env GOOS)
GOARCH=$(shell go env GOARCH)
DIST_NAME=$(BINARY_NAME)_$(VERSION)_$(GOOS)_$(GOARCH)
PKG=./cmd/gonzb

LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

.PHONY: all build build-release ui-build clean test test-ci test-postgres vet lint install gonzbnet-e2e-test gonzbnet-e2e-start gonzbnet-e2e-bootstrap gonzbnet-e2e-verify gonzbnet-e2e-stop gonzbnet-e2e-status gonzbnet-e2e-reset

all: build

build: ui-build
	@echo "Building $(DIST_NAME)..."
	GOCACHE=$${GOCACHE:-/tmp/gocache} go build $(LDFLAGS) -o bin/$(DIST_NAME) $(PKG)

build-release: ui-build
	@echo "Building release binaries..."
	@mkdir -p bin
	@for target in linux/amd64 windows/amd64; do \
		GOOS=$${target%/*}; \
		GOARCH=$${target#*/}; \
		EXT=""; \
		if [ "$$GOOS" = "windows" ]; then EXT=".exe"; fi; \
		OUT="bin/$(BINARY_NAME)_$(VERSION)_$$GOOS_$$GOARCH$$EXT"; \
		echo "Building $$OUT"; \
		CGO_ENABLED=0 GOCACHE=$${GOCACHE:-/tmp/gocache} GOOS=$$GOOS GOARCH=$$GOARCH go build $(LDFLAGS) -o "$$OUT" $(PKG); \
	done
		
ui-build:
	@echo "Building embedded web UI..."
	npm --prefix ui run build

test:
	@echo "Running tests..."
	GOCACHE=$${GOCACHE:-/tmp/gocache} go test ./...

test-ci:
	@echo "Running CI tests with skipped-test enforcement..."
	GOCACHE=$${GOCACHE:-/tmp/gocache} ./scripts/test_ci.sh

test-postgres:
	@echo "Running all tests with disposable PostgreSQL..."
	GOCACHE=$${GOCACHE:-/tmp/gocache} ./scripts/test_postgres.sh

vet:
	@echo "Running go vet..."
	GOCACHE=$${GOCACHE:-/tmp/gocache} go vet ./...

lint:
	@echo "Running golangci-lint..."
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "golangci-lint not found. Install: https://golangci-lint.run/welcome/install/"; \
		exit 1; \
	fi
	GOCACHE=$${GOCACHE:-/tmp/gocache} GOLANGCI_LINT_CACHE=$${GOLANGCI_LINT_CACHE:-/tmp/golangci-lint-cache} golangci-lint run ./...

# Installs the binary to /usr/local/bin or ~/go/bin
install: build
	cp bin/$(BINARY_NAME) $(shell go env GOPATH)/bin/

clean:
	@echo "Cleaning up..."
	rm -rf bin/
	rm -f downloads/
	rm -f *.nzb

gonzbnet-e2e-test:
	./scripts/gonzbnet_e2e.sh test

gonzbnet-e2e-start:
	./scripts/gonzbnet_e2e.sh start

gonzbnet-e2e-bootstrap:
	./scripts/gonzbnet_e2e.sh bootstrap
	./scripts/gonzbnet_e2e.sh configure-pool
	./scripts/gonzbnet_e2e.sh admission-smoke

gonzbnet-e2e-verify:
	./scripts/gonzbnet_e2e.sh smoke
	./scripts/gonzbnet_e2e.sh quorum-smoke
	./scripts/gonzbnet_e2e.sh federation-smoke
	./scripts/gonzbnet_e2e.sh release-smoke
	./scripts/gonzbnet_e2e.sh nntp-smoke

gonzbnet-e2e-stop:
	./scripts/gonzbnet_e2e.sh stop

gonzbnet-e2e-status:
	./scripts/gonzbnet_e2e.sh status

gonzbnet-e2e-reset:
	./scripts/gonzbnet_e2e.sh reset
