//go:build linux

package platform

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LinuxProvider implements the Provider interface for Linux systems
type LinuxProvider struct {
	config     *Config
	memory     *linuxMemoryProvider
	process    *linuxProcessProvider
	filesystem *linuxFileSystemProvider
	mu         sync.RWMutex
}

// Memory returns the Linux memory provider
func (p *LinuxProvider) Memory() MemoryProvider {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.memory
}

// Process returns the Linux process provider
func (p *LinuxProvider) Process() ProcessProvider {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.process
}

// FileSystem returns the Linux filesystem provider
func (p *LinuxProvider) FileSystem() FileSystemProvider {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.filesystem
}

// Platform returns the platform identifier
func (p *LinuxProvider) Platform() string {
	return "linux"
}

// IsSupported returns true if Linux platform features are available
func (p *LinuxProvider) IsSupported() bool {
	// Check for /proc filesystem
	if _, err := os.Stat("/proc/meminfo"); err != nil {
		return false
	}
	if _, err := os.Stat("/proc/stat"); err != nil {
		return false
	}
	return true
}

// linuxMemoryProvider implements MemoryProvider for Linux using /proc/meminfo
type linuxMemoryProvider struct {
	config    *Config
	cache     *MemoryInfo
	cacheTime time.Time
	mu        sync.RWMutex
}

func newLinuxMemoryProvider(config *Config) *linuxMemoryProvider {
	return &linuxMemoryProvider{
		config: config,
	}
}

// GetTotalMemory returns total physical memory in bytes
func (m *linuxMemoryProvider) GetTotalMemory(ctx context.Context) (uint64, error) {
	memInfo, err := m.GetMemoryInfo(ctx)
	if err != nil {
		return 0, err
	}
	return memInfo.TotalBytes, nil
}

// GetAvailableMemory returns available memory in bytes
func (m *linuxMemoryProvider) GetAvailableMemory(ctx context.Context) (uint64, error) {
	memInfo, err := m.GetMemoryInfo(ctx)
	if err != nil {
		return 0, err
	}
	return memInfo.AvailableBytes, nil
}

// GetMemoryInfo returns comprehensive memory statistics from /proc/meminfo
func (m *linuxMemoryProvider) GetMemoryInfo(ctx context.Context) (*MemoryInfo, error) {
	m.mu.RLock()
	if m.config.CacheEnabled && m.cache != nil && time.Since(m.cacheTime) < m.config.CacheTTL {
		defer m.mu.RUnlock()
		// Return cached copy with updated timestamp
		cachedInfo := *m.cache
		cachedInfo.Timestamp = time.Now()
		return &cachedInfo, nil
	}
	m.mu.RUnlock()

	// Read /proc/meminfo
	meminfo, err := m.readProcMeminfo()
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc/meminfo: %w", err)
	}

	// Parse memory information
	memInfo := &MemoryInfo{
		TotalBytes:     meminfo["MemTotal"],
		FreeBytes:      meminfo["MemFree"],
		AvailableBytes: meminfo["MemAvailable"],
		CachedBytes:    meminfo["Cached"],
		BufferedBytes:  meminfo["Buffers"],
		SwapTotalBytes: meminfo["SwapTotal"],
		SwapUsedBytes:  meminfo["SwapTotal"] - meminfo["SwapFree"],
		Timestamp:      time.Now(),
		Platform:       "linux",
	}

	// Calculate used memory (Total - Available is more accurate than Total - Free)
	if memInfo.AvailableBytes > 0 {
		memInfo.UsedBytes = memInfo.TotalBytes - memInfo.AvailableBytes
	} else {
		// Fallback calculation if MemAvailable is not available (older kernels)
		memInfo.UsedBytes = memInfo.TotalBytes - memInfo.FreeBytes - memInfo.CachedBytes - memInfo.BufferedBytes
		memInfo.AvailableBytes = memInfo.FreeBytes + memInfo.CachedBytes + memInfo.BufferedBytes
	}

	// Update cache
	if m.config.CacheEnabled {
		m.mu.Lock()
		m.cache = memInfo
		m.cacheTime = time.Now()
		m.mu.Unlock()
	}

	return memInfo, nil
}

// readProcMeminfo reads and parses /proc/meminfo
func (m *linuxMemoryProvider) readProcMeminfo() (map[string]uint64, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	meminfo := make(map[string]uint64)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		key := strings.TrimSuffix(parts[0], ":")
		valueStr := parts[1]

		value, err := strconv.ParseUint(valueStr, 10, 64)
		if err != nil {
			continue
		}

		// Convert from KB to bytes (most /proc/meminfo values are in kB)
		if len(parts) >= 3 && parts[2] == "kB" {
			value *= 1024
		}

		meminfo[key] = value
	}

	return meminfo, scanner.Err()
}

// Platform returns the platform identifier
func (m *linuxMemoryProvider) Platform() string {
	return "linux"
}

// IsSupported returns true if /proc/meminfo is available
func (m *linuxMemoryProvider) IsSupported() bool {
	_, err := os.Stat("/proc/meminfo")
	return err == nil
}

// linuxProcessProvider implements ProcessProvider for Linux using /proc filesystem
type linuxProcessProvider struct {
	config *Config
	mu     sync.RWMutex
}

func newLinuxProcessProvider(config *Config) *linuxProcessProvider {
	return &linuxProcessProvider{
		config: config,
	}
}

// GetProcessInfo returns detailed information about a specific process
func (p *linuxProcessProvider) GetProcessInfo(ctx context.Context, pid int) (*ProcessInfo, error) {
	// Read /proc/[pid]/stat for basic process information
	statInfo, err := p.readProcStat(pid)
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc/%d/stat: %w", pid, err)
	}

	// Read /proc/[pid]/status for additional memory information
	statusInfo, err := p.readProcStatus(pid)
	if err != nil {
		// Status info is optional, continue without it
		statusInfo = make(map[string]string)
	}

	// Create ProcessInfo
	processInfo := &ProcessInfo{
		PID:           pid,
		PPID:          statInfo.ppid,
		Name:          statInfo.name,
		Command:       statInfo.name, // Could be enhanced by reading /proc/[pid]/cmdline
		State:         statInfo.state,
		CPUPercent:    0, // Would need historical data to calculate
		MemoryBytes:   statInfo.rss * getPageSize(),
		VirtualBytes:  statInfo.vsize,
		ResidentBytes: statInfo.rss * getPageSize(),
		StartTime:     time.Now().Add(-time.Duration(statInfo.starttime/getClockTicks()) * time.Second),
		UserTime:      float64(statInfo.utime) / float64(getClockTicks()),
		SystemTime:    float64(statInfo.stime) / float64(getClockTicks()),
		ThreadCount:   statInfo.numThreads,
		Platform:      "linux",
	}

	// Parse additional memory info from status if available
	if vmPeak, ok := statusInfo["VmPeak"]; ok {
		if size, err := parseMemorySize(vmPeak); err == nil {
			processInfo.VirtualBytes = size
		}
	}
	if vmRSS, ok := statusInfo["VmRSS"]; ok {
		if size, err := parseMemorySize(vmRSS); err == nil {
			processInfo.ResidentBytes = size
			processInfo.MemoryBytes = size
		}
	}

	return processInfo, nil
}

// procStatInfo represents parsed /proc/[pid]/stat information
type procStatInfo struct {
	pid        int
	name       string
	state      string
	ppid       int
	utime      uint64
	stime      uint64
	starttime  uint64
	vsize      uint64
	rss        uint64
	numThreads int
}

// readProcStat reads and parses /proc/[pid]/stat
func (p *linuxProcessProvider) readProcStat(pid int) (*procStatInfo, error) {
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	content, err := os.ReadFile(statPath)
	if err != nil {
		return nil, err
	}

	line := string(content)

	// Parse the stat line - format is documented in proc(5)
	// The comm field (process name) can contain spaces and parentheses, so we need special handling

	// Find the last ')' which ends the comm field
	lastParen := strings.LastIndex(line, ")")
	if lastParen == -1 {
		return nil, fmt.Errorf("invalid stat format")
	}

	// Extract comm field (between first '(' and last ')')
	firstParen := strings.Index(line, "(")
	if firstParen == -1 {
		return nil, fmt.Errorf("invalid stat format")
	}

	comm := line[firstParen+1 : lastParen]

	// Parse the fields after the comm field
	fields := strings.Fields(line[lastParen+1:])
	if len(fields) < 40 { // We need at least 40 fields for the data we want
		return nil, fmt.Errorf("insufficient fields in stat")
	}

	// Parse required fields (0-based indexing after comm field)
	state := fields[0]                                    // field 3
	ppid, _ := strconv.Atoi(fields[1])                    // field 4
	utime, _ := strconv.ParseUint(fields[11], 10, 64)     // field 14
	stime, _ := strconv.ParseUint(fields[12], 10, 64)     // field 15
	starttime, _ := strconv.ParseUint(fields[19], 10, 64) // field 22
	vsize, _ := strconv.ParseUint(fields[20], 10, 64)     // field 23
	rss, _ := strconv.ParseUint(fields[21], 10, 64)       // field 24
	numThreads, _ := strconv.Atoi(fields[17])             // field 20

	return &procStatInfo{
		pid:        pid,
		name:       comm,
		state:      state,
		ppid:       ppid,
		utime:      utime,
		stime:      stime,
		starttime:  starttime,
		vsize:      vsize,
		rss:        rss,
		numThreads: numThreads,
	}, nil
}

// readProcStatus reads and parses /proc/[pid]/status
func (p *linuxProcessProvider) readProcStatus(pid int) (map[string]string, error) {
	statusPath := fmt.Sprintf("/proc/%d/status", pid)
	file, err := os.Open(statusPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	status := make(map[string]string)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			status[key] = value
		}
	}

	return status, scanner.Err()
}

// ListProcesses returns information about processes matching criteria
func (p *linuxProcessProvider) ListProcesses(ctx context.Context, filter ProcessFilter) ([]*ProcessInfo, error) {
	// Read /proc directory for process directories
	procDir, err := os.Open("/proc")
	if err != nil {
		return nil, fmt.Errorf("failed to open /proc: %w", err)
	}
	defer procDir.Close()

	entries, err := procDir.Readdir(-1)
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc: %w", err)
	}

	var processes []*ProcessInfo

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Check if directory name is a PID (numeric)
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		// Skip if PID filter is specified and doesn't match
		if len(filter.PIDs) > 0 {
			found := false
			for _, filterPID := range filter.PIDs {
				if pid == filterPID {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Get process information
		processInfo, err := p.GetProcessInfo(ctx, pid)
		if err != nil {
			// Skip processes we can't read (permission issues, etc.)
			continue
		}

		// Apply filters
		if p.matchesFilter(processInfo, filter) {
			processes = append(processes, processInfo)
		}
	}

	return processes, nil
}

// matchesFilter checks if a process matches the given filter criteria
func (p *linuxProcessProvider) matchesFilter(process *ProcessInfo, filter ProcessFilter) bool {
	// Name filter
	if filter.Name != "" && process.Name != filter.Name {
		return false
	}

	// Name pattern filter (regex)
	if filter.NamePattern != "" {
		matched, err := regexp.MatchString(filter.NamePattern, process.Name)
		if err != nil || !matched {
			return false
		}
	}

	// Command filter
	if filter.Command != "" && process.Command != filter.Command {
		return false
	}

	// CPU filter
	if filter.MinCPU > 0 && process.CPUPercent < filter.MinCPU {
		return false
	}

	// Memory filter
	if filter.MinMemory > 0 && process.MemoryBytes < filter.MinMemory {
		return false
	}

	// State filter
	if len(filter.States) > 0 {
		found := false
		for _, state := range filter.States {
			if process.State == state {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// MonitorProcess starts monitoring a process for resource usage
func (p *linuxProcessProvider) MonitorProcess(ctx context.Context, pid int) (ProcessMonitor, error) {
	return newLinuxProcessMonitor(pid, p), nil
}

// Platform returns the platform identifier
func (p *linuxProcessProvider) Platform() string {
	return "linux"
}

// IsSupported returns true if /proc filesystem is available
func (p *linuxProcessProvider) IsSupported() bool {
	_, err := os.Stat("/proc")
	return err == nil
}

// linuxFileSystemProvider implements FileSystemProvider for Linux
type linuxFileSystemProvider struct{}

func newLinuxFileSystemProvider() *linuxFileSystemProvider {
	return &linuxFileSystemProvider{}
}

// NormalizePath converts a path to the Linux-appropriate format
func (f *linuxFileSystemProvider) NormalizePath(path string) string {
	// Linux uses forward slashes, ensure no trailing slash except for root
	normalized := strings.ReplaceAll(path, "\\", "/")
	if len(normalized) > 1 && strings.HasSuffix(normalized, "/") {
		normalized = strings.TrimSuffix(normalized, "/")
	}
	return normalized
}

// GetConfigDirectory returns the standard configuration directory
func (f *linuxFileSystemProvider) GetConfigDirectory() string {
	if configDir := os.Getenv("XDG_CONFIG_HOME"); configDir != "" {
		return configDir
	}
	if homeDir := os.Getenv("HOME"); homeDir != "" {
		return homeDir + "/.config"
	}
	return "/etc"
}

// GetTempDirectory returns the system temporary directory
func (f *linuxFileSystemProvider) GetTempDirectory() string {
	if tmpDir := os.Getenv("TMPDIR"); tmpDir != "" {
		return tmpDir
	}
	return "/tmp"
}

// IsPathValid validates if a path is acceptable for Linux
func (f *linuxFileSystemProvider) IsPathValid(path string) bool {
	if path == "" {
		return false
	}

	// Check for invalid characters
	invalidChars := []string{"\x00"} // NULL character is invalid in Linux paths
	for _, char := range invalidChars {
		if strings.Contains(path, char) {
			return false
		}
	}

	// Check path length (Linux has a limit of 4096 bytes for paths)
	if len(path) > 4095 {
		return false
	}

	return true
}

// Platform returns the platform identifier
func (f *linuxFileSystemProvider) Platform() string {
	return "linux"
}

// Helper functions

// getPageSize returns the system page size in bytes
func getPageSize() uint64 {
	// Default page size for most systems, could be made more dynamic
	return 4096
}

// getClockTicks returns the number of clock ticks per second
func getClockTicks() uint64 {
	// Default to 100 Hz, could be read from sysconf(_SC_CLK_TCK)
	return 100
}

// parseMemorySize parses memory size strings like "1234 kB" into bytes
func parseMemorySize(sizeStr string) (uint64, error) {
	parts := strings.Fields(sizeStr)
	if len(parts) < 1 {
		return 0, fmt.Errorf("invalid memory size format")
	}

	value, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, err
	}

	// Convert based on unit
	if len(parts) >= 2 {
		unit := strings.ToLower(parts[1])
		switch unit {
		case "kb":
			value *= 1024
		case "mb":
			value *= 1024 * 1024
		case "gb":
			value *= 1024 * 1024 * 1024
		}
	}

	return value, nil
}

// linuxProcessMonitor implements ProcessMonitor for Linux
type linuxProcessMonitor struct {
	pid      int
	provider *linuxProcessProvider
	running  bool
	stats    *ProcessStats
	statsC   chan *ProcessStats
	stopC    chan struct{}
	mu       sync.RWMutex
}

func newLinuxProcessMonitor(pid int, provider *linuxProcessProvider) *linuxProcessMonitor {
	return &linuxProcessMonitor{
		pid:      pid,
		provider: provider,
		running:  false,
		statsC:   make(chan *ProcessStats, 10),
		stopC:    make(chan struct{}),
	}
}

// Start begins monitoring the process
func (m *linuxProcessMonitor) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return fmt.Errorf("monitor already running")
	}

	m.running = true
	go m.monitorLoop(ctx)
	return nil
}

// Stop halts monitoring
func (m *linuxProcessMonitor) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return nil
	}

	m.running = false
	close(m.stopC)
	return nil
}

// GetStats returns current process statistics
func (m *linuxProcessMonitor) GetStats() (*ProcessStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.stats == nil {
		return nil, fmt.Errorf("no stats available")
	}

	// Return a copy
	statsCopy := *m.stats
	return &statsCopy, nil
}

// Subscribe returns a channel for receiving process updates
func (m *linuxProcessMonitor) Subscribe() <-chan *ProcessStats {
	return m.statsC
}

// IsRunning returns true if the process is still running
func (m *linuxProcessMonitor) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

// monitorLoop runs the monitoring loop
func (m *linuxProcessMonitor) monitorLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopC:
			return
		case <-ticker.C:
			// Get current process info
			processInfo, err := m.provider.GetProcessInfo(ctx, m.pid)
			if err != nil {
				// Process might have exited
				m.mu.Lock()
				m.running = false
				m.mu.Unlock()
				return
			}

			// Create stats
			stats := &ProcessStats{
				PID:         m.pid,
				CPUPercent:  processInfo.CPUPercent,
				MemoryBytes: processInfo.MemoryBytes,
				Timestamp:   time.Now(),
				IsActive:    processInfo.State == "R" || processInfo.State == "S",
			}

			// Calculate deltas if we have previous stats
			m.mu.Lock()
			if m.stats != nil {
				stats.DeltaMemory = int64(stats.MemoryBytes) - int64(m.stats.MemoryBytes)
			}
			m.stats = stats
			m.mu.Unlock()

			// Send to subscribers (non-blocking)
			select {
			case m.statsC <- stats:
			default:
				// Channel full, skip this update
			}
		}
	}
}
