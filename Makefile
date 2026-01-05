BINARY_NAME=gonzb
VERSION=$(shell git describe --tags --always --dirty)
BUILD_TIME=$(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
GOOS=$(shell go env GOOS)
GOARCH=$(shell go env GOARCH)
DIST_NAME=$(BINARY_NAME)_$(VERSION)_$(GOOS)_$(GOARCH)
PKG=./cmd/gonzb

LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

.PHONY: all build clean test install

all: build

build:
	@echo "Building $(DIST_NAME)..."
	go build $(LDFLAGS) -o bin/$(DIST_NAME) $(PKG)

# Installs the binary to /usr/local/bin or ~/go/bin
install: build
	cp bin/$(BINARY_NAME) $(shell go env GOPATH)/bin/

clean:
	@echo "Cleaning up..."
	rm -rf bin/
	rm -f downloads/
	rm -f *.nzb