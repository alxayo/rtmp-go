# Makefile for rtmp-go project
.PHONY: help build test test-race test-unit test-integration test-interop clean fmt vet lint benchmark coverage golden-vectors install-tools

# Default target
help: ## Show this help message
	@echo "Available targets:"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Build targets
build: ## Build the rtmp-server binary
	@echo "Building rtmp-server..."
	go build -ldflags="-w -s" -o rtmp-server ./cmd/rtmp-server

build-race: ## Build with race detector
	@echo "Building rtmp-server with race detector..."
	go build -race -o rtmp-server-race ./cmd/rtmp-server

build-all: ## Build for all supported platforms
	@echo "Building for all platforms..."
	@mkdir -p dist
	GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o dist/rtmp-server-linux-amd64 ./cmd/rtmp-server
	GOOS=linux GOARCH=arm64 go build -ldflags="-w -s" -o dist/rtmp-server-linux-arm64 ./cmd/rtmp-server
	GOOS=darwin GOARCH=arm64 go build -ldflags="-w -s" -o dist/rtmp-server-darwin-arm64 ./cmd/rtmp-server
	GOOS=windows GOARCH=amd64 go build -ldflags="-w -s" -o dist/rtmp-server-windows-amd64.exe ./cmd/rtmp-server
	@echo "Built binaries in dist/ directory"

# Test targets
test: ## Run all tests
	@echo "Running all tests..."
	go test -v ./...

test-race: ## Run tests with race detector
	@echo "Running tests with race detector..."
	go test -race -v -count=1 ./...

test-unit: ## Run unit tests only
	@echo "Running unit tests..."
	go test -race -v -count=1 ./internal/...

test-integration: ## Run integration tests
	@echo "Running integration tests..."
	@if [ -d "tests/integration" ]; then \
		go test -race -v -count=1 -timeout=10m ./tests/integration/...; \
	else \
		echo "Integration tests directory not found"; \
	fi

test-interop: ## Run FFmpeg interop tests
	@echo "Running FFmpeg interop tests..."
	@if [ -f "tests/interop/ffmpeg_test.sh" ]; then \
		cd tests/interop && ./ffmpeg_test.sh; \
	else \
		echo "Interop test script not found"; \
	fi

# Code quality targets
fmt: ## Format Go code
	@echo "Formatting code..."
	go fmt ./...

vet: ## Run go vet
	@echo "Running go vet..."
	go vet ./...

lint: install-tools ## Run staticcheck linter
	@echo "Running staticcheck..."
	staticcheck ./...

# Coverage and benchmarks
coverage: ## Generate test coverage report
	@echo "Generating coverage report..."
	go test -coverprofile=coverage.out -covermode=atomic ./internal/...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

benchmark: ## Run benchmarks
	@echo "Running benchmarks..."
	@go test -bench=. -benchmem ./internal/rtmp/chunk || echo "No benchmarks in chunk package"
	@go test -bench=. -benchmem ./internal/rtmp/amf || echo "No benchmarks in amf package"
	@go test -bench=. -benchmem ./internal/rtmp/handshake || echo "No benchmarks in handshake package"

# Golden test vectors
golden-vectors: ## Generate golden test vectors
	@echo "Generating golden test vectors..."
	@cd tests/golden && \
	if [ -f gen_handshake_vectors.go ]; then go run gen_handshake_vectors.go; fi && \
	if [ -f gen_amf0_vectors.go ]; then go run gen_amf0_vectors.go; fi && \
	if [ -f gen_chunk_vectors.go ]; then go run gen_chunk_vectors.go; fi

# Development helpers
clean: ## Clean build artifacts
	@echo "Cleaning build artifacts..."
	rm -f rtmp-server rtmp-server-race rtmp-server.exe
	rm -rf dist/
	rm -f coverage.out coverage.html
	go clean -cache

mod-tidy: ## Tidy go modules
	@echo "Tidying go modules..."
	go mod tidy

mod-verify: ## Verify go modules
	@echo "Verifying go modules..."
	go mod verify

# Tool installation
install-tools: ## Install development tools
	@echo "Installing development tools..."
	go install honnef.co/go/tools/cmd/staticcheck@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest

# Security checks
security: install-tools ## Run security vulnerability check
	@echo "Running govulncheck..."
	govulncheck ./...

# CI simulation
ci: fmt vet test-race ## Run CI checks locally
	@echo "Running CI checks locally..."
	@echo "All CI checks completed successfully!"

ci-quick: fmt vet test-unit ## Run quick CI checks locally
	@echo "Running quick CI checks locally..."
	@echo "Quick CI checks completed successfully!"

# Development workflow targets
dev-setup: install-tools golden-vectors ## Set up development environment
	@echo "Setting up development environment..."
	go mod download
	@echo "Development environment ready!"

dev-test: test-unit build ## Quick development test cycle
	@echo "Development test cycle completed!"

# Release preparation
release-check: lint security test coverage ## Run all release checks
	@echo "Running all release checks..."
	@echo "Release checks completed successfully!"

# Help with project structure
info: ## Show project information
	@echo "RTMP Go Server Project Information:"
	@echo "=================================="
	@echo "Project: github.com/alxayo/go-rtmp"
	@echo "Go version: $(shell go version)"
	@echo "Module path: $(shell go list -m)"
	@echo ""
	@echo "Key directories:"
	@echo "  cmd/rtmp-server/     - Main server application"
	@echo "  internal/rtmp/       - RTMP protocol implementation"
	@echo "  tests/golden/        - Golden test vectors"
	@echo "  tests/integration/   - Integration tests"
	@echo "  tests/interop/       - FFmpeg interop tests"
	@echo ""
	@echo "Quick start:"
	@echo "  make dev-setup       - Set up development environment"
	@echo "  make dev-test        - Run development test cycle"
	@echo "  make ci              - Run full CI checks locally"

# Docker targets (if needed in the future)
docker-build: ## Build Docker image
	@echo "Building Docker image..."
	docker build -t rtmp-go:latest .

docker-run: ## Run Docker container
	@echo "Running Docker container..."
	docker run -p 1935:1935 rtmp-go:latest

# Default goal
.DEFAULT_GOAL := help