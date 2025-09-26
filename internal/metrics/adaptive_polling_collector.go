package metrics

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// AdaptivePollingCollector combines precise timing with adaptive polling frequency
type AdaptivePollingCollector struct {
	logger          *zap.Logger
	phpfpmClient    *PHPFPMClient
	timingCollector *PreciseTimingCollector

	// Polling configuration
	baseInterval    time.Duration
	currentInterval time.Duration
	minInterval     time.Duration
	maxInterval     time.Duration

	// Adaptive state
	lastActivityLevel ActivityLevel
	burstModeUntil    time.Time
	intervalHistory   []time.Duration

	// Coordination
	ctx    context.Context
	cancel context.CancelFunc
	mutex  sync.RWMutex
}

// NewAdaptivePollingCollector creates a collector that adapts polling frequency
func NewAdaptivePollingCollector(phpfpmClient *PHPFPMClient, logger *zap.Logger) *AdaptivePollingCollector {
	ctx, cancel := context.WithCancel(context.Background())

	return &AdaptivePollingCollector{
		logger:            logger.Named("adaptive-polling"),
		phpfpmClient:      phpfpmClient,
		timingCollector:   NewPreciseTimingCollector(logger),
		baseInterval:      200 * time.Millisecond, // Default base interval
		currentInterval:   200 * time.Millisecond,
		minInterval:       10 * time.Millisecond, // Minimum for burst mode
		maxInterval:       2 * time.Second,       // Maximum for idle
		lastActivityLevel: ActivityLevelLow,
		intervalHistory:   make([]time.Duration, 0, 10),
		ctx:               ctx,
		cancel:            cancel,
	}
}

// CollectWithAdaptivePolling performs intelligent polling with timing optimization
func (c *AdaptivePollingCollector) CollectWithAdaptivePolling(endpoint string) (*EnhancedWorkerMetrics, error) {
	pollTime := time.Now()

	// Get PHP-FPM status
	status, err := c.phpfpmClient.GetStatus(c.ctx, endpoint)
	if err != nil {
		return nil, err
	}

	// Process worker metrics
	workerMetrics, err := c.phpfpmClient.ProcessWorkerMetrics(status)
	if err != nil {
		return nil, err
	}

	// Detect state transitions for timing improvement
	transitions := c.timingCollector.DetectStateTransitions(status.Processes, pollTime)

	// Calculate activity level
	activityLevel := c.timingCollector.CalculateActivityLevel(workerMetrics)

	// Adjust polling frequency based on activity and transitions
	c.adjustPollingFrequency(activityLevel, transitions, workerMetrics)

	// Create enhanced metrics with timing information
	enhancedMetrics := &EnhancedWorkerMetrics{
		WorkerProcessMetrics:   *workerMetrics,
		StateTransitions:       transitions,
		ActivityLevel:          activityLevel,
		CurrentPollingInterval: c.currentInterval,
		TimingConfidence:       c.calculateOverallConfidence(transitions),
		PollTime:               pollTime,
	}

	c.logger.Debug("Adaptive polling metrics",
		zap.Duration("current_interval", c.currentInterval),
		zap.String("activity_level", activityLevel.String()),
		zap.Int("transitions", len(transitions)),
		zap.Float64("timing_confidence", enhancedMetrics.TimingConfidence))

	return enhancedMetrics, nil
}

// adjustPollingFrequency dynamically adjusts polling interval
func (c *AdaptivePollingCollector) adjustPollingFrequency(
	activityLevel ActivityLevel,
	transitions []ProcessStateTransition,
	metrics *WorkerProcessMetrics,
) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	now := time.Now()

	// Check if we're in burst mode
	if now.Before(c.burstModeUntil) {
		// Stay in burst mode
		return
	}

	// Determine if we should enter burst mode
	if c.shouldEnterBurstMode(transitions, activityLevel) {
		c.enterBurstMode(now)
		return
	}

	// Normal adaptive polling adjustment
	newInterval := c.timingCollector.GetRecommendedPollingInterval(c.baseInterval, activityLevel)

	// Apply smoothing to avoid rapid changes
	c.currentInterval = c.smoothIntervalChange(c.currentInterval, newInterval)

	// Record interval history
	c.recordIntervalHistory(c.currentInterval)
	c.lastActivityLevel = activityLevel
}

// shouldEnterBurstMode determines if high-frequency polling is needed
func (c *AdaptivePollingCollector) shouldEnterBurstMode(transitions []ProcessStateTransition, activity ActivityLevel) bool {
	// Enter burst mode if:
	// 1. Multiple transitions detected
	// 2. Activity level suddenly increased
	// 3. Low confidence transitions need better timing

	if len(transitions) >= 2 {
		return true
	}

	if activity > c.lastActivityLevel && activity >= ActivityLevelHigh {
		return true
	}

	// Check for low confidence transitions
	lowConfidenceCount := 0
	for _, transition := range transitions {
		if transition.Confidence < 0.6 {
			lowConfidenceCount++
		}
	}

	return lowConfidenceCount > 0
}

// enterBurstMode activates high-frequency polling temporarily
func (c *AdaptivePollingCollector) enterBurstMode(startTime time.Time) {
	c.currentInterval = c.minInterval
	c.burstModeUntil = startTime.Add(2 * time.Second) // Burst for 2 seconds

	c.logger.Info("Entering burst polling mode",
		zap.Duration("interval", c.currentInterval),
		zap.Time("until", c.burstModeUntil))
}

// smoothIntervalChange prevents rapid polling frequency changes
func (c *AdaptivePollingCollector) smoothIntervalChange(current, target time.Duration) time.Duration {
	// Limit change rate to prevent oscillation
	maxChange := current / 4 // Max 25% change per adjustment

	if target > current {
		// Increasing interval (slower polling)
		increase := target - current
		if increase > maxChange {
			return current + maxChange
		}
	} else {
		// Decreasing interval (faster polling)
		decrease := current - target
		if decrease > maxChange {
			return current - maxChange
		}
	}

	return target
}

// recordIntervalHistory tracks polling interval changes
func (c *AdaptivePollingCollector) recordIntervalHistory(interval time.Duration) {
	c.intervalHistory = append(c.intervalHistory, interval)
	if len(c.intervalHistory) > 10 {
		c.intervalHistory = c.intervalHistory[1:]
	}
}

// calculateOverallConfidence determines timing confidence across all transitions
func (c *AdaptivePollingCollector) calculateOverallConfidence(transitions []ProcessStateTransition) float64 {
	if len(transitions) == 0 {
		return 1.0 // No transitions = perfect confidence in no-change state
	}

	totalConfidence := 0.0
	for _, transition := range transitions {
		totalConfidence += transition.Confidence
	}

	return totalConfidence / float64(len(transitions))
}

// GetCurrentInterval returns the current polling interval
func (c *AdaptivePollingCollector) GetCurrentInterval() time.Duration {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.currentInterval
}

// Stop gracefully stops the adaptive polling collector
func (c *AdaptivePollingCollector) Stop() {
	c.cancel()
}

// EnhancedWorkerMetrics extends WorkerProcessMetrics with timing information
type EnhancedWorkerMetrics struct {
	WorkerProcessMetrics
	StateTransitions       []ProcessStateTransition `json:"state_transitions"`
	ActivityLevel          ActivityLevel            `json:"activity_level"`
	CurrentPollingInterval time.Duration            `json:"current_polling_interval"`
	TimingConfidence       float64                  `json:"timing_confidence"`
	PollTime               time.Time                `json:"poll_time"`
}

// String representation of ActivityLevel
// String method for ActivityLevel is defined in interval_manager.go
