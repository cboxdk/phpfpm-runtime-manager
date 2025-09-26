package metrics

import (
	"time"

	"go.uber.org/zap"
)

// EventDetectorConfig configures event detection behavior
type EventDetectorConfig struct {
	EnableStateTransitions  bool    `json:"enable_state_transitions"`
	EnableRequestBoundaries bool    `json:"enable_request_boundaries"`
	ConfidenceThreshold     float64 `json:"confidence_threshold"`
}

// EventDetector identifies timing events from process state changes
type EventDetector struct {
	config EventDetectorConfig
	logger *zap.Logger
}

// NewEventDetector creates a new event detector
func NewEventDetector(config EventDetectorConfig, logger *zap.Logger) *EventDetector {
	return &EventDetector{
		config: config,
		logger: logger,
	}
}

// DetectEvents analyzes process states and identifies timing events
func (ed *EventDetector) DetectEvents(
	processStates map[int]*ProcessStateRecord,
	currentProcesses []PHPFPMProcess,
	pollTime time.Time,
) []*TimingEvent {
	events := make([]*TimingEvent, 0)

	// Create process map for quick lookup
	processMap := make(map[int]PHPFPMProcess)
	for _, process := range currentProcesses {
		processMap[process.PID] = process
	}

	// Detect events for each process
	for pid, state := range processStates {
		currentProcess, exists := processMap[pid]
		if !exists {
			continue
		}

		// Detect state transition events
		if ed.config.EnableStateTransitions {
			if event := ed.detectStateTransition(state, currentProcess, pollTime); event != nil {
				events = append(events, event)
			}
		}

		// Detect request boundary events
		if ed.config.EnableRequestBoundaries {
			requestEvents := ed.detectRequestBoundaries(state, currentProcess, pollTime)
			events = append(events, requestEvents...)
		}
	}

	return events
}

// detectStateTransition identifies state change events
func (ed *EventDetector) detectStateTransition(
	state *ProcessStateRecord,
	currentProcess PHPFPMProcess,
	pollTime time.Time,
) *TimingEvent {
	// Skip if no previous state
	if !state.HasPrevious {
		return nil
	}

	// Check for state change
	if state.LastState == currentProcess.State {
		return nil
	}

	// Calculate confidence based on poll timing
	interval := pollTime.Sub(state.LastPollTime)
	confidence := ed.calculateStateTransitionConfidence(state.LastState, currentProcess.State, interval)

	// Skip low confidence events
	if confidence < ed.config.ConfidenceThreshold {
		return nil
	}

	event := &TimingEvent{
		PID:        currentProcess.PID,
		Type:       EventTypeStateChange,
		Timestamp:  pollTime,
		Confidence: confidence,
		Details: map[string]interface{}{
			"from_state": state.LastState.String(),
			"to_state":   currentProcess.State.String(),
			"interval":   interval,
		},
	}

	ed.logger.Debug("State transition event detected",
		zap.Int("pid", currentProcess.PID),
		zap.String("from", state.LastState.String()),
		zap.String("to", currentProcess.State.String()),
		zap.Float64("confidence", confidence))

	return event
}

// detectRequestBoundaries identifies request start/end events
func (ed *EventDetector) detectRequestBoundaries(
	state *ProcessStateRecord,
	currentProcess PHPFPMProcess,
	pollTime time.Time,
) []*TimingEvent {
	events := make([]*TimingEvent, 0)

	// Skip if no previous state
	if !state.HasPrevious {
		return events
	}

	// Check for request duration changes (indicates new request)
	if currentProcess.RequestDuration < state.LastRequestDuration && currentProcess.RequestDuration > 0 {
		// Request duration decreased - likely new request started
		if state.LastRequestDuration > 0 {
			// Previous request ended
			endEvent := ed.createRequestEndEvent(state, pollTime)
			if endEvent != nil {
				events = append(events, endEvent)
			}
		}

		// New request started
		startEvent := ed.createRequestStartEvent(currentProcess, pollTime)
		if startEvent != nil {
			events = append(events, startEvent)
		}
	} else if currentProcess.RequestDuration == 0 && state.LastRequestDuration > 0 {
		// Request completed (duration reset to 0)
		endEvent := ed.createRequestEndEvent(state, pollTime)
		if endEvent != nil {
			events = append(events, endEvent)
		}
	} else if currentProcess.RequestDuration > 0 && state.LastRequestDuration == 0 {
		// New request started (duration went from 0 to positive)
		startEvent := ed.createRequestStartEvent(currentProcess, pollTime)
		if startEvent != nil {
			events = append(events, startEvent)
		}
	}

	return events
}

// createRequestStartEvent creates a request start event
func (ed *EventDetector) createRequestStartEvent(
	process PHPFPMProcess,
	pollTime time.Time,
) *TimingEvent {
	// Estimate start time based on current duration
	estimatedStartTime := pollTime.Add(-time.Duration(process.RequestDuration) * time.Millisecond)

	event := &TimingEvent{
		PID:        process.PID,
		Type:       EventTypeRequestStart,
		Timestamp:  estimatedStartTime,
		Confidence: ed.calculateRequestBoundaryConfidence(process.RequestDuration),
		RequestURI: process.RequestURI,
		Details: map[string]interface{}{
			"request_duration_ms": process.RequestDuration,
			"poll_time":           pollTime,
			"state":               process.State.String(),
		},
	}

	ed.logger.Debug("Request start event detected",
		zap.Int("pid", process.PID),
		zap.String("uri", process.RequestURI),
		zap.Int64("duration", process.RequestDuration))

	return event
}

// createRequestEndEvent creates a request end event
func (ed *EventDetector) createRequestEndEvent(
	state *ProcessStateRecord,
	pollTime time.Time,
) *TimingEvent {
	// Use poll time as approximate end time
	duration := time.Duration(state.LastRequestDuration) * time.Millisecond

	event := &TimingEvent{
		PID:        state.PID,
		Type:       EventTypeRequestEnd,
		Timestamp:  pollTime,
		Confidence: ed.calculateRequestBoundaryConfidence(state.LastRequestDuration),
		Duration:   duration,
		RequestURI: state.LastRequestURI,
		Details: map[string]interface{}{
			"final_duration_ms": state.LastRequestDuration,
			"poll_time":         pollTime,
		},
	}

	ed.logger.Debug("Request end event detected",
		zap.Int("pid", state.PID),
		zap.String("uri", state.LastRequestURI),
		zap.Duration("duration", duration))

	return event
}

// calculateStateTransitionConfidence calculates confidence for state transitions
func (ed *EventDetector) calculateStateTransitionConfidence(
	fromState, toState PHPFPMProcessState,
	interval time.Duration,
) float64 {
	// Base confidence from polling interval
	var intervalConfidence float64
	switch {
	case interval <= 20*time.Millisecond:
		intervalConfidence = 0.95
	case interval <= 50*time.Millisecond:
		intervalConfidence = 0.85
	case interval <= 100*time.Millisecond:
		intervalConfidence = 0.75
	case interval <= 200*time.Millisecond:
		intervalConfidence = 0.65
	case interval <= 500*time.Millisecond:
		intervalConfidence = 0.55
	default:
		intervalConfidence = 0.40
	}

	// Adjust confidence based on transition type
	transitionConfidence := ed.getTransitionConfidence(fromState, toState)

	// Combine confidences (geometric mean)
	return (intervalConfidence + transitionConfidence) / 2
}

// getTransitionConfidence returns confidence based on state transition type
func (ed *EventDetector) getTransitionConfidence(
	fromState, toState PHPFPMProcessState,
) float64 {
	// High confidence transitions
	if (fromState == ProcessStateIdle && toState == ProcessStateRunning) ||
		(fromState == ProcessStateRunning && toState == ProcessStateIdle) {
		return 0.90
	}

	// Medium confidence transitions
	if (fromState == ProcessStateReading && toState == ProcessStateRunning) ||
		(fromState == ProcessStateRunning && toState == ProcessStateFinishing) {
		return 0.75
	}

	// Lower confidence for other transitions
	return 0.60
}

// calculateRequestBoundaryConfidence calculates confidence for request events
func (ed *EventDetector) calculateRequestBoundaryConfidence(requestDurationMs int64) float64 {
	// Higher confidence for longer requests (more likely to be accurate)
	switch {
	case requestDurationMs >= 1000: // >= 1 second
		return 0.90
	case requestDurationMs >= 500: // >= 500ms
		return 0.80
	case requestDurationMs >= 100: // >= 100ms
		return 0.70
	case requestDurationMs >= 50: // >= 50ms
		return 0.60
	default: // < 50ms
		return 0.45
	}
}

// GetStats returns detector statistics
func (ed *EventDetector) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"state_transitions_enabled":  ed.config.EnableStateTransitions,
		"request_boundaries_enabled": ed.config.EnableRequestBoundaries,
		"confidence_threshold":       ed.config.ConfidenceThreshold,
	}
}
