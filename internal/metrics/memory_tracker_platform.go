package metrics

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/platform"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/types"
)

// PlatformAwareMemoryTracker tracks memory usage using platform abstraction
type PlatformAwareMemoryTracker struct {
	logger   *zap.Logger
	provider platform.Provider

	// Process tracking
	processes map[int]*ProcessMemoryInfo
	mu        sync.RWMutex

	// State
	running bool
}

// NewPlatformAwareMemoryTracker creates a new platform-aware memory tracker
func NewPlatformAwareMemoryTracker(logger *zap.Logger, provider platform.Provider) *PlatformAwareMemoryTracker {
	return &PlatformAwareMemoryTracker{
		logger:    logger,
		provider:  provider,
		processes: make(map[int]*ProcessMemoryInfo),
	}
}

// Start begins memory tracking
func (m *PlatformAwareMemoryTracker) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return fmt.Errorf("memory tracker is already running")
	}
	m.running = true
	m.mu.Unlock()

	m.logger.Info("Starting platform-aware memory tracker",
		zap.String("platform", m.provider.Platform()))
	return nil
}

// Stop halts memory tracking
func (m *PlatformAwareMemoryTracker) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return nil
	}
	m.running = false
	m.mu.Unlock()

	m.logger.Info("Stopping platform-aware memory tracker")
	return nil
}

// TrackProcess starts tracking memory usage for a specific process
func (m *PlatformAwareMemoryTracker) TrackProcess(pid int) error {
	if !m.running {
		return fmt.Errorf("memory tracker is not running")
	}

	ctx := context.Background()
	processInfo, err := m.provider.Process().GetProcessInfo(ctx, pid)
	if err != nil {
		return fmt.Errorf("failed to get process memory info: %w", err)
	}

	m.mu.Lock()
	m.processes[pid] = &ProcessMemoryInfo{
		PID:            pid,
		VmSize:         processInfo.VirtualBytes,
		VmRSS:          processInfo.ResidentBytes,
		VmPeak:         processInfo.VirtualBytes,
		VmHWM:          processInfo.ResidentBytes,
		LastUpdate:     time.Now(),
		MaxMemoryUsage: processInfo.ResidentBytes,
		MinMemoryUsage: processInfo.ResidentBytes,
		SampleCount:    1,
		TotalMemory:    processInfo.ResidentBytes,
	}
	m.mu.Unlock()

	m.logger.Debug("Started tracking process memory using platform provider",
		zap.Int("pid", pid),
		zap.String("platform", m.provider.Platform()))
	return nil
}

// UntrackProcess stops tracking memory usage for a specific process
func (m *PlatformAwareMemoryTracker) UntrackProcess(pid int) {
	m.mu.Lock()
	delete(m.processes, pid)
	m.mu.Unlock()

	m.logger.Debug("Stopped tracking process memory", zap.Int("pid", pid))
}

// UpdateProcess updates memory statistics for a specific process
func (m *PlatformAwareMemoryTracker) UpdateProcess(pid int) error {
	m.mu.RLock()
	processInfo, exists := m.processes[pid]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("process %d is not being tracked", pid)
	}

	ctx := context.Background()
	currentInfo, err := m.provider.Process().GetProcessInfo(ctx, pid)
	if err != nil {
		return fmt.Errorf("failed to get current memory info: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Update current values
	processInfo.VmSize = currentInfo.VirtualBytes
	processInfo.VmRSS = currentInfo.ResidentBytes
	processInfo.LastUpdate = time.Now()

	// Update peaks if applicable
	if currentInfo.VirtualBytes > processInfo.VmPeak {
		processInfo.VmPeak = currentInfo.VirtualBytes
	}
	if currentInfo.ResidentBytes > processInfo.VmHWM {
		processInfo.VmHWM = currentInfo.ResidentBytes
	}

	// Update statistics
	processInfo.SampleCount++
	processInfo.TotalMemory += currentInfo.ResidentBytes

	if currentInfo.ResidentBytes > processInfo.MaxMemoryUsage {
		processInfo.MaxMemoryUsage = currentInfo.ResidentBytes
	}

	if currentInfo.ResidentBytes < processInfo.MinMemoryUsage {
		processInfo.MinMemoryUsage = currentInfo.ResidentBytes
	}

	return nil
}

// GetProcessStats returns memory statistics for a specific process
func (m *PlatformAwareMemoryTracker) GetProcessStats(pid int) (*ProcessMemoryInfo, error) {
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
func (m *PlatformAwareMemoryTracker) GetMetrics(ctx context.Context) ([]types.Metric, error) {
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
			"pid":      strconv.Itoa(pid),
			"platform": m.provider.Platform(),
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

	// Add platform-specific system metrics
	if err := m.addSystemMetrics(ctx, &metrics, timestamp); err != nil {
		m.logger.Warn("Failed to add system metrics", zap.Error(err))
	}

	return metrics, nil
}

// addSystemMetrics adds system-level memory metrics
func (m *PlatformAwareMemoryTracker) addSystemMetrics(ctx context.Context, metrics *[]types.Metric, timestamp time.Time) error {
	sysInfo, err := m.provider.Memory().GetMemoryInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to get system memory info: %w", err)
	}

	labels := map[string]string{
		"platform": m.provider.Platform(),
	}

	// System memory metrics
	*metrics = append(*metrics, types.Metric{
		Name:      "phpfpm_system_memory_total_bytes",
		Value:     float64(sysInfo.TotalBytes),
		Type:      types.MetricTypeGauge,
		Labels:    labels,
		Timestamp: timestamp,
	})

	*metrics = append(*metrics, types.Metric{
		Name:      "phpfpm_system_memory_available_bytes",
		Value:     float64(sysInfo.AvailableBytes),
		Type:      types.MetricTypeGauge,
		Labels:    labels,
		Timestamp: timestamp,
	})

	*metrics = append(*metrics, types.Metric{
		Name:      "phpfpm_system_memory_used_bytes",
		Value:     float64(sysInfo.UsedBytes),
		Type:      types.MetricTypeGauge,
		Labels:    labels,
		Timestamp: timestamp,
	})

	*metrics = append(*metrics, types.Metric{
		Name:      "phpfpm_system_memory_free_bytes",
		Value:     float64(sysInfo.FreeBytes),
		Type:      types.MetricTypeGauge,
		Labels:    labels,
		Timestamp: timestamp,
	})

	// Memory utilization percentage
	if sysInfo.TotalBytes > 0 {
		utilizationPercent := (float64(sysInfo.UsedBytes) / float64(sysInfo.TotalBytes)) * 100
		*metrics = append(*metrics, types.Metric{
			Name:      "phpfpm_system_memory_utilization_percent",
			Value:     utilizationPercent,
			Type:      types.MetricTypeGauge,
			Labels:    labels,
			Timestamp: timestamp,
		})
	}

	return nil
}

// GetMemoryUtilization calculates memory utilization percentage for a process
func (m *PlatformAwareMemoryTracker) GetMemoryUtilization(pid int) (float64, error) {
	processInfo, err := m.GetProcessStats(pid)
	if err != nil {
		return 0, err
	}

	// Use platform provider to get system memory
	ctx := context.Background()
	sysInfo, err := m.provider.Memory().GetMemoryInfo(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get system memory: %w", err)
	}

	if sysInfo.TotalBytes == 0 {
		return 0, fmt.Errorf("invalid system memory total")
	}

	utilization := (float64(processInfo.VmRSS) / float64(sysInfo.TotalBytes)) * 100
	return utilization, nil
}

// CalculateMedianMemory calculates the median memory usage for a process
func (m *PlatformAwareMemoryTracker) CalculateMedianMemory(pid int) (float64, error) {
	processInfo, err := m.GetProcessStats(pid)
	if err != nil {
		return 0, err
	}

	if processInfo.SampleCount == 0 {
		return 0, fmt.Errorf("no samples available")
	}

	// For now, return the average as an approximation
	// In production, you'd implement proper median calculation with sample history
	avgMemory := float64(processInfo.TotalMemory) / float64(processInfo.SampleCount)

	return avgMemory, nil
}

// GetMemoryInfoTotal returns total system memory using platform provider
func (m *PlatformAwareMemoryTracker) GetMemoryInfoTotal() (uint64, error) {
	ctx := context.Background()
	sysInfo, err := m.provider.Memory().GetMemoryInfo(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get system memory: %w", err)
	}
	return sysInfo.TotalBytes, nil
}

// GetAvailableMemory returns available system memory using platform provider
func (m *PlatformAwareMemoryTracker) GetAvailableMemory() (uint64, error) {
	ctx := context.Background()
	return m.provider.Memory().GetAvailableMemory(ctx)
}

// GetPlatformInfo returns information about the platform being used
func (m *PlatformAwareMemoryTracker) GetPlatformInfo() map[string]interface{} {
	return map[string]interface{}{
		"platform":           m.provider.Platform(),
		"memory_supported":   m.provider.Memory().IsSupported(),
		"process_supported":  m.provider.Process().IsSupported(),
		"provider_supported": m.provider.IsSupported(),
	}
}

// ValidateProcessExists checks if a process exists using platform provider
func (m *PlatformAwareMemoryTracker) ValidateProcessExists(pid int) (bool, error) {
	ctx := context.Background()
	_, err := m.provider.Process().GetProcessInfo(ctx, pid)
	if err != nil {
		return false, nil // Process doesn't exist or not accessible
	}
	return true, nil
}

// ListProcesses returns all available processes using platform provider
func (m *PlatformAwareMemoryTracker) ListProcesses() ([]*platform.ProcessInfo, error) {
	ctx := context.Background()
	return m.provider.Process().ListProcesses(ctx, platform.ProcessFilter{})
}

// FindProcessesByName finds processes by name pattern using platform provider
func (m *PlatformAwareMemoryTracker) FindProcessesByName(namePattern string) ([]*platform.ProcessInfo, error) {
	ctx := context.Background()
	filter := platform.ProcessFilter{
		NamePattern: namePattern,
	}
	return m.provider.Process().ListProcesses(ctx, filter)
}

// GlobalPlatformAwareTracker manages a global instance for backward compatibility
var globalPlatformAwareTracker *PlatformAwareMemoryTracker

// GetGlobalPlatformAwareTracker returns the global platform-aware tracker instance
func GetGlobalPlatformAwareTracker(logger *zap.Logger) *PlatformAwareMemoryTracker {
	if globalPlatformAwareTracker == nil {
		provider, err := platform.GetProvider()
		if err != nil {
			logger.Error("Failed to get platform provider", zap.Error(err))
			return nil
		}
		globalPlatformAwareTracker = NewPlatformAwareMemoryTracker(logger, provider)
	}
	return globalPlatformAwareTracker
}

// SetGlobalPlatformAwareTracker sets the global tracker (for testing)
func SetGlobalPlatformAwareTracker(tracker *PlatformAwareMemoryTracker) {
	globalPlatformAwareTracker = tracker
}

// ResetGlobalPlatformAwareTracker resets the global tracker
func ResetGlobalPlatformAwareTracker() {
	globalPlatformAwareTracker = nil
}
