package metrics

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ProcessExecutionTracker tracks full lifecycle CPU and memory usage of PHP-FPM processes
// including child processes, from request start to completion
type ProcessExecutionTracker struct {
	logger *zap.Logger
	mu     sync.RWMutex

	// Active process tracking
	activeProcesses map[int]*ProcessExecution

	// Historical execution data for baseline learning
	completedExecutions []ExecutionMetrics
	maxHistorySize      int

	// Performance optimization
	procfsPath     string
	updateInterval time.Duration

	// System page size for memory calculations
	pageSize int64
}

// ProcessExecution tracks a single PHP-FPM process execution from start to finish
type ProcessExecution struct {
	PID        int       `json:"pid"`
	PPID       int       `json:"ppid"` // Parent process ID
	StartTime  time.Time `json:"start_time"`
	LastUpdate time.Time `json:"last_update"`

	// Resource usage tracking (cumulative)
	InitialCPUTime    time.Duration `json:"initial_cpu_time"`
	CurrentCPUTime    time.Duration `json:"current_cpu_time"`
	CumulativeCPUTime time.Duration `json:"cumulative_cpu_time"`

	InitialMemoryKB int64 `json:"initial_memory_kb"`
	CurrentMemoryKB int64 `json:"current_memory_kb"`
	PeakMemoryKB    int64 `json:"peak_memory_kb"`

	// Child process tracking
	ChildProcesses map[int]*ChildProcess `json:"child_processes"`

	// Request context
	RequestURI       string    `json:"request_uri"`
	RequestMethod    string    `json:"request_method"`
	RequestStartTime time.Time `json:"request_start_time"`

	// State tracking
	IsActive  bool   `json:"is_active"`
	LastState string `json:"last_state"`
}

// ChildProcess tracks resource usage of child processes spawned during execution
type ChildProcess struct {
	PID               int           `json:"pid"`
	StartTime         time.Time     `json:"start_time"`
	EndTime           time.Time     `json:"end_time"`
	CumulativeCPUTime time.Duration `json:"cumulative_cpu_time"`
	PeakMemoryKB      int64         `json:"peak_memory_kb"`
	Command           string        `json:"command"`
}

// ExecutionMetrics contains completed execution metrics for baseline learning
type ExecutionMetrics struct {
	PID               int           `json:"pid"`
	Duration          time.Duration `json:"duration"`
	TotalCPUTime      time.Duration `json:"total_cpu_time"`
	CPUUtilization    float64       `json:"cpu_utilization"`
	PeakMemoryMB      float64       `json:"peak_memory_mb"`
	AvgMemoryMB       float64       `json:"avg_memory_mb"`
	ChildProcessCount int           `json:"child_process_count"`
	ChildTotalCPUTime time.Duration `json:"child_total_cpu_time"`
	ChildPeakMemoryMB float64       `json:"child_peak_memory_mb"`
	RequestURI        string        `json:"request_uri"`
	RequestMethod     string        `json:"request_method"`
	CompletedAt       time.Time     `json:"completed_at"`
}

// ProcStat represents data from /proc/[pid]/stat for CPU and memory tracking
type ProcStat struct {
	PID       int           `json:"pid"`
	PPID      int           `json:"ppid"`
	UTime     time.Duration `json:"utime"`     // CPU time spent in user mode
	STime     time.Duration `json:"stime"`     // CPU time spent in kernel mode
	RSS       int64         `json:"rss"`       // Resident Set Size (pages)
	VSize     int64         `json:"vsize"`     // Virtual memory size (bytes)
	StartTime int64         `json:"starttime"` // Process start time
}

// NewProcessExecutionTracker creates a new tracker for PHP-FPM process executions
func NewProcessExecutionTracker(logger *zap.Logger) (*ProcessExecutionTracker, error) {
	// Get system page size for memory calculations
	pageSize := int64(4096) // Default 4KB
	if pageSizeStr := os.Getenv("PAGE_SIZE"); pageSizeStr != "" {
		if ps, err := strconv.ParseInt(pageSizeStr, 10, 64); err == nil {
			pageSize = ps
		}
	}

	// Try to read actual page size from getconf
	if ps, err := getSystemPageSize(); err == nil {
		pageSize = ps
	}

	tracker := &ProcessExecutionTracker{
		logger:              logger,
		activeProcesses:     make(map[int]*ProcessExecution),
		completedExecutions: make([]ExecutionMetrics, 0, 1000),
		maxHistorySize:      1000,
		procfsPath:          "/proc",
		updateInterval:      100 * time.Millisecond, // High frequency for accurate tracking
		pageSize:            pageSize,
	}

	return tracker, nil
}

// StartTracking begins tracking a PHP-FPM process execution
func (t *ProcessExecutionTracker) StartTracking(pid int, requestURI, requestMethod string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Check if already tracking this process
	if _, exists := t.activeProcesses[pid]; exists {
		return fmt.Errorf("process %d is already being tracked", pid)
	}

	// Get initial process stats
	stat, err := t.readProcStat(pid)
	if err != nil {
		return fmt.Errorf("failed to read initial stats for process %d: %w", pid, err)
	}

	now := time.Now()
	execution := &ProcessExecution{
		PID:               pid,
		PPID:              stat.PPID,
		StartTime:         now,
		LastUpdate:        now,
		InitialCPUTime:    stat.UTime + stat.STime,
		CurrentCPUTime:    stat.UTime + stat.STime,
		CumulativeCPUTime: 0,
		InitialMemoryKB:   stat.RSS * t.pageSize / 1024,
		CurrentMemoryKB:   stat.RSS * t.pageSize / 1024,
		PeakMemoryKB:      stat.RSS * t.pageSize / 1024,
		ChildProcesses:    make(map[int]*ChildProcess),
		RequestURI:        requestURI,
		RequestMethod:     requestMethod,
		RequestStartTime:  now,
		IsActive:          true,
		LastState:         "Running",
	}

	t.activeProcesses[pid] = execution

	t.logger.Debug("Started tracking process execution",
		zap.Int("pid", pid),
		zap.String("uri", requestURI),
		zap.String("method", requestMethod))

	return nil
}

// UpdateTracking updates metrics for all actively tracked processes
func (t *ProcessExecutionTracker) UpdateTracking(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	for pid, execution := range t.activeProcesses {
		if err := t.updateProcessExecution(execution); err != nil {
			t.logger.Warn("Failed to update process execution",
				zap.Int("pid", pid),
				zap.Error(err))
			continue
		}

		// Check for child processes
		if err := t.updateChildProcesses(execution); err != nil {
			t.logger.Warn("Failed to update child processes",
				zap.Int("pid", pid),
				zap.Error(err))
		}
	}

	return nil
}

// StopTracking stops tracking a process and records final execution metrics
func (t *ProcessExecutionTracker) StopTracking(pid int) (*ExecutionMetrics, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	execution, exists := t.activeProcesses[pid]
	if !exists {
		return nil, fmt.Errorf("process %d is not being tracked", pid)
	}

	// Final update
	t.updateProcessExecution(execution)
	t.updateChildProcesses(execution)

	// Calculate final metrics
	now := time.Now()
	duration := now.Sub(execution.StartTime)
	cpuUtilization := float64(execution.CumulativeCPUTime) / float64(duration) * 100

	// Calculate child process totals
	childCPUTime := time.Duration(0)
	childPeakMemory := int64(0)
	for _, child := range execution.ChildProcesses {
		childCPUTime += child.CumulativeCPUTime
		if child.PeakMemoryKB > childPeakMemory {
			childPeakMemory = child.PeakMemoryKB
		}
	}

	metrics := &ExecutionMetrics{
		PID:               pid,
		Duration:          duration,
		TotalCPUTime:      execution.CumulativeCPUTime,
		CPUUtilization:    cpuUtilization,
		PeakMemoryMB:      float64(execution.PeakMemoryKB) / 1024,
		AvgMemoryMB:       float64(execution.InitialMemoryKB+execution.CurrentMemoryKB) / 2 / 1024,
		ChildProcessCount: len(execution.ChildProcesses),
		ChildTotalCPUTime: childCPUTime,
		ChildPeakMemoryMB: float64(childPeakMemory) / 1024,
		RequestURI:        execution.RequestURI,
		RequestMethod:     execution.RequestMethod,
		CompletedAt:       now,
	}

	// Add to history
	if len(t.completedExecutions) >= t.maxHistorySize {
		// Remove oldest entries
		copy(t.completedExecutions, t.completedExecutions[1:])
		t.completedExecutions = t.completedExecutions[:t.maxHistorySize-1]
	}
	t.completedExecutions = append(t.completedExecutions, *metrics)

	// Remove from active tracking
	delete(t.activeProcesses, pid)

	t.logger.Debug("Stopped tracking process execution",
		zap.Int("pid", pid),
		zap.Duration("duration", duration),
		zap.Float64("cpu_utilization", cpuUtilization),
		zap.Float64("peak_memory_mb", metrics.PeakMemoryMB),
		zap.Int("child_processes", len(execution.ChildProcesses)))

	return metrics, nil
}

// GetActiveExecutions returns currently tracked executions
func (t *ProcessExecutionTracker) GetActiveExecutions() map[int]*ProcessExecution {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Return a copy to avoid race conditions
	result := make(map[int]*ProcessExecution, len(t.activeProcesses))
	for pid, execution := range t.activeProcesses {
		// Deep copy execution data
		execCopy := *execution
		execCopy.ChildProcesses = make(map[int]*ChildProcess, len(execution.ChildProcesses))
		for childPID, child := range execution.ChildProcesses {
			childCopy := *child
			execCopy.ChildProcesses[childPID] = &childCopy
		}
		result[pid] = &execCopy
	}

	return result
}

// GetCompletedExecutions returns historical execution metrics
func (t *ProcessExecutionTracker) GetCompletedExecutions(limit int) []ExecutionMetrics {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if limit <= 0 || limit > len(t.completedExecutions) {
		limit = len(t.completedExecutions)
	}

	// Return most recent executions
	start := len(t.completedExecutions) - limit
	result := make([]ExecutionMetrics, limit)
	copy(result, t.completedExecutions[start:])

	return result
}

// updateProcessExecution updates metrics for a single process
func (t *ProcessExecutionTracker) updateProcessExecution(execution *ProcessExecution) error {
	stat, err := t.readProcStat(execution.PID)
	if err != nil {
		return err
	}

	now := time.Now()

	// Update CPU time (cumulative)
	newCPUTime := stat.UTime + stat.STime
	execution.CumulativeCPUTime = newCPUTime - execution.InitialCPUTime
	execution.CurrentCPUTime = newCPUTime

	// Update memory usage
	currentMemoryKB := stat.RSS * t.pageSize / 1024
	execution.CurrentMemoryKB = currentMemoryKB
	if currentMemoryKB > execution.PeakMemoryKB {
		execution.PeakMemoryKB = currentMemoryKB
	}

	execution.LastUpdate = now
	return nil
}

// updateChildProcesses discovers and tracks child processes
func (t *ProcessExecutionTracker) updateChildProcesses(execution *ProcessExecution) error {
	// Find child processes by scanning /proc for processes with PPID = execution.PID
	childPIDs, err := t.findChildProcesses(execution.PID)
	if err != nil {
		return err
	}

	// Update existing child processes and add new ones
	for _, childPID := range childPIDs {
		if _, exists := execution.ChildProcesses[childPID]; !exists {
			// New child process
			child, err := t.createChildProcess(childPID)
			if err != nil {
				t.logger.Warn("Failed to create child process tracking",
					zap.Int("parent_pid", execution.PID),
					zap.Int("child_pid", childPID),
					zap.Error(err))
				continue
			}
			execution.ChildProcesses[childPID] = child
		} else {
			// Update existing child process
			if err := t.updateChildProcess(execution.ChildProcesses[childPID]); err != nil {
				t.logger.Warn("Failed to update child process",
					zap.Int("parent_pid", execution.PID),
					zap.Int("child_pid", childPID),
					zap.Error(err))
			}
		}
	}

	// Mark ended child processes
	for childPID, child := range execution.ChildProcesses {
		found := false
		for _, activePID := range childPIDs {
			if activePID == childPID {
				found = true
				break
			}
		}
		if !found && child.EndTime.IsZero() {
			child.EndTime = time.Now()
		}
	}

	return nil
}

// readProcStat reads process statistics from /proc/[pid]/stat
func (t *ProcessExecutionTracker) readProcStat(pid int) (*ProcStat, error) {
	statPath := filepath.Join(t.procfsPath, strconv.Itoa(pid), "stat")
	data, err := os.ReadFile(statPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", statPath, err)
	}

	fields := strings.Fields(string(data))
	if len(fields) < 24 {
		return nil, fmt.Errorf("invalid stat format for PID %d", pid)
	}

	// Parse key fields (see proc(5) man page for field positions)
	pid64, _ := strconv.ParseInt(fields[0], 10, 32)
	ppid64, _ := strconv.ParseInt(fields[3], 10, 32)
	utime64, _ := strconv.ParseInt(fields[13], 10, 64)
	stime64, _ := strconv.ParseInt(fields[14], 10, 64)
	rss64, _ := strconv.ParseInt(fields[23], 10, 64)
	vsize64, _ := strconv.ParseInt(fields[22], 10, 64)
	starttime64, _ := strconv.ParseInt(fields[21], 10, 64)

	// Convert clock ticks to duration (assuming 100 Hz)
	clockTick := time.Second / 100

	return &ProcStat{
		PID:       int(pid64),
		PPID:      int(ppid64),
		UTime:     time.Duration(utime64) * clockTick,
		STime:     time.Duration(stime64) * clockTick,
		RSS:       rss64,
		VSize:     vsize64,
		StartTime: starttime64,
	}, nil
}

// findChildProcesses finds all child processes of a given PID
func (t *ProcessExecutionTracker) findChildProcesses(parentPID int) ([]int, error) {
	var childPIDs []int

	procDir, err := os.Open(t.procfsPath)
	if err != nil {
		return nil, err
	}
	defer procDir.Close()

	entries, err := procDir.Readdir(-1)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pidStr := entry.Name()
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}

		stat, err := t.readProcStat(pid)
		if err != nil {
			continue
		}

		if stat.PPID == parentPID {
			childPIDs = append(childPIDs, pid)
		}
	}

	return childPIDs, nil
}

// createChildProcess creates tracking for a new child process
func (t *ProcessExecutionTracker) createChildProcess(pid int) (*ChildProcess, error) {
	stat, err := t.readProcStat(pid)
	if err != nil {
		return nil, err
	}

	// Read command line
	cmdlinePath := filepath.Join(t.procfsPath, strconv.Itoa(pid), "cmdline")
	cmdlineData, err := os.ReadFile(cmdlinePath)
	command := "unknown"
	if err == nil {
		command = strings.ReplaceAll(string(cmdlineData), "\x00", " ")
		if len(command) > 100 {
			command = command[:100] + "..."
		}
	}

	return &ChildProcess{
		PID:               pid,
		StartTime:         time.Now(),
		CumulativeCPUTime: stat.UTime + stat.STime,
		PeakMemoryKB:      stat.RSS * t.pageSize / 1024,
		Command:           command,
	}, nil
}

// updateChildProcess updates metrics for a child process
func (t *ProcessExecutionTracker) updateChildProcess(child *ChildProcess) error {
	stat, err := t.readProcStat(child.PID)
	if err != nil {
		return err
	}

	child.CumulativeCPUTime = stat.UTime + stat.STime
	currentMemoryKB := stat.RSS * t.pageSize / 1024
	if currentMemoryKB > child.PeakMemoryKB {
		child.PeakMemoryKB = currentMemoryKB
	}

	return nil
}

// getSystemPageSize tries to determine the system page size
func getSystemPageSize() (int64, error) {
	// Try reading from /proc/self/stat and calculating
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, err
	}

	// statm contains memory info in pages
	// Compare with /proc/self/status VmSize to calculate page size
	statusData, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, err
	}

	scanner := bufio.NewScanner(strings.NewReader(string(statusData)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "VmSize:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				vmSizeKB, err := strconv.ParseInt(fields[1], 10, 64)
				if err == nil {
					statmFields := strings.Fields(string(data))
					if len(statmFields) >= 1 {
						pages, err := strconv.ParseInt(statmFields[0], 10, 64)
						if err == nil && pages > 0 {
							return (vmSizeKB * 1024) / pages, nil
						}
					}
				}
			}
		}
	}

	return 4096, nil // Default fallback
}

// GetExecutionSummary returns summary statistics for baseline learning
func (t *ProcessExecutionTracker) GetExecutionSummary() ExecutionSummary {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if len(t.completedExecutions) == 0 {
		return ExecutionSummary{}
	}

	var totalCPU time.Duration
	var totalMemory float64
	var totalChildCPU time.Duration
	var totalChildMemory float64
	var totalDuration time.Duration
	totalExecutions := len(t.completedExecutions)

	for _, exec := range t.completedExecutions {
		totalCPU += exec.TotalCPUTime
		totalMemory += exec.PeakMemoryMB
		totalChildCPU += exec.ChildTotalCPUTime
		totalChildMemory += exec.ChildPeakMemoryMB
		totalDuration += exec.Duration
	}

	return ExecutionSummary{
		TotalExecutions:         totalExecutions,
		AvgCPUTimePerExecution:  totalCPU / time.Duration(totalExecutions),
		AvgMemoryMBPerExecution: totalMemory / float64(totalExecutions),
		AvgChildCPUTime:         totalChildCPU / time.Duration(totalExecutions),
		AvgChildMemoryMB:        totalChildMemory / float64(totalExecutions),
		AvgExecutionDuration:    totalDuration / time.Duration(totalExecutions),
		ActiveProcesses:         len(t.activeProcesses),
	}
}

// ExecutionSummary provides aggregated execution metrics
type ExecutionSummary struct {
	TotalExecutions         int           `json:"total_executions"`
	AvgCPUTimePerExecution  time.Duration `json:"avg_cpu_time_per_execution"`
	AvgMemoryMBPerExecution float64       `json:"avg_memory_mb_per_execution"`
	AvgChildCPUTime         time.Duration `json:"avg_child_cpu_time"`
	AvgChildMemoryMB        float64       `json:"avg_child_memory_mb"`
	AvgExecutionDuration    time.Duration `json:"avg_execution_duration"`
	ActiveProcesses         int           `json:"active_processes"`
}
