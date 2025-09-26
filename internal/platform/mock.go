package platform

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// NewMockProvider creates a mock platform provider for testing
func NewMockProvider(config *Config) Provider {
	if config == nil {
		config = &Config{}
	}

	return &mockProvider{
		config:     config,
		memory:     newMockMemoryProvider(),
		process:    newMockProcessProvider(),
		filesystem: newMockFileSystemProvider(),
		platform:   "mock",
	}
}

// mockProvider implements the Provider interface for testing
type mockProvider struct {
	config     *Config
	memory     MemoryProvider
	process    ProcessProvider
	filesystem FileSystemProvider
	platform   string
	mu         sync.RWMutex
}

func (p *mockProvider) Memory() MemoryProvider {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.memory
}

func (p *mockProvider) Process() ProcessProvider {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.process
}

func (p *mockProvider) FileSystem() FileSystemProvider {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.filesystem
}

func (p *mockProvider) Platform() string {
	return p.platform
}

func (p *mockProvider) IsSupported() bool {
	return true // Mock provider is always supported
}

// SetupScenario configures the mock provider with specific test scenarios
func (p *mockProvider) SetupScenario(memInfo *MemoryInfo, processes []*ProcessInfo) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Configure memory mock
	if mockMem, ok := p.memory.(*mockMemoryProvider); ok && memInfo != nil {
		mockMem.setMemoryInfo(memInfo)
	}

	// Configure process mock
	if mockProc, ok := p.process.(*mockProcessProvider); ok && len(processes) > 0 {
		mockProc.setProcesses(processes)
	}

	return nil
}

// mockMemoryProvider implements MemoryProvider for testing
type mockMemoryProvider struct {
	mu     sync.RWMutex
	memory *MemoryInfo
}

func newMockMemoryProvider() *mockMemoryProvider {
	// Default mock memory configuration
	return &mockMemoryProvider{
		memory: &MemoryInfo{
			TotalBytes:     4 * 1024 * 1024 * 1024, // 4GB
			AvailableBytes: 2 * 1024 * 1024 * 1024, // 2GB
			UsedBytes:      2 * 1024 * 1024 * 1024, // 2GB
			FreeBytes:      2 * 1024 * 1024 * 1024, // 2GB
			Timestamp:      time.Now(),
			Platform:       "mock",
		},
	}
}

func (m *mockMemoryProvider) GetTotalMemory(ctx context.Context) (uint64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.memory.TotalBytes, nil
}

func (m *mockMemoryProvider) GetAvailableMemory(ctx context.Context) (uint64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.memory.AvailableBytes, nil
}

func (m *mockMemoryProvider) GetMemoryInfo(ctx context.Context) (*MemoryInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy to avoid race conditions
	memCopy := *m.memory
	memCopy.Timestamp = time.Now()
	return &memCopy, nil
}

func (m *mockMemoryProvider) Platform() string {
	return "mock"
}

func (m *mockMemoryProvider) IsSupported() bool {
	return true
}

func (m *mockMemoryProvider) setMemoryInfo(memInfo *MemoryInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if memInfo != nil {
		m.memory = memInfo
	}
}

// mockProcessProvider implements ProcessProvider for testing
type mockProcessProvider struct {
	mu        sync.RWMutex
	processes map[int]*ProcessInfo
}

func newMockProcessProvider() *mockProcessProvider {
	// Default mock processes
	defaultProcesses := map[int]*ProcessInfo{
		1: {
			PID:           1,
			PPID:          0,
			Name:          "mock-init",
			Command:       "/sbin/mock-init",
			State:         "running",
			CPUPercent:    0.1,
			MemoryBytes:   10 * 1024 * 1024, // 10MB
			VirtualBytes:  50 * 1024 * 1024, // 50MB
			ResidentBytes: 10 * 1024 * 1024, // 10MB
			StartTime:     time.Now().Add(-1 * time.Hour),
			UserTime:      0.5,
			SystemTime:    0.2,
			ThreadCount:   1,
			Platform:      "mock",
		},
		100: {
			PID:           100,
			PPID:          1,
			Name:          "mock-service",
			Command:       "/usr/bin/mock-service",
			State:         "running",
			CPUPercent:    5.0,
			MemoryBytes:   64 * 1024 * 1024,  // 64MB
			VirtualBytes:  128 * 1024 * 1024, // 128MB
			ResidentBytes: 64 * 1024 * 1024,  // 64MB
			StartTime:     time.Now().Add(-30 * time.Minute),
			UserTime:      15.0,
			SystemTime:    5.0,
			ThreadCount:   4,
			Platform:      "mock",
		},
	}

	return &mockProcessProvider{
		processes: defaultProcesses,
	}
}

func (p *mockProcessProvider) GetProcessInfo(ctx context.Context, pid int) (*ProcessInfo, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	proc, exists := p.processes[pid]
	if !exists {
		return nil, &PlatformError{
			Platform:  "mock",
			Operation: "GetProcessInfo",
			Err:       fmt.Errorf("process %d not found", pid),
			Code:      ErrorCodeResourceNotFound,
		}
	}

	// Return a copy to avoid race conditions
	procCopy := *proc
	return &procCopy, nil
}

func (p *mockProcessProvider) ListProcesses(ctx context.Context, filter ProcessFilter) ([]*ProcessInfo, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var result []*ProcessInfo
	for _, proc := range p.processes {
		if p.matchesFilter(proc, filter) {
			// Return a copy to avoid race conditions
			procCopy := *proc
			result = append(result, &procCopy)
		}
	}

	return result, nil
}

func (p *mockProcessProvider) MonitorProcess(ctx context.Context, pid int) (ProcessMonitor, error) {
	// Check if process exists
	_, err := p.GetProcessInfo(ctx, pid)
	if err != nil {
		return nil, err
	}

	return newMockProcessMonitor(pid), nil
}

func (p *mockProcessProvider) Platform() string {
	return "mock"
}

func (p *mockProcessProvider) IsSupported() bool {
	return true
}

func (p *mockProcessProvider) setProcesses(processes []*ProcessInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Clear existing processes and add new ones
	p.processes = make(map[int]*ProcessInfo)
	for _, proc := range processes {
		if proc != nil {
			procCopy := *proc
			p.processes[proc.PID] = &procCopy
		}
	}
}

func (p *mockProcessProvider) matchesFilter(proc *ProcessInfo, filter ProcessFilter) bool {
	// Name filter
	if filter.Name != "" && proc.Name != filter.Name {
		return false
	}

	// Command filter
	if filter.Command != "" && proc.Command != filter.Command {
		return false
	}

	// PID filter
	if len(filter.PIDs) > 0 {
		found := false
		for _, pid := range filter.PIDs {
			if proc.PID == pid {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// CPU filter
	if filter.MinCPU > 0 && proc.CPUPercent < filter.MinCPU {
		return false
	}

	// Memory filter
	if filter.MinMemory > 0 && proc.MemoryBytes < filter.MinMemory {
		return false
	}

	// State filter
	if len(filter.States) > 0 {
		found := false
		for _, state := range filter.States {
			if proc.State == state {
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

// mockProcessMonitor implements ProcessMonitor for testing
type mockProcessMonitor struct {
	pid     int
	running bool
	stats   *ProcessStats
	statsC  chan *ProcessStats
	stopC   chan struct{}
	mu      sync.RWMutex
}

func newMockProcessMonitor(pid int) *mockProcessMonitor {
	return &mockProcessMonitor{
		pid:     pid,
		running: false,
		stats: &ProcessStats{
			PID:          pid,
			CPUPercent:   5.0,
			MemoryBytes:  64 * 1024 * 1024,
			DeltaCPU:     0.1,
			DeltaMemory:  1024,
			Timestamp:    time.Now(),
			IsActive:     true,
			StateChanges: 0,
		},
		statsC: make(chan *ProcessStats, 10),
		stopC:  make(chan struct{}),
	}
}

func (m *mockProcessMonitor) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return fmt.Errorf("monitor already running")
	}

	m.running = true
	go m.monitorLoop(ctx)
	return nil
}

func (m *mockProcessMonitor) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return nil
	}

	m.running = false
	close(m.stopC)
	return nil
}

func (m *mockProcessMonitor) GetStats() (*ProcessStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.stats == nil {
		return nil, fmt.Errorf("no stats available")
	}

	// Return a copy
	statsCopy := *m.stats
	statsCopy.Timestamp = time.Now()
	return &statsCopy, nil
}

func (m *mockProcessMonitor) Subscribe() <-chan *ProcessStats {
	return m.statsC
}

func (m *mockProcessMonitor) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

func (m *mockProcessMonitor) monitorLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopC:
			return
		case <-ticker.C:
			m.mu.Lock()
			if m.stats != nil {
				// Update stats with slight variations
				m.stats.CPUPercent = 3.0 + (2.0 * float64(time.Now().Second()) / 60.0) // Vary between 3-5%
				m.stats.Timestamp = time.Now()
				m.stats.DeltaCPU = 0.1
				m.stats.DeltaMemory = 512

				// Send to subscribers (non-blocking)
				select {
				case m.statsC <- m.stats:
				default:
					// Channel full, skip this update
				}
			}
			m.mu.Unlock()
		}
	}
}

// mockFileSystemProvider implements FileSystemProvider for testing
type mockFileSystemProvider struct{}

func newMockFileSystemProvider() *mockFileSystemProvider {
	return &mockFileSystemProvider{}
}

func (f *mockFileSystemProvider) NormalizePath(path string) string {
	// For mock, just return the path as-is
	return path
}

func (f *mockFileSystemProvider) GetConfigDirectory() string {
	return "/mock/config"
}

func (f *mockFileSystemProvider) GetTempDirectory() string {
	return "/mock/tmp"
}

func (f *mockFileSystemProvider) IsPathValid(path string) bool {
	// Mock considers all paths valid
	return path != ""
}

func (f *mockFileSystemProvider) Platform() string {
	return "mock"
}
