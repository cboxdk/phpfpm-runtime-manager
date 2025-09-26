package types

import (
	"context"
	"time"
)

// ProcessSupervisor manages PHP-FPM processes and their lifecycle
type ProcessSupervisor interface {
	// Start initializes and starts all configured PHP-FPM pools
	Start(ctx context.Context) error

	// Stop gracefully stops all PHP-FPM processes
	Stop(ctx context.Context) error

	// Reload performs a zero-downtime reload of configuration
	Reload(ctx context.Context) error

	// Restart performs a zero-downtime restart of processes
	Restart(ctx context.Context) error

	// Health returns the current health status of all pools
	Health() HealthStatus

	// Subscribe returns a channel for process events
	Subscribe() <-chan ProcessEvent

	// ReloadPool reloads a specific pool
	ReloadPool(ctx context.Context, pool string) error

	// ScalePool scales a specific pool to the given number of workers
	ScalePool(ctx context.Context, pool string, workers int) error

	// GetPoolScale returns the current number of workers for a pool
	GetPoolScale(pool string) (int, error)

	// ValidatePoolConfig validates the configuration for a specific pool
	ValidatePoolConfig(pool string) error
}

// MetricsCollector gathers metrics from various sources
type MetricsCollector interface {
	// Start begins metrics collection
	Start(ctx context.Context) error

	// Stop halts metrics collection
	Stop(ctx context.Context) error

	// Collect performs a single metrics collection cycle
	Collect(ctx context.Context) (*MetricSet, error)

	// Subscribe returns a channel for collected metrics
	Subscribe() <-chan MetricSet

	// IsAutoscalingEnabled returns true if autoscaling is enabled
	IsAutoscalingEnabled() bool

	// IsAutoscalingPaused returns true if autoscaling is paused
	IsAutoscalingPaused() bool

	// PauseAutoscaling pauses the autoscaling functionality
	PauseAutoscaling() error

	// ResumeAutoscaling resumes the autoscaling functionality
	ResumeAutoscaling() error
}

// Storage handles persistent storage of timeseries metrics
type Storage interface {
	// Start initializes the storage backend
	Start(ctx context.Context) error

	// Stop closes the storage backend
	Stop(ctx context.Context) error

	// Store persists a set of metrics
	Store(ctx context.Context, metrics MetricSet) error

	// Query retrieves metrics based on the given query
	Query(ctx context.Context, query Query) (*Result, error)

	// Cleanup removes expired metrics according to retention policies
	Cleanup(ctx context.Context) error
}

// PrometheusExporter exposes metrics in Prometheus format
type PrometheusExporter interface {
	// Start begins serving metrics
	Start(ctx context.Context) error

	// Stop halts the metrics server
	Stop(ctx context.Context) error

	// UpdateMetrics refreshes the exposed metrics
	UpdateMetrics(metrics MetricSet) error
}

// HealthStatus represents the health state of the system
type HealthStatus struct {
	Overall HealthState            `json:"overall"`
	Pools   map[string]HealthState `json:"pools"`
	Updated time.Time              `json:"updated"`
}

// HealthState represents the health of a component
type HealthState string

const (
	HealthStateHealthy   HealthState = "healthy"
	HealthStateUnhealthy HealthState = "unhealthy"
	HealthStateUnknown   HealthState = "unknown"
	HealthStateStarting  HealthState = "starting"
	HealthStateStopping  HealthState = "stopping"
)

// ProcessEvent represents events from the process supervisor
type ProcessEvent struct {
	Type      ProcessEventType `json:"type"`
	Pool      string           `json:"pool"`
	PID       int              `json:"pid,omitempty"`
	Message   string           `json:"message"`
	Timestamp time.Time        `json:"timestamp"`
	Error     error            `json:"error,omitempty"`
}

// ProcessEventType defines types of process events
type ProcessEventType string

const (
	ProcessEventStarted   ProcessEventType = "started"
	ProcessEventStopped   ProcessEventType = "stopped"
	ProcessEventReloaded  ProcessEventType = "reloaded"
	ProcessEventRestarted ProcessEventType = "restarted"
	ProcessEventFailed    ProcessEventType = "failed"
	ProcessEventRecovered ProcessEventType = "recovered"
)

// MetricSet represents a collection of metrics at a point in time
type MetricSet struct {
	Timestamp time.Time         `json:"timestamp"`
	Metrics   []Metric          `json:"metrics"`
	Labels    map[string]string `json:"labels"`
}

// Metric represents a single metric measurement
type Metric struct {
	Name      string            `json:"name"`
	Value     float64           `json:"value"`
	Type      MetricType        `json:"type"`
	Labels    map[string]string `json:"labels"`
	Timestamp time.Time         `json:"timestamp"`
}

// MetricType defines the type of metric
type MetricType string

const (
	MetricTypeGauge     MetricType = "gauge"
	MetricTypeCounter   MetricType = "counter"
	MetricTypeHistogram MetricType = "histogram"
	MetricTypeSummary   MetricType = "summary"
)

// Query represents a metrics query
type Query struct {
	MetricName  string            `json:"metric_name"`
	Labels      map[string]string `json:"labels"`
	StartTime   time.Time         `json:"start_time"`
	EndTime     time.Time         `json:"end_time"`
	Aggregation string            `json:"aggregation"` // avg, sum, min, max
	Interval    time.Duration     `json:"interval"`
}

// Result represents query results
type Result struct {
	MetricName string            `json:"metric_name"`
	Labels     map[string]string `json:"labels"`
	Values     []DataPoint       `json:"values"`
}

// DataPoint represents a single data point in time
type DataPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}
