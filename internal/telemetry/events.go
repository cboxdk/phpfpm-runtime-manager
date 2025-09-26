package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// EventType represents the type of operational event
type EventType string

const (
	EventTypePoolScaling   EventType = "pool_scaling"
	EventTypePoolLifecycle EventType = "pool_lifecycle"
	EventTypeAutoscaling   EventType = "autoscaling"
	EventTypeConfiguration EventType = "configuration"
	EventTypeHealthChange  EventType = "health_change"
	EventTypeSystemMetric  EventType = "system_metric"
)

// Event represents a structured operational event
type Event struct {
	ID            string                 `json:"id"`
	Type          EventType              `json:"type"`
	Timestamp     time.Time              `json:"timestamp"`
	Pool          string                 `json:"pool,omitempty"`
	Summary       string                 `json:"summary"`
	Details       map[string]interface{} `json:"details"`
	CorrelationID string                 `json:"correlation_id,omitempty"`
	Severity      EventSeverity          `json:"severity"`
}

// EventSeverity represents the severity level of an event
type EventSeverity string

const (
	SeverityInfo     EventSeverity = "info"
	SeverityWarning  EventSeverity = "warning"
	SeverityError    EventSeverity = "error"
	SeverityCritical EventSeverity = "critical"
)

// ScalingEventDetails represents details for scaling events
type ScalingEventDetails struct {
	Action        string  `json:"action"` // "scale_up", "scale_down"
	PreviousCount int     `json:"previous_count"`
	NewCount      int     `json:"new_count"`
	Reason        string  `json:"reason"` // "manual", "autoscaling", "configuration"
	CPUUsage      float64 `json:"cpu_usage,omitempty"`
	MemoryUsage   float64 `json:"memory_usage,omitempty"`
	Trigger       string  `json:"trigger,omitempty"`
}

// PoolLifecycleEventDetails represents details for pool lifecycle events
type PoolLifecycleEventDetails struct {
	Action    string    `json:"action"` // "start", "stop", "reload", "restart"
	PID       int       `json:"pid,omitempty"`
	ExitCode  int       `json:"exit_code,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	StartTime time.Time `json:"start_time,omitempty"`
	Duration  string    `json:"duration,omitempty"`
}

// AutoscalingEventDetails represents details for autoscaling events
type AutoscalingEventDetails struct {
	Action          string  `json:"action"`             // "enabled", "disabled", "paused", "resumed", "decision"
	Decision        string  `json:"decision,omitempty"` // "scale_up", "scale_down", "no_action"
	CPUThreshold    float64 `json:"cpu_threshold,omitempty"`
	MemoryThreshold float64 `json:"memory_threshold,omitempty"`
	CurrentCPU      float64 `json:"current_cpu,omitempty"`
	CurrentMemory   float64 `json:"current_memory,omitempty"`
	Reason          string  `json:"reason,omitempty"`
}

// ConfigurationEventDetails represents details for configuration events
type ConfigurationEventDetails struct {
	Action   string                 `json:"action"` // "validated", "changed", "reloaded"
	Changes  map[string]interface{} `json:"changes,omitempty"`
	Errors   []string               `json:"errors,omitempty"`
	FilePath string                 `json:"file_path,omitempty"`
}

// HealthChangeEventDetails represents details for health change events
type HealthChangeEventDetails struct {
	PreviousState string `json:"previous_state"`
	NewState      string `json:"new_state"`
	CheckType     string `json:"check_type"` // "endpoint", "process", "metrics"
	Error         string `json:"error,omitempty"`
	ResponseTime  string `json:"response_time,omitempty"`
}

// EventEmitter handles structured event emission with telemetry integration
type EventEmitter struct {
	service *Service
	logger  *zap.Logger
	storage EventStorage
}

// EventStorage interface for persisting events
type EventStorage interface {
	StoreEvent(ctx context.Context, event Event) error
	GetEvents(ctx context.Context, filter EventFilter) ([]Event, error)
}

// EventFilter represents filters for querying events
type EventFilter struct {
	StartTime time.Time
	EndTime   time.Time
	Pool      string
	Type      EventType
	Severity  EventSeverity
	Limit     int
}

// NewEventEmitter creates a new event emitter
func NewEventEmitter(service *Service, logger *zap.Logger, storage EventStorage) *EventEmitter {
	return &EventEmitter{
		service: service,
		logger:  logger,
		storage: storage,
	}
}

// EmitScalingEvent emits a pool scaling event
func (e *EventEmitter) EmitScalingEvent(ctx context.Context, pool string, details ScalingEventDetails) error {
	event := Event{
		ID:        generateEventID(),
		Type:      EventTypePoolScaling,
		Timestamp: time.Now(),
		Pool:      pool,
		Summary:   formatScalingSummary(details),
		Details:   structToMap(details),
		Severity:  SeverityInfo,
	}

	return e.emitEvent(ctx, event)
}

// EmitPoolLifecycleEvent emits a pool lifecycle event
func (e *EventEmitter) EmitPoolLifecycleEvent(ctx context.Context, pool string, details PoolLifecycleEventDetails) error {
	severity := SeverityInfo
	if details.Action == "stop" && details.ExitCode != 0 {
		severity = SeverityWarning
	}

	event := Event{
		ID:        generateEventID(),
		Type:      EventTypePoolLifecycle,
		Timestamp: time.Now(),
		Pool:      pool,
		Summary:   formatLifecycleSummary(details),
		Details:   structToMap(details),
		Severity:  severity,
	}

	return e.emitEvent(ctx, event)
}

// EmitAutoscalingEvent emits an autoscaling event
func (e *EventEmitter) EmitAutoscalingEvent(ctx context.Context, details AutoscalingEventDetails) error {
	event := Event{
		ID:        generateEventID(),
		Type:      EventTypeAutoscaling,
		Timestamp: time.Now(),
		Summary:   formatAutoscalingSummary(details),
		Details:   structToMap(details),
		Severity:  SeverityInfo,
	}

	return e.emitEvent(ctx, event)
}

// EmitConfigurationEvent emits a configuration event
func (e *EventEmitter) EmitConfigurationEvent(ctx context.Context, pool string, details ConfigurationEventDetails) error {
	severity := SeverityInfo
	if len(details.Errors) > 0 {
		severity = SeverityError
	}

	event := Event{
		ID:        generateEventID(),
		Type:      EventTypeConfiguration,
		Timestamp: time.Now(),
		Pool:      pool,
		Summary:   formatConfigurationSummary(details),
		Details:   structToMap(details),
		Severity:  severity,
	}

	return e.emitEvent(ctx, event)
}

// EmitHealthChangeEvent emits a health change event
func (e *EventEmitter) EmitHealthChangeEvent(ctx context.Context, pool string, details HealthChangeEventDetails) error {
	severity := SeverityInfo
	if details.NewState == "unhealthy" || details.NewState == "unknown" {
		severity = SeverityWarning
	}

	event := Event{
		ID:        generateEventID(),
		Type:      EventTypeHealthChange,
		Timestamp: time.Now(),
		Pool:      pool,
		Summary:   formatHealthChangeSummary(details),
		Details:   structToMap(details),
		Severity:  severity,
	}

	return e.emitEvent(ctx, event)
}

// emitEvent handles the actual event emission with telemetry and storage
func (e *EventEmitter) emitEvent(ctx context.Context, event Event) error {
	// Add correlation ID from context if available
	if span := oteltrace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		event.CorrelationID = span.SpanContext().TraceID().String()
	}

	// Create telemetry span for the event
	if e.service.IsEnabled() {
		_, span := e.service.Tracer().Start(ctx, "event.emit",
			oteltrace.WithAttributes(
				attribute.String("event.type", string(event.Type)),
				attribute.String("event.pool", event.Pool),
				attribute.String("event.severity", string(event.Severity)),
				attribute.String("event.summary", event.Summary),
			),
		)
		defer span.End()
	}

	// Store event in database
	if e.storage != nil {
		if err := e.storage.StoreEvent(ctx, event); err != nil {
			e.logger.Error("Failed to store event",
				zap.String("event_id", event.ID),
				zap.String("event_type", string(event.Type)),
				zap.Error(err))
			return err
		}
	}

	// Log the event
	e.logger.Info("Event emitted",
		zap.String("event_id", event.ID),
		zap.String("event_type", string(event.Type)),
		zap.String("pool", event.Pool),
		zap.String("summary", event.Summary),
		zap.String("severity", string(event.Severity)))

	return nil
}

// GetEvents retrieves events from storage
func (e *EventEmitter) GetEvents(ctx context.Context, filter EventFilter) ([]Event, error) {
	if e.storage == nil {
		return nil, fmt.Errorf("event storage not configured")
	}

	return e.storage.GetEvents(ctx, filter)
}

// Helper functions for formatting event summaries
func formatScalingSummary(details ScalingEventDetails) string {
	return fmt.Sprintf("Pool scaled %s from %d to %d workers (%s)",
		details.Action, details.PreviousCount, details.NewCount, details.Reason)
}

func formatLifecycleSummary(details PoolLifecycleEventDetails) string {
	switch details.Action {
	case "start":
		return fmt.Sprintf("Pool started (PID: %d)", details.PID)
	case "stop":
		if details.ExitCode == 0 {
			return "Pool stopped gracefully"
		}
		return fmt.Sprintf("Pool stopped with exit code %d", details.ExitCode)
	case "reload":
		return "Pool configuration reloaded"
	case "restart":
		return "Pool restarted"
	default:
		return fmt.Sprintf("Pool %s", details.Action)
	}
}

func formatAutoscalingSummary(details AutoscalingEventDetails) string {
	switch details.Action {
	case "decision":
		return fmt.Sprintf("Autoscaling decision: %s (CPU: %.1f%%, Memory: %.1f%%)",
			details.Decision, details.CurrentCPU, details.CurrentMemory)
	default:
		return fmt.Sprintf("Autoscaling %s", details.Action)
	}
}

func formatConfigurationSummary(details ConfigurationEventDetails) string {
	if len(details.Errors) > 0 {
		return fmt.Sprintf("Configuration %s failed: %d errors", details.Action, len(details.Errors))
	}
	return fmt.Sprintf("Configuration %s successfully", details.Action)
}

func formatHealthChangeSummary(details HealthChangeEventDetails) string {
	return fmt.Sprintf("Health changed from %s to %s (%s)",
		details.PreviousState, details.NewState, details.CheckType)
}

// Utility functions
func generateEventID() string {
	// Generate 8 random bytes for a 16-character hex string
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID if random generation fails
		return fmt.Sprintf("evt_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("evt_%s", hex.EncodeToString(bytes))
}

func structToMap(v interface{}) map[string]interface{} {
	data, err := json.Marshal(v)
	if err != nil {
		// Return empty map on marshal error
		return make(map[string]interface{})
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		// Return empty map on unmarshal error
		return make(map[string]interface{})
	}

	return result
}
