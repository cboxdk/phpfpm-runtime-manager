package metrics

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// ProcessStateTransition tracks timing of PHP-FPM process state changes
type ProcessStateTransition struct {
	PID                     int
	PreviousState           PHPFPMProcessState
	CurrentState            PHPFPMProcessState
	PreviousRequestDuration int64
	CurrentRequestDuration  int64
	TransitionTime          time.Time // Estimated transition time
	PollInterval            time.Duration
	Confidence              float64 // 0.0-1.0 confidence in timing accuracy
}

// PreciseTimingCollector improves timing accuracy through state transition detection
type PreciseTimingCollector struct {
	logger                *zap.Logger
	lastProcessStates     map[int]*ProcessStateSnapshot
	stateTransitions      []ProcessStateTransition
	adaptivePollingActive bool
	lastPollingAdjustment time.Time
	mutex                 sync.RWMutex
}

// ProcessStateSnapshot captures process state at a specific time
type ProcessStateSnapshot struct {
	PID             int
	State           PHPFPMProcessState
	RequestDuration int64
	Timestamp       time.Time
	RequestURI      string
	RequestMethod   string
}

// NewPreciseTimingCollector creates a collector focused on timing accuracy
func NewPreciseTimingCollector(logger *zap.Logger) *PreciseTimingCollector {
	return &PreciseTimingCollector{
		logger:            logger.Named("precise-timing"),
		lastProcessStates: make(map[int]*ProcessStateSnapshot),
		stateTransitions:  make([]ProcessStateTransition, 0, 100),
	}
}

// DetectStateTransitions analyzes process state changes to improve timing accuracy
func (c *PreciseTimingCollector) DetectStateTransitions(processes []PHPFPMProcess, pollTime time.Time) []ProcessStateTransition {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	var transitions []ProcessStateTransition

	for _, process := range processes {
		lastState, exists := c.lastProcessStates[process.PID]

		// Create current snapshot
		currentSnapshot := &ProcessStateSnapshot{
			PID:             process.PID,
			State:           process.State,
			RequestDuration: process.RequestDuration,
			Timestamp:       pollTime,
			RequestURI:      process.RequestURI,
			RequestMethod:   process.RequestMethod,
		}

		if exists {
			// Detect state transitions
			if c.isSignificantTransition(lastState.State, process.State) {
				transition := c.calculateTransitionTiming(lastState, currentSnapshot)
				transitions = append(transitions, transition)

				c.logger.Debug("Detected state transition",
					zap.Int("pid", process.PID),
					zap.String("from_state", string(lastState.State)),
					zap.String("to_state", string(process.State)),
					zap.Time("estimated_time", transition.TransitionTime),
					zap.Float64("confidence", transition.Confidence))
			}

			// Detect request boundaries via RequestDuration changes
			if c.isNewRequestDetected(lastState, currentSnapshot) {
				requestTransition := c.calculateRequestBoundary(lastState, currentSnapshot)
				transitions = append(transitions, requestTransition)
			}
		}

		// Update state tracking
		c.lastProcessStates[process.PID] = currentSnapshot
	}

	// Clean up old states for processes that no longer exist
	c.cleanupOldStates(processes)

	return transitions
}

// isSignificantTransition determines if a state change should trigger timing analysis
func (c *PreciseTimingCollector) isSignificantTransition(from, to PHPFPMProcessState) bool {
	significantTransitions := map[PHPFPMProcessState]map[PHPFPMProcessState]bool{
		ProcessStateIdle: {
			ProcessStateRunning: true,
			ProcessStateReading: true,
		},
		ProcessStateRunning: {
			ProcessStateIdle:      true,
			ProcessStateFinishing: true,
		},
		ProcessStateFinishing: {
			ProcessStateIdle: true,
		},
	}

	if fromMap, exists := significantTransitions[from]; exists {
		return fromMap[to]
	}
	return false
}

// isNewRequestDetected identifies new request start via RequestDuration reset
func (c *PreciseTimingCollector) isNewRequestDetected(last, current *ProcessStateSnapshot) bool {
	// RequestDuration resets to 0 or small value indicates new request
	return current.RequestDuration < last.RequestDuration ||
		(current.RequestDuration < 10 && last.RequestDuration > 100) // Reset detected
}

// calculateTransitionTiming estimates when the state transition occurred
func (c *PreciseTimingCollector) calculateTransitionTiming(last, current *ProcessStateSnapshot) ProcessStateTransition {
	// Estimate transition occurred at midpoint between polls
	pollInterval := current.Timestamp.Sub(last.Timestamp)
	estimatedTransitionTime := last.Timestamp.Add(pollInterval / 2)

	// Calculate confidence based on poll interval
	confidence := c.calculateTimingConfidence(pollInterval)

	return ProcessStateTransition{
		PID:                     current.PID,
		PreviousState:           last.State,
		CurrentState:            current.State,
		PreviousRequestDuration: last.RequestDuration,
		CurrentRequestDuration:  current.RequestDuration,
		TransitionTime:          estimatedTransitionTime,
		PollInterval:            pollInterval,
		Confidence:              confidence,
	}
}

// calculateRequestBoundary identifies request start/end timing
func (c *PreciseTimingCollector) calculateRequestBoundary(last, current *ProcessStateSnapshot) ProcessStateTransition {
	pollInterval := current.Timestamp.Sub(last.Timestamp)

	// New request started (RequestDuration reset)
	estimatedTime := last.Timestamp.Add(pollInterval * 3 / 4) // Assume reset closer to current poll
	confidence := c.calculateTimingConfidence(pollInterval)

	return ProcessStateTransition{
		PID:                     current.PID,
		PreviousState:           last.State,
		CurrentState:            current.State,
		PreviousRequestDuration: last.RequestDuration,
		CurrentRequestDuration:  current.RequestDuration,
		TransitionTime:          estimatedTime,
		PollInterval:            pollInterval,
		Confidence:              confidence,
	}
}

// calculateTimingConfidence determines confidence level based on poll frequency
func (c *PreciseTimingCollector) calculateTimingConfidence(pollInterval time.Duration) float64 {
	switch {
	case pollInterval <= 10*time.Millisecond:
		return 0.95 // Very high confidence
	case pollInterval <= 50*time.Millisecond:
		return 0.85 // High confidence
	case pollInterval <= 100*time.Millisecond:
		return 0.75 // Good confidence
	case pollInterval <= 500*time.Millisecond:
		return 0.60 // Medium confidence
	default:
		return 0.40 // Low confidence
	}
}

// ShouldIncreasePollingFrequency determines if we should poll more frequently
func (c *PreciseTimingCollector) ShouldIncreasePollingFrequency(transitions []ProcessStateTransition, currentLoad float64) bool {
	// Increase frequency if:
	// 1. High number of recent transitions
	// 2. High system load
	// 3. Low confidence in recent timing measurements

	if len(transitions) > 3 {
		return true // Multiple transitions detected
	}

	if currentLoad > 0.7 {
		return true // High load, need better timing
	}

	// Check confidence of recent transitions
	lowConfidenceCount := 0
	for _, transition := range transitions {
		if transition.Confidence < 0.6 {
			lowConfidenceCount++
		}
	}

	return lowConfidenceCount > 1
}

// GetRecommendedPollingInterval suggests optimal polling frequency
func (c *PreciseTimingCollector) GetRecommendedPollingInterval(baseInterval time.Duration, activity ActivityLevel) time.Duration {
	switch activity {
	case ActivityLevelHigh:
		return baseInterval / 4 // 25% of base (e.g., 200ms -> 50ms)
	case ActivityLevelMedium:
		return baseInterval / 2 // 50% of base (e.g., 200ms -> 100ms)
	case ActivityLevelLow:
		return baseInterval // Keep base interval
	default:
		return baseInterval * 2 // Slower for very low activity
	}
}

// cleanupOldStates removes tracking data for processes that no longer exist
func (c *PreciseTimingCollector) cleanupOldStates(currentProcesses []PHPFPMProcess) {
	currentPIDs := make(map[int]bool)
	for _, process := range currentProcesses {
		currentPIDs[process.PID] = true
	}

	for pid := range c.lastProcessStates {
		if !currentPIDs[pid] {
			delete(c.lastProcessStates, pid)
		}
	}
}

// ActivityLevel is defined in interval_manager.go to avoid duplication

// CalculateActivityLevel determines current system activity
func (c *PreciseTimingCollector) CalculateActivityLevel(metrics *WorkerProcessMetrics) ActivityLevel {
	if metrics == nil {
		return ActivityLevelLow
	}

	// Calculate load factor
	utilizationRate := float64(metrics.ActiveWorkers) / float64(metrics.TotalWorkers)
	queuePressure := float64(metrics.QueueDepth) / float64(metrics.ActiveWorkers+1)

	combinedLoad := (utilizationRate + queuePressure/10) / 2

	switch {
	case combinedLoad >= 0.9:
		return ActivityLevelCritical
	case combinedLoad >= 0.7:
		return ActivityLevelHigh
	case combinedLoad >= 0.4:
		return ActivityLevelMedium
	case combinedLoad >= 0.1:
		return ActivityLevelLow
	default:
		return ActivityLevelIdle
	}
}
