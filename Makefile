BINARY_NAME=gonzb
VERSION=$(shell git describe --tags --always --dirty)
BUILD_TIME=$(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
GOOS=$(shell go env GOOS)
GOARCH=$(shell go env GOARCH)
DIST_NAME=$(BINARY_NAME)_$(VERSION)_$(GOOS)_$(GOARCH)
PKG=./cmd/gonzb

LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

.PHONY: all build ui-build clean test vet lint install

all: build

build: ui-build
	@echo "Building $(DIST_NAME)..."
	GOCACHE=$${GOCACHE:-/tmp/gocache} go build $(LDFLAGS) -o bin/$(DIST_NAME) $(PKG)

ui-build:
	@echo "Building embedded web UI..."
	npm --prefix ui run build

test:
	@echo "Running tests..."
	GOCACHE=$${GOCACHE:-/tmp/gocache} go test ./...

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
