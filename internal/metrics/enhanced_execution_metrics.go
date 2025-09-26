package metrics

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// EnhancedExecutionCollector integrates full lifecycle execution tracking with PHP-FPM monitoring
type EnhancedExecutionCollector struct {
	logger           *zap.Logger
	executionTracker *ProcessExecutionTracker
	phpfpmClient     *PHPFPMClient

	// Execution state management
	mu                sync.RWMutex
	lastSeenProcesses map[int]*PHPFPMProcess
	processingStates  map[int]ProcessState

	// Integration with autoscaler (interface to avoid import cycle)
	autoscalerFeedFunc func(ExecutionMetrics)

	// Performance metrics
	collectInterval    time.Duration
	lastCollectionTime time.Time
}

// ProcessState tracks the processing state of PHP-FPM workers
type ProcessState struct {
	PID              int                `json:"pid"`
	State            PHPFPMProcessState `json:"state"`
	LastStateChange  time.Time          `json:"last_state_change"`
	RequestStartTime time.Time          `json:"request_start_time"`
	IsExecuting      bool               `json:"is_executing"`
	RequestURI       string             `json:"request_uri"`
	RequestMethod    string             `json:"request_method"`
}

// FullExecutionMetrics combines PHP-FPM status data with full execution tracking
type FullExecutionMetrics struct {
	// From PHP-FPM status
	PHPFPMStatus  *PHPFPMStatus         `json:"phpfpm_status"`
	WorkerMetrics *WorkerProcessMetrics `json:"worker_metrics"`

	// From execution tracking
	ActiveExecutions    map[int]*ProcessExecution `json:"active_executions"`
	CompletedExecutions []ExecutionMetrics        `json:"completed_executions"`
	ExecutionSummary    ExecutionSummary          `json:"execution_summary"`

	// Combined metrics for scaling
	ScalingMetrics *ScalingExecutionMetrics `json:"scaling_metrics"`
	Timestamp      time.Time                `json:"timestamp"`
}

// ScalingExecutionMetrics provides enhanced metrics for intelligent scaling decisions
type ScalingExecutionMetrics struct {
	// Enhanced worker metrics including child processes
	TotalWorkers  int `json:"total_workers"`
	ActiveWorkers int `json:"active_workers"`
	IdleWorkers   int `json:"idle_workers"`

	// CPU metrics (including children)
	AvgCPUUtilization       float64 `json:"avg_cpu_utilization"`
	PeakCPUUtilization      float64 `json:"peak_cpu_utilization"`
	ChildProcessCPUOverhead float64 `json:"child_process_cpu_overhead"`

	// Memory metrics (including children)
	AvgMemoryMBPerWorker       float64 `json:"avg_memory_mb_per_worker"`
	PeakMemoryMBPerWorker      float64 `json:"peak_memory_mb_per_worker"`
	ChildProcessMemoryOverhead float64 `json:"child_process_memory_overhead"`

	// Execution characteristics
	AvgExecutionDuration time.Duration `json:"avg_execution_duration"`
	P95ExecutionDuration time.Duration `json:"p95_execution_duration"`
	P99ExecutionDuration time.Duration `json:"p99_execution_duration"`

	// Processing efficiency
	ProcessingVelocity       float64 `json:"processing_velocity"`
	EffectiveProcessingPower float64 `json:"effective_processing_power"`
	ResourceEfficiency       float64 `json:"resource_efficiency"`

	// Queue and scaling indicators
	QueueDepth                int           `json:"queue_depth"`
	EstimatedQueueProcessTime time.Duration `json:"estimated_queue_process_time"`
	RecommendedWorkerChange   int           `json:"recommended_worker_change"`
	ScalingConfidence         float64       `json:"scaling_confidence"`
}

// NewEnhancedExecutionCollector creates a new enhanced execution collector
func NewEnhancedExecutionCollector(phpfpmClient *PHPFPMClient, logger *zap.Logger) (*EnhancedExecutionCollector, error) {
	executionTracker, err := NewProcessExecutionTracker(logger.Named("execution-tracker"))
	if err != nil {
		return nil, fmt.Errorf("failed to create execution tracker: %w", err)
	}

	return &EnhancedExecutionCollector{
		logger:            logger,
		executionTracker:  executionTracker,
		phpfpmClient:      phpfpmClient,
		lastSeenProcesses: make(map[int]*PHPFPMProcess),
		processingStates:  make(map[int]ProcessState),
		collectInterval:   100 * time.Millisecond, // High frequency for accurate tracking
	}, nil
}

// CollectFullExecutionMetrics collects comprehensive execution metrics from PHP-FPM
func (c *EnhancedExecutionCollector) CollectFullExecutionMetrics(ctx context.Context, endpoint string) (*FullExecutionMetrics, error) {
	now := time.Now()

	// Get PHP-FPM status with full process details
	phpfpmStatus, err := c.phpfpmClient.GetStatus(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get PHP-FPM status: %w", err)
	}

	// Process worker metrics from PHP-FPM
	workerMetrics, err := c.phpfpmClient.ProcessWorkerMetrics(phpfpmStatus)
	if err != nil {
		return nil, fmt.Errorf("failed to process worker metrics: %w", err)
	}

	// Update execution tracking based on process state changes
	if err := c.updateExecutionTracking(phpfpmStatus.Processes); err != nil {
		c.logger.Warn("Failed to update execution tracking", zap.Error(err))
	}

	// Update active process tracking
	if err := c.executionTracker.UpdateTracking(ctx); err != nil {
		c.logger.Warn("Failed to update process tracking", zap.Error(err))
	}

	// Get execution data
	activeExecutions := c.executionTracker.GetActiveExecutions()
	completedExecutions := c.executionTracker.GetCompletedExecutions(100) // Last 100 executions
	executionSummary := c.executionTracker.GetExecutionSummary()

	// Calculate enhanced scaling metrics
	scalingMetrics := c.calculateScalingMetrics(workerMetrics, activeExecutions, completedExecutions, executionSummary)

	return &FullExecutionMetrics{
		PHPFPMStatus:        phpfpmStatus,
		WorkerMetrics:       workerMetrics,
		ActiveExecutions:    activeExecutions,
		CompletedExecutions: completedExecutions,
		ExecutionSummary:    executionSummary,
		ScalingMetrics:      scalingMetrics,
		Timestamp:           now,
	}, nil
}

// updateExecutionTracking tracks process state changes and starts/stops execution tracking
func (c *EnhancedExecutionCollector) updateExecutionTracking(processes []PHPFPMProcess) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	currentProcesses := make(map[int]*PHPFPMProcess)
	for i := range processes {
		currentProcesses[processes[i].PID] = &processes[i]
	}

	// Check for state changes and new executions
	for pid, currentProc := range currentProcesses {
		lastProc, existed := c.lastSeenProcesses[pid]
		currentState, stateExists := c.processingStates[pid]

		// Detect execution start (transition from Idle to active state)
		if !existed || (lastProc.State == ProcessStateIdle && c.isActiveState(currentProc.State)) {
			// Start tracking new execution
			requestURI := currentProc.RequestURI
			if requestURI == "" {
				requestURI = "unknown"
			}
			requestMethod := currentProc.RequestMethod
			if requestMethod == "" {
				requestMethod = "unknown"
			}

			if err := c.executionTracker.StartTracking(pid, requestURI, requestMethod); err != nil {
				c.logger.Debug("Failed to start tracking process",
					zap.Int("pid", pid),
					zap.Error(err))
			} else {
				c.logger.Debug("Started tracking execution",
					zap.Int("pid", pid),
					zap.String("state", string(currentProc.State)),
					zap.String("uri", requestURI))
			}

			// Update processing state
			c.processingStates[pid] = ProcessState{
				PID:              pid,
				State:            currentProc.State,
				LastStateChange:  time.Now(),
				RequestStartTime: time.Now(),
				IsExecuting:      true,
				RequestURI:       requestURI,
				RequestMethod:    requestMethod,
			}
		}

		// Detect execution completion (transition from active state to Idle)
		if existed && c.isActiveState(lastProc.State) && currentProc.State == ProcessStateIdle {
			// Stop tracking execution
			if metrics, err := c.executionTracker.StopTracking(pid); err != nil {
				c.logger.Debug("Failed to stop tracking process",
					zap.Int("pid", pid),
					zap.Error(err))
			} else {
				c.logger.Debug("Completed tracking execution",
					zap.Int("pid", pid),
					zap.Duration("duration", metrics.Duration),
					zap.Float64("cpu_utilization", metrics.CPUUtilization),
					zap.Float64("peak_memory_mb", metrics.PeakMemoryMB))

				// Feed to autoscaler if configured
				if c.autoscalerFeedFunc != nil {
					c.autoscalerFeedFunc(*metrics)
				}
			}

			// Update processing state
			if stateExists {
				currentState.State = currentProc.State
				currentState.LastStateChange = time.Now()
				currentState.IsExecuting = false
				c.processingStates[pid] = currentState
			}
		}

		// Update state tracking for existing processes
		if stateExists && currentProc.State != currentState.State {
			currentState.State = currentProc.State
			currentState.LastStateChange = time.Now()
			c.processingStates[pid] = currentState
		}
	}

	// Clean up processes that no longer exist
	for pid := range c.lastSeenProcesses {
		if _, exists := currentProcesses[pid]; !exists {
			// Process ended, stop tracking if active
			if _, err := c.executionTracker.StopTracking(pid); err == nil {
				c.logger.Debug("Stopped tracking ended process", zap.Int("pid", pid))
			}
			delete(c.processingStates, pid)
		}
	}

	// Update last seen processes
	c.lastSeenProcesses = currentProcesses

	return nil
}

// isActiveState checks if a process state indicates active execution
func (c *EnhancedExecutionCollector) isActiveState(state PHPFPMProcessState) bool {
	return state == ProcessStateRunning ||
		state == ProcessStateReading ||
		state == ProcessStateInfo ||
		state == ProcessStateFinishing ||
		state == ProcessStateEnding
}

// calculateScalingMetrics computes enhanced metrics for intelligent scaling
func (c *EnhancedExecutionCollector) calculateScalingMetrics(
	workerMetrics *WorkerProcessMetrics,
	activeExecutions map[int]*ProcessExecution,
	completedExecutions []ExecutionMetrics,
	executionSummary ExecutionSummary) *ScalingExecutionMetrics {

	scalingMetrics := &ScalingExecutionMetrics{
		TotalWorkers:              workerMetrics.TotalWorkers,
		ActiveWorkers:             workerMetrics.ActiveWorkers,
		IdleWorkers:               workerMetrics.IdleWorkers,
		QueueDepth:                workerMetrics.QueueDepth,
		ProcessingVelocity:        workerMetrics.ProcessingVelocity,
		EstimatedQueueProcessTime: workerMetrics.EstimatedProcessTime,
	}

	// Calculate CPU metrics including child process overhead
	if len(activeExecutions) > 0 {
		totalCPU := 0.0
		peakCPU := 0.0
		totalChildCPU := 0.0

		for _, exec := range activeExecutions {
			if exec.IsActive {
				duration := time.Since(exec.StartTime)
				if duration > 0 {
					cpuUtil := float64(exec.CumulativeCPUTime) / float64(duration) * 100
					totalCPU += cpuUtil
					if cpuUtil > peakCPU {
						peakCPU = cpuUtil
					}

					// Add child process CPU
					for _, child := range exec.ChildProcesses {
						childDuration := time.Since(child.StartTime)
						if childDuration > 0 {
							childCPU := float64(child.CumulativeCPUTime) / float64(childDuration) * 100
							totalChildCPU += childCPU
						}
					}
				}
			}
		}

		if len(activeExecutions) > 0 {
			scalingMetrics.AvgCPUUtilization = totalCPU / float64(len(activeExecutions))
			scalingMetrics.PeakCPUUtilization = peakCPU
			scalingMetrics.ChildProcessCPUOverhead = totalChildCPU / float64(len(activeExecutions))
		}
	}

	// Calculate memory metrics including child process overhead
	if len(activeExecutions) > 0 {
		totalMemory := 0.0
		peakMemory := 0.0
		totalChildMemory := 0.0

		for _, exec := range activeExecutions {
			memoryMB := float64(exec.CurrentMemoryKB) / 1024
			totalMemory += memoryMB

			peakMB := float64(exec.PeakMemoryKB) / 1024
			if peakMB > peakMemory {
				peakMemory = peakMB
			}

			// Add child process memory
			for _, child := range exec.ChildProcesses {
				childMemoryMB := float64(child.PeakMemoryKB) / 1024
				totalChildMemory += childMemoryMB
			}
		}

		scalingMetrics.AvgMemoryMBPerWorker = totalMemory / float64(len(activeExecutions))
		scalingMetrics.PeakMemoryMBPerWorker = peakMemory
		scalingMetrics.ChildProcessMemoryOverhead = totalChildMemory / float64(len(activeExecutions))
	}

	// Calculate execution duration percentiles from completed executions
	if len(completedExecutions) > 0 {
		durations := make([]time.Duration, len(completedExecutions))
		totalDuration := time.Duration(0)

		for i, exec := range completedExecutions {
			durations[i] = exec.Duration
			totalDuration += exec.Duration
		}

		scalingMetrics.AvgExecutionDuration = totalDuration / time.Duration(len(completedExecutions))

		// Calculate percentiles (simple implementation)
		if len(durations) >= 20 { // Need at least 20 samples for percentiles
			p95Index := int(float64(len(durations)) * 0.95)
			p99Index := int(float64(len(durations)) * 0.99)

			// Sort durations for percentile calculation
			for i := 0; i < len(durations)-1; i++ {
				for j := i + 1; j < len(durations); j++ {
					if durations[i] > durations[j] {
						durations[i], durations[j] = durations[j], durations[i]
					}
				}
			}

			scalingMetrics.P95ExecutionDuration = durations[p95Index]
			scalingMetrics.P99ExecutionDuration = durations[p99Index]
		}
	}

	// Calculate effective processing power (accounts for child process overhead)
	baseProcessingPower := scalingMetrics.ProcessingVelocity
	cpuOverheadFactor := 1.0 + (scalingMetrics.ChildProcessCPUOverhead / 100.0)
	memoryOverheadFactor := 1.0 + (scalingMetrics.ChildProcessMemoryOverhead / scalingMetrics.AvgMemoryMBPerWorker)

	scalingMetrics.EffectiveProcessingPower = baseProcessingPower / (cpuOverheadFactor * memoryOverheadFactor)

	// Calculate resource efficiency (throughput per resource unit)
	if scalingMetrics.AvgMemoryMBPerWorker > 0 && scalingMetrics.AvgCPUUtilization > 0 {
		resourceCost := scalingMetrics.AvgMemoryMBPerWorker + scalingMetrics.AvgCPUUtilization
		scalingMetrics.ResourceEfficiency = scalingMetrics.EffectiveProcessingPower / resourceCost
	}

	// Calculate scaling recommendation and confidence
	c.calculateScalingRecommendation(scalingMetrics)

	return scalingMetrics
}

// calculateScalingRecommendation determines scaling recommendations based on enhanced metrics
func (c *EnhancedExecutionCollector) calculateScalingRecommendation(metrics *ScalingExecutionMetrics) {
	// Initialize recommendation
	metrics.RecommendedWorkerChange = 0
	metrics.ScalingConfidence = 0.5

	// High queue pressure indicates need to scale up
	if metrics.QueueDepth > 0 {
		// Calculate queue pressure indicators
		jobsPerWorker := float64(metrics.QueueDepth) / float64(metrics.ActiveWorkers)
		processTimeSeconds := metrics.EstimatedQueueProcessTime.Seconds()

		// Scale up decision factors
		if processTimeSeconds > 30.0 || jobsPerWorker > 2.0 {
			// Calculate scale up amount based on queue pressure
			if processTimeSeconds > 60.0 {
				// Critical: scale up aggressively
				metrics.RecommendedWorkerChange = metrics.ActiveWorkers / 2 // Add 50% more workers
				metrics.ScalingConfidence = 0.9
			} else if processTimeSeconds > 30.0 {
				// High: scale up moderately
				metrics.RecommendedWorkerChange = metrics.ActiveWorkers / 3 // Add 33% more workers
				metrics.ScalingConfidence = 0.8
			} else {
				// Moderate: scale up conservatively
				metrics.RecommendedWorkerChange = 1 // Add 1 worker
				metrics.ScalingConfidence = 0.6
			}
		}
	}

	// Resource efficiency considerations
	if metrics.ResourceEfficiency < 0.1 { // Poor efficiency
		// Reduce scaling confidence for resource-intensive workloads
		metrics.ScalingConfidence *= 0.7
	} else if metrics.ResourceEfficiency > 1.0 { // Good efficiency
		// Increase scaling confidence for efficient workloads
		metrics.ScalingConfidence = min(metrics.ScalingConfidence*1.2, 1.0)
	}

	// Child process overhead considerations
	if metrics.ChildProcessCPUOverhead > 50.0 || metrics.ChildProcessMemoryOverhead > 100.0 {
		// High child process overhead - be more conservative
		metrics.RecommendedWorkerChange = metrics.RecommendedWorkerChange / 2
		metrics.ScalingConfidence *= 0.8
	}
}

// SetAutoscalerFeedFunc sets the function to feed execution metrics to autoscaler
func (c *EnhancedExecutionCollector) SetAutoscalerFeedFunc(feedFunc func(ExecutionMetrics)) {
	c.autoscalerFeedFunc = feedFunc
}

// helper function
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
