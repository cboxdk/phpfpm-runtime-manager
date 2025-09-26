# PHP-FPM Runtime Manager Makefile

# Variables
BINARY_NAME=phpfpm-manager
PACKAGE=github.com/cboxdk/phpfpm-runtime-manager
VERSION?=1.0.0-dev
BUILD_TIME=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
COMMIT_HASH?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Go variables
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=gofmt
GOLINT=golangci-lint

# Build flags
LDFLAGS=-ldflags "\
	-X main.Version=$(VERSION) \
	-X main.BuildTime=$(BUILD_TIME) \
	-X main.CommitHash=$(COMMIT_HASH) \
	-s -w"

# Directories
BUILD_DIR=build
BIN_DIR=bin
DIST_DIR=dist

# Default target
.PHONY: all
all: clean deps fmt lint test build

# Dependencies
.PHONY: deps
deps:
	@echo "Installing dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

# Format code
.PHONY: fmt
fmt:
	@echo "Formatting code..."
	$(GOFMT) -s -w .

# Lint code
.PHONY: lint
lint:
	@echo "Running linter..."
	$(GOLINT) run ./...

# Test
.PHONY: test
test:
	@echo "Running tests..."
	$(GOTEST) -v -race -coverprofile=coverage.out ./...

# Test with coverage report
.PHONY: test-coverage
test-coverage: test
	@echo "Generating coverage report..."
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Build binary
.PHONY: build
build:
	@echo "Building binary..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=1 $(GOBUILD) $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME) ./cmd/phpfpm-manager

# Build for multiple platforms
.PHONY: build-all
build-all: clean
	@echo "Building for multiple platforms..."
	@mkdir -p $(DIST_DIR)

	# Linux amd64
	@echo "Building for linux/amd64..."
	@mkdir -p $(DIST_DIR)/linux-amd64
	GOOS=linux GOARCH=amd64 CGO_ENABLED=1 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/linux-amd64/$(BINARY_NAME) ./cmd/phpfpm-manager

	# Linux arm64
	@echo "Building for linux/arm64..."
	@mkdir -p $(DIST_DIR)/linux-arm64
	GOOS=linux GOARCH=arm64 CGO_ENABLED=1 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/linux-arm64/$(BINARY_NAME) ./cmd/phpfpm-manager

# Install binary to system
.PHONY: install
install: build
	@echo "Installing binary..."
	cp $(BIN_DIR)/$(BINARY_NAME) /usr/local/bin/$(BINARY_NAME)

# Run the application
.PHONY: run
run: build
	@echo "Starting PHP-FPM Runtime Manager..."
	./$(BIN_DIR)/$(BINARY_NAME) -config configs/example.yaml

# Run with development settings
.PHONY: dev
dev:
	@echo "Starting in development mode..."
	$(GOCMD) run ./cmd/phpfpm-manager -config configs/example.yaml

# Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning build artifacts..."
	$(GOCLEAN)
	rm -rf $(BIN_DIR)
	rm -rf $(DIST_DIR)
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

# Create distribution packages
.PHONY: package
package: build-all
	@echo "Creating distribution packages..."

	# Linux amd64 package
	@cd $(DIST_DIR)/linux-amd64 && \
		cp ../../configs/example.yaml . && \
		tar -czf ../$(BINARY_NAME)-$(VERSION)-linux-amd64.tar.gz $(BINARY_NAME) example.yaml && \
		rm example.yaml

	# Linux arm64 package
	@cd $(DIST_DIR)/linux-arm64 && \
		cp ../../configs/example.yaml . && \
		tar -czf ../$(BINARY_NAME)-$(VERSION)-linux-arm64.tar.gz $(BINARY_NAME) example.yaml && \
		rm example.yaml

	@echo "Distribution packages created in $(DIST_DIR)/"
	@ls -la $(DIST_DIR)/*.tar.gz

# Docker build
.PHONY: docker-build
docker-build:
	@echo "Building Docker image..."
	docker build -t $(BINARY_NAME):$(VERSION) .
	docker tag $(BINARY_NAME):$(VERSION) $(BINARY_NAME):latest

# Docker run
.PHONY: docker-run
docker-run: docker-build
	@echo "Running Docker container..."
	docker run --rm -p 9090:9090 -v $(PWD)/configs:/app/configs $(BINARY_NAME):latest

# Database setup (for development)
.PHONY: setup-db
setup-db:
	@echo "Setting up database directory..."
	@mkdir -p data
	@chmod 755 data

# Benchmark tests
.PHONY: bench
bench:
	@echo "Running benchmark tests..."
	$(GOTEST) -bench=. -benchmem ./...

# Security scan
.PHONY: security
security:
	@echo "Running security scan..."
	@which gosec > /dev/null || (echo "Installing gosec..." && go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest)
	gosec ./...

# Generate mocks (if using mockery)
.PHONY: mocks
mocks:
	@echo "Generating mocks..."
	@which mockery > /dev/null || (echo "Installing mockery..." && go install github.com/vektra/mockery/v2@latest)
	mockery --all --dir internal/app --output tests/mocks

# Integration tests
.PHONY: test-integration
test-integration:
	@echo "Running integration tests..."
	$(GOTEST) -tags=integration -v ./tests/integration/...

# Docker test targets for complete test coverage with PHP-FPM 8.4
.PHONY: test-docker
test-docker: docker-test-build
	@echo "🧪 Running Full Test Suite with PHP-FPM 8.4 in Docker..."
	@docker-compose -f docker-compose.test.yml run --rm phpfpm-test

.PHONY: test-docker-quick
test-docker-quick: docker-test-build
	@echo "⚡ Running Quick Test Suite in Docker..."
	@docker-compose -f docker-compose.test.yml run --rm phpfpm-test-quick

.PHONY: test-docker-metrics
test-docker-metrics: docker-test-build
	@echo "📊 Running Metrics Tests with Real PHP-FPM in Docker..."
	@docker-compose -f docker-compose.test.yml run --rm phpfpm-test-metrics

.PHONY: docker-test-build
docker-test-build:
	@echo "📦 Building test Docker image..."
	@docker-compose -f docker-compose.test.yml build phpfpm-test

.PHONY: test-docker-clean
test-docker-clean:
	@echo "🧹 Cleaning up Docker test resources..."
	@docker-compose -f docker-compose.test.yml down -v
	@docker rmi phpfpm-runtime-manager_phpfpm-test 2>/dev/null || true

# Run tests in Docker with real PHP-FPM (recommended for full coverage)
.PHONY: test-complete
test-complete: test-docker
	@echo "✅ Complete test suite finished!"

# Load test (requires wrk or similar tool)
.PHONY: load-test
load-test:
	@echo "Running load test on metrics endpoint..."
	@which wrk > /dev/null || (echo "wrk not found. Install with: brew install wrk (macOS) or apt-get install wrk (Ubuntu)" && exit 1)
	wrk -t12 -c400 -d30s http://localhost:9090/metrics

# Check Go version
.PHONY: check-go-version
check-go-version:
	@echo "Checking Go version..."
	@$(GOCMD) version
	@echo "Required: Go 1.24 or higher"

# Development setup
.PHONY: dev-setup
dev-setup: check-go-version deps setup-db
	@echo "Development environment setup complete!"
	@echo "Run 'make dev' to start the application in development mode"

# Production build
.PHONY: prod-build
prod-build: clean deps fmt lint test build
	@echo "Production build complete!"
	@echo "Binary: $(BIN_DIR)/$(BINARY_NAME)"
	@echo "Version: $(VERSION)"
	@echo "Commit: $(COMMIT_HASH)"

# Help
.PHONY: help
help:
	@echo "Available targets:"
	@echo ""
	@echo "🔨 Build & Run:"
	@echo "  all              - Run clean, deps, fmt, lint, test, build"
	@echo "  deps             - Download Go dependencies"
	@echo "  fmt              - Format Go code"
	@echo "  lint             - Run linter"
	@echo "  build            - Build binary"
	@echo "  build-all        - Build for multiple platforms"
	@echo "  install          - Install binary to system"
	@echo "  run              - Build and run application"
	@echo "  dev              - Run in development mode"
	@echo "  clean            - Clean build artifacts"
	@echo "  package          - Create distribution packages"
	@echo ""
	@echo "🧪 Testing:"
	@echo "  test             - Run tests (local)"
	@echo "  test-coverage    - Run tests with coverage report"
	@echo "  test-docker      - Run full test suite with PHP-FPM 8.4 in Docker ✅"
	@echo "  test-docker-quick - Run quick tests in Docker"
	@echo "  test-docker-metrics - Run metrics tests with real PHP-FPM"
	@echo "  test-docker-clean - Clean Docker test resources"
	@echo "  test-complete    - Run complete test suite in Docker (recommended)"
	@echo "  test-integration - Run integration tests"
	@echo "  bench            - Run benchmark tests"
	@echo ""
	@echo "🐳 Docker:"
	@echo "  docker-build     - Build Docker image"
	@echo "  docker-run       - Build and run Docker container"
	@echo "  docker-test-build - Build test Docker image"
	@echo ""
	@echo "🔧 Development:"
	@echo "  setup-db         - Setup database directory"
	@echo "  security         - Run security scan"
	@echo "  mocks            - Generate test mocks"
	@echo "  load-test        - Run load test"
	@echo "  dev-setup        - Setup development environment"
	@echo "  prod-build       - Production build with all checks"
	@echo "  help             - Show this help"

# Version info
.PHONY: version
version:
	@echo "Version: $(VERSION)"
	@echo "Build Time: $(BUILD_TIME)"
	@echo "Commit Hash: $(COMMIT_HASH)"