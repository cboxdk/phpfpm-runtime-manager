package metrics

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/types"
	"go.uber.org/zap"
)

// MemoryTracker tracks memory usage for processes
type MemoryTracker struct {
	logger *zap.Logger

	// Process tracking
	processes map[int]*ProcessMemoryInfo
	mu        sync.RWMutex

	// State
	running bool
}

// ProcessMemoryInfo tracks memory information for a process
type ProcessMemoryInfo struct {
	PID            int
	VmSize         uint64 // Virtual memory size in bytes
	VmRSS          uint64 // Resident set size in bytes
	VmPeak         uint64 // Peak virtual memory usage in bytes
	VmHWM          uint64 // Peak resident set size in bytes
	LastUpdate     time.Time
	MaxMemoryUsage uint64 // Maximum memory usage observed
	MinMemoryUsage uint64 // Minimum memory usage observed
	SampleCount    int64  // Number of samples taken
	TotalMemory    uint64 // Sum of all memory samples for average calculation
}

// ProcessMemoryStats represents memory statistics from /proc/[pid]/status
type ProcessMemoryStats struct {
	VmPeak uint64 // Peak virtual memory size
	VmSize uint64 // Virtual memory size
	VmHWM  uint64 // Peak resident set size
	VmRSS  uint64 // Resident set size
	VmData uint64 // Size of data segments
	VmStk  uint64 // Size of stack
	VmExe  uint64 // Size of text segments
	VmLib  uint64 // Shared library code size
}

// NewMemoryTracker creates a new memory tracker
func NewMemoryTracker(logger *zap.Logger) (*MemoryTracker, error) {
	return &MemoryTracker{
		logger:    logger,
		processes: make(map[int]*ProcessMemoryInfo),
	}, nil
}

// Start begins memory tracking
func (m *MemoryTracker) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return fmt.Errorf("memory tracker is already running")
	}
	m.running = true
	m.mu.Unlock()

	m.logger.Info("Starting memory tracker")
	return nil
}

// Stop halts memory tracking
func (m *MemoryTracker) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return nil
	}
	m.running = false
	m.mu.Unlock()

	m.logger.Info("Stopping memory tracker")
	return nil
}

// TrackProcess starts tracking memory usage for a specific process
func (m *MemoryTracker) TrackProcess(pid int) error {
	if !m.running {
		return fmt.Errorf("memory tracker is not running")
	}

	stats, err := m.readProcessMemory(pid)
	if err != nil {
		return fmt.Errorf("failed to read process memory info: %w", err)
	}

	m.mu.Lock()
	m.processes[pid] = &ProcessMemoryInfo{
		PID:            pid,
		VmSize:         stats.VmSize,
		VmRSS:          stats.VmRSS,
		VmPeak:         stats.VmPeak,
		VmHWM:          stats.VmHWM,
		LastUpdate:     time.Now(),
		MaxMemoryUsage: stats.VmRSS,
		MinMemoryUsage: stats.VmRSS,
		SampleCount:    1,
		TotalMemory:    stats.VmRSS,
	}
	m.mu.Unlock()

	m.logger.Debug("Started tracking process memory", zap.Int("pid", pid))
	return nil
}

// UntrackProcess stops tracking memory usage for a specific process
func (m *MemoryTracker) UntrackProcess(pid int) {
	m.mu.Lock()
	delete(m.processes, pid)
	m.mu.Unlock()

	m.logger.Debug("Stopped tracking process memory", zap.Int("pid", pid))
}

// UpdateProcess updates memory statistics for a specific process
func (m *MemoryTracker) UpdateProcess(pid int) error {
	m.mu.RLock()
	processInfo, exists := m.processes[pid]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("process %d is not being tracked", pid)
	}

	stats, err := m.readProcessMemory(pid)
	if err != nil {
		return fmt.Errorf("failed to read current memory info: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Update current values
	processInfo.VmSize = stats.VmSize
	processInfo.VmRSS = stats.VmRSS
	processInfo.VmPeak = stats.VmPeak
	processInfo.VmHWM = stats.VmHWM
	processInfo.LastUpdate = time.Now()

	// Update statistics
	processInfo.SampleCount++
	processInfo.TotalMemory += stats.VmRSS

	if stats.VmRSS > processInfo.MaxMemoryUsage {
		processInfo.MaxMemoryUsage = stats.VmRSS
	}

	if stats.VmRSS < processInfo.MinMemoryUsage {
		processInfo.MinMemoryUsage = stats.VmRSS
	}

	return nil
}

// GetProcessStats returns memory statistics for a specific process
func (m *MemoryTracker) GetProcessStats(pid int) (*ProcessMemoryInfo, error) {
	// Update the process first
	if err := m.UpdateProcess(pid); err != nil {
		return nil, err
	}

	m.mu.RLock()
	processInfo, exists := m.processes[pid]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("process %d is not being tracked", pid)
	}

	// Return a copy to avoid concurrent access issues
	return &ProcessMemoryInfo{
		PID:            processInfo.PID,
		VmSize:         processInfo.VmSize,
		VmRSS:          processInfo.VmRSS,
		VmPeak:         processInfo.VmPeak,
		VmHWM:          processInfo.VmHWM,
		LastUpdate:     processInfo.LastUpdate,
		MaxMemoryUsage: processInfo.MaxMemoryUsage,
		MinMemoryUsage: processInfo.MinMemoryUsage,
		SampleCount:    processInfo.SampleCount,
		TotalMemory:    processInfo.TotalMemory,
	}, nil
}

// GetMetrics returns current memory metrics for all tracked processes
func (m *MemoryTracker) GetMetrics(ctx context.Context) ([]types.Metric, error) {
	var metrics []types.Metric
	timestamp := time.Now()

	// Collect PIDs first to avoid holding lock during UpdateProcess calls
	m.mu.RLock()
	pids := make([]int, 0, len(m.processes))
	for pid := range m.processes {
		pids = append(pids, pid)
	}
	m.mu.RUnlock()

	// Update all processes and collect metrics
	for _, pid := range pids {
		// Update process memory info
		if err := m.UpdateProcess(pid); err != nil {
			m.logger.Error("Failed to update process memory",
				zap.Int("pid", pid),
				zap.Error(err))
			continue
		}

		// Get updated info
		updatedInfo, err := m.GetProcessStats(pid)
		if err != nil {
			m.logger.Error("Failed to get process stats",
				zap.Int("pid", pid),
				zap.Error(err))
			continue
		}

		labels := map[string]string{
			"pid": strconv.Itoa(pid),
		}

		// Current memory metrics
		metrics = append(metrics, types.Metric{
			Name:      "phpfpm_process_memory_rss_bytes",
			Value:     float64(updatedInfo.VmRSS),
			Type:      types.MetricTypeGauge,
			Labels:    labels,
			Timestamp: timestamp,
		})

		metrics = append(metrics, types.Metric{
			Name:      "phpfpm_process_memory_vsize_bytes",
			Value:     float64(updatedInfo.VmSize),
			Type:      types.MetricTypeGauge,
			Labels:    labels,
			Timestamp: timestamp,
		})

		// Peak memory metrics
		metrics = append(metrics, types.Metric{
			Name:      "phpfpm_process_memory_peak_rss_bytes",
			Value:     float64(updatedInfo.VmHWM),
			Type:      types.MetricTypeGauge,
			Labels:    labels,
			Timestamp: timestamp,
		})

		metrics = append(metrics, types.Metric{
			Name:      "phpfpm_process_memory_peak_vsize_bytes",
			Value:     float64(updatedInfo.VmPeak),
			Type:      types.MetricTypeGauge,
			Labels:    labels,
			Timestamp: timestamp,
		})

		// Statistical metrics
		metrics = append(metrics, types.Metric{
			Name:      "phpfpm_process_memory_max_bytes",
			Value:     float64(updatedInfo.MaxMemoryUsage),
			Type:      types.MetricTypeGauge,
			Labels:    labels,
			Timestamp: timestamp,
		})

		metrics = append(metrics, types.Metric{
			Name:      "phpfpm_process_memory_min_bytes",
			Value:     float64(updatedInfo.MinMemoryUsage),
			Type:      types.MetricTypeGauge,
			Labels:    labels,
			Timestamp: timestamp,
		})

		// Average memory usage
		if updatedInfo.SampleCount > 0 {
			avgMemory := float64(updatedInfo.TotalMemory) / float64(updatedInfo.SampleCount)
			metrics = append(metrics, types.Metric{
				Name:      "phpfpm_process_memory_avg_bytes",
				Value:     avgMemory,
				Type:      types.MetricTypeGauge,
				Labels:    labels,
				Timestamp: timestamp,
			})
		}

		// Sample count
		metrics = append(metrics, types.Metric{
			Name:      "phpfpm_process_memory_samples_total",
			Value:     float64(updatedInfo.SampleCount),
			Type:      types.MetricTypeCounter,
			Labels:    labels,
			Timestamp: timestamp,
		})
	}

	return metrics, nil
}

// readProcessMemory reads memory information for a specific process from /proc/[pid]/status
func (m *MemoryTracker) readProcessMemory(pid int) (*ProcessMemoryStats, error) {
	statusPath := fmt.Sprintf("/proc/%d/status", pid)
	data, err := os.ReadFile(statusPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", statusPath, err)
	}

	stats := &ProcessMemoryStats{}
	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		key := strings.TrimSuffix(parts[0], ":")

		// Parse memory values (they're in kB)
		var value uint64
		if len(parts) >= 2 {
			if v, err := strconv.ParseUint(parts[1], 10, 64); err == nil {
				value = v * 1024 // Convert from kB to bytes
			}
		}

		switch key {
		case "VmPeak":
			stats.VmPeak = value
		case "VmSize":
			stats.VmSize = value
		case "VmHWM":
			stats.VmHWM = value
		case "VmRSS":
			stats.VmRSS = value
		case "VmData":
			stats.VmData = value
		case "VmStk":
			stats.VmStk = value
		case "VmExe":
			stats.VmExe = value
		case "VmLib":
			stats.VmLib = value
		}
	}

	return stats, nil
}

// CalculateMedianMemory calculates the median memory usage for a process
// This would require storing a history of memory samples
func (m *MemoryTracker) CalculateMedianMemory(pid int) (float64, error) {
	// This is a simplified implementation
	// In a real implementation, you'd maintain a sliding window of memory samples
	// and calculate the actual median

	processInfo, err := m.GetProcessStats(pid)
	if err != nil {
		return 0, err
	}

	if processInfo.SampleCount == 0 {
		return 0, fmt.Errorf("no samples available")
	}

	// For now, return the average as an approximation
	// In production, you'd implement proper median calculation
	avgMemory := float64(processInfo.TotalMemory) / float64(processInfo.SampleCount)

	return avgMemory, nil
}

// GetMemoryUtilization calculates memory utilization percentage for a process
func (m *MemoryTracker) GetMemoryUtilization(pid int) (float64, error) {
	processInfo, err := m.GetProcessStats(pid)
	if err != nil {
		return 0, err
	}

	// Read system memory info to calculate percentage
	systemMemory, err := m.getSystemMemoryTotal()
	if err != nil {
		return 0, fmt.Errorf("failed to get system memory: %w", err)
	}

	if systemMemory == 0 {
		return 0, fmt.Errorf("invalid system memory total")
	}

	utilization := (float64(processInfo.VmRSS) / float64(systemMemory)) * 100
	return utilization, nil
}

// getSystemMemoryTotal reads total system memory from /proc/meminfo
func (m *MemoryTracker) getSystemMemoryTotal() (uint64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, fmt.Errorf("failed to read /proc/meminfo: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if total, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
					return total * 1024, nil // Convert from kB to bytes
				}
			}
			break
		}
	}

	return 0, fmt.Errorf("MemTotal not found in /proc/meminfo")
}
