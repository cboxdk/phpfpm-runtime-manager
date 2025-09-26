//go:build linux

package resource

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LinuxResourceMonitor implements ResourceMonitor for Linux systems
type LinuxResourceMonitor struct {
	config    *Config
	detector  *ContainerDetector
	cgroupMgr *CgroupManager

	// Cached environment info
	envInfo     *EnvironmentInfo
	envInfoOnce sync.Once

	// Cached data
	mu          sync.RWMutex
	lastMemory  *MemoryStats
	lastCPU     *CPUStats
	lastIO      *IOStats
	lastNetwork *NetworkStats
	lastUpdate  time.Time

	// Host information cache
	hostInfo     *HostInfo
	hostInfoOnce sync.Once
}

// newPlatformMonitor creates a Linux-specific resource monitor
func newPlatformMonitor() (ResourceMonitor, error) {
	return newPlatformMonitorWithConfig(DefaultConfig())
}

// newPlatformMonitorWithConfig creates a Linux monitor with custom config
func newPlatformMonitorWithConfig(config *Config) (ResourceMonitor, error) {
	detector := NewContainerDetector()
	cgroupMgr, err := NewCgroupManager()
	if err != nil {
		// Continue without cgroup support
		cgroupMgr = nil
	}

	return &LinuxResourceMonitor{
		config:    config,
		detector:  detector,
		cgroupMgr: cgroupMgr,
	}, nil
}

// GetMemoryStats returns current memory usage and limits
func (m *LinuxResourceMonitor) GetMemoryStats(ctx context.Context) (*MemoryStats, error) {
	if m.config.EnableCaching && m.shouldUseCachedData() {
		m.mu.RLock()
		defer m.mu.RUnlock()
		if m.lastMemory != nil {
			return m.lastMemory, nil
		}
	}

	stats := &MemoryStats{
		Timestamp: time.Now(),
	}

	// Try to get memory stats from cgroup first
	if m.cgroupMgr != nil {
		if cgroupStats, err := m.cgroupMgr.GetMemoryStats(); err == nil {
			stats.UsageBytes = cgroupStats.UsageBytes
			stats.LimitBytes = cgroupStats.LimitBytes
			stats.RSSBytes = cgroupStats.RSSBytes
			stats.CacheBytes = cgroupStats.CacheBytes
			stats.SwapUsageBytes = cgroupStats.SwapUsageBytes
			stats.SwapLimitBytes = cgroupStats.SwapLimitBytes
			stats.LimitSource = LimitSourceCgroup
			stats.Pressure = cgroupStats.Pressure
		}
	}

	// Fallback to /proc/meminfo for system-wide stats
	if stats.UsageBytes == 0 {
		if err := m.getMemoryFromProc(stats); err != nil {
			return nil, fmt.Errorf("failed to get memory stats: %w", err)
		}
	}

	// Calculate available memory
	if stats.AvailableBytes == 0 {
		stats.AvailableBytes = stats.LimitBytes - stats.UsageBytes
		if stats.AvailableBytes < 0 {
			stats.AvailableBytes = 0
		}
	}

	// Cache the result
	if m.config.EnableCaching {
		m.mu.Lock()
		m.lastMemory = stats
		m.lastUpdate = time.Now()
		m.mu.Unlock()
	}

	return stats, nil
}

// GetCPUStats returns current CPU usage and limits
func (m *LinuxResourceMonitor) GetCPUStats(ctx context.Context) (*CPUStats, error) {
	if m.config.EnableCaching && m.shouldUseCachedData() {
		m.mu.RLock()
		defer m.mu.RUnlock()
		if m.lastCPU != nil {
			return m.lastCPU, nil
		}
	}

	stats := &CPUStats{
		Timestamp: time.Now(),
	}

	// Try to get CPU stats from cgroup first
	if m.cgroupMgr != nil {
		if cgroupStats, err := m.cgroupMgr.GetCPUStats(); err == nil {
			stats.LimitCores = cgroupStats.LimitCores
			stats.Shares = cgroupStats.Shares
			stats.QuotaPeriodUs = cgroupStats.QuotaPeriodUs
			stats.QuotaUs = cgroupStats.QuotaUs
			stats.Throttling = cgroupStats.Throttling
			stats.LimitSource = LimitSourceCgroup

			// Calculate available cores from quota
			if stats.QuotaUs > 0 && stats.QuotaPeriodUs > 0 {
				stats.AvailableCores = float64(stats.QuotaUs) / float64(stats.QuotaPeriodUs)
			}
		}
	}

	// Get system CPU count and usage
	if err := m.getCPUFromProc(stats); err != nil {
		return nil, fmt.Errorf("failed to get CPU stats: %w", err)
	}

	// Set defaults if not set by cgroup
	if stats.LimitCores == 0 {
		stats.LimitCores = float64(runtime.NumCPU())
		stats.LimitSource = LimitSourceSystem
	}
	if stats.AvailableCores == 0 {
		stats.AvailableCores = stats.LimitCores
	}

	// Cache the result
	if m.config.EnableCaching {
		m.mu.Lock()
		m.lastCPU = stats
		m.lastUpdate = time.Now()
		m.mu.Unlock()
	}

	return stats, nil
}

// GetIOStats returns current I/O statistics
func (m *LinuxResourceMonitor) GetIOStats(ctx context.Context) (*IOStats, error) {
	if m.config.EnableCaching && m.shouldUseCachedData() {
		m.mu.RLock()
		defer m.mu.RUnlock()
		if m.lastIO != nil {
			return m.lastIO, nil
		}
	}

	stats := &IOStats{
		Timestamp: time.Now(),
	}

	// Try to get I/O stats from cgroup first
	if m.cgroupMgr != nil {
		if cgroupStats, err := m.cgroupMgr.GetIOStats(); err == nil {
			stats.ReadBytes = cgroupStats.ReadBytes
			stats.WriteBytes = cgroupStats.WriteBytes
			stats.ReadOps = cgroupStats.ReadOps
			stats.WriteOps = cgroupStats.WriteOps
		}
	}

	// Get I/O stats from /proc/self/io
	if err := m.getIOFromProc(stats); err != nil {
		return nil, fmt.Errorf("failed to get I/O stats: %w", err)
	}

	// Get detailed device stats if enabled
	if m.config.EnableDetailedIO {
		if err := m.getDetailedIOStats(stats); err != nil {
			// Log error but don't fail
		}
	}

	// Cache the result
	if m.config.EnableCaching {
		m.mu.Lock()
		m.lastIO = stats
		m.lastUpdate = time.Now()
		m.mu.Unlock()
	}

	return stats, nil
}

// GetNetworkStats returns network usage statistics
func (m *LinuxResourceMonitor) GetNetworkStats(ctx context.Context) (*NetworkStats, error) {
	if m.config.EnableCaching && m.shouldUseCachedData() {
		m.mu.RLock()
		defer m.mu.RUnlock()
		if m.lastNetwork != nil {
			return m.lastNetwork, nil
		}
	}

	stats := &NetworkStats{
		Timestamp: time.Now(),
	}

	// Get network stats from /proc/net/dev
	if err := m.getNetworkFromProc(stats); err != nil {
		return nil, fmt.Errorf("failed to get network stats: %w", err)
	}

	// Cache the result
	if m.config.EnableCaching {
		m.mu.Lock()
		m.lastNetwork = stats
		m.lastUpdate = time.Now()
		m.mu.Unlock()
	}

	return stats, nil
}

// GetLimits returns resource limits for the current environment
func (m *LinuxResourceMonitor) GetLimits(ctx context.Context) (*ResourceLimits, error) {
	limits := &ResourceLimits{}

	// Get memory limits
	if memStats, err := m.GetMemoryStats(ctx); err == nil {
		limits.Memory = &MemoryLimits{
			LimitBytes:     memStats.LimitBytes,
			SwapLimitBytes: memStats.SwapLimitBytes,
			Source:         memStats.LimitSource,
		}
	}

	// Get CPU limits
	if cpuStats, err := m.GetCPUStats(ctx); err == nil {
		limits.CPU = &CPULimits{
			LimitCores:    cpuStats.LimitCores,
			Shares:        cpuStats.Shares,
			QuotaPeriodUs: cpuStats.QuotaPeriodUs,
			QuotaUs:       cpuStats.QuotaUs,
			Source:        cpuStats.LimitSource,
		}
	}

	// Get I/O limits (if available from cgroup)
	if m.cgroupMgr != nil {
		if ioLimits, err := m.cgroupMgr.GetIOLimits(); err == nil {
			limits.IO = ioLimits
		}
	}

	return limits, nil
}

// GetEnvironmentInfo returns information about the runtime environment
func (m *LinuxResourceMonitor) GetEnvironmentInfo() *EnvironmentInfo {
	m.envInfoOnce.Do(func() {
		m.envInfo = &EnvironmentInfo{
			Platform:     "linux",
			Architecture: runtime.GOARCH,
			Host:         m.getHostInfo(),
		}

		// Add container information
		if containerInfo := m.detector.DetectContainer(); containerInfo.Runtime != ContainerRuntimeUnknown {
			m.envInfo.Container = containerInfo
		}

		// Add cgroup information
		if m.cgroupMgr != nil {
			m.envInfo.Cgroup = m.cgroupMgr.GetInfo()
		}

		// Add virtualization information
		m.envInfo.Virtualization = m.detectVirtualization()
	})

	return m.envInfo
}

// IsContainerized returns true if running in a container
func (m *LinuxResourceMonitor) IsContainerized() bool {
	return m.detector.IsContainerized()
}

// Close releases any resources held by the monitor
func (m *LinuxResourceMonitor) Close() error {
	// No resources to clean up currently
	return nil
}

// Helper methods

func (m *LinuxResourceMonitor) shouldUseCachedData() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return time.Since(m.lastUpdate) < m.config.UpdateInterval
}

func (m *LinuxResourceMonitor) getMemoryFromProc(stats *MemoryStats) error {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return err
	}
	defer file.Close()

	memInfo := make(map[string]int64)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			key := strings.TrimSuffix(parts[0], ":")
			if value, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				// Convert from KB to bytes
				memInfo[key] = value * 1024
			}
		}
	}

	if total, ok := memInfo["MemTotal"]; ok {
		stats.LimitBytes = total
		stats.LimitSource = LimitSourceSystem
	}

	if available, ok := memInfo["MemAvailable"]; ok {
		stats.AvailableBytes = available
		stats.UsageBytes = stats.LimitBytes - available
	} else if free, ok := memInfo["MemFree"]; ok {
		// Fallback calculation
		buffers := memInfo["Buffers"]
		cached := memInfo["Cached"]
		stats.AvailableBytes = free + buffers + cached
		stats.UsageBytes = stats.LimitBytes - stats.AvailableBytes
	}

	// Set cache and RSS
	if cached, ok := memInfo["Cached"]; ok {
		stats.CacheBytes = cached
	}

	// Set swap information
	if swapTotal, ok := memInfo["SwapTotal"]; ok {
		stats.SwapLimitBytes = swapTotal
	}
	if swapFree, ok := memInfo["SwapFree"]; ok {
		stats.SwapUsageBytes = stats.SwapLimitBytes - swapFree
	}

	return scanner.Err()
}

func (m *LinuxResourceMonitor) getCPUFromProc(stats *CPUStats) error {
	// Get CPU usage from /proc/stat
	file, err := os.Open("/proc/stat")
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "cpu ") {
			// Parse overall CPU stats
			parts := strings.Fields(line)
			if len(parts) >= 8 {
				// Calculate CPU usage percentage
				var total, idle int64
				for i := 1; i < len(parts); i++ {
					if val, err := strconv.ParseInt(parts[i], 10, 64); err == nil {
						total += val
						if i == 4 { // idle time is the 4th field
							idle = val
						}
					}
				}
				if total > 0 {
					stats.UsagePercent = float64(total-idle) / float64(total) * 100
				}
			}
			break
		}
	}

	return scanner.Err()
}

func (m *LinuxResourceMonitor) getIOFromProc(stats *IOStats) error {
	file, err := os.Open("/proc/self/io")
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ": ", 2)
		if len(parts) != 2 {
			continue
		}

		value, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			continue
		}

		switch parts[0] {
		case "read_bytes":
			stats.ReadBytes = value
		case "write_bytes":
			stats.WriteBytes = value
		case "syscr":
			stats.ReadOps = value
		case "syscw":
			stats.WriteOps = value
		}
	}

	return scanner.Err()
}

func (m *LinuxResourceMonitor) getDetailedIOStats(stats *IOStats) error {
	// Get per-device I/O stats from /proc/diskstats
	file, err := os.Open("/proc/diskstats")
	if err != nil {
		return err
	}
	defer file.Close()

	stats.Devices = make(map[string]*DeviceIOStats)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) >= 14 {
			deviceName := parts[2]
			if strings.HasPrefix(deviceName, "loop") || strings.HasPrefix(deviceName, "ram") {
				continue // Skip loop and ram devices
			}

			deviceStats := &DeviceIOStats{}
			if readSectors, err := strconv.ParseInt(parts[5], 10, 64); err == nil {
				deviceStats.ReadBytes = readSectors * 512 // Sectors are 512 bytes
			}
			if writeSectors, err := strconv.ParseInt(parts[9], 10, 64); err == nil {
				deviceStats.WriteBytes = writeSectors * 512
			}
			if readOps, err := strconv.ParseInt(parts[3], 10, 64); err == nil {
				deviceStats.ReadOps = readOps
			}
			if writeOps, err := strconv.ParseInt(parts[7], 10, 64); err == nil {
				deviceStats.WriteOps = writeOps
			}

			stats.Devices[deviceName] = deviceStats
		}
	}

	return scanner.Err()
}

func (m *LinuxResourceMonitor) getNetworkFromProc(stats *NetworkStats) error {
	file, err := os.Open("/proc/net/dev")
	if err != nil {
		return err
	}
	defer file.Close()

	if m.config.EnableDetailedNetwork {
		stats.Interfaces = make(map[string]*InterfaceStats)
	}

	scanner := bufio.NewScanner(file)
	// Skip header lines
	scanner.Scan()
	scanner.Scan()

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) >= 17 {
			interfaceName := strings.TrimSuffix(parts[0], ":")

			// Skip loopback interface
			if interfaceName == "lo" {
				continue
			}

			rxBytes, _ := strconv.ParseInt(parts[1], 10, 64)
			rxPackets, _ := strconv.ParseInt(parts[2], 10, 64)
			rxErrors, _ := strconv.ParseInt(parts[3], 10, 64)
			txBytes, _ := strconv.ParseInt(parts[9], 10, 64)
			txPackets, _ := strconv.ParseInt(parts[10], 10, 64)
			txErrors, _ := strconv.ParseInt(parts[11], 10, 64)

			// Add to totals
			stats.RxBytes += rxBytes
			stats.TxBytes += txBytes
			stats.RxPackets += rxPackets
			stats.TxPackets += txPackets
			stats.RxErrors += rxErrors
			stats.TxErrors += txErrors

			// Add detailed interface stats if enabled
			if m.config.EnableDetailedNetwork {
				stats.Interfaces[interfaceName] = &InterfaceStats{
					RxBytes:   rxBytes,
					TxBytes:   txBytes,
					RxPackets: rxPackets,
					TxPackets: txPackets,
					RxErrors:  rxErrors,
					TxErrors:  txErrors,
				}
			}
		}
	}

	return scanner.Err()
}

func (m *LinuxResourceMonitor) getHostInfo() *HostInfo {
	m.hostInfoOnce.Do(func() {
		m.hostInfo = &HostInfo{
			OS:         "linux",
			TotalCores: runtime.NumCPU(),
		}

		// Get hostname
		if hostname, err := os.Hostname(); err == nil {
			m.hostInfo.Hostname = hostname
		}

		// Get kernel version
		if data, err := os.ReadFile("/proc/version"); err == nil {
			m.hostInfo.KernelVersion = strings.TrimSpace(string(data))
		}

		// Get OS version from /etc/os-release
		if data, err := os.ReadFile("/etc/os-release"); err == nil {
			m.parseOSRelease(string(data))
		}

		// Get total memory
		if memStats, err := m.GetMemoryStats(context.Background()); err == nil {
			m.hostInfo.TotalMemory = memStats.LimitBytes
		}
	})

	return m.hostInfo
}

func (m *LinuxResourceMonitor) parseOSRelease(content string) {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			m.hostInfo.OSVersion = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
			break
		}
	}
}

func (m *LinuxResourceMonitor) detectVirtualization() *VirtualizationInfo {
	virt := &VirtualizationInfo{
		Type: VirtualizationTypeNone,
	}

	// Check for various virtualization indicators
	if m.checkDMI() {
		return virt
	}

	if m.checkCPUFlags() {
		return virt
	}

	if m.checkVirtualFS() {
		return virt
	}

	return virt
}

func (m *LinuxResourceMonitor) checkDMI() bool {
	// Check DMI information for virtualization
	dmiPaths := []string{
		"/sys/class/dmi/id/product_name",
		"/sys/class/dmi/id/sys_vendor",
		"/sys/class/dmi/id/board_vendor",
	}

	for _, path := range dmiPaths {
		if data, err := os.ReadFile(path); err == nil {
			content := strings.ToLower(strings.TrimSpace(string(data)))
			if strings.Contains(content, "vmware") ||
				strings.Contains(content, "virtualbox") ||
				strings.Contains(content, "qemu") ||
				strings.Contains(content, "kvm") ||
				strings.Contains(content, "xen") ||
				strings.Contains(content, "microsoft") ||
				strings.Contains(content, "hyper-v") {
				return true
			}
		}
	}
	return false
}

func (m *LinuxResourceMonitor) checkCPUFlags() bool {
	file, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "flags") {
			if strings.Contains(line, "hypervisor") {
				return true
			}
			break
		}
	}
	return false
}

func (m *LinuxResourceMonitor) checkVirtualFS() bool {
	// Check for virtualization-specific filesystems
	virtualPaths := []string{
		"/proc/xen",
		"/sys/bus/xen",
		"/proc/device-tree/hypervisor/compatible",
	}

	for _, path := range virtualPaths {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}
