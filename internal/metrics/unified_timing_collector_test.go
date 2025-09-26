package metrics

import (
	"context"
	"net/http"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

func TestUnifiedTimingCollectorInitialization(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultUnifiedConfig()

	phpfpmClient := &PHPFPMClient{
		client: &http.Client{},
		logger: logger,
	}

	collector, err := NewUnifiedTimingCollector(config, phpfpmClient, logger)
	if err != nil {
		t.Fatalf("Failed to create unified timing collector: %v", err)
	}

	// Verify configuration
	if collector.mode != config.Mode {
		t.Errorf("Expected mode %v, got %v", config.Mode, collector.mode)
	}

	if collector.GetCurrentInterval() != config.BaseInterval {
		t.Errorf("Expected interval %v, got %v", config.BaseInterval, collector.GetCurrentInterval())
	}

	// Verify components are initialized
	if collector.stateTracker == nil {
		t.Error("State tracker not initialized")
	}

	if collector.eventDetector == nil {
		t.Error("Event detector not initialized")
	}

	if config.EnableAdaptivePolling && collector.intervalManager == nil {
		t.Error("Interval manager not initialized with adaptive polling enabled")
	}

	if config.EnableInternalMetrics && collector.metricsCollector == nil {
		t.Error("Metrics collector not initialized with internal metrics enabled")
	}
}

func TestUnifiedTimingCollectorModes(t *testing.T) {
	testCases := []struct {
		name string
		mode CollectorMode
	}{
		{"Precise Mode", ModePrecise},
		{"Adaptive Mode", ModeAdaptive},
		{"Hybrid Mode", ModeHybrid},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			logger := zaptest.NewLogger(t)
			config := DefaultUnifiedConfig()
			config.Mode = tc.mode

			phpfpmClient := &PHPFPMClient{
				client: &http.Client{},
				logger: logger,
			}

			collector, err := NewUnifiedTimingCollector(config, phpfpmClient, logger)
			if err != nil {
				t.Fatalf("Failed to create collector for mode %v: %v", tc.mode, err)
			}

			if collector.mode != tc.mode {
				t.Errorf("Expected mode %v, got %v", tc.mode, collector.mode)
			}
		})
	}
}

func TestUnifiedTimingCollectorConfiguration(t *testing.T) {
	t.Run("ValidConfiguration", func(t *testing.T) {
		config := DefaultUnifiedConfig()
		err := config.Validate()
		if err != nil {
			t.Errorf("Default configuration should be valid: %v", err)
		}
	})

	t.Run("InvalidBaseInterval", func(t *testing.T) {
		config := DefaultUnifiedConfig()
		config.BaseInterval = 0
		err := config.Validate()
		if err == nil {
			t.Error("Expected error for zero base interval")
		}
	})

	t.Run("InvalidMinInterval", func(t *testing.T) {
		config := DefaultUnifiedConfig()
		config.MinInterval = config.BaseInterval + time.Millisecond
		err := config.Validate()
		if err == nil {
			t.Error("Expected error for min interval > base interval")
		}
	})

	t.Run("InvalidMaxInterval", func(t *testing.T) {
		config := DefaultUnifiedConfig()
		config.MaxInterval = config.BaseInterval - time.Millisecond
		err := config.Validate()
		if err == nil {
			t.Error("Expected error for max interval < base interval")
		}
	})

	t.Run("InvalidConfidenceThreshold", func(t *testing.T) {
		config := DefaultUnifiedConfig()
		config.ConfidenceThreshold = 1.5
		err := config.Validate()
		if err == nil {
			t.Error("Expected error for confidence threshold > 1")
		}
	})
}

func TestUnifiedTimingCollectorStats(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultUnifiedConfig()

	phpfpmClient := &PHPFPMClient{
		client: &http.Client{},
		logger: logger,
	}

	collector, err := NewUnifiedTimingCollector(config, phpfpmClient, logger)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Get initial stats
	stats := collector.Stats()
	if stats.TotalCollections != 0 {
		t.Errorf("Expected 0 initial collections, got %d", stats.TotalCollections)
	}
}

func TestUnifiedTimingCollectorIntervalManagement(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultUnifiedConfig()
	config.Mode = ModePrecise // Use precise mode for manual interval setting

	phpfpmClient := &PHPFPMClient{
		client: &http.Client{},
		logger: logger,
	}

	collector, err := NewUnifiedTimingCollector(config, phpfpmClient, logger)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Test manual interval setting in precise mode
	newInterval := 100 * time.Millisecond
	err = collector.SetInterval(newInterval)
	if err != nil {
		t.Errorf("Failed to set interval: %v", err)
	}

	if collector.GetCurrentInterval() != newInterval {
		t.Errorf("Expected interval %v, got %v", newInterval, collector.GetCurrentInterval())
	}

	// Test interval bounds
	err = collector.SetInterval(config.MinInterval - time.Millisecond)
	if err == nil {
		t.Error("Expected error for interval below minimum")
	}

	err = collector.SetInterval(config.MaxInterval + time.Millisecond)
	if err == nil {
		t.Error("Expected error for interval above maximum")
	}
}

func TestUnifiedTimingCollectorModeChanges(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultUnifiedConfig()

	phpfpmClient := &PHPFPMClient{
		client: &http.Client{},
		logger: logger,
	}

	collector, err := NewUnifiedTimingCollector(config, phpfpmClient, logger)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Test mode change when not running
	err = collector.SetMode(ModePrecise)
	if err != nil {
		t.Errorf("Failed to change mode when not running: %v", err)
	}

	// Start collector
	ctx := context.Background()
	err = collector.Start(ctx)
	if err != nil {
		t.Errorf("Failed to start collector: %v", err)
	}

	// Test mode change when running (should fail)
	err = collector.SetMode(ModeAdaptive)
	if err == nil {
		t.Error("Expected error when changing mode while running")
	}

	// Stop collector
	err = collector.Stop()
	if err != nil {
		t.Errorf("Failed to stop collector: %v", err)
	}
}

func TestUnifiedTimingCollectorLifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultUnifiedConfig()

	phpfpmClient := &PHPFPMClient{
		client: &http.Client{},
		logger: logger,
	}

	collector, err := NewUnifiedTimingCollector(config, phpfpmClient, logger)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Test starting
	ctx := context.Background()
	err = collector.Start(ctx)
	if err != nil {
		t.Errorf("Failed to start collector: %v", err)
	}

	// Test double start
	err = collector.Start(ctx)
	if err == nil {
		t.Error("Expected error on double start")
	}

	// Test stopping
	err = collector.Stop()
	if err != nil {
		t.Errorf("Failed to stop collector: %v", err)
	}

	// Test double stop
	err = collector.Stop()
	if err == nil {
		t.Error("Expected error on double stop")
	}
}

func TestUnifiedTimingCollectorBurstMode(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultUnifiedConfig()
	config.BurstDuration = 100 * time.Millisecond

	phpfpmClient := &PHPFPMClient{
		client: &http.Client{},
		logger: logger,
	}

	collector, err := NewUnifiedTimingCollector(config, phpfpmClient, logger)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Test entering burst mode
	collector.enterBurstMode()

	// Check interval changed to minimum
	if collector.GetCurrentInterval() != config.MinInterval {
		t.Errorf("Expected burst interval %v, got %v",
			config.MinInterval, collector.GetCurrentInterval())
	}

	// Check burst mode is active
	if !collector.inBurstMode() {
		t.Error("Expected to be in burst mode")
	}

	// Wait for burst mode to expire
	time.Sleep(config.BurstDuration + 10*time.Millisecond)

	// Check burst mode expired
	if collector.inBurstMode() {
		t.Error("Expected burst mode to expire")
	}
}

func TestActivityLevelString(t *testing.T) {
	testCases := []struct {
		level    ActivityLevel
		expected string
	}{
		{ActivityLevelIdle, "idle"},
		{ActivityLevelLow, "low"},
		{ActivityLevelMedium, "medium"},
		{ActivityLevelHigh, "high"},
		{ActivityLevelCritical, "critical"},
		{ActivityLevel(99), "unknown"},
	}

	for _, tc := range testCases {
		result := tc.level.String()
		if result != tc.expected {
			t.Errorf("Expected %q, got %q for level %d", tc.expected, result, tc.level)
		}
	}
}

func TestCollectorModeString(t *testing.T) {
	testCases := []struct {
		mode     CollectorMode
		expected string
	}{
		{ModePrecise, "precise"},
		{ModeAdaptive, "adaptive"},
		{ModeHybrid, "hybrid"},
		{CollectorMode(99), "unknown"},
	}

	for _, tc := range testCases {
		result := tc.mode.String()
		if result != tc.expected {
			t.Errorf("Expected %q, got %q for mode %d", tc.expected, result, tc.mode)
		}
	}
}

// Benchmark tests for performance validation
func BenchmarkUnifiedTimingCollectorCreation(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultUnifiedConfig()

	phpfpmClient := &PHPFPMClient{
		client: &http.Client{},
		logger: logger,
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		collector, err := NewUnifiedTimingCollector(config, phpfpmClient, logger)
		if err != nil {
			b.Fatalf("Failed to create collector: %v", err)
		}
		_ = collector
	}
}

func BenchmarkUnifiedTimingCollectorModeExecution(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultUnifiedConfig()

	phpfpmClient := &PHPFPMClient{
		client: &http.Client{},
		logger: logger,
	}

	collector, err := NewUnifiedTimingCollector(config, phpfpmClient, logger)
	if err != nil {
		b.Fatalf("Failed to create collector: %v", err)
	}

	// Prepare test data
	processes := []PHPFPMProcess{
		{PID: 1001, State: ProcessStateRunning, RequestDuration: 150},
		{PID: 1002, State: ProcessStateIdle, RequestDuration: 0},
		{PID: 1003, State: ProcessStateRunning, RequestDuration: 75},
	}

	pollTime := time.Now()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = collector.executePreciseMode(processes, pollTime)
	}
}
