package metrics

import (
	"sync"
	"time"
)

// Forward declaration for ProcessSystemMetrics (defined in unified_timing_collector.go)
type ProcessSystemMetrics struct {
	PID             int           `json:"pid"`
	State           string        `json:"state"`
	CPUUserTime     time.Duration `json:"cpu_user_time"`
	CPUSystemTime   time.Duration `json:"cpu_system_time"`
	MemoryRSS       int64         `json:"memory_rss"`       // Resident Set Size (pages)
	MemoryVSize     int64         `json:"memory_vsize"`     // Virtual memory size (bytes)
	StartTime       int64         `json:"start_time"`       // Process start time
	Timestamp       time.Time     `json:"timestamp"`        // When metrics were collected
	CPUUsageRatio   float64       `json:"cpu_usage_ratio"`  // CPU time relative to request duration
	MemoryIntensity float64       `json:"memory_intensity"` // Memory usage relative to request duration
}

// EventType represents different types of timing events
type EventType int

const (
	EventTypeRequestStart EventType = iota
	EventTypeRequestEnd
	EventTypeStateChange
	EventTypeQueueEntry
	EventTypeQueueExit
)

// String returns string representation of EventType
func (e EventType) String() string {
	switch e {
	case EventTypeRequestStart:
		return "request_start"
	case EventTypeRequestEnd:
		return "request_end"
	case EventTypeStateChange:
		return "state_change"
	case EventTypeQueueEntry:
		return "queue_entry"
	case EventTypeQueueExit:
		return "queue_exit"
	default:
		return "unknown"
	}
}

// TimingEvent represents a detected timing event
type TimingEvent struct {
	PID           int                    `json:"pid"`
	ProcessID     int                    `json:"process_id"` // Alias for PID for backward compatibility
	Type          EventType              `json:"type"`
	Timestamp     time.Time              `json:"timestamp"`
	Confidence    float64                `json:"confidence"`
	Duration      time.Duration          `json:"duration,omitempty"`
	RequestURI    string                 `json:"request_uri,omitempty"`
	Details       map[string]interface{} `json:"details,omitempty"`
	SystemMetrics *ProcessSystemMetrics  `json:"system_metrics,omitempty"`
}

// Reset clears the timing event for reuse
func (e *TimingEvent) Reset() {
	e.PID = 0
	e.ProcessID = 0
	e.Type = EventTypeRequestStart
	e.Timestamp = time.Time{}
	e.Confidence = 0
	e.Duration = 0
	e.SystemMetrics = nil
	e.RequestURI = ""
	e.Details = nil
}

// Clone creates a copy of the timing event
func (e *TimingEvent) Clone() *TimingEvent {
	clone := &TimingEvent{
		PID:        e.PID,
		Type:       e.Type,
		Timestamp:  e.Timestamp,
		Confidence: e.Confidence,
		Duration:   e.Duration,
		RequestURI: e.RequestURI,
	}

	if e.Details != nil {
		clone.Details = make(map[string]interface{})
		for k, v := range e.Details {
			clone.Details[k] = v
		}
	}

	return clone
}

// StateTransition represents a process state change
type StateTransition struct {
	PID       int                `json:"pid"`
	FromState PHPFPMProcessState `json:"from_state"`
	ToState   PHPFPMProcessState `json:"to_state"`
	Timestamp time.Time          `json:"timestamp"`
}

// ProcessStateRecord tracks process state over time
type ProcessStateRecord struct {
	PID                 int
	LastState           PHPFPMProcessState
	LastRequestDuration int64
	LastRequestURI      string
	LastPollTime        time.Time
	HasPrevious         bool
	StateHistory        []PHPFPMProcessState
	RequestCount        int64
}

// Reset clears the state record for reuse
func (r *ProcessStateRecord) Reset() {
	r.PID = 0
	r.LastState = ProcessStateIdle
	r.LastRequestDuration = 0
	r.LastRequestURI = ""
	r.LastPollTime = time.Time{}
	r.HasPrevious = false
	r.StateHistory = r.StateHistory[:0]
	r.RequestCount = 0
}

// Update updates the state record with new process data
func (r *ProcessStateRecord) Update(process PHPFPMProcess, pollTime time.Time) {
	r.LastState = process.State
	r.LastRequestDuration = process.RequestDuration
	r.LastRequestURI = process.RequestURI
	r.LastPollTime = pollTime

	// Track state history (keep last 10)
	r.StateHistory = append(r.StateHistory, process.State)
	if len(r.StateHistory) > 10 {
		r.StateHistory = r.StateHistory[1:]
	}

	// Track request count changes
	if process.Requests > r.RequestCount {
		r.RequestCount = process.Requests
	}
}

// Clone creates a copy of the state record
func (r *ProcessStateRecord) Clone() *ProcessStateRecord {
	clone := &ProcessStateRecord{
		PID:                 r.PID,
		LastState:           r.LastState,
		LastRequestDuration: r.LastRequestDuration,
		LastRequestURI:      r.LastRequestURI,
		LastPollTime:        r.LastPollTime,
		HasPrevious:         r.HasPrevious,
		RequestCount:        r.RequestCount,
	}

	if r.StateHistory != nil {
		clone.StateHistory = make([]PHPFPMProcessState, len(r.StateHistory))
		copy(clone.StateHistory, r.StateHistory)
	}

	return clone
}

// TimingAnalysis contains the results of timing analysis
type TimingAnalysis struct {
	PollTime    time.Time              `json:"poll_time"`
	Events      []*TimingEvent         `json:"events"`
	Transitions []*StateTransition     `json:"transitions"`
	Confidence  float64                `json:"confidence"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// EnhancedMetrics combines worker metrics with timing analysis
type EnhancedMetrics struct {
	WorkerMetrics    *WorkerProcessMetrics `json:"worker_metrics"`
	TimingAnalysis   *TimingAnalysis       `json:"timing_analysis"`
	CollectorMetrics *InternalMetrics      `json:"collector_metrics"`
	Timestamp        time.Time             `json:"timestamp"`
}

// CollectorStats tracks collector performance statistics
type CollectorStats struct {
	TotalCollections          int64         `json:"total_collections"`
	FailedCollections         int64         `json:"failed_collections"`
	LastCollectionTime        time.Time     `json:"last_collection_time"`
	LastCollectionDuration    time.Duration `json:"last_collection_duration"`
	AverageCollectionDuration time.Duration `json:"average_collection_duration"`
	EventsDetected            int64         `json:"events_detected"`
	TransitionsDetected       int64         `json:"transitions_detected"`
}

// InternalMetrics tracks metrics about the collector itself
type InternalMetrics struct {
	CollectionsTotal   int64               `json:"collections_total"`
	CollectionDuration []time.Duration     `json:"collection_duration"`
	ErrorCounts        map[string]int64    `json:"error_counts"`
	EventCounts        map[EventType]int64 `json:"event_counts"`
	MemoryUsage        int64               `json:"memory_usage"`
}

// InternalMetricsCollector collects metrics about the monitoring system
type InternalMetricsCollector struct {
	mu               sync.RWMutex
	collectionsTotal int64
	durations        []time.Duration
	errorCounts      map[string]int64
	eventCounts      map[EventType]int64
}

// NewInternalMetricsCollector creates a new internal metrics collector
func NewInternalMetricsCollector() *InternalMetricsCollector {
	return &InternalMetricsCollector{
		durations:   make([]time.Duration, 0, 100),
		errorCounts: make(map[string]int64),
		eventCounts: make(map[EventType]int64),
	}
}

// RecordCollection records a collection event
func (m *InternalMetricsCollector) RecordCollection() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.collectionsTotal++
}

// RecordDuration records collection duration
func (m *InternalMetricsCollector) RecordDuration(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.durations = append(m.durations, d)
	if len(m.durations) > 100 {
		m.durations = m.durations[1:]
	}
}

// RecordError records an error occurrence
func (m *InternalMetricsCollector) RecordError(errorType string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errorCounts[errorType]++
}

// RecordEvent records a timing event
func (m *InternalMetricsCollector) RecordEvent(eventType EventType) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventCounts[eventType]++
}

// GetMetrics returns current internal metrics
func (m *InternalMetricsCollector) GetMetrics() *InternalMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metrics := &InternalMetrics{
		CollectionsTotal:   m.collectionsTotal,
		CollectionDuration: make([]time.Duration, len(m.durations)),
		ErrorCounts:        make(map[string]int64),
		EventCounts:        make(map[EventType]int64),
	}

	copy(metrics.CollectionDuration, m.durations)

	for k, v := range m.errorCounts {
		metrics.ErrorCounts[k] = v
	}

	for k, v := range m.eventCounts {
		metrics.EventCounts[k] = v
	}

	return metrics
}

// CircularBuffer is a fixed-size buffer that overwrites old data
type CircularBuffer struct {
	mu    sync.RWMutex
	data  []interface{}
	size  int
	head  int
	tail  int
	count int
}

// NewCircularBuffer creates a new circular buffer
func NewCircularBuffer(size int) *CircularBuffer {
	return &CircularBuffer{
		data: make([]interface{}, size),
		size: size,
	}
}

// Add adds an item to the buffer
func (b *CircularBuffer) Add(item interface{}) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.data[b.tail] = item
	b.tail = (b.tail + 1) % b.size

	if b.count < b.size {
		b.count++
	} else {
		b.head = (b.head + 1) % b.size
	}
}

// GetAll returns all items in the buffer
func (b *CircularBuffer) GetAll() []interface{} {
	b.mu.RLock()
	defer b.mu.RUnlock()

	result := make([]interface{}, 0, b.count)

	if b.count == 0 {
		return result
	}

	for i := 0; i < b.count; i++ {
		idx := (b.head + i) % b.size
		if b.data[idx] != nil {
			result = append(result, b.data[idx])
		}
	}

	return result
}

// GetRecent returns the most recent n items
func (b *CircularBuffer) GetRecent(n int) []interface{} {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if n > b.count {
		n = b.count
	}

	result := make([]interface{}, 0, n)
	start := (b.tail - n + b.size) % b.size

	for i := 0; i < n; i++ {
		idx := (start + i) % b.size
		if b.data[idx] != nil {
			result = append(result, b.data[idx])
		}
	}

	return result
}

// Clear empties the buffer
func (b *CircularBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i := range b.data {
		b.data[i] = nil
	}
	b.head = 0
	b.tail = 0
	b.count = 0
}

// Size returns the current number of items in the buffer
func (b *CircularBuffer) Size() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.count
}
