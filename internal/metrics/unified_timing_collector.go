// Package metrics provides comprehensive monitoring and timing analysis
// for PHP-FPM worker processes with adaptive polling strategies.
package metrics

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// Error variables
var (
	ErrCollectorStopped = errors.New("collector is stopped")
	ErrInvalidInterval  = errors.New("invalid interval")
)

// CollectorMode defines the operational mode of the timing collector
type CollectorMode int

const (
	// ModePrecise provides high-precision timing with fixed intervals
	ModePrecise CollectorMode = iota
	// ModeAdaptive automatically adjusts polling frequency based on activity
	ModeAdaptive
	// ModeHybrid combines precise timing with adaptive adjustments
	ModeHybrid
)

// String returns the string representation of CollectorMode
func (m CollectorMode) String() string {
	switch m {
	case ModePrecise:
		return "precise"
	case ModeAdaptive:
		return "adaptive"
	case ModeHybrid:
		return "hybrid"
	default:
		return "unknown"
	}
}

// UnifiedCollectorConfig provides comprehensive configuration for the unified timing collector
type UnifiedCollectorConfig struct {
	// Mode determines the collector's operational behavior
	Mode CollectorMode `json:"mode"`

	// Timing Configuration
	BaseInterval  time.Duration `json:"base_interval"`
	MinInterval   time.Duration `json:"min_interval"`
	MaxInterval   time.Duration `json:"max_interval"`
	BurstDuration time.Duration `json:"burst_duration"`

	// Feature Toggles
	EnableAdaptivePolling   bool `json:"enable_adaptive_polling"`
	EnableStateTransitions  bool `json:"enable_state_transitions"`
	EnableRequestBoundaries bool `json:"enable_request_boundaries"`
	EnableSystemCorrelation bool `json:"enable_system_correlation"`
	EnableInternalMetrics   bool `json:"enable_internal_metrics"`

	// Optimization Settings
	UseObjectPools      bool    `json:"use_object_pools"`
	MaxHistorySize      int     `json:"max_history_size"`
	MaxProcessStates    int     `json:"max_process_states"`
	ConfidenceThreshold float64 `json:"confidence_threshold"`

	// Advanced Settings
	SmoothingFactor   float64       `json:"smoothing_factor"`    // For interval transitions
	ActivityThreshold float64       `json:"activity_threshold"`  // For activity level changes
	BurstTriggerCount int           `json:"burst_trigger_count"` // Events to trigger burst mode
	CollectionTimeout time.Duration `json:"collection_timeout"`  // Max time for single collection
}

// DefaultUnifiedConfig returns a production-ready default configuration
func DefaultUnifiedConfig() UnifiedCollectorConfig {
	return UnifiedCollectorConfig{
		Mode: ModeHybrid,

		// Timing
		BaseInterval:  200 * time.Millisecond,
		MinInterval:   10 * time.Millisecond,
		MaxInterval:   2 * time.Second,
		BurstDuration: 2 * time.Second,

		// Features
		EnableAdaptivePolling:   true,
		EnableStateTransitions:  true,
		EnableRequestBoundaries: true,
		EnableSystemCorrelation: false, // Requires /proc access
		EnableInternalMetrics:   true,

		// Optimization
		UseObjectPools:      true,
		MaxHistorySize:      1000,
		MaxProcessStates:    500,
		ConfidenceThreshold: 0.75,

		// Advanced
		SmoothingFactor:   0.25,
		ActivityThreshold: 0.1,
		BurstTriggerCount: 3,
		CollectionTimeout: 5 * time.Second,
	}
}

// UnifiedTimingCollector consolidates all timing collection functionality
type UnifiedTimingCollector struct {
	// Configuration
	config UnifiedCollectorConfig
	mode   CollectorMode
	logger *zap.Logger

	// Core components (using interfaces for dependency injection)
	phpfpmClient     PHPFPMClientInterface
	stateTracker     ProcessTracker
	eventDetector    EventDetectorInterface
	intervalManager  IntervalManagerInterface
	metricsCollector InternalMetricsCollectorInterface

	// State management
	processStates map[int]*ProcessStateRecord
	stateHistory  CircularBufferInterface
	eventHistory  CircularBufferInterface

	// Adaptive state
	currentInterval atomic.Value // time.Duration
	activityLevel   atomic.Value // ActivityLevel
	burstModeUntil  atomic.Value // time.Time
	lastCollection  atomic.Value // time.Time

	// Statistics
	stats atomic.Value // CollectorStats

	// Resource pools
	statePool *sync.Pool
	eventPool *sync.Pool

	// Lifecycle
	ctx     context.Context
	cancel  context.CancelFunc
	running atomic.Bool
	stopped atomic.Bool

	// Synchronization
	mu         sync.RWMutex
	collecting sync.Mutex // Prevents concurrent collections
}

// NewUnifiedTimingCollector creates a consolidated timing collector with default implementations
func NewUnifiedTimingCollector(
	config UnifiedCollectorConfig,
	phpfpmClient PHPFPMClientInterface,
	logger *zap.Logger,
) (*UnifiedTimingCollector, error) {
	return NewUnifiedTimingCollectorWithDependencies(config, phpfpmClient, nil, nil, nil, nil, logger)
}

// NewUnifiedTimingCollectorWithDependencies creates a collector with injectable dependencies
func NewUnifiedTimingCollectorWithDependencies(
	config UnifiedCollectorConfig,
	phpfpmClient PHPFPMClientInterface,
	stateTracker ProcessTracker,
	eventDetector EventDetectorInterface,
	intervalManager IntervalManagerInterface,
	metricsCollector InternalMetricsCollectorInterface,
	logger *zap.Logger,
) (*UnifiedTimingCollector, error) {
	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	collector := &UnifiedTimingCollector{
		config:           config,
		mode:             config.Mode,
		logger:           logger.Named("unified-timing"),
		phpfpmClient:     phpfpmClient,
		stateTracker:     stateTracker,
		eventDetector:    eventDetector,
		intervalManager:  intervalManager,
		metricsCollector: metricsCollector,
		processStates:    make(map[int]*ProcessStateRecord),
		stateHistory:     NewCircularBuffer(config.MaxHistorySize),
		eventHistory:     NewCircularBuffer(config.MaxHistorySize),
		ctx:              ctx,
		cancel:           cancel,
	}

	// Initialize components based on configuration (create defaults if nil)
	if err := collector.initializeComponents(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize components: %w", err)
	}

	// Initialize atomic values
	collector.currentInterval.Store(config.BaseInterval)
	collector.activityLevel.Store(ActivityLevelLow)
	collector.burstModeUntil.Store(time.Time{})
	collector.lastCollection.Store(time.Time{})
	collector.stats.Store(CollectorStats{})

	// Initialize resource pools if enabled
	if config.UseObjectPools {
		collector.initializePools()
	}

	collector.logger.Info("Unified timing collector initialized",
		zap.String("mode", config.Mode.String()),
		zap.Duration("base_interval", config.BaseInterval),
		zap.Bool("adaptive", config.EnableAdaptivePolling))

	return collector, nil
}

// initializeComponents sets up internal components based on configuration
// Creates default implementations for nil interfaces
func (c *UnifiedTimingCollector) initializeComponents() error {
	// State tracker for process state management
	if c.stateTracker == nil {
		c.stateTracker = NewStateTracker(c.logger.Named("state-tracker"))
	}

	// Event detector for timing event detection
	if c.eventDetector == nil {
		detectorConfig := EventDetectorConfig{
			EnableStateTransitions:  c.config.EnableStateTransitions,
			EnableRequestBoundaries: c.config.EnableRequestBoundaries,
			ConfidenceThreshold:     c.config.ConfidenceThreshold,
		}
		c.eventDetector = NewEventDetector(detectorConfig, c.logger.Named("event-detector"))
	}

	// Interval manager for adaptive polling
	if c.config.EnableAdaptivePolling && c.intervalManager == nil {
		intervalConfig := IntervalManagerConfig{
			BaseInterval:      c.config.BaseInterval,
			MinInterval:       c.config.MinInterval,
			MaxInterval:       c.config.MaxInterval,
			BurstDuration:     c.config.BurstDuration,
			SmoothingFactor:   c.config.SmoothingFactor,
			ActivityThreshold: c.config.ActivityThreshold,
		}
		c.intervalManager = NewIntervalManager(intervalConfig, c.logger.Named("interval-manager"))
	}

	// Internal metrics collector
	if c.config.EnableInternalMetrics && c.metricsCollector == nil {
		c.metricsCollector = NewInternalMetricsCollector()
	}

	return nil
}

// initializePools sets up resource pools for object reuse
func (c *UnifiedTimingCollector) initializePools() {
	c.statePool = &sync.Pool{
		New: func() interface{} {
			return &ProcessStateRecord{}
		},
	}

	c.eventPool = &sync.Pool{
		New: func() interface{} {
			return &TimingEvent{}
		},
	}
}

// Collect performs a collection cycle with mode-appropriate behavior
func (c *UnifiedTimingCollector) Collect(ctx context.Context, endpoint string) (*EnhancedMetrics, error) {
	// Prevent concurrent collections
	if !c.collecting.TryLock() {
		return nil, errors.New("collection already in progress")
	}
	defer c.collecting.Unlock()

	// Check if stopped
	if c.stopped.Load() {
		return nil, ErrCollectorStopped
	}

	// Apply collection timeout
	collectCtx, cancel := context.WithTimeout(ctx, c.config.CollectionTimeout)
	defer cancel()

	startTime := time.Now()
	c.lastCollection.Store(startTime)

	// Record collection start
	if c.metricsCollector != nil {
		c.metricsCollector.RecordCollection()
	}

	// Execute mode-specific collection
	metrics, err := c.executeCollection(collectCtx, endpoint, startTime)
	if err != nil {
		if c.metricsCollector != nil {
			c.metricsCollector.RecordError("collection")
		}
		return nil, err
	}

	// Update statistics
	c.updateStatistics(time.Since(startTime), metrics)

	return metrics, nil
}

// executeCollection performs the actual collection based on mode
func (c *UnifiedTimingCollector) executeCollection(
	ctx context.Context,
	endpoint string,
	startTime time.Time,
) (*EnhancedMetrics, error) {
	// Get PHP-FPM status
	status, err := c.phpfpmClient.GetStatus(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get PHP-FPM status: %w", err)
	}

	// Process worker metrics
	workerMetrics, err := c.phpfpmClient.ProcessWorkerMetrics(status)
	if err != nil {
		return nil, fmt.Errorf("failed to process worker metrics: %w", err)
	}

	// Mode-specific processing
	var analysis *TimingAnalysis

	switch c.mode {
	case ModePrecise:
		analysis = c.executePreciseMode(status.Processes, startTime)
	case ModeAdaptive:
		analysis = c.executeAdaptiveMode(status.Processes, startTime, workerMetrics)
	case ModeHybrid:
		analysis = c.executeHybridMode(status.Processes, startTime, workerMetrics)
	default:
		return nil, fmt.Errorf("unsupported collector mode: %v", c.mode)
	}

	// Build enhanced metrics
	enhancedMetrics := &EnhancedMetrics{
		WorkerMetrics:  workerMetrics,
		TimingAnalysis: analysis,
		Timestamp:      startTime,
	}

	// Add internal metrics if enabled
	if c.metricsCollector != nil {
		enhancedMetrics.CollectorMetrics = c.metricsCollector.GetMetrics()
	}

	return enhancedMetrics, nil
}

// executePreciseMode performs high-precision timing with fixed intervals
func (c *UnifiedTimingCollector) executePreciseMode(
	processes []PHPFPMProcess,
	pollTime time.Time,
) *TimingAnalysis {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Update process states
	c.stateTracker.UpdateStates(processes, pollTime)

	// Get updated process states
	processStates := c.stateTracker.GetProcessStates()

	// Detect timing events
	events := c.eventDetector.DetectEvents(processStates, processes, pollTime)

	// Build timing analysis
	analysis := &TimingAnalysis{
		PollTime:   pollTime,
		Events:     events,
		Confidence: c.calculateConfidence(c.currentInterval.Load().(time.Duration)),
	}

	// Record events in history
	for _, event := range events {
		c.eventHistory.Add(event.Clone())
	}

	return analysis
}

// executeAdaptiveMode performs adaptive polling with dynamic intervals
func (c *UnifiedTimingCollector) executeAdaptiveMode(
	processes []PHPFPMProcess,
	pollTime time.Time,
	metrics *WorkerProcessMetrics,
) *TimingAnalysis {
	// Execute precise timing first
	analysis := c.executePreciseMode(processes, pollTime)

	// Adjust polling interval based on activity
	if c.intervalManager != nil {
		newInterval := c.intervalManager.CalculateInterval(
			metrics,
			analysis,
			c.activityLevel.Load().(ActivityLevel),
		)

		// Check for burst mode
		if c.intervalManager.ShouldEnterBurstMode(analysis, metrics) {
			c.enterBurstMode()
		} else if !c.inBurstMode() {
			c.updateInterval(newInterval)
		}
	}

	return analysis
}

// executeHybridMode combines precise timing with adaptive adjustments
func (c *UnifiedTimingCollector) executeHybridMode(
	processes []PHPFPMProcess,
	pollTime time.Time,
	metrics *WorkerProcessMetrics,
) *TimingAnalysis {
	// Combine both modes
	analysis := c.executeAdaptiveMode(processes, pollTime, metrics)

	// Additional hybrid-specific processing
	if c.config.EnableSystemCorrelation {
		c.correlateWithSystemMetrics(analysis, processes)
	}

	return analysis
}

// Helper methods

func (c *UnifiedTimingCollector) calculateConfidence(interval time.Duration) float64 {
	switch {
	case interval <= 20*time.Millisecond:
		return 0.95
	case interval <= 50*time.Millisecond:
		return 0.85
	case interval <= 100*time.Millisecond:
		return 0.75
	case interval <= 200*time.Millisecond:
		return 0.65
	case interval <= 500*time.Millisecond:
		return 0.55
	default:
		return 0.40
	}
}

func (c *UnifiedTimingCollector) enterBurstMode() {
	c.currentInterval.Store(c.config.MinInterval)
	c.burstModeUntil.Store(time.Now().Add(c.config.BurstDuration))

	c.logger.Info("Entering burst mode",
		zap.Duration("interval", c.config.MinInterval),
		zap.Duration("duration", c.config.BurstDuration))
}

func (c *UnifiedTimingCollector) inBurstMode() bool {
	return time.Now().Before(c.burstModeUntil.Load().(time.Time))
}

func (c *UnifiedTimingCollector) updateInterval(newInterval time.Duration) {
	currentInterval := c.currentInterval.Load().(time.Duration)

	// Apply smoothing
	smoothedInterval := c.smoothInterval(currentInterval, newInterval)

	// Apply bounds
	if smoothedInterval < c.config.MinInterval {
		smoothedInterval = c.config.MinInterval
	} else if smoothedInterval > c.config.MaxInterval {
		smoothedInterval = c.config.MaxInterval
	}

	c.currentInterval.Store(smoothedInterval)

	c.logger.Debug("Polling interval updated",
		zap.Duration("from", currentInterval),
		zap.Duration("to", smoothedInterval))
}

func (c *UnifiedTimingCollector) smoothInterval(current, target time.Duration) time.Duration {
	// Apply exponential smoothing
	diff := float64(target - current)
	maxChange := float64(current) * c.config.SmoothingFactor

	if diff > maxChange {
		return current + time.Duration(maxChange)
	} else if diff < -maxChange {
		return current - time.Duration(maxChange)
	}

	return target
}

func (c *UnifiedTimingCollector) correlateWithSystemMetrics(
	analysis *TimingAnalysis,
	processes []PHPFPMProcess,
) {
	// Skip correlation on non-Linux systems or if procfs is not available
	if _, err := os.Stat("/proc"); os.IsNotExist(err) {
		c.logger.Debug("Skipping system metrics correlation: /proc not available")
		return
	}

	// Create a map to store system metrics for each process
	systemMetrics := make(map[int]*ProcessSystemMetrics)

	// Read system metrics for each PHP-FPM process
	for _, process := range processes {
		if process.PID <= 0 {
			continue
		}

		procStat, err := c.readProcStat(process.PID)
		if err != nil {
			c.logger.Debug("Failed to read proc stat for process",
				zap.Int("pid", process.PID),
				zap.Error(err))
			continue
		}

		systemMetrics[process.PID] = &ProcessSystemMetrics{
			PID:           process.PID,
			State:         string(process.State),
			CPUUserTime:   procStat.UTime,
			CPUSystemTime: procStat.STime,
			MemoryRSS:     procStat.RSS,
			MemoryVSize:   procStat.VSize,
			StartTime:     procStat.StartTime,
			Timestamp:     time.Now(),
		}
	}

	// Correlate timing events with system metrics
	for _, event := range analysis.Events {
		if event.PID <= 0 {
			continue
		}

		// Set ProcessID alias for backward compatibility
		event.ProcessID = event.PID

		sysMetrics, exists := systemMetrics[event.PID]
		if !exists {
			continue
		}

		// Calculate CPU usage correlation
		totalCPUTime := sysMetrics.CPUUserTime + sysMetrics.CPUSystemTime
		if totalCPUTime > 0 {
			// Estimate CPU usage intensity based on request duration and CPU time
			requestDurationSec := float64(event.Duration) / float64(time.Second)
			cpuUsageRatio := float64(totalCPUTime) / float64(time.Second) / requestDurationSec
			if cpuUsageRatio > 0 {
				sysMetrics.CPUUsageRatio = cpuUsageRatio
			}
		}

		// Calculate memory correlation
		if sysMetrics.MemoryRSS > 0 {
			// Memory intensity relative to request duration
			memoryIntensity := float64(sysMetrics.MemoryRSS) / float64(event.Duration.Milliseconds())
			if memoryIntensity > 0 {
				sysMetrics.MemoryIntensity = memoryIntensity
			}
		}

		// Store correlated metrics in the event
		event.SystemMetrics = sysMetrics
	}

	// Update analysis with correlation statistics
	c.updateCorrelationStatistics(analysis, systemMetrics)
}

// readProcStat reads process statistics from /proc/[pid]/stat
func (c *UnifiedTimingCollector) readProcStat(pid int) (*ProcStat, error) {
	statPath := filepath.Join("/proc", strconv.Itoa(pid), "stat")
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

// updateCorrelationStatistics updates the timing analysis with correlation statistics
func (c *UnifiedTimingCollector) updateCorrelationStatistics(
	analysis *TimingAnalysis,
	systemMetrics map[int]*ProcessSystemMetrics,
) {
	if len(systemMetrics) == 0 {
		return
	}

	// Calculate aggregate correlation statistics
	var totalCPUTime time.Duration
	var totalMemoryRSS int64
	var processCount int

	for _, metrics := range systemMetrics {
		totalCPUTime += metrics.CPUUserTime + metrics.CPUSystemTime
		totalMemoryRSS += metrics.MemoryRSS
		processCount++
	}

	if processCount > 0 {
		// Store correlation metadata in the analysis
		if analysis.Metadata == nil {
			analysis.Metadata = make(map[string]interface{})
		}

		analysis.Metadata["system_correlation"] = map[string]interface{}{
			"process_count":         processCount,
			"avg_cpu_time_ms":       float64(totalCPUTime.Nanoseconds()) / float64(processCount) / 1e6,
			"avg_memory_rss_kb":     float64(totalMemoryRSS) * 4 / float64(processCount), // Convert pages to KB (4KB pages)
			"correlation_timestamp": time.Now(),
		}

		c.logger.Debug("System metrics correlation completed",
			zap.Int("processes", processCount),
			zap.Duration("total_cpu_time", totalCPUTime),
			zap.Int64("total_memory_rss", totalMemoryRSS))
	}
}

func (c *UnifiedTimingCollector) updateStatistics(
	duration time.Duration,
	metrics *EnhancedMetrics,
) {
	stats := c.stats.Load().(CollectorStats)
	stats.TotalCollections++
	stats.LastCollectionTime = time.Now()
	stats.LastCollectionDuration = duration

	if metrics != nil && metrics.TimingAnalysis != nil {
		stats.EventsDetected += int64(len(metrics.TimingAnalysis.Events))
	}

	// Update average duration
	if stats.AverageCollectionDuration == 0 {
		stats.AverageCollectionDuration = duration
	} else {
		stats.AverageCollectionDuration = (stats.AverageCollectionDuration + duration) / 2
	}

	c.stats.Store(stats)
}

// Public interface methods

// GetCurrentInterval returns the current polling interval
func (c *UnifiedTimingCollector) GetCurrentInterval() time.Duration {
	return c.currentInterval.Load().(time.Duration)
}

// SetMode changes the collector's operational mode
func (c *UnifiedTimingCollector) SetMode(mode CollectorMode) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running.Load() {
		return errors.New("cannot change mode while collector is running")
	}

	c.mode = mode
	c.logger.Info("Collector mode changed", zap.String("mode", mode.String()))

	return nil
}

// Start begins the collection process
func (c *UnifiedTimingCollector) Start(ctx context.Context) error {
	if !c.running.CompareAndSwap(false, true) {
		return errors.New("collector already running")
	}

	c.logger.Info("Starting unified timing collector",
		zap.String("mode", c.mode.String()),
		zap.Duration("base_interval", c.config.BaseInterval))

	// Start background processing if needed
	// go c.backgroundProcessor()

	return nil
}

// Stop gracefully stops the collector
func (c *UnifiedTimingCollector) Stop() error {
	if !c.stopped.CompareAndSwap(false, true) {
		return errors.New("collector already stopped")
	}

	c.running.Store(false)
	c.cancel()

	// Wait for any ongoing collection to complete
	c.collecting.Lock()
	c.collecting.Unlock()

	stats := c.stats.Load().(CollectorStats)
	c.logger.Info("Stopped unified timing collector",
		zap.Int64("total_collections", stats.TotalCollections),
		zap.Int64("events_detected", stats.EventsDetected))

	return nil
}

// Stats returns collector statistics
func (c *UnifiedTimingCollector) Stats() CollectorStats {
	return c.stats.Load().(CollectorStats)
}

// SetInterval manually sets the polling interval (precise mode only)
func (c *UnifiedTimingCollector) SetInterval(interval time.Duration) error {
	if c.mode != ModePrecise {
		return fmt.Errorf("manual interval setting only supported in precise mode")
	}

	if interval < c.config.MinInterval || interval > c.config.MaxInterval {
		return fmt.Errorf("%w: interval %v outside bounds [%v, %v]",
			ErrInvalidInterval, interval, c.config.MinInterval, c.config.MaxInterval)
	}

	c.currentInterval.Store(interval)
	c.logger.Info("Polling interval manually set", zap.Duration("interval", interval))

	return nil
}

// Validate checks if the configuration is valid
func (c UnifiedCollectorConfig) Validate() error {
	if c.BaseInterval <= 0 {
		return fmt.Errorf("base interval must be positive")
	}

	if c.MinInterval <= 0 || c.MinInterval > c.BaseInterval {
		return fmt.Errorf("min interval must be positive and <= base interval")
	}

	if c.MaxInterval < c.BaseInterval {
		return fmt.Errorf("max interval must be >= base interval")
	}

	if c.MaxHistorySize <= 0 {
		return fmt.Errorf("max history size must be positive")
	}

	if c.ConfidenceThreshold < 0 || c.ConfidenceThreshold > 1 {
		return fmt.Errorf("confidence threshold must be between 0 and 1")
	}

	if c.SmoothingFactor < 0 || c.SmoothingFactor > 1 {
		return fmt.Errorf("smoothing factor must be between 0 and 1")
	}

	return nil
}
