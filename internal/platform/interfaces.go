// Package platform provides cross-platform abstractions for system operations
// used throughout the PHP-FPM Runtime Manager. It enables consistent behavior
// across Linux, macOS, and Windows while maintaining high performance.
//
// The package implements the adapter pattern to abstract platform-specific
// functionality behind clean interfaces, enabling reliable testing and
// cross-platform compatibility.
package platform

import (
	"context"
	"time"
)

// MemoryProvider abstracts system memory information across platforms
type MemoryProvider interface {
	// GetTotalMemory returns the total physical memory in bytes
	GetTotalMemory(ctx context.Context) (uint64, error)

	// GetAvailableMemory returns available memory for allocation in bytes
	GetAvailableMemory(ctx context.Context) (uint64, error)

	// GetMemoryInfo returns comprehensive memory statistics
	GetMemoryInfo(ctx context.Context) (*MemoryInfo, error)

	// Platform returns the platform identifier for this provider
	Platform() string

	// IsSupported returns true if memory detection is supported on this platform
	IsSupported() bool
}

// ProcessProvider abstracts process monitoring across platforms
type ProcessProvider interface {
	// GetProcessInfo returns detailed information about a specific process
	GetProcessInfo(ctx context.Context, pid int) (*ProcessInfo, error)

	// ListProcesses returns information about processes matching criteria
	ListProcesses(ctx context.Context, filter ProcessFilter) ([]*ProcessInfo, error)

	// MonitorProcess starts monitoring a process for resource usage
	MonitorProcess(ctx context.Context, pid int) (ProcessMonitor, error)

	// Platform returns the platform identifier for this provider
	Platform() string

	// IsSupported returns true if process monitoring is supported
	IsSupported() bool
}

// FileSystemProvider abstracts file system operations across platforms
type FileSystemProvider interface {
	// NormalizePath converts a path to the platform-appropriate format
	NormalizePath(path string) string

	// GetConfigDirectory returns the standard configuration directory
	GetConfigDirectory() string

	// GetTempDirectory returns the system temporary directory
	GetTempDirectory() string

	// IsPathValid validates if a path is acceptable for the platform
	IsPathValid(path string) bool

	// Platform returns the platform identifier for this provider
	Platform() string
}

// MemoryInfo represents comprehensive memory statistics
type MemoryInfo struct {
	TotalBytes     uint64    `json:"total_bytes"`
	AvailableBytes uint64    `json:"available_bytes"`
	UsedBytes      uint64    `json:"used_bytes"`
	FreeBytes      uint64    `json:"free_bytes"`
	CachedBytes    uint64    `json:"cached_bytes,omitempty"`   // Linux/macOS specific
	BufferedBytes  uint64    `json:"buffered_bytes,omitempty"` // Linux specific
	SwapTotalBytes uint64    `json:"swap_total_bytes,omitempty"`
	SwapUsedBytes  uint64    `json:"swap_used_bytes,omitempty"`
	Timestamp      time.Time `json:"timestamp"`
	Platform       string    `json:"platform"`
}

// ProcessInfo represents detailed process information
type ProcessInfo struct {
	PID             int       `json:"pid"`
	PPID            int       `json:"ppid"`
	Name            string    `json:"name"`
	Command         string    `json:"command"`
	State           string    `json:"state"`
	CPUPercent      float64   `json:"cpu_percent"`
	MemoryBytes     uint64    `json:"memory_bytes"`
	VirtualBytes    uint64    `json:"virtual_bytes,omitempty"`
	ResidentBytes   uint64    `json:"resident_bytes,omitempty"`
	StartTime       time.Time `json:"start_time"`
	UserTime        float64   `json:"user_time_seconds"`
	SystemTime      float64   `json:"system_time_seconds"`
	ThreadCount     int       `json:"thread_count,omitempty"`
	FileDescriptors int       `json:"file_descriptors,omitempty"` // Unix-specific
	Platform        string    `json:"platform"`
}

// ProcessFilter defines criteria for filtering processes
type ProcessFilter struct {
	Name        string   `json:"name,omitempty"`
	NamePattern string   `json:"name_pattern,omitempty"` // Regex pattern
	Command     string   `json:"command,omitempty"`
	PIDs        []int    `json:"pids,omitempty"`
	MinCPU      float64  `json:"min_cpu,omitempty"`
	MinMemory   uint64   `json:"min_memory,omitempty"`
	States      []string `json:"states,omitempty"`
}

// ProcessMonitor provides real-time process monitoring
type ProcessMonitor interface {
	// Start begins monitoring the process
	Start(ctx context.Context) error

	// Stop halts monitoring
	Stop() error

	// GetStats returns current process statistics
	GetStats() (*ProcessStats, error)

	// Subscribe returns a channel for receiving process updates
	Subscribe() <-chan *ProcessStats

	// IsRunning returns true if the process is still running
	IsRunning() bool
}

// ProcessStats represents real-time process statistics
type ProcessStats struct {
	PID          int       `json:"pid"`
	CPUPercent   float64   `json:"cpu_percent"`
	MemoryBytes  uint64    `json:"memory_bytes"`
	DeltaCPU     float64   `json:"delta_cpu"`
	DeltaMemory  int64     `json:"delta_memory"`
	Timestamp    time.Time `json:"timestamp"`
	IsActive     bool      `json:"is_active"`
	StateChanges int       `json:"state_changes"`
}

// Provider is a unified interface for all platform abstractions
type Provider interface {
	Memory() MemoryProvider
	Process() ProcessProvider
	FileSystem() FileSystemProvider
	Platform() string
	IsSupported() bool
}

// PlatformError represents platform-specific errors
type PlatformError struct {
	Platform  string
	Operation string
	Err       error
	Code      ErrorCode
}

func (e *PlatformError) Error() string {
	return e.Platform + " " + e.Operation + ": " + e.Err.Error()
}

func (e *PlatformError) Unwrap() error {
	return e.Err
}

// ErrorCode represents platform-specific error categories
type ErrorCode int

const (
	ErrorCodeUnknown ErrorCode = iota
	ErrorCodeNotSupported
	ErrorCodePermissionDenied
	ErrorCodeResourceNotFound
	ErrorCodeTimeout
	ErrorCodeInvalidArgument
)

// String returns the string representation of the error code
func (e ErrorCode) String() string {
	switch e {
	case ErrorCodeNotSupported:
		return "not_supported"
	case ErrorCodePermissionDenied:
		return "permission_denied"
	case ErrorCodeResourceNotFound:
		return "resource_not_found"
	case ErrorCodeTimeout:
		return "timeout"
	case ErrorCodeInvalidArgument:
		return "invalid_argument"
	default:
		return "unknown"
	}
}

// Platform feature constants for capability detection
const (
	// FeatureProcFS indicates support for Linux /proc filesystem
	FeatureProcFS = "procfs"

	// FeatureSysctl indicates support for BSD/macOS sysctl interface
	FeatureSysctl = "sysctl"

	// FeatureWMI indicates support for Windows WMI (Windows Management Instrumentation)
	FeatureWMI = "wmi"

	// FeaturePerfMon indicates support for Windows Performance Monitor
	FeaturePerfMon = "perfmon"

	// FeatureCGroups indicates support for Linux control groups
	FeatureCGroups = "cgroups"

	// FeatureBSD indicates BSD-style system interfaces
	FeatureBSD = "bsd"
)

// Config represents platform provider configuration
type Config struct {
	// PreferredPlatform specifies which platform implementation to prefer
	PreferredPlatform string `json:"preferred_platform,omitempty"`

	// EnableMockProvider enables mock provider for testing
	EnableMockProvider bool `json:"enable_mock_provider,omitempty"`

	// MemoryUpdateInterval specifies how often to refresh memory information
	MemoryUpdateInterval time.Duration `json:"memory_update_interval,omitempty"`

	// ProcessUpdateInterval specifies how often to refresh process information
	ProcessUpdateInterval time.Duration `json:"process_update_interval,omitempty"`

	// TimeoutDuration specifies the maximum time to wait for platform operations
	TimeoutDuration time.Duration `json:"timeout_duration,omitempty"`

	// CacheEnabled enables caching of platform information
	CacheEnabled bool `json:"cache_enabled,omitempty"`

	// CacheTTL specifies how long to cache platform information
	CacheTTL time.Duration `json:"cache_ttl,omitempty"`
}
