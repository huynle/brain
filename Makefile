# Brain API - Go Build System
# Usage: make <target>

# Build variables
BINARY_DIR := bin
MODULE := github.com/huynle/brain-api
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -ldflags "-s -w -X $(MODULE)/internal/config.Version=$(VERSION) -X $(MODULE)/internal/config.Commit=$(COMMIT) -X $(MODULE)/internal/config.BuildTime=$(BUILD_TIME)"

# Go settings
GOBIN := $(shell go env GOPATH)/bin
GOLANGCI_LINT_VERSION := v1.62.2

# Binaries to build
CMDS := brain-api brain-runner brain

.PHONY: all build test lint typecheck clean release docker help

## Default target
all: build

## Build all binaries
build:
	@mkdir -p $(BINARY_DIR)
	@for cmd in $(CMDS); do \
		echo "Building $$cmd..."; \
		go build $(LDFLAGS) -o $(BINARY_DIR)/$$cmd ./cmd/$$cmd; \
	done
	@echo "Build complete: $(BINARY_DIR)/"

## Build a specific binary (e.g., make build-brain-api)
build-%:
	@mkdir -p $(BINARY_DIR)
	go build $(LDFLAGS) -o $(BINARY_DIR)/$* ./cmd/$*

## Run all tests
test:
	go test ./... -v -count=1

## Run tests with coverage
test-cover:
	go test ./... -v -count=1 -coverprofile=coverage.out -covermode=atomic
	go tool cover -func=coverage.out
	@echo ""
	@echo "HTML report: go tool cover -html=coverage.out -o coverage.html"

## Run tests in short mode (skip long-running tests)
test-short:
	go test ./... -v -short -count=1

## Run golangci-lint
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not found. Install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)"; \
		exit 1; \
	fi

## Run go vet (type checking / static analysis)
typecheck:
	go vet ./...

## Run all checks (vet + test + lint)
check: typecheck test lint

## Format code
fmt:
	go fmt ./...
	@if command -v goimports >/dev/null 2>&1; then \
		goimports -w .; \
	fi

## Tidy dependencies
tidy:
	go mod tidy

## Clean build artifacts
clean:
	rm -rf $(BINARY_DIR) coverage.out coverage.html
	go clean -cache -testcache

## Cross-compile for release (linux/darwin/windows, amd64/arm64)
release:
	@mkdir -p $(BINARY_DIR)/release
	@for cmd in $(CMDS); do \
		echo "Cross-compiling $$cmd..."; \
		GOOS=linux   GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_DIR)/release/$$cmd-linux-amd64 ./cmd/$$cmd; \
		GOOS=linux   GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY_DIR)/release/$$cmd-linux-arm64 ./cmd/$$cmd; \
		GOOS=darwin  GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_DIR)/release/$$cmd-darwin-amd64 ./cmd/$$cmd; \
		GOOS=darwin  GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY_DIR)/release/$$cmd-darwin-arm64 ./cmd/$$cmd; \
		GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_DIR)/release/$$cmd-windows-amd64.exe ./cmd/$$cmd; \
	done
	@echo "Release binaries: $(BINARY_DIR)/release/"

## Build Docker image
docker:
	docker build -t brain-api:$(VERSION) .
	@echo "Built: brain-api:$(VERSION)"

## Install binaries to GOPATH/bin
install:
	@for cmd in $(CMDS); do \
		go install $(LDFLAGS) ./cmd/$$cmd; \
	done
	@echo "Installed to $(GOBIN)"

## Show help
help:
	@echo "Brain API - Go Build System"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' Makefile | sed 's/^## /  /'
	@echo ""
	@echo "Usage: make <target>"
