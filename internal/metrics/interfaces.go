package metrics

import (
	"context"
	"time"
)

// ProcessTracker defines the interface for process state management and lifecycle monitoring
type ProcessTracker interface {
	// UpdateStates updates process states with new data from a polling cycle
	UpdateStates(processes []PHPFPMProcess, pollTime time.Time)

	// GetProcessState returns the state record for a specific process
	GetProcessState(pid int) (*ProcessStateRecord, bool)

	// GetProcessStates returns a copy of all current process states
	GetProcessStates() map[int]*ProcessStateRecord

	// GetStateHistory returns the state history for a specific process
	GetStateHistory(pid int) []PHPFPMProcessState

	// DetectStateTransitions identifies state changes between polling cycles
	DetectStateTransitions(processes []PHPFPMProcess, pollTime time.Time) []*StateTransition

	// GetStats returns tracker statistics and metrics
	GetStats() map[string]interface{}

	// Reset clears all tracked state (useful for testing and cleanup)
	Reset()
}

// EventDetectorInterface defines the interface for timing event detection from process state changes
type EventDetectorInterface interface {
	// DetectEvents analyzes process states and identifies timing events
	DetectEvents(
		processStates map[int]*ProcessStateRecord,
		currentProcesses []PHPFPMProcess,
		pollTime time.Time,
	) []*TimingEvent

	// GetStats returns detector configuration and statistics
	GetStats() map[string]interface{}
}

// IntervalManagerInterface defines the interface for adaptive polling interval management
type IntervalManagerInterface interface {
	// CalculateInterval determines optimal polling interval based on current metrics
	CalculateInterval(
		metrics *WorkerProcessMetrics,
		analysis *TimingAnalysis,
		currentActivity ActivityLevel,
	) time.Duration

	// ShouldEnterBurstMode determines if burst mode should be activated
	ShouldEnterBurstMode(analysis *TimingAnalysis, metrics *WorkerProcessMetrics) bool

	// GetBurstInterval returns the interval to use during burst mode
	GetBurstInterval() time.Duration

	// GetBurstDuration returns how long burst mode should last
	GetBurstDuration() time.Duration

	// AdaptToActivity adjusts adaptation rate based on activity changes
	AdaptToActivity(currentActivity ActivityLevel)

	// PredictOptimalInterval predicts optimal interval for upcoming period
	PredictOptimalInterval(
		recentMetrics []*WorkerProcessMetrics,
		recentAnalyses []*TimingAnalysis,
	) time.Duration

	// GetStats returns manager statistics and configuration
	GetStats() map[string]interface{}

	// Reset resets the manager to initial state
	Reset()
}

// InternalMetricsCollectorInterface defines the interface for monitoring the monitoring system itself
type InternalMetricsCollectorInterface interface {
	// RecordCollection records a collection event
	RecordCollection()

	// RecordDuration records collection duration
	RecordDuration(d time.Duration)

	// RecordError records an error occurrence
	RecordError(errorType string)

	// RecordEvent records a timing event
	RecordEvent(eventType EventType)

	// GetMetrics returns current internal metrics
	GetMetrics() *InternalMetrics
}

// PHPFPMClientInterface defines the interface for PHP-FPM status endpoint communication
type PHPFPMClientInterface interface {
	// GetStatus retrieves PHP-FPM status from the specified endpoint
	GetStatus(ctx context.Context, endpoint string) (*PHPFPMStatus, error)

	// ProcessWorkerMetrics processes raw status into worker metrics
	ProcessWorkerMetrics(status *PHPFPMStatus) (*WorkerProcessMetrics, error)
}

// ObjectPool defines the interface for generic object pooling
type ObjectPool interface {
	// Get retrieves an object from the pool
	Get() interface{}

	// Put returns an object to the pool
	Put(item interface{})

	// Reset clears the pool (for testing and cleanup)
	Reset()
}

// TimingCollector interface is defined in timing_precision_improved.go
// to avoid duplication

// MetricsCollector defines the interface for general metrics collection
type MetricsCollector interface {
	// Collect performs a single metrics collection cycle
	Collect(ctx context.Context) error

	// Start begins continuous metrics collection
	Start(ctx context.Context) error

	// Stop gracefully stops metrics collection
	Stop() error

	// IsRunning returns whether the collector is currently running
	IsRunning() bool

	// GetStats returns collector statistics
	GetStats() map[string]interface{}
}

// StateManager defines the interface for managing process state persistence
type StateManager interface {
	// SaveState persists current process states
	SaveState(states map[int]*ProcessStateRecord) error

	// LoadState retrieves previously saved process states
	LoadState() (map[int]*ProcessStateRecord, error)

	// ClearState removes all saved state data
	ClearState() error
}

// CircularBufferInterface defines the interface for circular buffer operations
type CircularBufferInterface interface {
	// Add adds an item to the buffer
	Add(item interface{})

	// GetAll returns all items in the buffer
	GetAll() []interface{}

	// GetRecent returns the most recent n items
	GetRecent(n int) []interface{}

	// Clear empties the buffer
	Clear()

	// Size returns the current number of items in the buffer
	Size() int
}

// ConfigValidator defines the interface for configuration validation
type ConfigValidator interface {
	// Validate checks if the configuration is valid
	Validate() error

	// GetDefaults returns default configuration values
	GetDefaults() interface{}
}

// Logger defines the interface for structured logging (abstraction over zap.Logger)
type Logger interface {
	// Debug logs a debug message with optional fields
	Debug(msg string, fields ...interface{})

	// Info logs an info message with optional fields
	Info(msg string, fields ...interface{})

	// Warn logs a warning message with optional fields
	Warn(msg string, fields ...interface{})

	// Error logs an error message with optional fields
	Error(msg string, fields ...interface{})

	// Named creates a new logger with the specified name
	Named(name string) Logger
}

// MetricsProcessor defines the interface for processing collected metrics
type MetricsProcessor interface {
	// Process handles a batch of collected metrics
	Process(metrics []interface{}) error

	// ProcessSingle handles a single metric
	ProcessSingle(metric interface{}) error

	// Flush forces processing of any buffered metrics
	Flush() error
}

// HealthChecker defines the interface for component health monitoring
type HealthChecker interface {
	// Check performs a health check and returns any errors
	Check(ctx context.Context) error

	// GetStatus returns the current health status
	GetStatus() string

	// IsHealthy returns true if the component is healthy
	IsHealthy() bool
}

// MetricsExporter defines the interface for exporting metrics to external systems
type MetricsExporter interface {
	// Export sends metrics to the external system
	Export(ctx context.Context, metrics *EnhancedMetrics) error

	// GetExportStats returns export statistics
	GetExportStats() map[string]interface{}

	// IsConnected returns true if connected to the external system
	IsConnected() bool
}

// Interface Implementation Validation
// These variables ensure that concrete types implement the interfaces at compile time

var (
	// Core component interfaces
	_ ProcessTracker                    = (*StateTracker)(nil)
	_ EventDetectorInterface            = (*EventDetector)(nil)
	_ IntervalManagerInterface          = (*IntervalManager)(nil)
	_ InternalMetricsCollectorInterface = (*InternalMetricsCollector)(nil)
	_ CircularBufferInterface           = (*CircularBuffer)(nil)
	_ TimingCollector                   = (*UnifiedTimingCollector)(nil)
	// Note: ConfigValidator and PHPFPMClientInterface validations removed due to interface conflicts
)
