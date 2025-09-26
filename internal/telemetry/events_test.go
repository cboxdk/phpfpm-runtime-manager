package telemetry

import (
	"context"
	"strings"
	"testing"

	"go.uber.org/zap/zaptest"
)

// MockEventStorage implements EventStorage for testing
type MockEventStorage struct {
	storedEvents []Event
	storeError   error
	getError     error
}

func (m *MockEventStorage) StoreEvent(ctx context.Context, event Event) error {
	if m.storeError != nil {
		return m.storeError
	}
	m.storedEvents = append(m.storedEvents, event)
	return nil
}

func (m *MockEventStorage) GetEvents(ctx context.Context, filter EventFilter) ([]Event, error) {
	if m.getError != nil {
		return nil, m.getError
	}

	var filtered []Event
	for _, event := range m.storedEvents {
		// Simple filtering logic for testing
		if filter.Pool != "" && event.Pool != filter.Pool {
			continue
		}
		if filter.Type != "" && event.Type != filter.Type {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered, nil
}

func TestEmitScalingEvent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	service, _ := NewService(Config{Enabled: false}, logger)
	storage := &MockEventStorage{}
	emitter := NewEventEmitter(service, logger, storage)

	ctx := context.Background()
	details := ScalingEventDetails{
		Action:        "scale_up",
		PreviousCount: 5,
		NewCount:      10,
		Reason:        "manual",
		CPUUsage:      75.5,
		MemoryUsage:   60.2,
		Trigger:       "api",
	}

	err := emitter.EmitScalingEvent(ctx, "web-pool", details)
	if err != nil {
		t.Fatalf("EmitScalingEvent failed: %v", err)
	}

	if len(storage.storedEvents) != 1 {
		t.Errorf("expected 1 stored event, got %d", len(storage.storedEvents))
	}

	event := storage.storedEvents[0]
	if event.Type != EventTypePoolScaling {
		t.Errorf("expected event type %s, got %s", EventTypePoolScaling, event.Type)
	}
	if event.Pool != "web-pool" {
		t.Errorf("expected pool 'web-pool', got %s", event.Pool)
	}
	if event.Severity != SeverityInfo {
		t.Errorf("expected severity %s, got %s", SeverityInfo, event.Severity)
	}
}

func TestEmitPoolLifecycleEvent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	service, _ := NewService(Config{Enabled: false}, logger)
	storage := &MockEventStorage{}
	emitter := NewEventEmitter(service, logger, storage)

	tests := []struct {
		name             string
		details          PoolLifecycleEventDetails
		expectedSeverity EventSeverity
	}{
		{
			name: "successful start",
			details: PoolLifecycleEventDetails{
				Action: "start",
				PID:    1234,
			},
			expectedSeverity: SeverityInfo,
		},
		{
			name: "failed stop",
			details: PoolLifecycleEventDetails{
				Action:   "stop",
				ExitCode: 1,
				Reason:   "crash",
			},
			expectedSeverity: SeverityWarning,
		},
		{
			name: "graceful stop",
			details: PoolLifecycleEventDetails{
				Action:   "stop",
				ExitCode: 0,
			},
			expectedSeverity: SeverityInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage.storedEvents = nil // Reset storage

			ctx := context.Background()
			err := emitter.EmitPoolLifecycleEvent(ctx, "test-pool", tt.details)
			if err != nil {
				t.Fatalf("EmitPoolLifecycleEvent failed: %v", err)
			}

			if len(storage.storedEvents) != 1 {
				t.Errorf("expected 1 stored event, got %d", len(storage.storedEvents))
			}

			event := storage.storedEvents[0]
			if event.Type != EventTypePoolLifecycle {
				t.Errorf("expected event type %s, got %s", EventTypePoolLifecycle, event.Type)
			}
			if event.Severity != tt.expectedSeverity {
				t.Errorf("expected severity %s, got %s", tt.expectedSeverity, event.Severity)
			}
		})
	}
}

func TestEmitHealthChangeEvent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	service, _ := NewService(Config{Enabled: false}, logger)
	storage := &MockEventStorage{}
	emitter := NewEventEmitter(service, logger, storage)

	tests := []struct {
		name             string
		details          HealthChangeEventDetails
		expectedSeverity EventSeverity
	}{
		{
			name: "transition to healthy",
			details: HealthChangeEventDetails{
				PreviousState: "unhealthy",
				NewState:      "healthy",
				CheckType:     "endpoint",
			},
			expectedSeverity: SeverityInfo,
		},
		{
			name: "transition to unhealthy",
			details: HealthChangeEventDetails{
				PreviousState: "healthy",
				NewState:      "unhealthy",
				CheckType:     "endpoint",
				Error:         "connection timeout",
			},
			expectedSeverity: SeverityWarning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage.storedEvents = nil // Reset storage

			ctx := context.Background()
			err := emitter.EmitHealthChangeEvent(ctx, "test-pool", tt.details)
			if err != nil {
				t.Fatalf("EmitHealthChangeEvent failed: %v", err)
			}

			event := storage.storedEvents[0]
			if event.Severity != tt.expectedSeverity {
				t.Errorf("expected severity %s, got %s", tt.expectedSeverity, event.Severity)
			}
		})
	}
}

func TestGetEvents(t *testing.T) {
	logger := zaptest.NewLogger(t)
	service, _ := NewService(Config{Enabled: false}, logger)
	storage := &MockEventStorage{}
	emitter := NewEventEmitter(service, logger, storage)

	// Add some test events
	ctx := context.Background()
	emitter.EmitScalingEvent(ctx, "pool1", ScalingEventDetails{
		Action:        "scale_up",
		PreviousCount: 1,
		NewCount:      2,
	})
	emitter.EmitScalingEvent(ctx, "pool2", ScalingEventDetails{
		Action:        "scale_down",
		PreviousCount: 3,
		NewCount:      2,
	})

	// Test filtering by pool
	events, err := emitter.GetEvents(ctx, EventFilter{Pool: "pool1"})
	if err != nil {
		t.Fatalf("GetEvents failed: %v", err)
	}

	if len(events) != 1 {
		t.Errorf("expected 1 event for pool1, got %d", len(events))
	}
	if events[0].Pool != "pool1" {
		t.Errorf("expected pool 'pool1', got %s", events[0].Pool)
	}
}

func TestGenerateEventID(t *testing.T) {
	id1 := generateEventID()
	id2 := generateEventID()

	if id1 == id2 {
		t.Error("generateEventID should produce unique IDs")
	}

	if !strings.HasPrefix(id1, "evt_") {
		t.Errorf("event ID should start with 'evt_', got %s", id1)
	}
}

func TestStructToMap(t *testing.T) {
	type TestStruct struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	input := TestStruct{
		Name:  "test",
		Value: 42,
	}

	result := structToMap(input)

	if result["name"] != "test" {
		t.Errorf("expected name='test', got %v", result["name"])
	}

	// Test with nil input
	nilResult := structToMap(nil)
	if len(nilResult) != 0 {
		t.Error("structToMap should return empty map for nil input")
	}
}

func TestFormatScalingSummary(t *testing.T) {
	details := ScalingEventDetails{
		Action:        "scale_up",
		PreviousCount: 5,
		NewCount:      10,
		Reason:        "autoscaling",
	}

	summary := formatScalingSummary(details)
	expected := "Pool scaled scale_up from 5 to 10 workers (autoscaling)"

	if summary != expected {
		t.Errorf("expected summary '%s', got '%s'", expected, summary)
	}
}

func TestFormatLifecycleSummary(t *testing.T) {
	tests := []struct {
		name     string
		details  PoolLifecycleEventDetails
		expected string
	}{
		{
			name:     "start action",
			details:  PoolLifecycleEventDetails{Action: "start", PID: 1234},
			expected: "Pool started (PID: 1234)",
		},
		{
			name:     "graceful stop",
			details:  PoolLifecycleEventDetails{Action: "stop", ExitCode: 0},
			expected: "Pool stopped gracefully",
		},
		{
			name:     "failed stop",
			details:  PoolLifecycleEventDetails{Action: "stop", ExitCode: 1},
			expected: "Pool stopped with exit code 1",
		},
		{
			name:     "reload action",
			details:  PoolLifecycleEventDetails{Action: "reload"},
			expected: "Pool configuration reloaded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := formatLifecycleSummary(tt.details)
			if summary != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, summary)
			}
		})
	}
}

func BenchmarkEmitEvent(b *testing.B) {
	logger := zaptest.NewLogger(b)
	service, _ := NewService(Config{Enabled: false}, logger)
	storage := &MockEventStorage{}
	emitter := NewEventEmitter(service, logger, storage)

	ctx := context.Background()
	details := ScalingEventDetails{
		Action:        "scale_up",
		PreviousCount: 5,
		NewCount:      10,
		Reason:        "manual",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		emitter.EmitScalingEvent(ctx, "test-pool", details)
	}
}
