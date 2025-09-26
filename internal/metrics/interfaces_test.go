package metrics

import (
	"net/http"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

// TestInterfaceImplementations verifies that concrete types implement their interfaces
func TestInterfaceImplementations(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("ProcessTracker Interface", func(t *testing.T) {
		var tracker ProcessTracker = NewStateTracker(logger)

		// Test basic interface methods
		if tracker == nil {
			t.Error("StateTracker should implement ProcessTracker interface")
		}

		// Test that interface methods are callable
		tracker.Reset()
		stats := tracker.GetStats()
		if stats == nil {
			t.Error("GetStats should return non-nil map")
		}
	})

	t.Run("EventDetectorInterface", func(t *testing.T) {
		config := EventDetectorConfig{
			EnableStateTransitions:  true,
			EnableRequestBoundaries: true,
			ConfidenceThreshold:     0.75,
		}
		var detector EventDetectorInterface = NewEventDetector(config, logger)

		if detector == nil {
			t.Error("EventDetector should implement EventDetectorInterface")
		}

		// Test interface methods
		stats := detector.GetStats()
		if stats == nil {
			t.Error("GetStats should return non-nil map")
		}
	})

	t.Run("IntervalManagerInterface", func(t *testing.T) {
		config := IntervalManagerConfig{
			BaseInterval:      200 * time.Millisecond,
			MinInterval:       10 * time.Millisecond,
			MaxInterval:       2 * time.Second,
			BurstDuration:     2 * time.Second,
			SmoothingFactor:   0.25,
			ActivityThreshold: 0.1,
		}
		var manager IntervalManagerInterface = NewIntervalManager(config, logger)

		if manager == nil {
			t.Error("IntervalManager should implement IntervalManagerInterface")
		}

		// Test interface methods
		burstInterval := manager.GetBurstInterval()
		if burstInterval != config.MinInterval {
			t.Errorf("Expected burst interval %v, got %v", config.MinInterval, burstInterval)
		}

		burstDuration := manager.GetBurstDuration()
		if burstDuration != config.BurstDuration {
			t.Errorf("Expected burst duration %v, got %v", config.BurstDuration, burstDuration)
		}

		manager.Reset()
		stats := manager.GetStats()
		if stats == nil {
			t.Error("GetStats should return non-nil map")
		}
	})

	t.Run("InternalMetricsCollectorInterface", func(t *testing.T) {
		var collector InternalMetricsCollectorInterface = NewInternalMetricsCollector()

		if collector == nil {
			t.Error("InternalMetricsCollector should implement InternalMetricsCollectorInterface")
		}

		// Test interface methods
		collector.RecordCollection()
		collector.RecordDuration(100 * time.Millisecond)
		collector.RecordError("test_error")
		collector.RecordEvent(EventTypeRequestStart)

		metrics := collector.GetMetrics()
		if metrics == nil {
			t.Error("GetMetrics should return non-nil metrics")
		}

		if metrics.CollectionsTotal != 1 {
			t.Errorf("Expected 1 collection recorded, got %d", metrics.CollectionsTotal)
		}
	})

	t.Run("CircularBufferInterface", func(t *testing.T) {
		var buffer CircularBufferInterface = NewCircularBuffer(5)

		if buffer == nil {
			t.Error("CircularBuffer should implement CircularBufferInterface")
		}

		// Test interface methods
		buffer.Add("test1")
		buffer.Add("test2")

		if buffer.Size() != 2 {
			t.Errorf("Expected size 2, got %d", buffer.Size())
		}

		items := buffer.GetAll()
		if len(items) != 2 {
			t.Errorf("Expected 2 items, got %d", len(items))
		}

		recent := buffer.GetRecent(1)
		if len(recent) != 1 {
			t.Errorf("Expected 1 recent item, got %d", len(recent))
		}

		buffer.Clear()
		if buffer.Size() != 0 {
			t.Errorf("Expected size 0 after clear, got %d", buffer.Size())
		}
	})

	t.Run("PHPFPMClientInterface", func(t *testing.T) {
		phpfpmClient := &PHPFPMClient{
			client: &http.Client{},
			logger: logger,
		}

		var client PHPFPMClientInterface = phpfpmClient

		if client == nil {
			t.Error("PHPFPMClient should implement PHPFPMClientInterface")
		}

		// Note: We can't easily test the actual methods without a running PHP-FPM instance,
		// but we can verify the interface is implemented correctly at compile time
	})

	t.Run("TimingCollector Interface", func(t *testing.T) {
		config := DefaultUnifiedConfig()
		phpfpmClient := &PHPFPMClient{
			client: &http.Client{},
			logger: logger,
		}

		collector, err := NewUnifiedTimingCollector(config, phpfpmClient, logger)
		if err != nil {
			t.Fatalf("Failed to create unified timing collector: %v", err)
		}

		var timingCollector TimingCollector = collector

		if timingCollector == nil {
			t.Error("UnifiedTimingCollector should implement TimingCollector interface")
		}

		// Test interface methods
		interval := timingCollector.GetCurrentInterval()
		if interval != config.BaseInterval {
			t.Errorf("Expected interval %v, got %v", config.BaseInterval, interval)
		}

		stats := timingCollector.Stats()
		if stats.TotalCollections != 0 {
			t.Errorf("Expected 0 total collections, got %d", stats.TotalCollections)
		}
	})
}

// TestDependencyInjection verifies that dependency injection works correctly
func TestDependencyInjection(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("WithCustomDependencies", func(t *testing.T) {
		config := DefaultUnifiedConfig()

		// Create custom implementations
		phpfpmClient := &PHPFPMClient{
			client: &http.Client{},
			logger: logger,
		}

		stateTracker := NewStateTracker(logger.Named("custom-state-tracker"))

		eventDetectorConfig := EventDetectorConfig{
			EnableStateTransitions:  true,
			EnableRequestBoundaries: true,
			ConfidenceThreshold:     0.8,
		}
		eventDetector := NewEventDetector(eventDetectorConfig, logger.Named("custom-event-detector"))

		intervalManagerConfig := IntervalManagerConfig{
			BaseInterval:      100 * time.Millisecond,
			MinInterval:       5 * time.Millisecond,
			MaxInterval:       1 * time.Second,
			BurstDuration:     1 * time.Second,
			SmoothingFactor:   0.3,
			ActivityThreshold: 0.2,
		}
		intervalManager := NewIntervalManager(intervalManagerConfig, logger.Named("custom-interval-manager"))

		metricsCollector := NewInternalMetricsCollector()

		// Create collector with custom dependencies
		collector, err := NewUnifiedTimingCollectorWithDependencies(
			config,
			phpfpmClient,
			stateTracker,
			eventDetector,
			intervalManager,
			metricsCollector,
			logger,
		)

		if err != nil {
			t.Fatalf("Failed to create collector with custom dependencies: %v", err)
		}

		if collector == nil {
			t.Error("Collector should not be nil")
		}

		// Verify that the custom dependencies are being used
		if collector.GetCurrentInterval() != config.BaseInterval {
			t.Errorf("Expected interval %v, got %v", config.BaseInterval, collector.GetCurrentInterval())
		}
	})

	t.Run("WithNilDependencies", func(t *testing.T) {
		config := DefaultUnifiedConfig()

		phpfpmClient := &PHPFPMClient{
			client: &http.Client{},
			logger: logger,
		}

		// Create collector with nil dependencies (should create defaults)
		collector, err := NewUnifiedTimingCollectorWithDependencies(
			config,
			phpfpmClient,
			nil, // state tracker
			nil, // event detector
			nil, // interval manager
			nil, // metrics collector
			logger,
		)

		if err != nil {
			t.Fatalf("Failed to create collector with nil dependencies: %v", err)
		}

		if collector == nil {
			t.Error("Collector should not be nil")
		}

		// Verify that default implementations were created
		if collector.GetCurrentInterval() != config.BaseInterval {
			t.Errorf("Expected interval %v, got %v", config.BaseInterval, collector.GetCurrentInterval())
		}
	})
}

// TestInterfaceContracts verifies that interfaces define the expected contracts
func TestInterfaceContracts(t *testing.T) {
	t.Run("ProcessTracker Contract", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		tracker := NewStateTracker(logger)

		// Test contract behavior
		processes := []PHPFPMProcess{
			{PID: 1001, State: ProcessStateRunning, RequestDuration: 100},
			{PID: 1002, State: ProcessStateIdle, RequestDuration: 0},
		}

		pollTime := time.Now()
		tracker.UpdateStates(processes, pollTime)

		// Should be able to retrieve state
		state, exists := tracker.GetProcessState(1001)
		if !exists {
			t.Error("Process state should exist after update")
		}
		if state.PID != 1001 {
			t.Errorf("Expected PID 1001, got %d", state.PID)
		}

		// Should be able to get all states
		allStates := tracker.GetProcessStates()
		if len(allStates) != 2 {
			t.Errorf("Expected 2 process states, got %d", len(allStates))
		}
	})

	t.Run("EventDetector Contract", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		config := EventDetectorConfig{
			EnableStateTransitions:  true,
			EnableRequestBoundaries: true,
			ConfidenceThreshold:     0.75,
		}
		detector := NewEventDetector(config, logger)

		// Test contract behavior - should handle empty inputs gracefully
		events := detector.DetectEvents(
			make(map[int]*ProcessStateRecord),
			[]PHPFPMProcess{},
			time.Now(),
		)

		// DetectEvents should return a valid slice (can be empty)
		if events == nil {
			t.Error("DetectEvents should return non-nil slice, even if empty")
		} else if len(events) != 0 {
			t.Logf("DetectEvents returned %d events (expected 0 for empty input)", len(events))
		}
	})

	t.Run("IntervalManager Contract", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		config := IntervalManagerConfig{
			BaseInterval:      200 * time.Millisecond,
			MinInterval:       10 * time.Millisecond,
			MaxInterval:       2 * time.Second,
			BurstDuration:     2 * time.Second,
			SmoothingFactor:   0.25,
			ActivityThreshold: 0.1,
		}
		manager := NewIntervalManager(config, logger)

		// Test contract behavior
		metrics := &WorkerProcessMetrics{
			ActiveWorkers: 5,
			TotalWorkers:  10,
			QueueDepth:    0,
		}

		analysis := &TimingAnalysis{
			Events: []*TimingEvent{},
		}

		interval := manager.CalculateInterval(metrics, analysis, ActivityLevelMedium)

		// Should return a valid interval within bounds
		if interval < config.MinInterval || interval > config.MaxInterval {
			t.Errorf("Interval %v outside bounds [%v, %v]",
				interval, config.MinInterval, config.MaxInterval)
		}
	})
}

// BenchmarkInterfaceUsage measures performance impact of interface usage
func BenchmarkInterfaceUsage(b *testing.B) {
	logger := zaptest.NewLogger(b)

	b.Run("DirectCall", func(b *testing.B) {
		tracker := NewStateTracker(logger)

		for i := 0; i < b.N; i++ {
			tracker.Reset()
		}
	})

	b.Run("InterfaceCall", func(b *testing.B) {
		var tracker ProcessTracker = NewStateTracker(logger)

		for i := 0; i < b.N; i++ {
			tracker.Reset()
		}
	})
}
