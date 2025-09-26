package metrics

import (
	"os"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestUnifiedTimingCollector_correlateWithSystemMetrics(t *testing.T) {
	logger := zap.NewNop()

	// Create a test collector
	config := DefaultUnifiedConfig()
	config.EnableSystemCorrelation = true

	collector, err := NewUnifiedTimingCollector(config, nil, logger)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Create test timing analysis
	analysis := &TimingAnalysis{
		PollTime:   time.Now(),
		Events:     make([]*TimingEvent, 0),
		Confidence: 0.8,
	}

	// Create test timing events
	event1 := &TimingEvent{
		PID:        1234,
		Type:       EventTypeRequestStart,
		Timestamp:  time.Now(),
		Confidence: 0.9,
		Duration:   100 * time.Millisecond,
	}
	event1.ProcessID = event1.PID // Set alias

	event2 := &TimingEvent{
		PID:        5678,
		Type:       EventTypeRequestEnd,
		Timestamp:  time.Now(),
		Confidence: 0.8,
		Duration:   200 * time.Millisecond,
	}
	event2.ProcessID = event2.PID // Set alias

	analysis.Events = []*TimingEvent{event1, event2}

	// Create test PHP-FPM processes
	processes := []PHPFPMProcess{
		{
			PID:               1234,
			State:             ProcessStateRunning,
			StartTime:         time.Now().Unix() - 3600,
			StartSince:        3600,
			Requests:          10,
			RequestDuration:   50000, // microseconds
			LastRequestCPU:    25.5,
			LastRequestMemory: 1024000,
		},
		{
			PID:               5678,
			State:             ProcessStateIdle,
			StartTime:         time.Now().Unix() - 1800,
			StartSince:        1800,
			Requests:          5,
			RequestDuration:   0,
			LastRequestCPU:    0.1,
			LastRequestMemory: 512000,
		},
	}

	// Skip test on systems without /proc filesystem
	if _, err := os.Stat("/proc"); os.IsNotExist(err) {
		t.Skip("Skipping system metrics correlation test: /proc not available")
	}

	// Test correlation (this will try to read /proc/PID/stat which may not exist for test PIDs)
	// but we're mainly testing that the function runs without errors
	collector.correlateWithSystemMetrics(analysis, processes)

	// Verify that the function ran without panicking
	// and that metadata was added if any processes were found
	if analysis.Metadata != nil {
		if systemCorr, exists := analysis.Metadata["system_correlation"]; exists {
			corrMap, ok := systemCorr.(map[string]interface{})
			if ok {
				if processCount, exists := corrMap["process_count"]; exists {
					t.Logf("System correlation processed %v processes", processCount)
				}
			}
		}
	}

	// Test that ProcessID is set correctly
	for _, event := range analysis.Events {
		if event.ProcessID != event.PID {
			t.Errorf("ProcessID alias not set correctly: expected %d, got %d", event.PID, event.ProcessID)
		}
	}
}

func TestUnifiedTimingCollector_readProcStat(t *testing.T) {
	logger := zap.NewNop()

	config := DefaultUnifiedConfig()
	collector, err := NewUnifiedTimingCollector(config, nil, logger)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Skip test on systems without /proc filesystem
	if _, err := os.Stat("/proc"); os.IsNotExist(err) {
		t.Skip("Skipping readProcStat test: /proc not available")
	}

	// Test with self PID (should work on Linux systems)
	selfPID := os.Getpid()
	procStat, err := collector.readProcStat(selfPID)
	if err != nil {
		t.Fatalf("Failed to read proc stat for self PID %d: %v", selfPID, err)
	}

	// Verify the parsed data
	if procStat.PID != selfPID {
		t.Errorf("Expected PID %d, got %d", selfPID, procStat.PID)
	}

	if procStat.UTime < 0 {
		t.Errorf("Expected non-negative UTime, got %v", procStat.UTime)
	}

	if procStat.STime < 0 {
		t.Errorf("Expected non-negative STime, got %v", procStat.STime)
	}

	if procStat.RSS <= 0 {
		t.Errorf("Expected positive RSS, got %d", procStat.RSS)
	}

	if procStat.VSize <= 0 {
		t.Errorf("Expected positive VSize, got %d", procStat.VSize)
	}

	t.Logf("ProcStat for PID %d: UTime=%v, STime=%v, RSS=%d, VSize=%d",
		procStat.PID, procStat.UTime, procStat.STime, procStat.RSS, procStat.VSize)
}

func TestUnifiedTimingCollector_readProcStat_InvalidPID(t *testing.T) {
	logger := zap.NewNop()

	config := DefaultUnifiedConfig()
	collector, err := NewUnifiedTimingCollector(config, nil, logger)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Skip test on systems without /proc filesystem
	if _, err := os.Stat("/proc"); os.IsNotExist(err) {
		t.Skip("Skipping readProcStat invalid PID test: /proc not available")
	}

	// Test with invalid PID
	_, err = collector.readProcStat(999999)
	if err == nil {
		t.Error("Expected error for invalid PID, got nil")
	}
}

func TestProcessSystemMetrics_Structure(t *testing.T) {
	// Test ProcessSystemMetrics structure
	metrics := &ProcessSystemMetrics{
		PID:             1234,
		State:           "Running",
		CPUUserTime:     100 * time.Millisecond,
		CPUSystemTime:   50 * time.Millisecond,
		MemoryRSS:       1024,
		MemoryVSize:     4096,
		StartTime:       time.Now().Unix(),
		Timestamp:       time.Now(),
		CPUUsageRatio:   0.5,
		MemoryIntensity: 10.24,
	}

	if metrics.PID != 1234 {
		t.Errorf("Expected PID 1234, got %d", metrics.PID)
	}

	if metrics.State != "Running" {
		t.Errorf("Expected state 'Running', got '%s'", metrics.State)
	}

	if metrics.CPUUserTime != 100*time.Millisecond {
		t.Errorf("Expected CPUUserTime 100ms, got %v", metrics.CPUUserTime)
	}

	if metrics.CPUSystemTime != 50*time.Millisecond {
		t.Errorf("Expected CPUSystemTime 50ms, got %v", metrics.CPUSystemTime)
	}

	if metrics.MemoryRSS != 1024 {
		t.Errorf("Expected MemoryRSS 1024, got %d", metrics.MemoryRSS)
	}

	if metrics.MemoryVSize != 4096 {
		t.Errorf("Expected MemoryVSize 4096, got %d", metrics.MemoryVSize)
	}

	if metrics.CPUUsageRatio != 0.5 {
		t.Errorf("Expected CPUUsageRatio 0.5, got %f", metrics.CPUUsageRatio)
	}

	if metrics.MemoryIntensity != 10.24 {
		t.Errorf("Expected MemoryIntensity 10.24, got %f", metrics.MemoryIntensity)
	}
}

func TestTimingEvent_SystemMetrics(t *testing.T) {
	// Test that TimingEvent can store system metrics
	event := &TimingEvent{
		PID:        1234,
		Type:       EventTypeRequestStart,
		Timestamp:  time.Now(),
		Confidence: 0.9,
		Duration:   100 * time.Millisecond,
	}

	// Test initial state
	if event.SystemMetrics != nil {
		t.Error("Expected initial SystemMetrics to be nil")
	}

	// Add system metrics
	event.SystemMetrics = &ProcessSystemMetrics{
		PID:             1234,
		State:           "Running",
		CPUUserTime:     100 * time.Millisecond,
		CPUSystemTime:   50 * time.Millisecond,
		MemoryRSS:       1024,
		MemoryVSize:     4096,
		StartTime:       time.Now().Unix(),
		Timestamp:       time.Now(),
		CPUUsageRatio:   0.5,
		MemoryIntensity: 10.24,
	}

	// Verify system metrics are stored
	if event.SystemMetrics == nil {
		t.Fatal("Expected SystemMetrics to be set")
	}

	if event.SystemMetrics.PID != 1234 {
		t.Errorf("Expected SystemMetrics PID 1234, got %d", event.SystemMetrics.PID)
	}

	if event.SystemMetrics.State != "Running" {
		t.Errorf("Expected SystemMetrics state 'Running', got '%s'", event.SystemMetrics.State)
	}

	// Test Reset clears system metrics
	event.Reset()
	if event.SystemMetrics != nil {
		t.Error("Expected Reset to clear SystemMetrics")
	}
}
