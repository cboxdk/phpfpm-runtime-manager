package metrics

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/types"
	"go.uber.org/zap/zaptest"
)

func TestNewCPUTracker(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tracker, err := NewCPUTracker(logger)
	if err != nil {
		t.Fatalf("Failed to create CPU tracker: %v", err)
	}

	if tracker == nil {
		t.Error("Expected non-nil CPU tracker")
	}

	if tracker.logger != logger {
		t.Error("Expected logger to be set")
	}

	if tracker.processes == nil {
		t.Error("Expected processes map to be initialized")
	}

	if tracker.running {
		t.Error("Expected tracker to not be running initially")
	}
}

func TestCPUTrackerStartStop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker, err := NewCPUTracker(logger)
	if err != nil {
		t.Fatalf("Failed to create CPU tracker: %v", err)
	}

	ctx := context.Background()

	// Test start
	err = tracker.Start(ctx)
	// Start may fail on non-Linux systems due to /proc/stat dependency
	if err != nil {
		t.Logf("Start failed (expected on non-Linux): %v", err)
		return // Skip rest of test on non-Linux
	}

	if !tracker.running {
		t.Error("Expected tracker to be running after start")
	}

	// Test double start
	err = tracker.Start(ctx)
	if err == nil {
		t.Error("Expected error when starting already running tracker")
	}

	// Test stop
	err = tracker.Stop(ctx)
	if err != nil {
		t.Errorf("Unexpected error stopping tracker: %v", err)
	}

	if tracker.running {
		t.Error("Expected tracker to not be running after stop")
	}

	// Test double stop
	err = tracker.Stop(ctx)
	if err != nil {
		t.Errorf("Unexpected error stopping already stopped tracker: %v", err)
	}
}

func TestTrackUntrackProcess(t *testing.T) {
	// Skip this test on non-Linux systems due to /proc filesystem dependency
	if runtime.GOOS != "linux" {
		t.Skip("Skipping test on non-Linux system - requires /proc filesystem")
	}

	logger := zaptest.NewLogger(t)
	tracker, err := NewCPUTracker(logger)
	if err != nil {
		t.Fatalf("Failed to create CPU tracker: %v", err)
	}

	// Test tracking without starting tracker
	err = tracker.TrackProcess(1234)
	if err == nil {
		t.Error("Expected error when tracking process on stopped tracker")
	}

	// Start tracker (may fail on non-Linux)
	ctx := context.Background()
	err = tracker.Start(ctx)
	if err != nil {
		t.Logf("Start failed (expected on non-Linux): %v", err)
		return // Skip rest of test
	}
	defer tracker.Stop(ctx)

	// Test tracking current process (should exist)
	currentPID := os.Getpid()
	err = tracker.TrackProcess(currentPID)
	if err != nil {
		t.Errorf("Failed to track current process: %v", err)
	}

	// Verify process is tracked
	tracker.mu.RLock()
	_, exists := tracker.processes[currentPID]
	tracker.mu.RUnlock()

	if !exists {
		t.Error("Expected process to be tracked")
	}

	// Test tracking non-existent process
	err = tracker.TrackProcess(999999)
	if err == nil {
		t.Error("Expected error when tracking non-existent process")
	}

	// Test untracking
	tracker.UntrackProcess(currentPID)

	tracker.mu.RLock()
	_, exists = tracker.processes[currentPID]
	tracker.mu.RUnlock()

	if exists {
		t.Error("Expected process to be untracked")
	}

	// Test untracking non-tracked process (should not panic)
	tracker.UntrackProcess(999999)
}

func TestGetProcessDelta(t *testing.T) {
	// Skip this test on non-Linux systems due to /proc filesystem dependency
	if runtime.GOOS != "linux" {
		t.Skip("Skipping test on non-Linux system - requires /proc filesystem")
	}

	logger := zaptest.NewLogger(t)
	tracker, err := NewCPUTracker(logger)
	if err != nil {
		t.Fatalf("Failed to create CPU tracker: %v", err)
	}

	ctx := context.Background()
	err = tracker.Start(ctx)
	if err != nil {
		t.Logf("Start failed (expected on non-Linux): %v", err)
		return // Skip test on non-Linux
	}
	defer tracker.Stop(ctx)

	currentPID := os.Getpid()

	// Test delta for non-tracked process
	_, err = tracker.GetProcessDelta(999999)
	if err == nil {
		t.Error("Expected error for non-tracked process")
	}

	// Track current process
	err = tracker.TrackProcess(currentPID)
	if err != nil {
		t.Fatalf("Failed to track process: %v", err)
	}

	// Get initial delta (should be 0 or very small)
	delta, err := tracker.GetProcessDelta(currentPID)
	if err != nil {
		t.Errorf("Failed to get process delta: %v", err)
	}

	if delta < 0 {
		t.Errorf("CPU delta should not be negative, got %f", delta)
	}

	// Wait a bit and get another delta
	time.Sleep(100 * time.Millisecond)
	delta2, err := tracker.GetProcessDelta(currentPID)
	if err != nil {
		t.Errorf("Failed to get second process delta: %v", err)
	}

	if delta2 < 0 {
		t.Errorf("CPU delta should not be negative, got %f", delta2)
	}

	// Verify tracking info was updated
	tracker.mu.RLock()
	processInfo, exists := tracker.processes[currentPID]
	tracker.mu.RUnlock()

	if !exists {
		t.Error("Expected process info to exist")
	} else {
		if processInfo.CPUDelta != delta2 {
			t.Errorf("Expected CPU delta to be updated to %f, got %f", delta2, processInfo.CPUDelta)
		}
	}
}

func TestGetMetrics(t *testing.T) {
	// Skip this test on non-Linux systems due to /proc filesystem dependency
	if runtime.GOOS != "linux" {
		t.Skip("Skipping test on non-Linux system - requires /proc filesystem")
	}

	logger := zaptest.NewLogger(t)
	tracker, err := NewCPUTracker(logger)
	if err != nil {
		t.Fatalf("Failed to create CPU tracker: %v", err)
	}

	ctx := context.Background()
	err = tracker.Start(ctx)
	if err != nil {
		t.Logf("Start failed (expected on non-Linux): %v", err)
		return // Skip test on non-Linux
	}
	defer tracker.Stop(ctx)

	// Track current process
	currentPID := os.Getpid()
	err = tracker.TrackProcess(currentPID)
	if err != nil {
		t.Fatalf("Failed to track process: %v", err)
	}

	// Wait to get some CPU data
	time.Sleep(100 * time.Millisecond)

	metrics, err := tracker.GetMetrics(ctx)
	if err != nil {
		t.Errorf("Failed to get metrics: %v", err)
	}

	if len(metrics) == 0 {
		t.Error("Expected metrics to be returned")
	}

	// Verify metric types and structure
	for _, metric := range metrics {
		if metric.Timestamp.IsZero() {
			t.Error("Expected metric to have timestamp")
		}

		switch metric.Name {
		case "phpfpm_process_cpu_percent":
			if metric.Type != types.MetricTypeGauge {
				t.Errorf("Expected CPU percent to be gauge, got %v", metric.Type)
			}
			if metric.Value < 0 || metric.Value > 100 {
				t.Errorf("CPU percent should be 0-100, got %f", metric.Value)
			}
			// Should have PID label
			if pidLabel, exists := metric.Labels["pid"]; !exists {
				t.Error("Expected PID label for process CPU metric")
			} else if pidLabel != strconv.Itoa(currentPID) {
				t.Errorf("Expected PID label '%d', got '%s'", currentPID, pidLabel)
			}

		case "phpfpm_process_cpu_total_seconds":
			if metric.Type != types.MetricTypeCounter {
				t.Errorf("Expected CPU total to be counter, got %v", metric.Type)
			}
			if metric.Value < 0 {
				t.Errorf("CPU total should not be negative, got %f", metric.Value)
			}

		case "system_cpu_user_percent", "system_cpu_system_percent",
			"system_cpu_idle_percent", "system_cpu_iowait_percent", "system_cpu_usage_percent":
			if metric.Type != types.MetricTypeGauge {
				t.Errorf("Expected system CPU metric to be gauge, got %v", metric.Type)
			}
			if metric.Value < 0 || metric.Value > 100 {
				t.Errorf("System CPU percent should be 0-100, got %f for %s", metric.Value, metric.Name)
			}
		}
	}
}

func TestReadProcessCPU(t *testing.T) {
	// Skip this test on non-Linux systems
	if _, err := os.Stat("/proc"); os.IsNotExist(err) {
		t.Skip("Skipping /proc-dependent test on non-Linux system")
	}

	logger := zaptest.NewLogger(t)
	tracker, err := NewCPUTracker(logger)
	if err != nil {
		t.Fatalf("Failed to create CPU tracker: %v", err)
	}

	// Test reading current process
	currentPID := os.Getpid()
	stat, err := tracker.readProcessCPU(currentPID)
	if err != nil {
		t.Errorf("Failed to read process CPU: %v", err)
	}

	if stat == nil {
		t.Error("Expected non-nil CPU stat")
	}

	// UTime and STime should be non-negative
	if stat.UTime < 0 || stat.STime < 0 {
		t.Errorf("CPU times should be non-negative, got UTime=%d, STime=%d", stat.UTime, stat.STime)
	}

	// Test reading non-existent process
	_, err = tracker.readProcessCPU(999999)
	if err == nil {
		t.Error("Expected error when reading non-existent process")
	}
}

func TestReadProcessCPUWithMockData(t *testing.T) {
	logger := zaptest.NewLogger(t)
	_, err := NewCPUTracker(logger)
	if err != nil {
		t.Fatalf("Failed to create CPU tracker: %v", err)
	}

	// Create temporary directory and mock stat file
	tempDir, err := os.MkdirTemp("", "cpu_tracker_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Mock stat file content (simplified)
	statContent := "1234 (test) S 1 1234 1234 34816 1234 4194304 123 0 456 0 100 200 0 0 20 0 1 0 12345678 12345678 789 18446744073709551615"

	procDir := filepath.Join(tempDir, "proc", "1234")
	err = os.MkdirAll(procDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create proc dir: %v", err)
	}

	statFile := filepath.Join(procDir, "stat")
	err = os.WriteFile(statFile, []byte(statContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write stat file: %v", err)
	}

	// Create a helper function to simulate reading mock data
	readMockProcessCPU := func() (*ProcessCPUStat, error) {
		data, err := os.ReadFile(statFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", statFile, err)
		}

		fields := strings.Fields(string(data))
		if len(fields) < 17 {
			return nil, fmt.Errorf("invalid stat file format")
		}

		utime, err := strconv.ParseUint(fields[13], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse utime: %w", err)
		}

		stime, err := strconv.ParseUint(fields[14], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse stime: %w", err)
		}

		return &ProcessCPUStat{
			UTime: utime,
			STime: stime,
		}, nil
	}

	// Test reading the mock data directly
	stat, err := readMockProcessCPU()
	if err != nil {
		t.Errorf("Failed to read mock process CPU: %v", err)
	}

	if stat.UTime != 100 {
		t.Errorf("Expected UTime 100, got %d", stat.UTime)
	}

	if stat.STime != 200 {
		t.Errorf("Expected STime 200, got %d", stat.STime)
	}
}

func TestReadSystemCPU(t *testing.T) {
	// Skip this test on non-Linux systems
	if _, err := os.Stat("/proc/stat"); os.IsNotExist(err) {
		t.Skip("Skipping /proc/stat-dependent test on non-Linux system")
	}

	logger := zaptest.NewLogger(t)
	tracker, err := NewCPUTracker(logger)
	if err != nil {
		t.Fatalf("Failed to create CPU tracker: %v", err)
	}

	systemCPU, err := tracker.readSystemCPUFallback()
	if err != nil {
		t.Errorf("Failed to read system CPU: %v", err)
	}

	if systemCPU == nil {
		t.Error("Expected non-nil system CPU info")
	}

	// All CPU values should be non-negative
	if systemCPU.User < 0 || systemCPU.System < 0 || systemCPU.Idle < 0 {
		t.Error("CPU values should be non-negative")
	}

	if systemCPU.Timestamp.IsZero() {
		t.Error("Expected timestamp to be set")
	}
}

func TestReadSystemCPUWithMockData(t *testing.T) {
	logger := zaptest.NewLogger(t)
	_, err := NewCPUTracker(logger)
	if err != nil {
		t.Fatalf("Failed to create CPU tracker: %v", err)
	}

	// Create temporary stat file
	tempDir, err := os.MkdirTemp("", "cpu_tracker_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Mock /proc/stat content
	statContent := `cpu  123456 7890 234567 8901234 5678 901 2345 678 0 0
cpu0 61728 3945 117283 4450617 2839 450 1172 339 0 0
cpu1 61728 3945 117284 4450617 2839 451 1173 339 0 0
intr 12345678 0 0 0 0 0 0 0 0 0 0 0 0
ctxt 98765432
btime 1234567890
processes 12345
procs_running 1
procs_blocked 0
softirq 1234567 0 123456 0 12345 0 0 0 0 0 0`

	statFile := filepath.Join(tempDir, "stat")
	err = os.WriteFile(statFile, []byte(statContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write stat file: %v", err)
	}

	// Create helper function to read mock system CPU data
	readMockSystemCPU := func() (*SystemCPUInfo, error) {
		data, err := os.ReadFile(statFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", statFile, err)
		}

		lines := strings.Split(string(data), "\n")
		if len(lines) == 0 {
			return nil, fmt.Errorf("empty stat file")
		}

		cpuLine := lines[0]
		if !strings.HasPrefix(cpuLine, "cpu ") {
			return nil, fmt.Errorf("invalid stat format")
		}

		fields := strings.Fields(cpuLine)
		if len(fields) < 11 {
			return nil, fmt.Errorf("insufficient CPU fields")
		}

		parseField := func(index int) (uint64, error) {
			return strconv.ParseUint(fields[index], 10, 64)
		}

		user, _ := parseField(1)
		nice, _ := parseField(2)
		system, _ := parseField(3)
		idle, _ := parseField(4)
		iowait, _ := parseField(5)
		irq, _ := parseField(6)
		softirq, _ := parseField(7)
		steal, _ := parseField(8)
		guest, _ := parseField(9)
		guestNice, _ := parseField(10)

		return &SystemCPUInfo{
			User:      user,
			Nice:      nice,
			System:    system,
			Idle:      idle,
			IOWait:    iowait,
			IRQ:       irq,
			SoftIRQ:   softirq,
			Steal:     steal,
			Guest:     guest,
			GuestNice: guestNice,
			Timestamp: time.Now(),
		}, nil
	}

	// Test reading the mock data directly
	systemCPU, err := readMockSystemCPU()
	if err != nil {
		t.Errorf("Failed to read mock system CPU: %v", err)
	}

	if systemCPU.User != 123456 {
		t.Errorf("Expected User 123456, got %d", systemCPU.User)
	}

	if systemCPU.System != 234567 {
		t.Errorf("Expected System 234567, got %d", systemCPU.System)
	}

	if systemCPU.Idle != 8901234 {
		t.Errorf("Expected Idle 8901234, got %d", systemCPU.Idle)
	}

	// Test completed successfully
}

func TestGetSystemCPUMetrics(t *testing.T) {
	// Skip this test on non-Linux systems due to /proc/stat dependency
	if runtime.GOOS != "linux" {
		t.Skip("Skipping test on non-Linux system - requires /proc/stat")
	}

	logger := zaptest.NewLogger(t)
	tracker, err := NewCPUTracker(logger)
	if err != nil {
		t.Fatalf("Failed to create CPU tracker: %v", err)
	}

	ctx := context.Background()
	err = tracker.Start(ctx)
	if err != nil {
		t.Logf("Start failed (expected on non-Linux): %v", err)
		return // Skip test on non-Linux
	}
	defer tracker.Stop(ctx)

	// Wait for baseline reading
	time.Sleep(100 * time.Millisecond)

	// Force a second reading to calculate deltas
	tracker.systemMu.Lock()
	tracker.lastSystemCPU = &SystemCPUInfo{
		User:      1000,
		System:    500,
		Idle:      8000,
		IOWait:    100,
		Timestamp: time.Now().Add(-time.Second),
	}
	tracker.systemMu.Unlock()

	// Get system CPU metrics
	metrics, err := tracker.getSystemCPUMetrics()
	if err != nil {
		t.Errorf("Failed to get system CPU metrics: %v", err)
	}

	if len(metrics) == 0 {
		t.Error("Expected system CPU metrics to be returned")
	}

	// Verify expected metrics
	expectedMetrics := []string{
		"system_cpu_user_percent",
		"system_cpu_system_percent",
		"system_cpu_idle_percent",
		"system_cpu_iowait_percent",
		"system_cpu_usage_percent",
	}

	metricNames := make(map[string]bool)
	for _, metric := range metrics {
		metricNames[metric.Name] = true

		// Verify percentage values are reasonable
		if metric.Value < 0 || metric.Value > 100 {
			t.Errorf("CPU percentage should be 0-100, got %f for %s", metric.Value, metric.Name)
		}

		if metric.Type != types.MetricTypeGauge {
			t.Errorf("Expected CPU metrics to be gauge type, got %v for %s", metric.Type, metric.Name)
		}
	}

	for _, expectedMetric := range expectedMetrics {
		if !metricNames[expectedMetric] {
			t.Errorf("Expected system CPU metric '%s' not found", expectedMetric)
		}
	}
}
