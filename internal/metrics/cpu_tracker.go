package metrics

import (
	"context"
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/metrics/resource"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/types"
	"go.uber.org/zap"
)

// CPUTracker tracks CPU usage and deltas for processes
type CPUTracker struct {
	logger *zap.Logger

	// Process tracking
	processes map[int]*ProcessCPUInfo
	mu        sync.RWMutex

	// System CPU info
	lastSystemCPU *SystemCPUInfo
	systemMu      sync.RWMutex

	// Cross-platform resource monitor
	resourceMonitor resource.ResourceMonitor

	// State
	running bool
}

// ProcessCPUInfo tracks CPU information for a process
type ProcessCPUInfo struct {
	PID       int
	LastUTime uint64
	LastSTime uint64
	LastTime  time.Time
	TotalCPU  float64
	CPUDelta  float64
}

// SystemCPUInfo tracks system-wide CPU information
type SystemCPUInfo struct {
	User      uint64
	Nice      uint64
	System    uint64
	Idle      uint64
	IOWait    uint64
	IRQ       uint64
	SoftIRQ   uint64
	Steal     uint64
	Guest     uint64
	GuestNice uint64
	Timestamp time.Time
}

// NewCPUTracker creates a new CPU tracker
func NewCPUTracker(logger *zap.Logger) (*CPUTracker, error) {
	// Initialize cross-platform resource monitor
	resourceMonitor, err := resource.NewResourceMonitor()
	if err != nil {
		logger.Warn("Failed to create resource monitor, CPU tracking may be limited", zap.Error(err))
		// Continue without resource monitor for basic process tracking
	}

	return &CPUTracker{
		logger:          logger,
		processes:       make(map[int]*ProcessCPUInfo),
		resourceMonitor: resourceMonitor,
	}, nil
}

// Start begins CPU tracking
func (c *CPUTracker) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("CPU tracker is already running")
	}
	c.running = true
	c.mu.Unlock()

	c.logger.Info("Starting CPU tracker")

	// Initialize system CPU baseline using cross-platform resource monitor
	if c.resourceMonitor != nil {
		systemCPU, err := c.readSystemCPUCrossPlatform(ctx)
		if err != nil {
			c.logger.Warn("Failed to read initial system CPU via resource monitor, using fallback", zap.Error(err))
			// Try fallback method
			systemCPU, err = c.readSystemCPUFallback()
			if err != nil {
				return fmt.Errorf("failed to read initial system CPU: %w", err)
			}
		}

		c.systemMu.Lock()
		c.lastSystemCPU = systemCPU
		c.systemMu.Unlock()
	} else {
		// Fallback for systems without resource monitor
		systemCPU, err := c.readSystemCPUFallback()
		if err != nil {
			c.logger.Warn("System CPU tracking unavailable", zap.Error(err))
			// Continue without system CPU tracking
		} else {
			c.systemMu.Lock()
			c.lastSystemCPU = systemCPU
			c.systemMu.Unlock()
		}
	}

	return nil
}

// Stop halts CPU tracking
func (c *CPUTracker) Stop(ctx context.Context) error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = false
	c.mu.Unlock()

	c.logger.Info("Stopping CPU tracker")

	// Cleanup resource monitor
	if c.resourceMonitor != nil {
		if err := c.resourceMonitor.Close(); err != nil {
			c.logger.Warn("Failed to close resource monitor", zap.Error(err))
		}
	}

	return nil
}

// TrackProcess starts tracking CPU usage for a specific process
func (c *CPUTracker) TrackProcess(pid int) error {
	if !c.running {
		return fmt.Errorf("CPU tracker is not running")
	}

	cpuInfo, err := c.readProcessCPU(pid)
	if err != nil {
		return fmt.Errorf("failed to read process CPU info: %w", err)
	}

	c.mu.Lock()
	c.processes[pid] = &ProcessCPUInfo{
		PID:       pid,
		LastUTime: cpuInfo.UTime,
		LastSTime: cpuInfo.STime,
		LastTime:  time.Now(),
		TotalCPU:  0,
		CPUDelta:  0,
	}
	c.mu.Unlock()

	c.logger.Debug("Started tracking process CPU", zap.Int("pid", pid))
	return nil
}

// UntrackProcess stops tracking CPU usage for a specific process
func (c *CPUTracker) UntrackProcess(pid int) {
	c.mu.Lock()
	delete(c.processes, pid)
	c.mu.Unlock()

	c.logger.Debug("Stopped tracking process CPU", zap.Int("pid", pid))
}

// GetProcessDelta calculates CPU delta for a specific process
func (c *CPUTracker) GetProcessDelta(pid int) (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	processInfo, exists := c.processes[pid]
	if !exists {
		return 0, fmt.Errorf("process %d is not being tracked", pid)
	}

	currentCPU, err := c.readProcessCPU(pid)
	if err != nil {
		return 0, fmt.Errorf("failed to read current CPU info: %w", err)
	}

	now := time.Now()
	timeDelta := now.Sub(processInfo.LastTime).Seconds()

	if timeDelta <= 0 {
		return 0, nil
	}

	// Calculate CPU time delta (in clock ticks)
	uTimeDelta := currentCPU.UTime - processInfo.LastUTime
	sTimeDelta := currentCPU.STime - processInfo.LastSTime
	totalTimeDelta := float64(uTimeDelta + sTimeDelta)

	// Convert to CPU percentage
	// CPU ticks per second (usually 100 on Linux)
	clockTicks := float64(100) // sysconf(_SC_CLK_TCK)
	cpuPercent := (totalTimeDelta / clockTicks) / timeDelta * 100

	// Update tracking info
	processInfo.LastUTime = currentCPU.UTime
	processInfo.LastSTime = currentCPU.STime
	processInfo.LastTime = now
	processInfo.CPUDelta = cpuPercent
	processInfo.TotalCPU += cpuPercent * timeDelta

	return cpuPercent, nil
}

// GetMetrics returns current CPU metrics
func (c *CPUTracker) GetMetrics(ctx context.Context) ([]types.Metric, error) {
	var metrics []types.Metric
	timestamp := time.Now()

	// System CPU metrics
	systemMetrics, err := c.getSystemCPUMetrics()
	if err != nil {
		c.logger.Debug("Skipping system CPU metrics", zap.Error(err))
	} else if systemMetrics != nil {
		metrics = append(metrics, systemMetrics...)
	}

	// Process CPU metrics - collect PIDs first to avoid holding lock during GetProcessDelta
	c.mu.RLock()
	pids := make([]int, 0, len(c.processes))
	for pid := range c.processes {
		pids = append(pids, pid)
	}
	c.mu.RUnlock()

	// Now process each PID (GetProcessDelta will handle its own locking)
	for _, pid := range pids {
		// Update process delta
		if delta, err := c.GetProcessDelta(pid); err == nil {
			// Get process info for TotalCPU after delta calculation
			c.mu.RLock()
			processInfo, exists := c.processes[pid]
			c.mu.RUnlock()

			if exists {
				labels := map[string]string{
					"pid": strconv.Itoa(pid),
				}

				metrics = append(metrics, types.Metric{
					Name:      "phpfpm_process_cpu_percent",
					Value:     delta,
					Type:      types.MetricTypeGauge,
					Labels:    labels,
					Timestamp: timestamp,
				})

				metrics = append(metrics, types.Metric{
					Name:      "phpfpm_process_cpu_total_seconds",
					Value:     processInfo.TotalCPU / 100, // Convert percentage back to seconds
					Type:      types.MetricTypeCounter,
					Labels:    labels,
					Timestamp: timestamp,
				})
			}
		}
	}

	return metrics, nil
}

// getSystemCPUMetrics calculates system-wide CPU metrics
func (c *CPUTracker) getSystemCPUMetrics() ([]types.Metric, error) {
	var currentCPU *SystemCPUInfo
	var err error

	// Try cross-platform approach first
	if c.resourceMonitor != nil {
		currentCPU, err = c.readSystemCPUCrossPlatform(context.Background())
	}

	// Fallback to Linux-specific approach if needed
	if currentCPU == nil || err != nil {
		currentCPU, err = c.readSystemCPUFallback()
		if err != nil {
			return nil, fmt.Errorf("failed to read system CPU: %w", err)
		}
	}

	c.systemMu.Lock()
	lastCPU := c.lastSystemCPU
	c.lastSystemCPU = currentCPU
	c.systemMu.Unlock()

	if lastCPU == nil {
		c.logger.Debug("No previous CPU data available, skipping CPU calculation")
		return nil, fmt.Errorf("no previous CPU data available")
	}

	// Calculate deltas
	timeDelta := currentCPU.Timestamp.Sub(lastCPU.Timestamp).Seconds()
	if timeDelta <= 0 {
		return nil, fmt.Errorf("invalid time delta")
	}

	// Calculate total CPU time delta
	totalDelta := float64(
		(currentCPU.User - lastCPU.User) +
			(currentCPU.Nice - lastCPU.Nice) +
			(currentCPU.System - lastCPU.System) +
			(currentCPU.Idle - lastCPU.Idle) +
			(currentCPU.IOWait - lastCPU.IOWait) +
			(currentCPU.IRQ - lastCPU.IRQ) +
			(currentCPU.SoftIRQ - lastCPU.SoftIRQ) +
			(currentCPU.Steal - lastCPU.Steal))

	if totalDelta <= 0 {
		// Not enough time elapsed for accurate CPU calculation
		c.logger.Debug("Insufficient time delta for CPU calculation",
			zap.Float64("time_delta", timeDelta),
			zap.Float64("total_delta", totalDelta))
		return nil, fmt.Errorf("insufficient time delta for CPU calculation")
	}

	// Calculate percentages with bounds checking
	userPercent := math.Min(100.0, math.Max(0.0, float64(currentCPU.User-lastCPU.User)/totalDelta*100))
	systemPercent := math.Min(100.0, math.Max(0.0, float64(currentCPU.System-lastCPU.System)/totalDelta*100))
	idlePercent := math.Min(100.0, math.Max(0.0, float64(currentCPU.Idle-lastCPU.Idle)/totalDelta*100))
	iowaitPercent := math.Min(100.0, math.Max(0.0, float64(currentCPU.IOWait-lastCPU.IOWait)/totalDelta*100))

	timestamp := time.Now()

	return []types.Metric{
		{
			Name:      "system_cpu_user_percent",
			Value:     userPercent,
			Type:      types.MetricTypeGauge,
			Timestamp: timestamp,
		},
		{
			Name:      "system_cpu_system_percent",
			Value:     systemPercent,
			Type:      types.MetricTypeGauge,
			Timestamp: timestamp,
		},
		{
			Name:      "system_cpu_idle_percent",
			Value:     idlePercent,
			Type:      types.MetricTypeGauge,
			Timestamp: timestamp,
		},
		{
			Name:      "system_cpu_iowait_percent",
			Value:     iowaitPercent,
			Type:      types.MetricTypeGauge,
			Timestamp: timestamp,
		},
		{
			Name:      "system_cpu_usage_percent",
			Value:     100 - idlePercent,
			Type:      types.MetricTypeGauge,
			Timestamp: timestamp,
		},
	}, nil
}

// ProcessCPUStat represents CPU statistics for a process from /proc/[pid]/stat
type ProcessCPUStat struct {
	UTime uint64 // CPU time spent in user mode
	STime uint64 // CPU time spent in kernel mode
}

// readProcessCPU reads CPU information for a specific process
func (c *CPUTracker) readProcessCPU(pid int) (*ProcessCPUStat, error) {
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	data, err := os.ReadFile(statPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", statPath, err)
	}

	// Parse stat file - fields are space-separated
	fields := strings.Fields(string(data))
	if len(fields) < 17 {
		return nil, fmt.Errorf("invalid stat file format")
	}

	// Fields 13 and 14 contain utime and stime
	utime, err := strconv.ParseUint(fields[13], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse utime: %w", err)
	}

	stime, err := strconv.ParseUint(fields[14], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse stime: %w", err)
	}

	return &ProcessCPUStat{
		UTime: utime,
		STime: stime,
	}, nil
}

// readSystemCPUCrossPlatform reads system CPU info using cross-platform resource monitor
func (c *CPUTracker) readSystemCPUCrossPlatform(ctx context.Context) (*SystemCPUInfo, error) {
	if c.resourceMonitor == nil {
		return nil, fmt.Errorf("resource monitor not available")
	}

	cpuStats, err := c.resourceMonitor.GetCPUStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get CPU stats: %w", err)
	}

	// Convert resource monitor stats to SystemCPUInfo format
	// Use monotonic time-based values to ensure positive deltas
	now := time.Now()
	timeSinceStart := now.Sub(time.Unix(0, 0)).Nanoseconds() / 1000000 // Convert to milliseconds

	// Calculate CPU time proportions based on usage percentage
	usageDecimal := cpuStats.UsagePercent / 100.0
	if usageDecimal > 1.0 {
		usageDecimal = 1.0 // Cap at 100%
	}

	totalTime := uint64(timeSinceStart)
	usedTime := uint64(float64(totalTime) * usageDecimal)
	idleTime := totalTime - usedTime

	return &SystemCPUInfo{
		User:      usedTime / 2, // Split between user and system
		Nice:      0,
		System:    usedTime / 2,
		Idle:      idleTime,
		IOWait:    0,
		IRQ:       0,
		SoftIRQ:   0,
		Steal:     0,
		Guest:     0,
		GuestNice: 0,
		Timestamp: now,
	}, nil
}

// readSystemCPUFallback reads system-wide CPU information from /proc/stat (Linux only)
func (c *CPUTracker) readSystemCPUFallback() (*SystemCPUInfo, error) {
	// Only attempt on Linux systems
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("system CPU tracking not supported on %s", runtime.GOOS)
	}
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc/stat: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 {
		return nil, fmt.Errorf("empty /proc/stat file")
	}

	// First line contains overall CPU stats
	cpuLine := lines[0]
	if !strings.HasPrefix(cpuLine, "cpu ") {
		return nil, fmt.Errorf("invalid /proc/stat format")
	}

	fields := strings.Fields(cpuLine)
	if len(fields) < 11 {
		return nil, fmt.Errorf("insufficient CPU fields in /proc/stat")
	}

	// Parse CPU values
	parseField := func(index int) (uint64, error) {
		return strconv.ParseUint(fields[index], 10, 64)
	}

	user, err := parseField(1)
	if err != nil {
		return nil, fmt.Errorf("failed to parse user CPU: %w", err)
	}

	nice, err := parseField(2)
	if err != nil {
		return nil, fmt.Errorf("failed to parse nice CPU: %w", err)
	}

	system, err := parseField(3)
	if err != nil {
		return nil, fmt.Errorf("failed to parse system CPU: %w", err)
	}

	idle, err := parseField(4)
	if err != nil {
		return nil, fmt.Errorf("failed to parse idle CPU: %w", err)
	}

	iowait, err := parseField(5)
	if err != nil {
		return nil, fmt.Errorf("failed to parse iowait CPU: %w", err)
	}

	irq, err := parseField(6)
	if err != nil {
		return nil, fmt.Errorf("failed to parse irq CPU: %w", err)
	}

	softirq, err := parseField(7)
	if err != nil {
		return nil, fmt.Errorf("failed to parse softirq CPU: %w", err)
	}

	steal, err := parseField(8)
	if err != nil {
		return nil, fmt.Errorf("failed to parse steal CPU: %w", err)
	}

	guest, err := parseField(9)
	if err != nil {
		return nil, fmt.Errorf("failed to parse guest CPU: %w", err)
	}

	guestNice, err := parseField(10)
	if err != nil {
		return nil, fmt.Errorf("failed to parse guest_nice CPU: %w", err)
	}

	return &SystemCPUInfo{
		User:      user,
		Nice:      nice,
		System:    system,
		Idle:      idle,
		IOWait:    iowait,
		IRQ:       irq,
		SoftIRQ:   softirq,
		Steal:     steal,
		Guest:     guest,
		GuestNice: guestNice,
		Timestamp: time.Now(),
	}, nil
}
