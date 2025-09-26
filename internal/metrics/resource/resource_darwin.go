//go:build darwin

package resource

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Darwin system constants
const (
	CTL_VM          = 2
	CTL_HW          = 6
	VM_SWAPUSAGE    = 5
	HW_MEMSIZE      = 24
	HW_NCPU         = 3
	HW_CPUFREQUENCY = 15
)

// DarwinResourceMonitor implements ResourceMonitor for macOS systems
type DarwinResourceMonitor struct {
	config   *Config
	detector *ContainerDetector

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

	// System information cache
	physicalMemory uint64
	cpuCount       int
	systemInfoOnce sync.Once
}

// VMSwapUsage represents swap usage information from Darwin kernel
type VMSwapUsage struct {
	Total     uint64
	Used      uint64
	Free      uint64
	Encrypted uint64
}

// newPlatformMonitor creates a Darwin-specific resource monitor
func newPlatformMonitor() (ResourceMonitor, error) {
	return newPlatformMonitorWithConfig(DefaultConfig())
}

// newPlatformMonitorWithConfig creates a Darwin monitor with custom config
func newPlatformMonitorWithConfig(config *Config) (ResourceMonitor, error) {
	detector := NewContainerDetector()

	monitor := &DarwinResourceMonitor{
		config:   config,
		detector: detector,
	}

	// Initialize system information
	monitor.initSystemInfo()

	return monitor, nil
}

// initSystemInfo initializes system information cache
func (m *DarwinResourceMonitor) initSystemInfo() {
	m.systemInfoOnce.Do(func() {
		// Get physical memory
		if mem, err := m.sysctlUint64("hw.memsize"); err == nil {
			m.physicalMemory = mem
		}

		// Get CPU count
		if cpus, err := m.sysctlInt("hw.ncpu"); err == nil {
			m.cpuCount = cpus
		} else {
			m.cpuCount = runtime.NumCPU()
		}
	})
}

// GetMemoryStats returns current memory usage and limits
func (m *DarwinResourceMonitor) GetMemoryStats(ctx context.Context) (*MemoryStats, error) {
	if m.config.EnableCaching && m.shouldUseCachedData() {
		m.mu.RLock()
		defer m.mu.RUnlock()
		if m.lastMemory != nil {
			return m.lastMemory, nil
		}
	}

	stats := &MemoryStats{
		Timestamp:   time.Now(),
		LimitSource: LimitSourceSystem,
	}

	// Get virtual memory statistics
	if err := m.getVMStats(stats); err != nil {
		return nil, fmt.Errorf("failed to get VM stats: %w", err)
	}

	// Get swap usage
	if err := m.getSwapStats(stats); err != nil {
		// Swap stats are optional, continue without error
	}

	// Check for Apple Virtualization (VZ) limits
	if m.detectVZEnvironment() {
		if vzLimits := m.getVZLimits(); vzLimits != nil {
			if vzLimits.MemoryLimit > 0 {
				stats.LimitBytes = vzLimits.MemoryLimit
				stats.LimitSource = LimitSourceVM
			}
		}
	}

	// Calculate available memory
	stats.AvailableBytes = stats.LimitBytes - stats.UsageBytes
	if stats.AvailableBytes < 0 {
		stats.AvailableBytes = 0
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
func (m *DarwinResourceMonitor) GetCPUStats(ctx context.Context) (*CPUStats, error) {
	if m.config.EnableCaching && m.shouldUseCachedData() {
		m.mu.RLock()
		defer m.mu.RUnlock()
		if m.lastCPU != nil {
			return m.lastCPU, nil
		}
	}

	stats := &CPUStats{
		Timestamp:      time.Now(),
		LimitSource:    LimitSourceSystem,
		LimitCores:     float64(m.cpuCount),
		AvailableCores: float64(m.cpuCount),
	}

	// Get CPU usage
	if err := m.getCPUUsage(stats); err != nil {
		return nil, fmt.Errorf("failed to get CPU usage: %w", err)
	}

	// Check for VZ VM limits
	if m.detectVZEnvironment() {
		if vzLimits := m.getVZLimits(); vzLimits != nil {
			if vzLimits.CPULimit > 0 {
				stats.LimitCores = float64(vzLimits.CPULimit)
				stats.AvailableCores = float64(vzLimits.CPULimit)
				stats.LimitSource = LimitSourceVM
			}
		}
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
func (m *DarwinResourceMonitor) GetIOStats(ctx context.Context) (*IOStats, error) {
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

	// Get I/O statistics using iostat
	if err := m.getIOStatsFromIOStat(stats); err != nil {
		// Fallback to basic process I/O if available
		if err := m.getProcessIOStats(stats); err != nil {
			return nil, fmt.Errorf("failed to get I/O stats: %w", err)
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
func (m *DarwinResourceMonitor) GetNetworkStats(ctx context.Context) (*NetworkStats, error) {
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

	// Get network statistics using netstat
	if err := m.getNetworkStatsFromNetstat(stats); err != nil {
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
func (m *DarwinResourceMonitor) GetLimits(ctx context.Context) (*ResourceLimits, error) {
	limits := &ResourceLimits{}

	// Get memory limits directly to avoid deadlock
	memLimit := int64(m.physicalMemory)
	swapLimit := int64(0) // Default no swap limit
	limitSource := LimitSourceSystem

	// Check for VZ limits
	if m.detectVZEnvironment() {
		if vzLimits := m.getVZLimits(); vzLimits != nil {
			if vzLimits.MemoryLimit > 0 {
				memLimit = vzLimits.MemoryLimit
				limitSource = LimitSourceVM
			}
		}
	}

	limits.Memory = &MemoryLimits{
		LimitBytes:     memLimit,
		SwapLimitBytes: swapLimit,
		Source:         limitSource,
	}

	// Get CPU limits directly
	cpuLimit := float64(m.cpuCount)
	cpuSource := LimitSourceSystem

	// Check for VZ CPU limits
	if m.detectVZEnvironment() {
		if vzLimits := m.getVZLimits(); vzLimits != nil {
			if vzLimits.CPULimit > 0 {
				cpuLimit = float64(vzLimits.CPULimit)
				cpuSource = LimitSourceVM
			}
		}
	}

	limits.CPU = &CPULimits{
		LimitCores: cpuLimit,
		Source:     cpuSource,
	}

	return limits, nil
}

// GetEnvironmentInfo returns information about the runtime environment
func (m *DarwinResourceMonitor) GetEnvironmentInfo() *EnvironmentInfo {
	m.envInfoOnce.Do(func() {
		m.envInfo = &EnvironmentInfo{
			Platform:     "darwin",
			Architecture: runtime.GOARCH,
			Host:         m.getHostInfo(),
		}

		// Add container information (less common on macOS but possible)
		if containerInfo := m.detector.DetectContainer(); containerInfo.Runtime != ContainerRuntimeUnknown {
			m.envInfo.Container = containerInfo
		}

		// Add virtualization information
		m.envInfo.Virtualization = m.detectVirtualization()
	})

	return m.envInfo
}

// IsContainerized returns true if running in a container
func (m *DarwinResourceMonitor) IsContainerized() bool {
	return m.detector.IsContainerized()
}

// Close releases any resources held by the monitor
func (m *DarwinResourceMonitor) Close() error {
	// No resources to clean up currently
	return nil
}

// Helper methods

func (m *DarwinResourceMonitor) shouldUseCachedData() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return time.Since(m.lastUpdate) < m.config.UpdateInterval
}

// getVMStats gets virtual memory statistics using sysctl
func (m *DarwinResourceMonitor) getVMStats(stats *MemoryStats) error {
	// Use vm_stat command to get memory information
	cmd := exec.Command("vm_stat")
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	pageSize := int64(4096) // Default page size, can be obtained via sysctl
	if ps, err := m.sysctlInt("hw.pagesize"); err == nil {
		pageSize = int64(ps)
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.Contains(line, "Pages free:") {
			if pages := m.extractPagesFromVMStat(line); pages > 0 {
				stats.AvailableBytes = pages * pageSize
			}
		} else if strings.Contains(line, "Pages wired down:") {
			if pages := m.extractPagesFromVMStat(line); pages > 0 {
				stats.UsageBytes += pages * pageSize
			}
		} else if strings.Contains(line, "Pages active:") {
			if pages := m.extractPagesFromVMStat(line); pages > 0 {
				stats.UsageBytes += pages * pageSize
			}
		} else if strings.Contains(line, "Pages inactive:") {
			if pages := m.extractPagesFromVMStat(line); pages > 0 {
				stats.CacheBytes = pages * pageSize
			}
		}
	}

	// Set total memory limit
	stats.LimitBytes = int64(m.physicalMemory)

	return nil
}

// extractPagesFromVMStat extracts page count from vm_stat output line
func (m *DarwinResourceMonitor) extractPagesFromVMStat(line string) int64 {
	parts := strings.Fields(line)
	if len(parts) >= 3 {
		// Remove trailing period and parse
		pageStr := strings.TrimSuffix(parts[len(parts)-1], ".")
		if pages, err := strconv.ParseInt(pageStr, 10, 64); err == nil {
			return pages
		}
	}
	return 0
}

// getSwapStats gets swap usage statistics
func (m *DarwinResourceMonitor) getSwapStats(stats *MemoryStats) error {
	// Try to use sysctl to get swap usage
	var swapUsage VMSwapUsage
	size := unsafe.Sizeof(swapUsage)
	mib := [2]int32{CTL_VM, VM_SWAPUSAGE}

	ret, _, errno := syscall.Syscall6(
		syscall.SYS___SYSCTL,
		uintptr(unsafe.Pointer(&mib[0])),
		2,
		uintptr(unsafe.Pointer(&swapUsage)),
		uintptr(unsafe.Pointer(&size)),
		0,
		0,
	)

	if ret != 0 {
		return fmt.Errorf("sysctl failed with errno %d", errno)
	}

	stats.SwapLimitBytes = int64(swapUsage.Total)
	stats.SwapUsageBytes = int64(swapUsage.Used)

	return nil
}

// getCPUUsage gets CPU usage statistics
func (m *DarwinResourceMonitor) getCPUUsage(stats *CPUStats) error {
	// Use top command to get CPU usage
	cmd := exec.Command("top", "-l", "1", "-n", "0")
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "CPU usage:") {
			// Parse CPU usage line: "CPU usage: 5.23% user, 2.34% sys, 92.43% idle"
			parts := strings.Split(line, ",")
			var userPercent, sysPercent float64

			for _, part := range parts {
				part = strings.TrimSpace(part)
				if strings.Contains(part, "user") {
					if pct := m.extractPercentage(part); pct >= 0 {
						userPercent = pct
					}
				} else if strings.Contains(part, "sys") {
					if pct := m.extractPercentage(part); pct >= 0 {
						sysPercent = pct
					}
				}
			}

			stats.UsagePercent = userPercent + sysPercent
			break
		}
	}

	return nil
}

// extractPercentage extracts percentage value from string like "5.23% user"
func (m *DarwinResourceMonitor) extractPercentage(s string) float64 {
	parts := strings.Fields(s)
	for _, part := range parts {
		if strings.HasSuffix(part, "%") {
			if pct, err := strconv.ParseFloat(strings.TrimSuffix(part, "%"), 64); err == nil {
				return pct
			}
		}
	}
	return -1
}

// getIOStatsFromIOStat gets I/O statistics using iostat command
func (m *DarwinResourceMonitor) getIOStatsFromIOStat(stats *IOStats) error {
	cmd := exec.Command("iostat", "-d", "-c", "1")
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	lines := strings.Split(string(output), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "disk") && strings.Contains(line, "KB/t") {
			// Found header, parse next line
			if i+1 < len(lines) {
				dataLine := strings.TrimSpace(lines[i+1])
				fields := strings.Fields(dataLine)
				if len(fields) >= 3 {
					// Fields: device KB/t tps MB/s
					if tps, err := strconv.ParseFloat(fields[1], 64); err == nil {
						if mbps, err := strconv.ParseFloat(fields[2], 64); err == nil {
							// Estimate read/write bytes (this is approximate)
							totalBytes := int64(mbps * 1024 * 1024) // Convert MB/s to bytes
							stats.ReadBytes = totalBytes / 2        // Assume 50/50 split
							stats.WriteBytes = totalBytes / 2
							stats.ReadOps = int64(tps / 2)
							stats.WriteOps = int64(tps / 2)
						}
					}
				}
				break
			}
		}
	}

	return nil
}

// getProcessIOStats gets process-specific I/O statistics (limited on macOS)
func (m *DarwinResourceMonitor) getProcessIOStats(stats *IOStats) error {
	// macOS doesn't provide easy access to process I/O stats like Linux
	// This is a placeholder for potential future implementation
	return fmt.Errorf("process I/O stats not available on macOS")
}

// getNetworkStatsFromNetstat gets network statistics using netstat
func (m *DarwinResourceMonitor) getNetworkStatsFromNetstat(stats *NetworkStats) error {
	cmd := exec.Command("netstat", "-ib")
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	if m.config.EnableDetailedNetwork {
		stats.Interfaces = make(map[string]*InterfaceStats)
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Name") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) >= 10 {
			interfaceName := fields[0]

			// Skip loopback interface
			if interfaceName == "lo0" {
				continue
			}

			rxBytes, _ := strconv.ParseInt(fields[6], 10, 64)
			rxPackets, _ := strconv.ParseInt(fields[4], 10, 64)
			rxErrors, _ := strconv.ParseInt(fields[5], 10, 64)
			txBytes, _ := strconv.ParseInt(fields[9], 10, 64)
			txPackets, _ := strconv.ParseInt(fields[7], 10, 64)
			txErrors, _ := strconv.ParseInt(fields[8], 10, 64)

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

	return nil
}

// getHostInfo gets host system information
func (m *DarwinResourceMonitor) getHostInfo() *HostInfo {
	m.hostInfoOnce.Do(func() {
		m.hostInfo = &HostInfo{
			OS:          "darwin",
			TotalCores:  m.cpuCount,
			TotalMemory: int64(m.physicalMemory),
		}

		// Get hostname
		if hostname, err := os.Hostname(); err == nil {
			m.hostInfo.Hostname = hostname
		}

		// Get macOS version
		if version, err := m.getMacOSVersion(); err == nil {
			m.hostInfo.OSVersion = version
		}

		// Get kernel version
		if kernel, err := m.sysctlString("kern.version"); err == nil {
			m.hostInfo.KernelVersion = kernel
		}
	})

	return m.hostInfo
}

// getMacOSVersion gets macOS version information
func (m *DarwinResourceMonitor) getMacOSVersion() (string, error) {
	cmd := exec.Command("sw_vers", "-productVersion")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// detectVirtualization detects virtualization environment
func (m *DarwinResourceMonitor) detectVirtualization() *VirtualizationInfo {
	virt := &VirtualizationInfo{
		Type: VirtualizationTypeNone,
	}

	// Check for Apple Virtualization (VZ)
	if m.detectVZEnvironment() {
		virt.Type = VirtualizationTypeVZ
		virt.Hypervisor = "Apple Virtualization"
		if guest := m.getVZLimits(); guest != nil {
			virt.Guest = guest
		}
	}

	// Check for other virtualization indicators
	if m.checkHardwareModel() {
		// Additional virtualization detection can be added here
	}

	return virt
}

// detectVZEnvironment detects Apple Virtualization environment
func (m *DarwinResourceMonitor) detectVZEnvironment() bool {
	// Check for VZ-specific indicators
	if model, err := m.sysctlString("hw.model"); err == nil {
		if strings.Contains(strings.ToLower(model), "virtualization") {
			return true
		}
	}

	// Check for VZ kernel extensions
	if _, err := os.Stat("/System/Library/Extensions/VZKit.kext"); err == nil {
		return true
	}

	return false
}

// getVZLimits gets resource limits in VZ environment
func (m *DarwinResourceMonitor) getVZLimits() *GuestInfo {
	// This is a placeholder - actual VZ limit detection would require
	// VZ-specific APIs or system information
	return &GuestInfo{
		MemoryLimit: int64(m.physicalMemory),
		CPULimit:    m.cpuCount,
	}
}

// checkHardwareModel checks hardware model for virtualization indicators
func (m *DarwinResourceMonitor) checkHardwareModel() bool {
	if model, err := m.sysctlString("hw.model"); err == nil {
		model = strings.ToLower(model)
		virtualIndicators := []string{"vmware", "parallels", "virtualbox", "qemu"}
		for _, indicator := range virtualIndicators {
			if strings.Contains(model, indicator) {
				return true
			}
		}
	}
	return false
}

// sysctlString gets string value via sysctl
func (m *DarwinResourceMonitor) sysctlString(name string) (string, error) {
	cmd := exec.Command("sysctl", "-n", name)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// sysctlInt gets integer value via sysctl
func (m *DarwinResourceMonitor) sysctlInt(name string) (int, error) {
	str, err := m.sysctlString(name)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(str)
}

// sysctlUint64 gets uint64 value via sysctl
func (m *DarwinResourceMonitor) sysctlUint64(name string) (uint64, error) {
	str, err := m.sysctlString(name)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(str, 10, 64)
}
