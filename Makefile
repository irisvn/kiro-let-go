GO ?= go
export GOFLAGS := -buildvcs=false
BINARY_DIR := bin
VERSION := $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X github.com/irisvn/kiro-let-go/internal/version.Version=$(VERSION)"

.PHONY: build vet test lint run clean

build:
	$(GO) build $(LDFLAGS) -o $(BINARY_DIR)/kiro-let-go ./cmd/server
	$(GO) build $(LDFLAGS) -o $(BINARY_DIR)/kiro-let-go-cli ./cmd/cli

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

lint:
	@if command -v golint >/dev/null 2>&1; then golint ./...; else echo "golint not installed"; fi

run:
	$(GO) run ./cmd/server

clean:
	rm -rf $(BINARY_DIR)/
