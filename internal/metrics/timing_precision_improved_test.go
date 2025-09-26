package metrics

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

// TestPrecisionTimingCollector validates the improved timing collector
func TestPrecisionTimingCollector(t *testing.T) {
	t.Run("Initialization", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		config := DefaultCollectorConfig()
		phpfpmClient := &PHPFPMClient{
			client: &http.Client{},
			logger: logger,
		}

		collector, err := NewPrecisionTimingCollector(config, phpfpmClient, logger)
		if err != nil {
			t.Fatalf("Failed to create collector: %v", err)
		}

		// Verify initial state
		if collector.GetCurrentInterval() != config.BaseInterval {
			t.Errorf("Expected base interval %v, got %v",
				config.BaseInterval, collector.GetCurrentInterval())
		}

		stats := collector.Stats()
		if stats.TotalCollections != 0 {
			t.Errorf("Expected 0 collections, got %d", stats.TotalCollections)
		}
	})

	t.Run("InvalidConfiguration", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		phpfpmClient := &PHPFPMClient{
			client: &http.Client{},
			logger: logger,
		}

		// Test invalid base interval
		config := DefaultCollectorConfig()
		config.BaseInterval = 0
		_, err := NewPrecisionTimingCollector(config, phpfpmClient, logger)
		if err == nil {
			t.Error("Expected error for zero base interval")
		}

		// Test invalid min interval
		config = DefaultCollectorConfig()
		config.MinInterval = config.BaseInterval + 1
		_, err = NewPrecisionTimingCollector(config, phpfpmClient, logger)
		if err == nil {
			t.Error("Expected error for min interval > base interval")
		}

		// Test invalid max interval
		config = DefaultCollectorConfig()
		config.MaxInterval = config.BaseInterval - 1
		_, err = NewPrecisionTimingCollector(config, phpfpmClient, logger)
		if err == nil {
			t.Error("Expected error for max interval < base interval")
		}
	})

	t.Run("StateTransitionDetection", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		config := DefaultCollectorConfig()
		config.TransitionDetectionEnabled = true

		phpfpmClient := &PHPFPMClient{
			client: &http.Client{},
			logger: logger,
		}

		collector, err := NewPrecisionTimingCollector(config, phpfpmClient, logger)
		if err != nil {
			t.Fatalf("Failed to create collector: %v", err)
		}

		// Simulate process state transitions
		processes1 := []PHPFPMProcess{
			{PID: 1001, State: ProcessStateIdle, RequestDuration: 0},
		}

		analysis1 := collector.analyzeTimingWithLock(processes1, time.Now())
		if len(analysis1.Events) != 0 {
			t.Errorf("Expected no events for initial state, got %d", len(analysis1.Events))
		}

		// Process starts request
		time.Sleep(100 * time.Millisecond)
		processes2 := []PHPFPMProcess{
			{PID: 1001, State: ProcessStateRunning, RequestDuration: 50, RequestURI: "/api/test"},
		}

		analysis2 := collector.analyzeTimingWithLock(processes2, time.Now())
		if len(analysis2.Events) != 1 {
			t.Fatalf("Expected 1 event for state transition, got %d", len(analysis2.Events))
		}

		event := analysis2.Events[0]
		if event.Type != EventTypeStateChange {
			t.Errorf("Expected StateChange event, got %v", event.Type)
		}
		if event.Confidence < 0.5 {
			t.Errorf("Expected confidence > 0.5, got %.2f", event.Confidence)
		}
	})

	t.Run("RequestBoundaryDetection", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		config := DefaultCollectorConfig()

		phpfpmClient := &PHPFPMClient{
			client: &http.Client{},
			logger: logger,
		}

		collector, err := NewPrecisionTimingCollector(config, phpfpmClient, logger)
		if err != nil {
			t.Fatalf("Failed to create collector: %v", err)
		}

		// First poll: Running request
		processes1 := []PHPFPMProcess{
			{PID: 2001, State: ProcessStateRunning, RequestDuration: 150, RequestURI: "/api/users"},
		}
		collector.analyzeTimingWithLock(processes1, time.Now())

		// Second poll: Request duration decreased (new request)
		time.Sleep(50 * time.Millisecond)
		processes2 := []PHPFPMProcess{
			{PID: 2001, State: ProcessStateRunning, RequestDuration: 30, RequestURI: "/api/products"},
		}

		analysis := collector.analyzeTimingWithLock(processes2, time.Now())

		// Should detect request end and start
		eventTypes := make(map[EventType]int)
		for _, event := range analysis.Events {
			eventTypes[event.Type]++
		}

		if eventTypes[EventTypeRequestEnd] != 1 {
			t.Errorf("Expected 1 RequestEnd event, got %d", eventTypes[EventTypeRequestEnd])
		}
		if eventTypes[EventTypeRequestStart] != 1 {
			t.Errorf("Expected 1 RequestStart event, got %d", eventTypes[EventTypeRequestStart])
		}
	})

	t.Run("AdaptivePolling", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		config := DefaultCollectorConfig()
		config.AdaptiveEnabled = true

		phpfpmClient := &PHPFPMClient{
			client: &http.Client{},
			logger: logger,
		}

		collector, err := NewPrecisionTimingCollector(config, phpfpmClient, logger)
		if err != nil {
			t.Fatalf("Failed to create collector: %v", err)
		}

		// Test different activity levels
		testCases := []struct {
			name          string
			metrics       *WorkerProcessMetrics
			expectedLevel ActivityLevel
		}{
			{
				name: "Idle",
				metrics: &WorkerProcessMetrics{
					ActiveWorkers: 0,
					TotalWorkers:  10,
					QueueDepth:    0,
				},
				expectedLevel: ActivityLevelIdle,
			},
			{
				name: "Low",
				metrics: &WorkerProcessMetrics{
					ActiveWorkers: 2,
					TotalWorkers:  10,
					QueueDepth:    0,
				},
				expectedLevel: ActivityLevelLow,
			},
			{
				name: "Medium",
				metrics: &WorkerProcessMetrics{
					ActiveWorkers: 5,
					TotalWorkers:  10,
					QueueDepth:    2,
				},
				expectedLevel: ActivityLevelMedium,
			},
			{
				name: "High",
				metrics: &WorkerProcessMetrics{
					ActiveWorkers: 8,
					TotalWorkers:  10,
					QueueDepth:    10,
				},
				expectedLevel: ActivityLevelHigh,
			},
			{
				name: "Critical",
				metrics: &WorkerProcessMetrics{
					ActiveWorkers: 10,
					TotalWorkers:  10,
					QueueDepth:    50,
				},
				expectedLevel: ActivityLevelCritical,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				level := collector.calculateActivityLevel(tc.metrics)
				if level != tc.expectedLevel {
					t.Errorf("Expected activity level %v, got %v", tc.expectedLevel, level)
				}
			})
		}
	})

	t.Run("BurstMode", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		config := DefaultCollectorConfig()
		config.AdaptiveEnabled = true
		config.MinInterval = 10 * time.Millisecond
		config.BurstDuration = 100 * time.Millisecond

		phpfpmClient := &PHPFPMClient{
			client: &http.Client{},
			logger: logger,
		}

		collector, err := NewPrecisionTimingCollector(config, phpfpmClient, logger)
		if err != nil {
			t.Fatalf("Failed to create collector: %v", err)
		}

		// Trigger burst mode with multiple events
		analysis := &TimingAnalysis{
			Events: []*TimingEvent{
				{Type: EventTypeRequestStart},
				{Type: EventTypeRequestEnd},
				{Type: EventTypeStateChange},
			},
		}

		if !collector.shouldEnterBurstMode(analysis, ActivityLevelMedium) {
			t.Error("Expected burst mode to be triggered with multiple events")
		}

		collector.enterBurstMode()

		// Verify burst mode settings
		if collector.GetCurrentInterval() != config.MinInterval {
			t.Errorf("Expected interval %v in burst mode, got %v",
				config.MinInterval, collector.GetCurrentInterval())
		}

		// Verify burst mode expires
		burstUntil := collector.burstModeUntil.Load().(time.Time)
		if time.Until(burstUntil) > config.BurstDuration {
			t.Error("Burst mode duration incorrect")
		}
	})

	t.Run("ConfidenceCalculation", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		config := DefaultCollectorConfig()

		phpfpmClient := &PHPFPMClient{
			client: &http.Client{},
			logger: logger,
		}

		collector, err := NewPrecisionTimingCollector(config, phpfpmClient, logger)
		if err != nil {
			t.Fatalf("Failed to create collector: %v", err)
		}

		testCases := []struct {
			interval      time.Duration
			minConfidence float64
		}{
			{10 * time.Millisecond, 0.90},
			{50 * time.Millisecond, 0.80},
			{100 * time.Millisecond, 0.70},
			{200 * time.Millisecond, 0.60},
			{1 * time.Second, 0.35},
		}

		for _, tc := range testCases {
			confidence := collector.calculateEventConfidence(tc.interval)
			if confidence < tc.minConfidence {
				t.Errorf("Interval %v: expected confidence >= %.2f, got %.2f",
					tc.interval, tc.minConfidence, confidence)
			}
		}
	})

	t.Run("ProcessCleanup", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		config := DefaultCollectorConfig()

		phpfpmClient := &PHPFPMClient{
			client: &http.Client{},
			logger: logger,
		}

		collector, err := NewPrecisionTimingCollector(config, phpfpmClient, logger)
		if err != nil {
			t.Fatalf("Failed to create collector: %v", err)
		}

		// Add multiple processes
		processes1 := []PHPFPMProcess{
			{PID: 3001, State: ProcessStateRunning},
			{PID: 3002, State: ProcessStateIdle},
			{PID: 3003, State: ProcessStateRunning},
		}
		collector.analyzeTimingWithLock(processes1, time.Now())

		// Verify all processes are tracked
		collector.mu.RLock()
		if len(collector.processStates) != 3 {
			t.Errorf("Expected 3 tracked processes, got %d", len(collector.processStates))
		}
		collector.mu.RUnlock()

		// Remove one process
		processes2 := []PHPFPMProcess{
			{PID: 3001, State: ProcessStateRunning},
			{PID: 3003, State: ProcessStateIdle},
		}
		collector.analyzeTimingWithLock(processes2, time.Now())

		// Verify cleanup
		collector.mu.RLock()
		if len(collector.processStates) != 2 {
			t.Errorf("Expected 2 tracked processes after cleanup, got %d", len(collector.processStates))
		}
		if _, exists := collector.processStates[3002]; exists {
			t.Error("Process 3002 should have been cleaned up")
		}
		collector.mu.RUnlock()
	})
}

// TestCircularBuffer validates the circular buffer implementation
func TestCircularBuffer(t *testing.T) {
	t.Run("BasicOperations", func(t *testing.T) {
		buffer := NewCircularBuffer(5)

		// Add items
		for i := 1; i <= 5; i++ {
			buffer.Add(i)
		}

		if buffer.Size() != 5 {
			t.Errorf("Expected size 5, got %d", buffer.Size())
		}

		// Get all items
		items := buffer.GetAll()
		if len(items) != 5 {
			t.Errorf("Expected 5 items, got %d", len(items))
		}

		// Add more items (should overwrite old ones)
		buffer.Add(6)
		buffer.Add(7)

		items = buffer.GetAll()
		if len(items) != 5 {
			t.Errorf("Buffer should maintain size 5, got %d", len(items))
		}

		// First item should now be 3 (1 and 2 were overwritten)
		if items[0].(int) != 3 {
			t.Errorf("Expected first item to be 3, got %v", items[0])
		}
	})

	t.Run("GetRecent", func(t *testing.T) {
		buffer := NewCircularBuffer(10)

		for i := 1; i <= 15; i++ {
			buffer.Add(i)
		}

		recent := buffer.GetRecent(3)
		if len(recent) != 3 {
			t.Errorf("Expected 3 recent items, got %d", len(recent))
		}

		// Should get 13, 14, 15
		expected := []int{13, 14, 15}
		for i, item := range recent {
			if item.(int) != expected[i] {
				t.Errorf("Expected recent[%d] = %d, got %v", i, expected[i], item)
			}
		}
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		buffer := NewCircularBuffer(100)
		var wg sync.WaitGroup

		// Multiple writers
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					buffer.Add(id*1000 + j)
				}
			}(i)
		}

		// Multiple readers
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 50; j++ {
					buffer.GetAll()
					buffer.GetRecent(10)
					time.Sleep(time.Microsecond)
				}
			}()
		}

		wg.Wait()

		// Buffer should be full but not corrupted
		if buffer.Size() != 100 {
			t.Errorf("Expected buffer size 100, got %d", buffer.Size())
		}
	})

	t.Run("Clear", func(t *testing.T) {
		buffer := NewCircularBuffer(5)

		for i := 1; i <= 5; i++ {
			buffer.Add(i)
		}

		buffer.Clear()

		if buffer.Size() != 0 {
			t.Errorf("Expected size 0 after clear, got %d", buffer.Size())
		}

		items := buffer.GetAll()
		if len(items) != 0 {
			t.Errorf("Expected 0 items after clear, got %d", len(items))
		}
	})
}

// BenchmarkPrecisionTimingCollector measures performance
func BenchmarkPrecisionTimingCollector(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultCollectorConfig()
	phpfpmClient := &PHPFPMClient{
		client: &http.Client{},
		logger: logger,
	}

	collector, err := NewPrecisionTimingCollector(config, phpfpmClient, logger)
	if err != nil {
		b.Fatalf("Failed to create collector: %v", err)
	}

	// Prepare test data
	processes := make([]PHPFPMProcess, 100)
	for i := 0; i < 100; i++ {
		state := ProcessStateIdle
		duration := int64(0)
		if i%3 == 0 {
			state = ProcessStateRunning
			duration = int64(100 + i%500)
		}
		processes[i] = PHPFPMProcess{
			PID:             1000 + i,
			State:           state,
			RequestDuration: duration,
			RequestURI:      "/api/test",
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		collector.analyzeTimingWithLock(processes, time.Now())
	}
}

// BenchmarkCircularBuffer measures circular buffer performance
func BenchmarkCircularBuffer(b *testing.B) {
	buffer := NewCircularBuffer(1000)

	b.Run("Add", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			buffer.Add(i)
		}
	})

	b.Run("GetAll", func(b *testing.B) {
		// Fill buffer first
		for i := 0; i < 1000; i++ {
			buffer.Add(i)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buffer.GetAll()
		}
	})

	b.Run("GetRecent", func(b *testing.B) {
		// Fill buffer first
		for i := 0; i < 1000; i++ {
			buffer.Add(i)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buffer.GetRecent(10)
		}
	})
}
