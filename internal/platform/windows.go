//go:build windows

package platform

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WindowsProvider implements the Provider interface for Windows systems
type WindowsProvider struct {
	config     *Config
	memory     *windowsMemoryProvider
	process    *windowsProcessProvider
	filesystem *windowsFileSystemProvider
	mu         sync.RWMutex
}

// Memory returns the memory provider
func (p *WindowsProvider) Memory() MemoryProvider {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.memory
}

// Process returns the process provider
func (p *WindowsProvider) Process() ProcessProvider {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.process
}

// FileSystem returns the filesystem provider
func (p *WindowsProvider) FileSystem() FileSystemProvider {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.filesystem
}

// Platform returns the platform identifier
func (p *WindowsProvider) Platform() string {
	return "windows"
}

// IsSupported returns true if the platform is fully supported
func (p *WindowsProvider) IsSupported() bool {
	return p.memory.IsSupported() && p.process.IsSupported()
}

// windowsMemoryProvider implements MemoryProvider for Windows
type windowsMemoryProvider struct {
	config *Config
	cache  map[string]*cachedMemoryInfo
	mu     sync.RWMutex
}

// MEMORYSTATUSEX structure for Windows memory information
type memoryStatusEx struct {
	dwLength                uint32
	dwMemoryLoad            uint32
	ullTotalPhys            uint64
	ullAvailPhys            uint64
	ullTotalPageFile        uint64
	ullAvailPageFile        uint64
	ullTotalVirtual         uint64
	ullAvailVirtual         uint64
	ullAvailExtendedVirtual uint64
}

// GetTotalMemory returns the total physical memory in bytes
func (m *windowsMemoryProvider) GetTotalMemory(ctx context.Context) (uint64, error) {
	info, err := m.GetMemoryInfo(ctx)
	if err != nil {
		return 0, err
	}
	return info.TotalBytes, nil
}

// GetAvailableMemory returns available memory for allocation in bytes
func (m *windowsMemoryProvider) GetAvailableMemory(ctx context.Context) (uint64, error) {
	info, err := m.GetMemoryInfo(ctx)
	if err != nil {
		return 0, err
	}
	return info.AvailableBytes, nil
}

// Platform returns the platform identifier
func (m *windowsMemoryProvider) Platform() string {
	return "windows"
}

// GetMemoryInfo returns current memory information using Windows API
func (m *windowsMemoryProvider) GetMemoryInfo(ctx context.Context) (*MemoryInfo, error) {
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

// collectMemoryInfo gathers memory information using Windows API
func (m *windowsMemoryProvider) collectMemoryInfo(ctx context.Context) (*MemoryInfo, error) {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	globalMemoryStatusEx := kernel32.NewProc("GlobalMemoryStatusEx")

	var memStatus memoryStatusEx
	memStatus.dwLength = uint32(unsafe.Sizeof(memStatus))

	ret, _, err := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&memStatus)))
	if ret == 0 {
		return nil, fmt.Errorf("GlobalMemoryStatusEx failed: %w", err)
	}

	totalMem := memStatus.ullTotalPhys
	availMem := memStatus.ullAvailPhys
	usedMem := totalMem - availMem
	freeMem := availMem

	// Windows doesn't distinguish buffers/cache like Linux, so we'll use available page file as cache approximation
	cachedMem := memStatus.ullAvailPageFile - memStatus.ullAvailPhys
	if cachedMem > totalMem {
		cachedMem = 0 // Prevent invalid values
	}

	return &MemoryInfo{
		TotalBytes:     totalMem,
		FreeBytes:      freeMem,
		UsedBytes:      usedMem,
		AvailableBytes: availMem,
		BufferedBytes:  0, // Windows doesn't expose buffer cache separately
		CachedBytes:    cachedMem,
		Timestamp:      time.Now(),
		Platform:       "windows",
	}, nil
}

// IsSupported returns true if memory operations are supported
func (m *windowsMemoryProvider) IsSupported() bool {
	return true
}

// windowsProcessProvider implements ProcessProvider for Windows
type windowsProcessProvider struct {
	config *Config
	cache  map[int]*cachedProcessInfo
	mu     sync.RWMutex
}

// PROCESS_MEMORY_COUNTERS for Windows process memory information
type processMemoryCounters struct {
	cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uint64
	WorkingSetSize             uint64
	QuotaPeakPagedPoolUsage    uint64
	QuotaPagedPoolUsage        uint64
	QuotaPeakNonPagedPoolUsage uint64
	QuotaNonPagedPoolUsage     uint64
	PagefileUsage              uint64
	PeakPagefileUsage          uint64
}

// GetProcessInfo returns information about a specific process
func (p *windowsProcessProvider) GetProcessInfo(ctx context.Context, pid int) (*ProcessInfo, error) {
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

// collectProcessInfo gathers process information using Windows API
func (p *windowsProcessProvider) collectProcessInfo(ctx context.Context, pid int) (*ProcessInfo, error) {
	// Open process handle
	handle, err := windows.OpenProcess(
		windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ,
		false,
		uint32(pid),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open process: %w", err)
	}
	defer windows.CloseHandle(handle)

	// Get process memory information
	psapi := windows.NewLazySystemDLL("psapi.dll")
	getProcessMemoryInfo := psapi.NewProc("GetProcessMemoryInfo")

	var memCounters processMemoryCounters
	memCounters.cb = uint32(unsafe.Sizeof(memCounters))

	ret, _, err := getProcessMemoryInfo.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&memCounters)),
		uintptr(memCounters.cb),
	)
	if ret == 0 {
		return nil, fmt.Errorf("GetProcessMemoryInfo failed: %w", err)
	}

	// Get process name using GetModuleFileNameEx
	getModuleFileNameEx := psapi.NewProc("GetModuleFileNameExW")
	nameBuffer := make([]uint16, 260) // MAX_PATH
	ret, _, _ = getModuleFileNameEx.Call(
		uintptr(handle),
		0,
		uintptr(unsafe.Pointer(&nameBuffer[0])),
		uintptr(len(nameBuffer)),
	)

	var processName string
	if ret > 0 {
		processName = windows.UTF16ToString(nameBuffer)
		// Extract just the filename
		if idx := strings.LastIndex(processName, "\\"); idx != -1 {
			processName = processName[idx+1:]
		}
	} else {
		processName = "unknown"
	}

	// Get parent process ID using NtQueryInformationProcess
	ntdll := windows.NewLazySystemDLL("ntdll.dll")
	ntQueryInformationProcess := ntdll.NewProc("NtQueryInformationProcess")

	type processBasicInformation struct {
		Reserved1       uintptr
		PebBaseAddress  uintptr
		Reserved2       [2]uintptr
		UniqueProcessId uintptr
		ParentProcessId uintptr
	}

	var pbi processBasicInformation
	var returnLength uint32

	ret, _, _ = ntQueryInformationProcess.Call(
		uintptr(handle),
		0, // ProcessBasicInformation
		uintptr(unsafe.Pointer(&pbi)),
		uintptr(unsafe.Sizeof(pbi)),
		uintptr(unsafe.Pointer(&returnLength)),
	)

	ppid := 0
	if ret == 0 { // NT_SUCCESS
		ppid = int(pbi.ParentProcessId)
	}

	// Get process creation time and CPU usage
	var creationTime, exitTime, kernelTime, userTime windows.Filetime
	err = windows.GetProcessTimes(handle, &creationTime, &exitTime, &kernelTime, &userTime)
	if err != nil {
		// Non-fatal error, continue with default values
		kernelTime = windows.Filetime{}
		userTime = windows.Filetime{}
	}

	// Calculate CPU usage (simplified approximation)
	cpuTime := int64(kernelTime.Nanoseconds()) + int64(userTime.Nanoseconds())
	cpuPercent := float64(cpuTime) / float64(time.Since(time.Unix(0, creationTime.Nanoseconds())).Nanoseconds()) * 100

	return &ProcessInfo{
		PID:           pid,
		PPID:          ppid,
		Name:          processName,
		Command:       processName,
		State:         "running",
		CPUPercent:    cpuPercent,
		MemoryBytes:   memCounters.WorkingSetSize,
		VirtualBytes:  memCounters.PagefileUsage,
		ResidentBytes: memCounters.WorkingSetSize,
		StartTime:     time.Unix(0, creationTime.Nanoseconds()),
		UserTime:      float64(userTime.Nanoseconds()) / 1e9,
		SystemTime:    float64(kernelTime.Nanoseconds()) / 1e9,
		Platform:      "windows",
	}, nil
}

// Platform returns the platform identifier
func (p *windowsProcessProvider) Platform() string {
	return "windows"
}

// MonitorProcess starts monitoring a process for resource usage
func (p *windowsProcessProvider) MonitorProcess(ctx context.Context, pid int) (ProcessMonitor, error) {
	return nil, fmt.Errorf("process monitoring not implemented for Windows")
}

// ListProcesses returns a list of all running processes
func (p *windowsProcessProvider) ListProcesses(ctx context.Context, filter ProcessFilter) ([]*ProcessInfo, error) {
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
func (p *windowsProcessProvider) getAllProcesses(ctx context.Context) ([]*ProcessInfo, error) {
	// Get list of process IDs
	psapi := windows.NewLazySystemDLL("psapi.dll")
	enumProcesses := psapi.NewProc("EnumProcesses")

	const maxProcesses = 1024
	processIds := make([]uint32, maxProcesses)
	var bytesReturned uint32

	ret, _, err := enumProcesses.Call(
		uintptr(unsafe.Pointer(&processIds[0])),
		uintptr(len(processIds)*4),
		uintptr(unsafe.Pointer(&bytesReturned)),
	)
	if ret == 0 {
		return nil, fmt.Errorf("EnumProcesses failed: %w", err)
	}

	numProcesses := int(bytesReturned / 4)
	var processes []*ProcessInfo

	for i := 0; i < numProcesses; i++ {
		pid := int(processIds[i])
		if pid == 0 {
			continue // Skip System Idle Process
		}

		processInfo, err := p.GetProcessInfo(ctx, pid)
		if err != nil {
			// Skip processes we can't access
			continue
		}

		processes = append(processes, processInfo)
	}

	return processes, nil
}

// matchesFilter checks if a process matches the given filter
func (p *windowsProcessProvider) matchesFilter(process *ProcessInfo, filter ProcessFilter) bool {
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
func (p *windowsProcessProvider) IsSupported() bool {
	return true
}

// windowsFileSystemProvider implements FileSystemProvider for Windows
type windowsFileSystemProvider struct {
	config *Config
}

// NormalizePath converts a path to the platform-appropriate format
func (f *windowsFileSystemProvider) NormalizePath(path string) string {
	// Windows uses backslashes
	return strings.ReplaceAll(path, "/", "\\")
}

// GetConfigDirectory returns the standard configuration directory
func (f *windowsFileSystemProvider) GetConfigDirectory() string {
	if appData := os.Getenv("APPDATA"); appData != "" {
		return appData
	}
	return "C:\\ProgramData"
}

// GetTempDirectory returns the system temporary directory
func (f *windowsFileSystemProvider) GetTempDirectory() string {
	if tmpDir := os.Getenv("TEMP"); tmpDir != "" {
		return tmpDir
	}
	if tmpDir := os.Getenv("TMP"); tmpDir != "" {
		return tmpDir
	}
	return "C:\\Windows\\Temp"
}

// IsPathValid validates if a path is acceptable for the platform
func (f *windowsFileSystemProvider) IsPathValid(path string) bool {
	// Basic validation for Windows paths
	if path == "" {
		return false
	}
	// Check for invalid characters in Windows paths
	invalidChars := []string{"<", ">", ":", "\"", "|", "?", "*"}
	for _, char := range invalidChars {
		if strings.Contains(path, char) {
			return false
		}
	}
	return true
}

// Platform returns the platform identifier
func (f *windowsFileSystemProvider) Platform() string {
	return "windows"
}
