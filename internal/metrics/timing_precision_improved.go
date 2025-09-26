// Package metrics provides comprehensive PHP-FPM execution monitoring with precise timing
package metrics

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// Common errors for timing precision module
// Note: ErrCollectorStopped and ErrInvalidInterval are defined in unified_timing_collector.go
var (
	ErrNoProcessData = errors.New("no process data available")
)

// TimingCollector defines the interface for all timing collectors
type TimingCollector interface {
	// Collect performs a collection cycle and returns enhanced metrics
	Collect(ctx context.Context, endpoint string) (*EnhancedMetrics, error)

	// GetCurrentInterval returns the current polling interval
	GetCurrentInterval() time.Duration

	// SetInterval manually sets the polling interval
	SetInterval(interval time.Duration) error

	// Start begins the collection process
	Start(ctx context.Context) error

	// Stop gracefully stops the collector
	Stop() error

	// Stats returns collector statistics
	Stats() CollectorStats
}

// CollectorConfig holds configuration for the timing collector
type CollectorConfig struct {
	// Base polling interval
	BaseInterval time.Duration `json:"base_interval"`

	// Minimum interval for burst mode
	MinInterval time.Duration `json:"min_interval"`

	// Maximum interval for idle periods
	MaxInterval time.Duration `json:"max_interval"`

	// Duration of burst mode when triggered
	BurstDuration time.Duration `json:"burst_duration"`

	// Enable adaptive polling
	AdaptiveEnabled bool `json:"adaptive_enabled"`

	// Enable state transition detection
	TransitionDetectionEnabled bool `json:"transition_detection_enabled"`

	// Maximum history size for analysis
	MaxHistorySize int `json:"max_history_size"`

	// Confidence threshold for timing decisions
	ConfidenceThreshold float64 `json:"confidence_threshold"`
}

// DefaultCollectorConfig returns a production-ready default configuration
func DefaultCollectorConfig() CollectorConfig {
	return CollectorConfig{
		BaseInterval:               200 * time.Millisecond,
		MinInterval:                10 * time.Millisecond,
		MaxInterval:                2 * time.Second,
		BurstDuration:              2 * time.Second,
		AdaptiveEnabled:            true,
		TransitionDetectionEnabled: true,
		MaxHistorySize:             1000,
		ConfidenceThreshold:        0.75,
	}
}

// PrecisionTimingCollector implements high-precision timing with adaptive polling
type PrecisionTimingCollector struct {
	config       CollectorConfig
	logger       *zap.Logger
	phpfpmClient *PHPFPMClient

	// State tracking
	processStates map[int]*ProcessStateRecord
	stateHistory  *CircularBuffer
	eventHistory  *CircularBuffer

	// Adaptive polling state
	currentInterval atomic.Value // time.Duration
	activityLevel   atomic.Value // ActivityLevel
	burstModeUntil  atomic.Value // time.Time

	// Metrics and statistics
	stats            atomic.Value // CollectorStats
	metricsCollector *InternalMetricsCollector

	// Lifecycle management
	ctx     context.Context
	cancel  context.CancelFunc
	started atomic.Bool
	stopped atomic.Bool

	// Synchronization
	mu        sync.RWMutex
	statePool *sync.Pool
	eventPool *sync.Pool
}

// NewPrecisionTimingCollector creates a new precision timing collector
func NewPrecisionTimingCollector(
	config CollectorConfig,
	phpfpmClient *PHPFPMClient,
	logger *zap.Logger,
) (*PrecisionTimingCollector, error) {
	if config.BaseInterval <= 0 {
		return nil, fmt.Errorf("%w: base interval must be positive", ErrInvalidInterval)
	}

	if config.MinInterval <= 0 || config.MinInterval > config.BaseInterval {
		return nil, fmt.Errorf("%w: invalid min interval", ErrInvalidInterval)
	}

	if config.MaxInterval < config.BaseInterval {
		return nil, fmt.Errorf("%w: max interval must be >= base interval", ErrInvalidInterval)
	}

	ctx, cancel := context.WithCancel(context.Background())

	collector := &PrecisionTimingCollector{
		config:           config,
		logger:           logger.Named("precision-timing"),
		phpfpmClient:     phpfpmClient,
		processStates:    make(map[int]*ProcessStateRecord),
		stateHistory:     NewCircularBuffer(config.MaxHistorySize),
		eventHistory:     NewCircularBuffer(config.MaxHistorySize),
		metricsCollector: NewInternalMetricsCollector(),
		ctx:              ctx,
		cancel:           cancel,
		statePool: &sync.Pool{
			New: func() interface{} {
				return &ProcessStateRecord{}
			},
		},
		eventPool: &sync.Pool{
			New: func() interface{} {
				return &TimingEvent{}
			},
		},
	}

	// Initialize atomic values
	collector.currentInterval.Store(config.BaseInterval)
	collector.activityLevel.Store(ActivityLevelLow)
	collector.burstModeUntil.Store(time.Time{})
	collector.stats.Store(CollectorStats{})

	return collector, nil
}

// Collect performs a collection cycle with timing analysis
func (c *PrecisionTimingCollector) Collect(ctx context.Context, endpoint string) (*EnhancedMetrics, error) {
	if c.stopped.Load() {
		return nil, ErrCollectorStopped
	}

	startTime := time.Now()
	c.metricsCollector.RecordCollection()

	// Get PHP-FPM status
	status, err := c.phpfpmClient.GetStatus(ctx, endpoint)
	if err != nil {
		c.metricsCollector.RecordError("phpfpm_status")
		return nil, fmt.Errorf("failed to get PHP-FPM status: %w", err)
	}

	// Process worker metrics
	workerMetrics, err := c.phpfpmClient.ProcessWorkerMetrics(status)
	if err != nil {
		c.metricsCollector.RecordError("process_metrics")
		return nil, fmt.Errorf("failed to process worker metrics: %w", err)
	}

	// Perform timing analysis
	timingAnalysis := c.analyzeTimingWithLock(status.Processes, startTime)

	// Update activity level and polling interval
	if c.config.AdaptiveEnabled {
		c.updateAdaptivePolling(workerMetrics, timingAnalysis)
	}

	// Build enhanced metrics
	enhancedMetrics := &EnhancedMetrics{
		WorkerMetrics:    workerMetrics,
		TimingAnalysis:   timingAnalysis,
		CollectorMetrics: c.metricsCollector.GetMetrics(),
		Timestamp:        startTime,
	}

	// Update statistics
	c.updateStats(time.Since(startTime))

	return enhancedMetrics, nil
}

// analyzeTimingWithLock performs timing analysis with proper locking
func (c *PrecisionTimingCollector) analyzeTimingWithLock(processes []PHPFPMProcess, pollTime time.Time) *TimingAnalysis {
	c.mu.Lock()
	defer c.mu.Unlock()

	analysis := &TimingAnalysis{
		PollTime:    pollTime,
		Events:      make([]*TimingEvent, 0),
		Transitions: make([]*StateTransition, 0),
		Confidence:  0.0,
	}

	currentPIDs := make(map[int]bool)

	for _, process := range processes {
		currentPIDs[process.PID] = true

		// Get or create state record
		state, exists := c.processStates[process.PID]
		if !exists {
			state = c.statePool.Get().(*ProcessStateRecord)
			state.Reset()
			state.PID = process.PID
			c.processStates[process.PID] = state
		}

		// Analyze state changes and timing
		events := c.detectTimingEvents(state, process, pollTime)
		analysis.Events = append(analysis.Events, events...)

		// Update state record
		state.Update(process, pollTime)

		// Record state in history
		c.stateHistory.Add(state.Clone())
	}

	// Clean up terminated processes
	c.cleanupTerminatedProcesses(currentPIDs)

	// Calculate overall confidence
	analysis.Confidence = c.calculateAnalysisConfidence(analysis)

	return analysis
}

// detectTimingEvents identifies timing events from state changes
func (c *PrecisionTimingCollector) detectTimingEvents(
	state *ProcessStateRecord,
	process PHPFPMProcess,
	pollTime time.Time,
) []*TimingEvent {
	if !state.HasPrevious {
		state.HasPrevious = true
		return nil
	}

	var events []*TimingEvent

	// Detect state transitions
	if c.config.TransitionDetectionEnabled {
		if transition := c.detectStateTransition(state, process); transition != nil {
			event := c.eventPool.Get().(*TimingEvent)
			event.Reset()
			event.PID = process.PID
			event.Type = EventTypeStateChange
			event.Timestamp = c.estimateEventTime(state.LastPollTime, pollTime, 0.5)
			event.Confidence = c.calculateEventConfidence(pollTime.Sub(state.LastPollTime))
			event.Details = map[string]interface{}{
				"from_state": state.LastState,
				"to_state":   process.State,
			}
			events = append(events, event)
		}
	}

	// Detect request boundaries
	if c.isRequestBoundary(state, process) {
		// Previous request ended
		endEvent := c.eventPool.Get().(*TimingEvent)
		endEvent.Reset()
		endEvent.PID = process.PID
		endEvent.Type = EventTypeRequestEnd
		endEvent.Timestamp = c.estimateEventTime(state.LastPollTime, pollTime, 0.7)
		endEvent.Confidence = c.calculateEventConfidence(pollTime.Sub(state.LastPollTime))
		endEvent.Duration = time.Duration(state.LastRequestDuration) * time.Millisecond
		events = append(events, endEvent)

		// New request started
		startEvent := c.eventPool.Get().(*TimingEvent)
		startEvent.Reset()
		startEvent.PID = process.PID
		startEvent.Type = EventTypeRequestStart
		startEvent.Timestamp = c.estimateEventTime(state.LastPollTime, pollTime, 0.8)
		startEvent.Confidence = c.calculateEventConfidence(pollTime.Sub(state.LastPollTime))
		startEvent.RequestURI = process.RequestURI
		events = append(events, startEvent)
	}

	// Record events in history
	for _, event := range events {
		c.eventHistory.Add(event.Clone())
	}

	return events
}

// Helper methods

func (c *PrecisionTimingCollector) detectStateTransition(state *ProcessStateRecord, process PHPFPMProcess) *StateTransition {
	if state.LastState == process.State {
		return nil
	}

	// Check if this is a significant transition
	significant := false
	switch {
	case state.LastState == ProcessStateIdle && process.State == ProcessStateRunning:
		significant = true // Request start
	case state.LastState == ProcessStateRunning && process.State == ProcessStateIdle:
		significant = true // Request end
	case state.LastState == ProcessStateRunning && process.State == ProcessStateFinishing:
		significant = true // Request finishing
	}

	if !significant {
		return nil
	}

	return &StateTransition{
		PID:       process.PID,
		FromState: state.LastState,
		ToState:   process.State,
	}
}

func (c *PrecisionTimingCollector) isRequestBoundary(state *ProcessStateRecord, process PHPFPMProcess) bool {
	// Request duration decreased = new request started
	return process.RequestDuration < state.LastRequestDuration &&
		state.LastRequestDuration > 50 // Ignore very short requests
}

func (c *PrecisionTimingCollector) estimateEventTime(lastPoll, currentPoll time.Time, fraction float64) time.Time {
	interval := currentPoll.Sub(lastPoll)
	return lastPoll.Add(time.Duration(float64(interval) * fraction))
}

func (c *PrecisionTimingCollector) calculateEventConfidence(pollInterval time.Duration) float64 {
	switch {
	case pollInterval <= 20*time.Millisecond:
		return 0.95
	case pollInterval <= 50*time.Millisecond:
		return 0.85
	case pollInterval <= 100*time.Millisecond:
		return 0.75
	case pollInterval <= 200*time.Millisecond:
		return 0.65
	case pollInterval <= 500*time.Millisecond:
		return 0.55
	default:
		return 0.40
	}
}

func (c *PrecisionTimingCollector) calculateAnalysisConfidence(analysis *TimingAnalysis) float64 {
	if len(analysis.Events) == 0 {
		return 1.0 // No events = stable state, high confidence
	}

	totalConfidence := 0.0
	for _, event := range analysis.Events {
		totalConfidence += event.Confidence
	}

	return totalConfidence / float64(len(analysis.Events))
}

func (c *PrecisionTimingCollector) cleanupTerminatedProcesses(currentPIDs map[int]bool) {
	for pid := range c.processStates {
		if !currentPIDs[pid] {
			if state, exists := c.processStates[pid]; exists {
				c.statePool.Put(state)
				delete(c.processStates, pid)
			}
		}
	}
}

func (c *PrecisionTimingCollector) updateAdaptivePolling(metrics *WorkerProcessMetrics, analysis *TimingAnalysis) {
	// Calculate new activity level
	activityLevel := c.calculateActivityLevel(metrics)
	c.activityLevel.Store(activityLevel)

	// Check for burst mode trigger
	if c.shouldEnterBurstMode(analysis, activityLevel) {
		c.enterBurstMode()
		return
	}

	// Check if still in burst mode
	if time.Now().Before(c.burstModeUntil.Load().(time.Time)) {
		return // Stay in burst mode
	}

	// Calculate optimal interval
	newInterval := c.calculateOptimalInterval(activityLevel, metrics, analysis)

	// Apply smoothing
	currentInterval := c.currentInterval.Load().(time.Duration)
	smoothedInterval := c.smoothIntervalTransition(currentInterval, newInterval)

	c.currentInterval.Store(smoothedInterval)
}

func (c *PrecisionTimingCollector) calculateActivityLevel(metrics *WorkerProcessMetrics) ActivityLevel {
	if metrics.TotalWorkers == 0 {
		return ActivityLevelIdle
	}

	utilization := float64(metrics.ActiveWorkers) / float64(metrics.TotalWorkers)
	queuePressure := float64(metrics.QueueDepth) / float64(metrics.TotalWorkers)

	score := (utilization*0.7 + queuePressure*0.3)

	switch {
	case score >= 0.9:
		return ActivityLevelCritical
	case score >= 0.7:
		return ActivityLevelHigh
	case score >= 0.4:
		return ActivityLevelMedium
	case score >= 0.1:
		return ActivityLevelLow
	default:
		return ActivityLevelIdle
	}
}

func (c *PrecisionTimingCollector) shouldEnterBurstMode(analysis *TimingAnalysis, activity ActivityLevel) bool {
	// Multiple events detected
	if len(analysis.Events) >= 3 {
		return true
	}

	// Sudden activity increase
	previousActivity := c.activityLevel.Load().(ActivityLevel)
	if activity > previousActivity && activity >= ActivityLevelHigh {
		return true
	}

	// Low confidence events
	lowConfidenceCount := 0
	for _, event := range analysis.Events {
		if event.Confidence < c.config.ConfidenceThreshold {
			lowConfidenceCount++
		}
	}

	return lowConfidenceCount >= 2
}

func (c *PrecisionTimingCollector) enterBurstMode() {
	c.currentInterval.Store(c.config.MinInterval)
	c.burstModeUntil.Store(time.Now().Add(c.config.BurstDuration))

	c.logger.Info("Entering burst polling mode",
		zap.Duration("interval", c.config.MinInterval),
		zap.Duration("duration", c.config.BurstDuration))
}

func (c *PrecisionTimingCollector) calculateOptimalInterval(
	activity ActivityLevel,
	metrics *WorkerProcessMetrics,
	analysis *TimingAnalysis,
) time.Duration {
	baseInterval := c.config.BaseInterval

	// Activity-based multiplier
	var multiplier float64
	switch activity {
	case ActivityLevelCritical:
		multiplier = 0.1 // 10x faster
	case ActivityLevelHigh:
		multiplier = 0.25 // 4x faster
	case ActivityLevelMedium:
		multiplier = 0.5 // 2x faster
	case ActivityLevelLow:
		multiplier = 1.0 // Normal
	default:
		multiplier = 2.0 // 2x slower
	}

	// Queue pressure adjustment
	if metrics.QueueDepth > 10 {
		multiplier *= 0.5 // Double speed
	}

	// Confidence adjustment
	if analysis.Confidence < c.config.ConfidenceThreshold {
		multiplier *= 0.75 // Speed up for better confidence
	}

	interval := time.Duration(float64(baseInterval) * multiplier)

	// Apply bounds
	if interval < c.config.MinInterval {
		return c.config.MinInterval
	}
	if interval > c.config.MaxInterval {
		return c.config.MaxInterval
	}

	return interval
}

func (c *PrecisionTimingCollector) smoothIntervalTransition(current, target time.Duration) time.Duration {
	// Limit rate of change to prevent oscillation
	maxChangeRate := 0.25 // 25% max change per adjustment

	diff := float64(target - current)
	maxChange := float64(current) * maxChangeRate

	if diff > maxChange {
		return current + time.Duration(maxChange)
	} else if diff < -maxChange {
		return current - time.Duration(maxChange)
	}

	return target
}

func (c *PrecisionTimingCollector) updateStats(collectionDuration time.Duration) {
	stats := c.stats.Load().(CollectorStats)
	stats.TotalCollections++
	stats.LastCollectionTime = time.Now()
	stats.LastCollectionDuration = collectionDuration

	// Update average duration
	if stats.AverageCollectionDuration == 0 {
		stats.AverageCollectionDuration = collectionDuration
	} else {
		stats.AverageCollectionDuration = (stats.AverageCollectionDuration + collectionDuration) / 2
	}

	c.stats.Store(stats)
}

// Interface implementation methods

func (c *PrecisionTimingCollector) GetCurrentInterval() time.Duration {
	return c.currentInterval.Load().(time.Duration)
}

func (c *PrecisionTimingCollector) SetInterval(interval time.Duration) error {
	if interval < c.config.MinInterval || interval > c.config.MaxInterval {
		return fmt.Errorf("%w: interval %v outside bounds [%v, %v]",
			ErrInvalidInterval, interval, c.config.MinInterval, c.config.MaxInterval)
	}

	c.currentInterval.Store(interval)
	c.logger.Info("Polling interval updated", zap.Duration("interval", interval))

	return nil
}

func (c *PrecisionTimingCollector) Start(ctx context.Context) error {
	if !c.started.CompareAndSwap(false, true) {
		return errors.New("collector already started")
	}

	c.logger.Info("Starting precision timing collector",
		zap.Duration("base_interval", c.config.BaseInterval),
		zap.Bool("adaptive", c.config.AdaptiveEnabled))

	return nil
}

func (c *PrecisionTimingCollector) Stop() error {
	if !c.stopped.CompareAndSwap(false, true) {
		return errors.New("collector already stopped")
	}

	c.cancel()

	c.logger.Info("Stopped precision timing collector",
		zap.Int64("total_collections", c.stats.Load().(CollectorStats).TotalCollections))

	return nil
}

func (c *PrecisionTimingCollector) Stats() CollectorStats {
	return c.stats.Load().(CollectorStats)
}
