package metrics

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// StateTracker manages process state tracking and lifecycle monitoring
type StateTracker struct {
	logger *zap.Logger
	mu     sync.RWMutex

	// Process state management
	processStates map[int]*ProcessStateRecord
	stateHistory  map[int][]PHPFPMProcessState

	// Configuration
	maxStates      int
	maxHistorySize int
}

// NewStateTracker creates a new state tracker
func NewStateTracker(logger *zap.Logger) *StateTracker {
	return &StateTracker{
		logger:         logger,
		processStates:  make(map[int]*ProcessStateRecord),
		stateHistory:   make(map[int][]PHPFPMProcessState),
		maxStates:      500,
		maxHistorySize: 10,
	}
}

// UpdateStates updates process states with new data
func (st *StateTracker) UpdateStates(processes []PHPFPMProcess, pollTime time.Time) {
	st.mu.Lock()
	defer st.mu.Unlock()

	// Track current PIDs for cleanup
	currentPIDs := make(map[int]bool)

	for _, process := range processes {
		currentPIDs[process.PID] = true

		// Get or create state record
		state, exists := st.processStates[process.PID]
		if !exists {
			state = &ProcessStateRecord{
				PID: process.PID,
			}
			st.processStates[process.PID] = state
		}

		// Mark as having previous state if this is an update
		if exists {
			state.HasPrevious = true
		}

		// Update state record
		state.Update(process, pollTime)

		// Update state history
		st.updateStateHistory(process.PID, process.State)
	}

	// Clean up terminated processes
	st.cleanupTerminatedProcesses(currentPIDs)
}

// updateStateHistory maintains state history for pattern detection
func (st *StateTracker) updateStateHistory(pid int, state PHPFPMProcessState) {
	history := st.stateHistory[pid]

	// Only add if state changed
	if len(history) == 0 || history[len(history)-1] != state {
		history = append(history, state)

		// Maintain max history size
		if len(history) > st.maxHistorySize {
			history = history[1:]
		}

		st.stateHistory[pid] = history
	}
}

// cleanupTerminatedProcesses removes states for processes no longer running
func (st *StateTracker) cleanupTerminatedProcesses(currentPIDs map[int]bool) {
	// Clean up process states
	for pid := range st.processStates {
		if !currentPIDs[pid] {
			delete(st.processStates, pid)
		}
	}

	// Clean up state history
	for pid := range st.stateHistory {
		if !currentPIDs[pid] {
			delete(st.stateHistory, pid)
		}
	}

	// Enforce max states limit
	if len(st.processStates) > st.maxStates {
		st.logger.Warn("Too many process states tracked, cleanup may be needed",
			zap.Int("current", len(st.processStates)),
			zap.Int("max", st.maxStates))
	}
}

// GetProcessState returns the state record for a process
func (st *StateTracker) GetProcessState(pid int) (*ProcessStateRecord, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	state, exists := st.processStates[pid]
	return state, exists
}

// GetProcessStates returns a copy of all process states
func (st *StateTracker) GetProcessStates() map[int]*ProcessStateRecord {
	st.mu.RLock()
	defer st.mu.RUnlock()

	states := make(map[int]*ProcessStateRecord)
	for pid, state := range st.processStates {
		states[pid] = state.Clone()
	}

	return states
}

// GetStateHistory returns the state history for a process
func (st *StateTracker) GetStateHistory(pid int) []PHPFPMProcessState {
	st.mu.RLock()
	defer st.mu.RUnlock()

	history := st.stateHistory[pid]
	if history == nil {
		return nil
	}

	// Return copy
	result := make([]PHPFPMProcessState, len(history))
	copy(result, history)
	return result
}

// DetectStateTransitions identifies state changes between polls
func (st *StateTracker) DetectStateTransitions(
	processes []PHPFPMProcess,
	pollTime time.Time,
) []*StateTransition {
	st.mu.RLock()
	defer st.mu.RUnlock()

	var transitions []*StateTransition

	for _, process := range processes {
		if state, exists := st.processStates[process.PID]; exists && state.HasPrevious {
			if state.LastState != process.State {
				transition := &StateTransition{
					PID:       process.PID,
					FromState: state.LastState,
					ToState:   process.State,
					Timestamp: pollTime,
				}
				transitions = append(transitions, transition)

				st.logger.Debug("State transition detected",
					zap.Int("pid", process.PID),
					zap.String("from", state.LastState.String()),
					zap.String("to", process.State.String()))
			}
		}
	}

	return transitions
}

// GetStats returns tracker statistics
func (st *StateTracker) GetStats() map[string]interface{} {
	st.mu.RLock()
	defer st.mu.RUnlock()

	stats := map[string]interface{}{
		"tracked_processes": len(st.processStates),
		"max_states":        st.maxStates,
		"max_history_size":  st.maxHistorySize,
	}

	// Count states by type
	stateCounts := make(map[string]int)
	for _, state := range st.processStates {
		stateName := state.LastState.String()
		stateCounts[stateName]++
	}
	stats["state_distribution"] = stateCounts

	return stats
}

// Reset clears all tracked state
func (st *StateTracker) Reset() {
	st.mu.Lock()
	defer st.mu.Unlock()

	st.processStates = make(map[int]*ProcessStateRecord)
	st.stateHistory = make(map[int][]PHPFPMProcessState)

	st.logger.Info("State tracker reset")
}
