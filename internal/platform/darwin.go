//go:build darwin

package platform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DarwinProvider implements the Provider interface for macOS/Darwin systems
type DarwinProvider struct {
	config     *Config
	memory     *darwinMemoryProvider
	process    *darwinProcessProvider
	filesystem *darwinFileSystemProvider
	mu         sync.RWMutex
}

// Memory returns the memory provider
func (p *DarwinProvider) Memory() MemoryProvider {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.memory
}

// Process returns the process provider
func (p *DarwinProvider) Process() ProcessProvider {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.process
}

// FileSystem returns the filesystem provider
func (p *DarwinProvider) FileSystem() FileSystemProvider {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.filesystem
}

// Platform returns the platform identifier
func (p *DarwinProvider) Platform() string {
	return "darwin"
}

// IsSupported returns true if the platform is fully supported
func (p *DarwinProvider) IsSupported() bool {
	return p.memory.IsSupported() && p.process.IsSupported()
}

// darwinMemoryProvider implements MemoryProvider for macOS/Darwin
type darwinMemoryProvider struct {
	config *Config
	cache  map[string]*cachedMemoryInfo
	mu     sync.RWMutex
}

// cachedMemoryInfo stores cached memory information with TTL
type cachedMemoryInfo struct {
	info      *MemoryInfo
	timestamp time.Time
}

// GetTotalMemory returns the total physical memory in bytes
func (m *darwinMemoryProvider) GetTotalMemory(ctx context.Context) (uint64, error) {
	info, err := m.GetMemoryInfo(ctx)
	if err != nil {
		return 0, err
	}
	return info.TotalBytes, nil
}

// GetAvailableMemory returns available memory for allocation in bytes
func (m *darwinMemoryProvider) GetAvailableMemory(ctx context.Context) (uint64, error) {
	info, err := m.GetMemoryInfo(ctx)
	if err != nil {
		return 0, err
	}
	return info.AvailableBytes, nil
}

// Platform returns the platform identifier
func (m *darwinMemoryProvider) Platform() string {
	return "darwin"
}

// GetMemoryInfo returns current memory information using sysctl
func (m *darwinMemoryProvider) GetMemoryInfo(ctx context.Context) (*MemoryInfo, error) {
	if m.config.CacheEnabled {
		m.mu.RLock()
		if cached, exists := m.cache["memory"]; exists {
			if time.Since(cached.timestamp) < m.config.CacheTTL {
				m.mu.RUnlock()
				return cached.info, nil
			}
		}
		m.mu.RUnlock()
	}

	info, err := m.collectMemoryInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to collect memory info: %w", err)
	}

	if m.config.CacheEnabled {
		m.mu.Lock()
		m.cache["memory"] = &cachedMemoryInfo{
			info:      info,
			timestamp: time.Now(),
		}
		m.mu.Unlock()
	}

	return info, nil
}

// collectMemoryInfo gathers memory information using Darwin syscalls
func (m *darwinMemoryProvider) collectMemoryInfo(ctx context.Context) (*MemoryInfo, error) {
	// Get total physical memory using sysctl
	totalMem, err := getSysctlUint64("hw.memsize")
	if err != nil {
		return nil, fmt.Errorf("failed to get total memory: %w", err)
	}

	// Get page size
	pageSize, err := getSysctlUint64("hw.pagesize")
	if err != nil {
		return nil, fmt.Errorf("failed to get page size: %w", err)
	}

	// Get VM statistics using vm_stat command
	vmStats, err := getVMStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get VM stats: %w", err)
	}

	// Calculate memory usage
	freePages := vmStats["Pages free"]
	inactivePages := vmStats["Pages inactive"]
	speculativePages := vmStats["Pages speculative"]
	_ = vmStats["Pages wired down"] // wiredPages not used
	_ = vmStats["Pages active"]     // activePages not used

	freeMem := (freePages + inactivePages + speculativePages) * pageSize
	usedMem := totalMem - freeMem
	availableMem := freeMem
	buffersMem := uint64(0) // macOS doesn't distinguish buffers like Linux
	cachedMem := inactivePages * pageSize

	return &MemoryInfo{
		TotalBytes:     totalMem,
		FreeBytes:      freeMem,
		UsedBytes:      usedMem,
		AvailableBytes: availableMem,
		BufferedBytes:  buffersMem,
		CachedBytes:    cachedMem,
		Timestamp:      time.Now(),
		Platform:       "darwin",
	}, nil
}

// getVMStats parses vm_stat output to get memory statistics
func getVMStats(ctx context.Context) (map[string]uint64, error) {
	cmd := exec.CommandContext(ctx, "vm_stat")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute vm_stat: %w", err)
	}

	stats := make(map[string]uint64)
	lines := strings.Split(string(output), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Mach Virtual Memory Statistics:") {
			continue
		}

		// Parse lines like "Pages free: 123456."
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		valueStr := strings.TrimSpace(strings.TrimSuffix(parts[1], "."))

		value, err := strconv.ParseUint(valueStr, 10, 64)
		if err != nil {
			continue
		}

		stats[key] = value
	}

	return stats, nil
}

// getSysctlUint64 retrieves a uint64 value using sysctl
func getSysctlUint64(name string) (uint64, error) {
	cmd := exec.Command("sysctl", "-n", name)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to execute sysctl: %w", err)
	}

	value, err := strconv.ParseUint(strings.TrimSpace(string(output)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse sysctl value: %w", err)
	}

	return value, nil
}

// IsSupported returns true if memory operations are supported
func (m *darwinMemoryProvider) IsSupported() bool {
	return true
}

// darwinProcessProvider implements ProcessProvider for macOS/Darwin
type darwinProcessProvider struct {
	config *Config
	cache  map[int]*cachedProcessInfo
	mu     sync.RWMutex
}

// cachedProcessInfo stores cached process information with TTL
type cachedProcessInfo struct {
	info      *ProcessInfo
	timestamp time.Time
}

// GetProcessInfo returns information about a specific process
func (p *darwinProcessProvider) GetProcessInfo(ctx context.Context, pid int) (*ProcessInfo, error) {
	if p.config.CacheEnabled {
		p.mu.RLock()
		if cached, exists := p.cache[pid]; exists {
			if time.Since(cached.timestamp) < p.config.CacheTTL {
				p.mu.RUnlock()
				return cached.info, nil
			}
		}
		p.mu.RUnlock()
	}

	info, err := p.collectProcessInfo(ctx, pid)
	if err != nil {
		return nil, fmt.Errorf("failed to collect process info: %w", err)
	}

	if p.config.CacheEnabled {
		p.mu.Lock()
		p.cache[pid] = &cachedProcessInfo{
			info:      info,
			timestamp: time.Now(),
		}
		p.mu.Unlock()
	}

	return info, nil
}

// collectProcessInfo gathers process information using Darwin syscalls
func (p *darwinProcessProvider) collectProcessInfo(ctx context.Context, pid int) (*ProcessInfo, error) {
	// Get process information using ps command
	cmd := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "pid,ppid,uid,gid,vsz,rss,pcpu,time,comm")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("process not found or ps command failed: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("invalid ps output format")
	}

	// Parse the process information line
	fields := strings.Fields(lines[1])
	if len(fields) < 9 {
		return nil, fmt.Errorf("insufficient ps output fields")
	}

	// Parse fields
	parsedPID, _ := strconv.Atoi(fields[0])
	ppid, _ := strconv.Atoi(fields[1])
	_ = fields[2] // uid not used in current implementation
	_ = fields[3] // gid not used in current implementation
	vsize, _ := strconv.ParseUint(fields[4], 10, 64)
	rss, _ := strconv.ParseUint(fields[5], 10, 64)
	cpuPercent, _ := strconv.ParseFloat(fields[6], 64)
	command := strings.Join(fields[8:], " ")

	// Convert VSZ from KB to bytes, RSS from KB to bytes
	vsize *= 1024
	rss *= 1024

	// Get additional process information using sysctl
	processName, err := getSysctlString(fmt.Sprintf("kern.proc.pid.%d", pid))
	if err != nil {
		processName = command // fallback to ps command field
	}

	return &ProcessInfo{
		PID:           parsedPID,
		PPID:          ppid,
		Name:          processName,
		Command:       command,
		State:         "unknown",
		CPUPercent:    cpuPercent,
		MemoryBytes:   rss,
		VirtualBytes:  vsize,
		ResidentBytes: rss,
		StartTime:     time.Now(),
		UserTime:      0,
		SystemTime:    0,
		Platform:      "darwin",
	}, nil
}

// getSysctlString retrieves a string value using sysctl
func getSysctlString(name string) (string, error) {
	cmd := exec.Command("sysctl", "-n", name)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to execute sysctl: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// Platform returns the platform identifier
func (p *darwinProcessProvider) Platform() string {
	return "darwin"
}

// MonitorProcess starts monitoring a process for resource usage
func (p *darwinProcessProvider) MonitorProcess(ctx context.Context, pid int) (ProcessMonitor, error) {
	return nil, fmt.Errorf("process monitoring not implemented for Darwin")
}

// ListProcesses returns a list of all running processes
func (p *darwinProcessProvider) ListProcesses(ctx context.Context, filter ProcessFilter) ([]*ProcessInfo, error) {
	// Get all processes first
	allProcesses, err := p.getAllProcesses(ctx)
	if err != nil {
		return nil, err
	}

	// Apply filter
	var filteredProcesses []*ProcessInfo
	for _, process := range allProcesses {
		if p.matchesFilter(process, filter) {
			filteredProcesses = append(filteredProcesses, process)
		}
	}

	return filteredProcesses, nil
}

// getAllProcesses gets all running processes without filtering
func (p *darwinProcessProvider) getAllProcesses(ctx context.Context) ([]*ProcessInfo, error) {
	// Use ps to get all processes
	cmd := exec.CommandContext(ctx, "ps", "-A", "-o", "pid,ppid,uid,gid,vsz,rss,pcpu,time,comm")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list processes: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("no processes found")
	}

	var processes []*ProcessInfo
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}

		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}

		ppid, _ := strconv.Atoi(fields[1])
		_ = fields[2] // uid not used in current implementation
		_ = fields[3] // gid not used in current implementation
		vsize, _ := strconv.ParseUint(fields[4], 10, 64)
		rss, _ := strconv.ParseUint(fields[5], 10, 64)
		cpuPercent, _ := strconv.ParseFloat(fields[6], 64)
		command := strings.Join(fields[8:], " ")

		// Convert from KB to bytes
		vsize *= 1024
		rss *= 1024

		process := &ProcessInfo{
			PID:           pid,
			PPID:          ppid,
			Name:          command,
			Command:       command,
			State:         "unknown",
			CPUPercent:    cpuPercent,
			MemoryBytes:   rss,
			VirtualBytes:  vsize,
			ResidentBytes: rss,
			StartTime:     time.Now(),
			UserTime:      0,
			SystemTime:    0,
			Platform:      "darwin",
		}

		processes = append(processes, process)
	}

	return processes, nil
}

// matchesFilter checks if a process matches the given filter
func (p *darwinProcessProvider) matchesFilter(process *ProcessInfo, filter ProcessFilter) bool {
	// If no filter criteria specified, include all
	if filter.Name == "" && filter.NamePattern == "" && filter.Command == "" && len(filter.PIDs) == 0 {
		return true
	}

	// Check PID filter
	if len(filter.PIDs) > 0 {
		found := false
		for _, pid := range filter.PIDs {
			if process.PID == pid {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check name filter
	if filter.Name != "" && !strings.Contains(strings.ToLower(process.Name), strings.ToLower(filter.Name)) {
		return false
	}

	// Check command filter
	if filter.Command != "" && !strings.Contains(strings.ToLower(process.Command), strings.ToLower(filter.Command)) {
		return false
	}

	// Check CPU filter
	if filter.MinCPU > 0 && process.CPUPercent < filter.MinCPU {
		return false
	}

	// Check memory filter
	if filter.MinMemory > 0 && process.MemoryBytes < filter.MinMemory {
		return false
	}

	return true
}

// IsSupported returns true if process operations are supported
func (p *darwinProcessProvider) IsSupported() bool {
	return true
}

// darwinFileSystemProvider implements FileSystemProvider for macOS/Darwin
type darwinFileSystemProvider struct {
	config *Config
}

// NormalizePath converts a path to the platform-appropriate format
func (f *darwinFileSystemProvider) NormalizePath(path string) string {
	// macOS uses forward slashes like Linux
	return strings.ReplaceAll(path, "\\", "/")
}

// GetConfigDirectory returns the standard configuration directory
func (f *darwinFileSystemProvider) GetConfigDirectory() string {
	if homeDir := os.Getenv("HOME"); homeDir != "" {
		return homeDir + "/Library/Application Support"
	}
	return "/Library/Application Support"
}

// GetTempDirectory returns the system temporary directory
func (f *darwinFileSystemProvider) GetTempDirectory() string {
	if tmpDir := os.Getenv("TMPDIR"); tmpDir != "" {
		return tmpDir
	}
	return "/tmp"
}

// IsPathValid validates if a path is acceptable for the platform
func (f *darwinFileSystemProvider) IsPathValid(path string) bool {
	// Basic validation for macOS paths (similar to Linux)
	if path == "" {
		return false
	}
	// Check for null bytes which are invalid in Unix paths
	return !strings.Contains(path, "\x00")
}

// Platform returns the platform identifier
func (f *darwinFileSystemProvider) Platform() string {
	return "darwin"
}
