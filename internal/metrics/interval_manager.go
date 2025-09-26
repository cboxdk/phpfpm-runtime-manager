package metrics

import (
	"math"
	"time"

	"go.uber.org/zap"
)

// ActivityLevel represents the current system activity level
type ActivityLevel int

const (
	ActivityLevelIdle ActivityLevel = iota
	ActivityLevelLow
	ActivityLevelMedium
	ActivityLevelHigh
	ActivityLevelCritical
)

// String returns the string representation of ActivityLevel
func (a ActivityLevel) String() string {
	switch a {
	case ActivityLevelIdle:
		return "idle"
	case ActivityLevelLow:
		return "low"
	case ActivityLevelMedium:
		return "medium"
	case ActivityLevelHigh:
		return "high"
	case ActivityLevelCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// IntervalManagerConfig configures adaptive interval management
type IntervalManagerConfig struct {
	BaseInterval      time.Duration `json:"base_interval"`
	MinInterval       time.Duration `json:"min_interval"`
	MaxInterval       time.Duration `json:"max_interval"`
	BurstDuration     time.Duration `json:"burst_duration"`
	SmoothingFactor   float64       `json:"smoothing_factor"`
	ActivityThreshold float64       `json:"activity_threshold"`
}

// IntervalManager manages adaptive polling intervals based on system activity
type IntervalManager struct {
	config IntervalManagerConfig
	logger *zap.Logger

	// State tracking
	lastActivity   ActivityLevel
	lastInterval   time.Duration
	lastEventCount int
	adaptationRate float64
}

// NewIntervalManager creates a new interval manager
func NewIntervalManager(config IntervalManagerConfig, logger *zap.Logger) *IntervalManager {
	return &IntervalManager{
		config:         config,
		logger:         logger,
		lastActivity:   ActivityLevelLow,
		lastInterval:   config.BaseInterval,
		adaptationRate: 1.0,
	}
}

// CalculateInterval determines optimal polling interval based on current metrics
func (im *IntervalManager) CalculateInterval(
	metrics *WorkerProcessMetrics,
	analysis *TimingAnalysis,
	currentActivity ActivityLevel,
) time.Duration {
	// Calculate activity level from metrics
	activityLevel := im.calculateActivityLevel(metrics)

	// Determine base interval for activity level
	baseInterval := im.getIntervalForActivity(activityLevel)

	// Adjust based on event density
	eventAdjustment := im.calculateEventAdjustment(analysis)

	// Apply smoothing
	targetInterval := time.Duration(float64(baseInterval) * eventAdjustment)
	smoothedInterval := im.applySmoothingToInterval(im.lastInterval, targetInterval)

	// Apply bounds
	finalInterval := im.applyBounds(smoothedInterval)

	// Update state
	im.lastActivity = activityLevel
	im.lastInterval = finalInterval
	im.lastEventCount = len(analysis.Events)

	im.logger.Debug("Interval calculated",
		zap.String("activity", activityLevel.String()),
		zap.Duration("base", baseInterval),
		zap.Float64("event_adjustment", eventAdjustment),
		zap.Duration("final", finalInterval))

	return finalInterval
}

// calculateActivityLevel determines activity level from worker metrics
func (im *IntervalManager) calculateActivityLevel(metrics *WorkerProcessMetrics) ActivityLevel {
	if metrics.TotalWorkers == 0 {
		return ActivityLevelIdle
	}

	// Calculate utilization ratio
	utilization := float64(metrics.ActiveWorkers) / float64(metrics.TotalWorkers)

	// Factor in queue depth
	queuePressure := 0.0
	if metrics.QueueDepth > 0 {
		// Normalize queue depth (assume max reasonable queue is 100)
		queuePressure = math.Min(float64(metrics.QueueDepth)/100.0, 1.0)
	}

	// Combined activity score
	activityScore := (utilization * 0.7) + (queuePressure * 0.3)

	// Map to activity levels
	switch {
	case activityScore >= 0.9:
		return ActivityLevelCritical
	case activityScore >= 0.7:
		return ActivityLevelHigh
	case activityScore >= 0.4:
		return ActivityLevelMedium
	case activityScore >= 0.1:
		return ActivityLevelLow
	default:
		return ActivityLevelIdle
	}
}

// getIntervalForActivity returns base interval for activity level
func (im *IntervalManager) getIntervalForActivity(level ActivityLevel) time.Duration {
	switch level {
	case ActivityLevelCritical:
		return im.config.MinInterval
	case ActivityLevelHigh:
		return im.config.MinInterval * 2
	case ActivityLevelMedium:
		return im.config.BaseInterval
	case ActivityLevelLow:
		return im.config.BaseInterval * 2
	case ActivityLevelIdle:
		return im.config.MaxInterval
	default:
		return im.config.BaseInterval
	}
}

// calculateEventAdjustment adjusts interval based on event density
func (im *IntervalManager) calculateEventAdjustment(analysis *TimingAnalysis) float64 {
	eventCount := len(analysis.Events)

	// No events - can increase interval
	if eventCount == 0 {
		return 1.5 // Increase interval by 50%
	}

	// High event density - decrease interval
	if eventCount >= 5 {
		return 0.5 // Decrease interval by 50%
	}

	// Medium event density - slight decrease
	if eventCount >= 3 {
		return 0.75 // Decrease interval by 25%
	}

	// Low event density - keep current
	return 1.0
}

// applySmoothingToInterval applies exponential smoothing to interval changes
func (im *IntervalManager) applySmoothingToInterval(current, target time.Duration) time.Duration {
	if current == 0 {
		return target
	}

	// Calculate smoothed value
	alpha := im.config.SmoothingFactor
	smoothed := float64(current)*(1-alpha) + float64(target)*alpha

	return time.Duration(smoothed)
}

// applyBounds ensures interval stays within configured bounds
func (im *IntervalManager) applyBounds(interval time.Duration) time.Duration {
	if interval < im.config.MinInterval {
		return im.config.MinInterval
	}
	if interval > im.config.MaxInterval {
		return im.config.MaxInterval
	}
	return interval
}

// ShouldEnterBurstMode determines if burst mode should be activated
func (im *IntervalManager) ShouldEnterBurstMode(
	analysis *TimingAnalysis,
	metrics *WorkerProcessMetrics,
) bool {
	// High event density
	if len(analysis.Events) >= 5 {
		return true
	}

	// Critical activity level
	activityLevel := im.calculateActivityLevel(metrics)
	if activityLevel == ActivityLevelCritical {
		return true
	}

	// Queue backup with slow processing
	if metrics.QueueDepth > 50 && metrics.ProcessingVelocity < 1.0 {
		return true
	}

	// Low confidence events (need more precision)
	lowConfidenceCount := 0
	for _, event := range analysis.Events {
		if event.Confidence < 0.6 {
			lowConfidenceCount++
		}
	}

	return lowConfidenceCount >= 2
}

// GetBurstInterval returns the interval to use during burst mode
func (im *IntervalManager) GetBurstInterval() time.Duration {
	return im.config.MinInterval
}

// GetBurstDuration returns how long burst mode should last
func (im *IntervalManager) GetBurstDuration() time.Duration {
	return im.config.BurstDuration
}

// AdaptToActivity adjusts adaptation rate based on activity changes
func (im *IntervalManager) AdaptToActivity(currentActivity ActivityLevel) {
	// Increase adaptation rate if activity is changing rapidly
	if currentActivity != im.lastActivity {
		im.adaptationRate = math.Min(im.adaptationRate*1.1, 2.0)
	} else {
		// Decrease adaptation rate for stability
		im.adaptationRate = math.Max(im.adaptationRate*0.95, 0.5)
	}
}

// PredictOptimalInterval predicts optimal interval for upcoming period
func (im *IntervalManager) PredictOptimalInterval(
	recentMetrics []*WorkerProcessMetrics,
	recentAnalyses []*TimingAnalysis,
) time.Duration {
	if len(recentMetrics) == 0 {
		return im.config.BaseInterval
	}

	// Calculate trend in activity
	activityTrend := im.calculateActivityTrend(recentMetrics)

	// Calculate event density trend
	eventTrend := im.calculateEventTrend(recentAnalyses)

	// Predict next interval based on trends
	baseInterval := im.getIntervalForActivity(im.lastActivity)

	// Apply trend adjustments
	trendAdjustment := 1.0
	if activityTrend > 0.1 { // Increasing activity
		trendAdjustment *= 0.8 // Decrease interval
	} else if activityTrend < -0.1 { // Decreasing activity
		trendAdjustment *= 1.2 // Increase interval
	}

	if eventTrend > 0.1 { // Increasing events
		trendAdjustment *= 0.9 // Decrease interval
	}

	predictedInterval := time.Duration(float64(baseInterval) * trendAdjustment)

	return im.applyBounds(predictedInterval)
}

// calculateActivityTrend calculates the trend in activity levels
func (im *IntervalManager) calculateActivityTrend(metrics []*WorkerProcessMetrics) float64 {
	if len(metrics) < 2 {
		return 0.0
	}

	// Simple linear trend calculation
	n := len(metrics)
	activities := make([]float64, n)

	for i, m := range metrics {
		if m.TotalWorkers > 0 {
			activities[i] = float64(m.ActiveWorkers) / float64(m.TotalWorkers)
		}
	}

	// Calculate slope
	first := activities[0]
	last := activities[n-1]

	return (last - first) / float64(n-1)
}

// calculateEventTrend calculates the trend in event density
func (im *IntervalManager) calculateEventTrend(analyses []*TimingAnalysis) float64 {
	if len(analyses) < 2 {
		return 0.0
	}

	n := len(analyses)
	first := float64(len(analyses[0].Events))
	last := float64(len(analyses[n-1].Events))

	return (last - first) / float64(n-1)
}

// GetStats returns manager statistics
func (im *IntervalManager) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"last_activity":      im.lastActivity.String(),
		"last_interval":      im.lastInterval,
		"last_event_count":   im.lastEventCount,
		"adaptation_rate":    im.adaptationRate,
		"min_interval":       im.config.MinInterval,
		"max_interval":       im.config.MaxInterval,
		"base_interval":      im.config.BaseInterval,
		"smoothing_factor":   im.config.SmoothingFactor,
		"activity_threshold": im.config.ActivityThreshold,
	}
}

// Reset resets the manager to initial state
func (im *IntervalManager) Reset() {
	im.lastActivity = ActivityLevelLow
	im.lastInterval = im.config.BaseInterval
	im.lastEventCount = 0
	im.adaptationRate = 1.0

	im.logger.Info("Interval manager reset")
}
