// Package resource provides cross-platform resource monitoring capabilities
// with support for containers, cgroups, and virtualization environments.
package resource

import (
	"context"
	"errors"
	"time"
)

// Common errors
var (
	ErrUnsupportedPlatform = errors.New("unsupported platform")
	ErrContainerNotFound   = errors.New("container environment not detected")
	ErrCgroupNotFound      = errors.New("cgroup not found or accessible")
	ErrInsufficientPerms   = errors.New("insufficient permissions to read resource data")
)

// ResourceMonitor provides cross-platform resource monitoring capabilities
type ResourceMonitor interface {
	// GetMemoryStats returns current memory usage and limits
	GetMemoryStats(ctx context.Context) (*MemoryStats, error)

	// GetCPUStats returns current CPU usage and limits
	GetCPUStats(ctx context.Context) (*CPUStats, error)

	// GetIOStats returns current I/O statistics
	GetIOStats(ctx context.Context) (*IOStats, error)

	// GetNetworkStats returns network usage statistics
	GetNetworkStats(ctx context.Context) (*NetworkStats, error)

	// GetLimits returns resource limits for the current environment
	GetLimits(ctx context.Context) (*ResourceLimits, error)

	// GetEnvironmentInfo returns information about the runtime environment
	GetEnvironmentInfo() *EnvironmentInfo

	// IsContainerized returns true if running in a container
	IsContainerized() bool

	// Close releases any resources held by the monitor
	Close() error
}

// MemoryStats represents memory usage information
type MemoryStats struct {
	// Current memory usage in bytes
	UsageBytes int64 `json:"usage_bytes"`

	// Available memory in bytes (may be container limit or system available)
	AvailableBytes int64 `json:"available_bytes"`

	// Memory limit in bytes (container limit or system total)
	LimitBytes int64 `json:"limit_bytes"`

	// Memory limit is from container/cgroup vs system
	LimitSource LimitSource `json:"limit_source"`

	// RSS (Resident Set Size) in bytes
	RSSBytes int64 `json:"rss_bytes"`

	// Cache memory in bytes
	CacheBytes int64 `json:"cache_bytes"`

	// Swap usage in bytes (if available)
	SwapUsageBytes int64 `json:"swap_usage_bytes"`

	// Swap limit in bytes (if available)
	SwapLimitBytes int64 `json:"swap_limit_bytes"`

	// Memory pressure information (Linux cgroup v2)
	Pressure *MemoryPressure `json:"pressure,omitempty"`

	// Last updated timestamp
	Timestamp time.Time `json:"timestamp"`
}

// CPUStats represents CPU usage information
type CPUStats struct {
	// CPU usage percentage (0-100, can be > 100 with multiple cores)
	UsagePercent float64 `json:"usage_percent"`

	// Number of CPU cores available
	AvailableCores float64 `json:"available_cores"`

	// CPU limit in cores (container limit or system cores)
	LimitCores float64 `json:"limit_cores"`

	// CPU limit source
	LimitSource LimitSource `json:"limit_source"`

	// CPU shares (Linux cgroup weight)
	Shares int64 `json:"shares,omitempty"`

	// CPU quota period in microseconds
	QuotaPeriodUs int64 `json:"quota_period_us,omitempty"`

	// CPU quota in microseconds per period
	QuotaUs int64 `json:"quota_us,omitempty"`

	// Throttling information
	Throttling *CPUThrottling `json:"throttling,omitempty"`

	// Per-CPU usage (if available)
	PerCPUUsage []float64 `json:"per_cpu_usage,omitempty"`

	// Last updated timestamp
	Timestamp time.Time `json:"timestamp"`
}

// IOStats represents I/O usage information
type IOStats struct {
	// Read bytes
	ReadBytes int64 `json:"read_bytes"`

	// Write bytes
	WriteBytes int64 `json:"write_bytes"`

	// Read operations
	ReadOps int64 `json:"read_ops"`

	// Write operations
	WriteOps int64 `json:"write_ops"`

	// I/O wait time in nanoseconds
	IOWaitNs int64 `json:"io_wait_ns"`

	// Per-device statistics
	Devices map[string]*DeviceIOStats `json:"devices,omitempty"`

	// Last updated timestamp
	Timestamp time.Time `json:"timestamp"`
}

// NetworkStats represents network usage information
type NetworkStats struct {
	// Received bytes
	RxBytes int64 `json:"rx_bytes"`

	// Transmitted bytes
	TxBytes int64 `json:"tx_bytes"`

	// Received packets
	RxPackets int64 `json:"rx_packets"`

	// Transmitted packets
	TxPackets int64 `json:"tx_packets"`

	// Received errors
	RxErrors int64 `json:"rx_errors"`

	// Transmitted errors
	TxErrors int64 `json:"tx_errors"`

	// Per-interface statistics
	Interfaces map[string]*InterfaceStats `json:"interfaces,omitempty"`

	// Last updated timestamp
	Timestamp time.Time `json:"timestamp"`
}

// ResourceLimits represents all resource limits
type ResourceLimits struct {
	Memory  *MemoryLimits  `json:"memory"`
	CPU     *CPULimits     `json:"cpu"`
	IO      *IOLimits      `json:"io,omitempty"`
	Network *NetworkLimits `json:"network,omitempty"`
}

// MemoryLimits represents memory-related limits
type MemoryLimits struct {
	LimitBytes     int64       `json:"limit_bytes"`
	SwapLimitBytes int64       `json:"swap_limit_bytes"`
	Source         LimitSource `json:"source"`
}

// CPULimits represents CPU-related limits
type CPULimits struct {
	LimitCores    float64     `json:"limit_cores"`
	Shares        int64       `json:"shares,omitempty"`
	QuotaPeriodUs int64       `json:"quota_period_us,omitempty"`
	QuotaUs       int64       `json:"quota_us,omitempty"`
	Source        LimitSource `json:"source"`
}

// IOLimits represents I/O-related limits
type IOLimits struct {
	ReadBytesPerSec  int64            `json:"read_bytes_per_sec,omitempty"`
	WriteBytesPerSec int64            `json:"write_bytes_per_sec,omitempty"`
	ReadOpsPerSec    int64            `json:"read_ops_per_sec,omitempty"`
	WriteOpsPerSec   int64            `json:"write_ops_per_sec,omitempty"`
	Devices          map[string]int64 `json:"devices,omitempty"`
}

// NetworkLimits represents network-related limits
type NetworkLimits struct {
	BandwidthBytesPerSec int64 `json:"bandwidth_bytes_per_sec,omitempty"`
}

// EnvironmentInfo provides information about the runtime environment
type EnvironmentInfo struct {
	// Platform (linux, darwin, etc.)
	Platform string `json:"platform"`

	// Architecture (amd64, arm64, etc.)
	Architecture string `json:"architecture"`

	// Container runtime information
	Container *ContainerInfo `json:"container,omitempty"`

	// Virtualization information
	Virtualization *VirtualizationInfo `json:"virtualization,omitempty"`

	// Cgroup information (Linux only)
	Cgroup *CgroupInfo `json:"cgroup,omitempty"`

	// Host information
	Host *HostInfo `json:"host"`
}

// ContainerInfo represents container runtime information
type ContainerInfo struct {
	// Runtime type (docker, podman, containerd, etc.)
	Runtime ContainerRuntime `json:"runtime"`

	// Container ID
	ID string `json:"id,omitempty"`

	// Container name
	Name string `json:"name,omitempty"`

	// Image name
	Image string `json:"image,omitempty"`

	// Orchestrator (kubernetes, swarm, etc.)
	Orchestrator string `json:"orchestrator,omitempty"`

	// Namespace (Kubernetes)
	Namespace string `json:"namespace,omitempty"`

	// Pod name (Kubernetes)
	PodName string `json:"pod_name,omitempty"`
}

// VirtualizationInfo represents virtualization environment information
type VirtualizationInfo struct {
	// Type of virtualization
	Type VirtualizationType `json:"type"`

	// Hypervisor name
	Hypervisor string `json:"hypervisor,omitempty"`

	// Guest environment details
	Guest *GuestInfo `json:"guest,omitempty"`
}

// CgroupInfo represents cgroup information (Linux only)
type CgroupInfo struct {
	// Cgroup version (1 or 2)
	Version CgroupVersion `json:"version"`

	// Cgroup path
	Path string `json:"path"`

	// Controller information
	Controllers []string `json:"controllers"`

	// Hierarchy ID (cgroup v1)
	HierarchyID int `json:"hierarchy_id,omitempty"`
}

// HostInfo represents host system information
type HostInfo struct {
	// Hostname
	Hostname string `json:"hostname"`

	// Operating system
	OS string `json:"os"`

	// OS version
	OSVersion string `json:"os_version"`

	// Kernel version
	KernelVersion string `json:"kernel_version"`

	// Total system memory
	TotalMemory int64 `json:"total_memory"`

	// Total CPU cores
	TotalCores int `json:"total_cores"`
}

// Supporting types

// LimitSource indicates where resource limits come from
type LimitSource string

const (
	LimitSourceUnknown   LimitSource = "unknown"
	LimitSourceSystem    LimitSource = "system"
	LimitSourceContainer LimitSource = "container"
	LimitSourceCgroup    LimitSource = "cgroup"
	LimitSourceVM        LimitSource = "vm"
	LimitSourceK8s       LimitSource = "kubernetes"
)

// ContainerRuntime represents different container runtimes
type ContainerRuntime string

const (
	ContainerRuntimeUnknown    ContainerRuntime = "unknown"
	ContainerRuntimeDocker     ContainerRuntime = "docker"
	ContainerRuntimePodman     ContainerRuntime = "podman"
	ContainerRuntimeContainerd ContainerRuntime = "containerd"
	ContainerRuntimeCRIO       ContainerRuntime = "cri-o"
	ContainerRuntimeSystemd    ContainerRuntime = "systemd"
	ContainerRuntimeLXC        ContainerRuntime = "lxc"
)

// VirtualizationType represents different virtualization types
type VirtualizationType string

const (
	VirtualizationTypeUnknown VirtualizationType = "unknown"
	VirtualizationTypeNone    VirtualizationType = "none"
	VirtualizationTypeKVM     VirtualizationType = "kvm"
	VirtualizationTypeVZ      VirtualizationType = "vz" // Apple Virtualization
	VirtualizationTypeVMware  VirtualizationType = "vmware"
	VirtualizationTypeXen     VirtualizationType = "xen"
	VirtualizationTypeHyperV  VirtualizationType = "hyper-v"
	VirtualizationTypeQEMU    VirtualizationType = "qemu"
)

// CgroupVersion represents cgroup version
type CgroupVersion int

const (
	CgroupVersionUnknown CgroupVersion = 0
	CgroupVersion1       CgroupVersion = 1
	CgroupVersion2       CgroupVersion = 2
)

// MemoryPressure represents memory pressure information (cgroup v2)
type MemoryPressure struct {
	Some *PressureStats `json:"some,omitempty"`
	Full *PressureStats `json:"full,omitempty"`
}

// CPUThrottling represents CPU throttling information
type CPUThrottling struct {
	ThrottledTime    time.Duration `json:"throttled_time"`
	ThrottledPeriods int64         `json:"throttled_periods"`
	TotalPeriods     int64         `json:"total_periods"`
}

// PressureStats represents PSI (Pressure Stall Information) data
type PressureStats struct {
	Avg10  float64 `json:"avg10"`  // 10-second average
	Avg60  float64 `json:"avg60"`  // 60-second average
	Avg300 float64 `json:"avg300"` // 300-second average
	Total  int64   `json:"total"`  // Total time in microseconds
}

// DeviceIOStats represents per-device I/O statistics
type DeviceIOStats struct {
	ReadBytes  int64 `json:"read_bytes"`
	WriteBytes int64 `json:"write_bytes"`
	ReadOps    int64 `json:"read_ops"`
	WriteOps   int64 `json:"write_ops"`
}

// InterfaceStats represents per-interface network statistics
type InterfaceStats struct {
	RxBytes   int64 `json:"rx_bytes"`
	TxBytes   int64 `json:"tx_bytes"`
	RxPackets int64 `json:"rx_packets"`
	TxPackets int64 `json:"tx_packets"`
	RxErrors  int64 `json:"rx_errors"`
	TxErrors  int64 `json:"tx_errors"`
}

// GuestInfo represents guest environment information in VMs
type GuestInfo struct {
	MemoryLimit int64 `json:"memory_limit"`
	CPULimit    int   `json:"cpu_limit"`
}

// NewResourceMonitor creates a new platform-specific resource monitor
func NewResourceMonitor() (ResourceMonitor, error) {
	return newPlatformMonitor()
}

// NewResourceMonitorWithConfig creates a resource monitor with custom configuration
func NewResourceMonitorWithConfig(config *Config) (ResourceMonitor, error) {
	return newPlatformMonitorWithConfig(config)
}

// Config represents configuration for the resource monitor
type Config struct {
	// Update interval for cached values
	UpdateInterval time.Duration `json:"update_interval"`

	// Enable caching of resource data
	EnableCaching bool `json:"enable_caching"`

	// Paths to check for container detection (Linux)
	ContainerPaths []string `json:"container_paths,omitempty"`

	// Paths to check for cgroup information (Linux)
	CgroupPaths []string `json:"cgroup_paths,omitempty"`

	// Enable detailed per-device I/O stats
	EnableDetailedIO bool `json:"enable_detailed_io"`

	// Enable detailed per-interface network stats
	EnableDetailedNetwork bool `json:"enable_detailed_network"`
}

// DefaultConfig returns a default configuration
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

// String methods for enums

func (ls LimitSource) String() string        { return string(ls) }
func (cr ContainerRuntime) String() string   { return string(cr) }
func (vt VirtualizationType) String() string { return string(vt) }
func (cv CgroupVersion) String() string {
	switch cv {
	case CgroupVersion1:
		return "v1"
	case CgroupVersion2:
		return "v2"
	default:
		return "unknown"
	}
}
