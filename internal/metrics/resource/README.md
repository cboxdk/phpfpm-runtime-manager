# Resource Monitoring Package

The resource monitoring package provides cross-platform resource monitoring capabilities with support for containers, cgroups, and virtualization environments.

## Overview

This package implements comprehensive resource monitoring for:
- **Memory**: Usage, limits, pressure information, and swap statistics
- **CPU**: Usage percentages, core limits, throttling, and pressure information
- **I/O**: Read/write statistics, per-device metrics, and wait times
- **Network**: Interface statistics, bandwidth utilization, and error rates

### Key Features

- **Cross-platform Support**: Native implementations for macOS and Linux with build tag abstraction
- **Container Awareness**: Automatic detection and support for Docker, Podman, Kubernetes, systemd, and other container runtimes
- **Cgroup Integration**: Full support for both cgroup v1 and v2 with memory pressure and CPU throttling metrics
- **Virtualization Detection**: Apple Virtualization (VZ), KVM, VMware, Xen, and Hyper-V detection with proper limit reporting
- **Performance Optimized**: Sub-microsecond collection times with intelligent caching
- **Production Ready**: Comprehensive error handling, resource cleanup, and configurable behavior

## Architecture

```
ResourceMonitor (interface)
├── DarwinResourceMonitor (macOS)
│   ├── System calls (sysctl, vm_stat)
│   ├── Apple Virtualization (VZ) detection
│   └── Network interface statistics
└── LinuxResourceMonitor (Linux)
    ├── CgroupManager (v1/v2 support)
    ├── ContainerDetector (multi-runtime)
    └── /proc filesystem integration
```

## Quick Start

### Basic Usage

```go
import "github.com/cboxdk/phpfpm-runtime-manager/internal/metrics/resource"

// Create a resource monitor
monitor, err := resource.NewResourceMonitor()
if err != nil {
    log.Fatalf("Failed to create monitor: %v", err)
}
defer monitor.Close()

// Get memory statistics
memStats, err := monitor.GetMemoryStats(context.Background())
if err != nil {
    log.Fatalf("Failed to get memory stats: %v", err)
}

fmt.Printf("Memory Usage: %d MB / %d MB\n",
    memStats.UsageBytes/(1024*1024),
    memStats.LimitBytes/(1024*1024))

// Check if running in container
if monitor.IsContainerized() {
    fmt.Println("Running in container environment")
}
```

### Advanced Configuration

```go
config := &resource.Config{
    UpdateInterval:        time.Second,
    EnableCaching:         true,
    EnableDetailedIO:      true,
    EnableDetailedNetwork: true,
    ContainerPaths: []string{
        "/.dockerenv",
        "/run/.containerenv",
    },
    CgroupPaths: []string{
        "/sys/fs/cgroup",
        "/proc/cgroups",
    },
}

monitor, err := resource.NewResourceMonitorWithConfig(config)
```

## API Reference

### Core Interface

```go
type ResourceMonitor interface {
    // Resource statistics
    GetMemoryStats(ctx context.Context) (*MemoryStats, error)
    GetCPUStats(ctx context.Context) (*CPUStats, error)
    GetIOStats(ctx context.Context) (*IOStats, error)
    GetNetworkStats(ctx context.Context) (*NetworkStats, error)

    // Resource limits
    GetLimits(ctx context.Context) (*ResourceLimits, error)

    // Environment information
    GetEnvironmentInfo() *EnvironmentInfo
    IsContainerized() bool

    // Lifecycle
    Close() error
}
```

### Memory Statistics

```go
type MemoryStats struct {
    UsageBytes      int64        `json:"usage_bytes"`      // Current memory usage
    AvailableBytes  int64        `json:"available_bytes"`  // Available memory
    LimitBytes      int64        `json:"limit_bytes"`      // Memory limit
    LimitSource     LimitSource  `json:"limit_source"`     // Source of limit
    RSSBytes        int64        `json:"rss_bytes"`        // Resident set size
    CacheBytes      int64        `json:"cache_bytes"`      // Cache memory
    SwapUsageBytes  int64        `json:"swap_usage_bytes"` // Swap usage
    SwapLimitBytes  int64        `json:"swap_limit_bytes"` // Swap limit
    Pressure        *MemoryPressure `json:"pressure"`      // Memory pressure (cgroup v2)
    Timestamp       time.Time    `json:"timestamp"`        // Collection time
}
```

### CPU Statistics

```go
type CPUStats struct {
    UsagePercent   float64        `json:"usage_percent"`    // CPU usage percentage
    AvailableCores float64        `json:"available_cores"`  // Available CPU cores
    LimitCores     float64        `json:"limit_cores"`      // CPU core limit
    LimitSource    LimitSource    `json:"limit_source"`     // Source of limit
    Shares         int64          `json:"shares"`           // CPU shares (cgroup)
    QuotaPeriodUs  int64          `json:"quota_period_us"`  // Quota period
    QuotaUs        int64          `json:"quota_us"`         // CPU quota
    Throttling     *CPUThrottling `json:"throttling"`       // Throttling info
    PerCPUUsage    []float64      `json:"per_cpu_usage"`    // Per-CPU usage
    Timestamp      time.Time      `json:"timestamp"`        // Collection time
}
```

## Platform Support

### macOS (Darwin)

**Supported Features:**
- Physical memory statistics via `sysctl`
- CPU usage via `host_processor_info`
- Apple Virtualization (VZ) detection and limits
- Network interface statistics via `netstat`
- I/O statistics via `iostat` parsing
- Host information and system details

**VZ Detection:**
The monitor automatically detects Apple Virtualization Framework environments and reports VM-specific limits:

```go
info := monitor.GetEnvironmentInfo()
if info.Virtualization != nil && info.Virtualization.Type == VirtualizationTypeVZ {
    fmt.Printf("Running in Apple VZ with %d MB memory limit\n",
        info.Virtualization.Guest.MemoryLimit/(1024*1024))
}
```

### Linux

**Supported Features:**
- Full cgroup v1 and v2 support
- Container runtime detection (Docker, Podman, containerd, CRI-O, systemd, LXC)
- Kubernetes pod and namespace detection
- Memory pressure information (PSI)
- CPU throttling statistics
- Per-device I/O statistics
- Network interface monitoring

**Container Detection:**
Automatic detection of container environments:

```go
if monitor.IsContainerized() {
    info := monitor.GetEnvironmentInfo()
    fmt.Printf("Container: Runtime=%s, ID=%s\n",
        info.Container.Runtime, info.Container.ID)

    if info.Container.Orchestrator == "kubernetes" {
        fmt.Printf("Kubernetes: Pod=%s, Namespace=%s\n",
            info.Container.PodName, info.Container.Namespace)
    }
}
```

## Cgroup Support

### Cgroup v1
- Memory: usage, limit, cache, RSS, swap
- CPU: usage, quota, period, shares
- I/O: read/write bytes and operations per device

### Cgroup v2
- Unified hierarchy support
- Memory pressure stall information (PSI)
- CPU pressure and throttling statistics
- Enhanced I/O statistics with latency data

### Usage Example

```go
// Linux-specific: Access cgroup manager directly
if runtime.GOOS == "linux" {
    cgroupManager, err := NewCgroupManager()
    if err == nil {
        version := cgroupManager.GetVersion()
        fmt.Printf("Cgroup version: %s\n", version.String())

        controllers := cgroupManager.GetControllers()
        fmt.Printf("Available controllers: %v\n", controllers)
    }
}
```

## Performance Characteristics

### Benchmarks (M3 Pro)
```
BenchmarkMemoryStats-12    42M ops    24.90 ns/op
BenchmarkCPUStats-12       13M ops    73.88 ns/op
```

### Resource Usage
- **Memory overhead**: ~1-2 KB per monitor instance
- **CPU impact**: <0.1% system CPU usage under normal conditions
- **I/O impact**: Minimal, primarily reading from /proc and /sys filesystems

### Caching Strategy
- **Default cache duration**: 1 second
- **Cache efficiency**: >95% hit rate for high-frequency polling
- **Cache invalidation**: Time-based with configurable intervals

## Error Handling

### Common Errors

```go
var (
    ErrUnsupportedPlatform = errors.New("unsupported platform")
    ErrContainerNotFound   = errors.New("container environment not detected")
    ErrCgroupNotFound      = errors.New("cgroup not found or accessible")
    ErrInsufficientPerms   = errors.New("insufficient permissions to read resource data")
)
```

### Error Recovery

```go
memStats, err := monitor.GetMemoryStats(ctx)
if err != nil {
    switch {
    case errors.Is(err, resource.ErrInsufficientPerms):
        log.Warn("Insufficient permissions, using fallback method")
        // Implement fallback logic
    case errors.Is(err, resource.ErrCgroupNotFound):
        log.Info("Cgroup not available, using system stats")
        // Use system-level statistics
    default:
        return fmt.Errorf("memory stats collection failed: %w", err)
    }
}
```

## Configuration

### Default Configuration

```go
func DefaultConfig() *Config {
    return &Config{
        UpdateInterval:        time.Second,
        EnableCaching:         true,
        EnableDetailedIO:      false,
        EnableDetailedNetwork: false,
        ContainerPaths: []string{
            "/.dockerenv",
            "/run/.containerenv",
            "/proc/1/cgroup",
        },
        CgroupPaths: []string{
            "/sys/fs/cgroup",
            "/proc/cgroups",
            "/proc/self/cgroup",
        },
    }
}
```

### Production Recommendations

- **Enable caching** for high-frequency monitoring (>1Hz)
- **Use context timeouts** to prevent hanging operations
- **Monitor memory usage** of the monitoring process itself
- **Implement circuit breakers** for critical monitoring paths

## Testing

The package includes comprehensive tests:

### Running Tests

```bash
# Run all tests
go test -v ./internal/metrics/resource/...

# Run platform-specific tests
go test -v ./internal/metrics/resource/... -tags linux
go test -v ./internal/metrics/resource/... -tags darwin

# Run benchmarks
go test -bench=. ./internal/metrics/resource/...
```

### Test Coverage

- **Unit tests**: Core functionality and edge cases
- **Integration tests**: Real system interaction
- **Platform tests**: Platform-specific implementations
- **Container tests**: Container environment detection
- **Benchmark tests**: Performance validation

## Troubleshooting

### Common Issues

1. **Permission Denied**: Ensure process has read access to `/proc` and `/sys` filesystems
2. **Cgroup Not Found**: Verify cgroup mount points in `/sys/fs/cgroup`
3. **Container Detection Failures**: Check container runtime environment files
4. **High CPU Usage**: Reduce monitoring frequency or enable caching

### Debug Logging

```go
// Enable detailed logging
config := resource.DefaultConfig()
monitor, err := resource.NewResourceMonitorWithConfig(config)

// Check environment information
info := monitor.GetEnvironmentInfo()
fmt.Printf("Platform: %s, Container: %+v, Cgroup: %+v\n",
    info.Platform, info.Container, info.Cgroup)
```

## Examples

See the `examples/` directory for complete usage examples:
- Basic monitoring setup
- Container-aware monitoring
- High-frequency metrics collection
- Custom configuration patterns

## Contributing

1. Platform-specific implementations go in `resource_<GOOS>.go` files
2. Use build tags for platform isolation: `//go:build linux`
3. Add comprehensive tests for new features
4. Update documentation for API changes
5. Run benchmarks to verify performance impact

## License

This package is part of the phpfpm-runtime-manager project and follows the same license terms.