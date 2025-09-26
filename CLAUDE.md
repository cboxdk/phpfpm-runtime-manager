# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Quick Start Commands

### Development
```bash
# Build the binary
make build

# Run in development mode with example config
make dev

# Run tests locally (skips tests requiring PHP-FPM)
make test

# Run complete test suite with PHP-FPM 8.4 in Docker
make test-docker

# Quick tests in Docker (core components only)
make test-docker-quick

# Clean up Docker test resources
make test-docker-clean
```

### Testing Single Components
```bash
# Test specific package
go test ./internal/metrics -v

# Run specific test
go test ./internal/metrics -run TestCollectPoolMetrics -v

# Test with coverage
go test -coverprofile=coverage.out ./internal/metrics
go tool cover -html=coverage.out
```

### Code Quality
```bash
# Format code
make fmt

# Run linter (requires golangci-lint)
make lint

# Security scan
make security
```

## Architecture Overview

### Core Components

The system follows a **supervisor pattern** with direct FastCGI communication:

1. **Manager** (`internal/app/manager.go`) - Orchestrates all components, manages lifecycle
2. **Supervisor** (`internal/supervisor/`) - Manages PHP-FPM master process lifecycle and pool configurations
3. **Metrics Collector** (`internal/metrics/`) - Collects metrics via FastCGI protocol directly from PHP-FPM
4. **Autoscaler** (`internal/autoscaler/`) - Intelligent scaling decisions based on metrics patterns
5. **Storage** (`internal/storage/`) - SQLite-based timeseries storage with automatic retention
6. **Telemetry** (`internal/telemetry/`) - OpenTelemetry tracing integration

### Key Design Patterns

**FastCGI Protocol Communication**: Direct communication with PHP-FPM using `github.com/cboxdk/fcgx` library. No HTTP proxy or status page scraping. See `internal/metrics/collector.go:collectPoolMetrics()` for implementation.

**Circuit Breaker Pattern**: All external calls (FastCGI, process management) wrapped in circuit breakers (`internal/resilience/`). Prevents cascade failures when PHP-FPM becomes unresponsive.

**Event-Driven Architecture**: Components communicate via channels:
- `events chan ManagerEvent` - Lifecycle events
- `metrics chan types.MetricSet` - Metric flow
- Process events from supervisor trigger immediate metric collection

**Intelligent Autoscaling**: Two-phase approach in `internal/autoscaler/intelligent_scaler.go`:
1. Baseline learning phase (collects patterns)
2. Confidence-based scaling (makes decisions based on confidence level)

### Critical Implementation Details

**OPcache Monitoring**: Executes PHP script via FastCGI to get OPcache status. See `collectOpcacheMetrics()` in collector.go. Requires PHP script execution through FastCGI params.

**Process Tracking**: Individual worker process CPU/memory tracking via `/proc` filesystem on Linux. Falls back to system-level metrics on macOS. See `internal/metrics/cpu_tracker.go`.

**Orphaned Worker Cleanup**: When stopping PHP-FPM, uses `pgrep/pkill` to find and terminate orphaned workers. Critical for preventing zombie processes. See `killOrphanedWorkers()` in supervisor.go.

**Test Environment**: Tests require real PHP-FPM for integration testing. Use Docker environment (`make test-docker`) for full coverage including FastCGI connections and process management.

## Verification Scripts
- Do not create verification scripts or tinker when tests cover that functionality and prove it works. Unit and feature tests are more important.

## Configuration System

### Pool Discovery
Automatic discovery from PHP-FPM config files or manual specification via YAML. Each pool tracked independently with its own:
- FastCGI endpoint (TCP or Unix socket)
- Worker limits and scaling parameters
- Metrics collection intervals

### Adaptive Collection
Metrics collection frequency adjusts based on system load:
- High load (>80%): 1-second intervals
- Normal load: 5-second intervals
- Low load (<30%): 15-second intervals

## Testing Strategy

### Test Categories
1. **Unit Tests**: Run locally, mock external dependencies
2. **Integration Tests**: Require Docker with PHP-FPM 8.4
3. **Platform Tests**: Linux-specific tests for /proc filesystem

### Known Test Dependencies
- FastCGI tests need real PHP-FPM endpoint
- CPU tracking tests need Linux /proc filesystem
- OPcache tests need PHP with OPcache extension
- Supervisor tests need PHP-FPM binary

### Docker Test Environment
Complete test environment with PHP-FPM 8.4, configured pools, and Linux /proc filesystem. Defined in:
- `Dockerfile.test` - Test container setup
- `docker-compose.test.yml` - Service definitions
- `docker/test-pool.conf` - PHP-FPM pool configuration

## Module Dependencies

Primary dependencies:
- `github.com/cboxdk/fcgx` - FastCGI client library (critical)
- `go.opentelemetry.io/otel` - Distributed tracing
- `github.com/prometheus/client_golang` - Metrics exposition
- `github.com/mattn/go-sqlite3` - Storage backend
- `go.uber.org/zap` - Structured logging

## Common Development Tasks

### Adding New Metrics
1. Define metric in `internal/types/metrics.go`
2. Add collection logic in `internal/metrics/collector.go`
3. Update Prometheus exporter in `internal/prometheus/`
4. Add tests with mock FastCGI responses

### Debugging FastCGI Issues
Enable debug logging to see FastCGI protocol details:
```bash
make dev LOG_LEVEL=debug
```

Check FastCGI connection with test script:
```go
// See internal/metrics/collector_test.go for mock server examples
```

### Performance Profiling
```bash
# CPU profiling
go test -cpuprofile=cpu.prof -bench=. ./internal/metrics
go tool pprof cpu.prof

# Memory profiling
go test -memprofile=mem.prof -bench=. ./internal/metrics
go tool pprof mem.prof
```