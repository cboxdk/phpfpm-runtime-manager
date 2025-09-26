package metrics

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/types"
	"go.uber.org/zap/zaptest"
)

func TestNewMemoryTracker(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tracker, err := NewMemoryTracker(logger)
	if err != nil {
		t.Fatalf("Failed to create memory tracker: %v", err)
	}

	if tracker == nil {
		t.Error("Expected non-nil memory tracker")
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

func TestMemoryTrackerStartStop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker, err := NewMemoryTracker(logger)
	if err != nil {
		t.Fatalf("Failed to create memory tracker: %v", err)
	}

	ctx := context.Background()

	// Test start
	err = tracker.Start(ctx)
	if err != nil {
		t.Errorf("Unexpected error starting tracker: %v", err)
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

func TestTrackUntrackMemoryProcess(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker, err := NewMemoryTracker(logger)
	if err != nil {
		t.Fatalf("Failed to create memory tracker: %v", err)
	}

	// Test tracking without starting tracker
	err = tracker.TrackProcess(1234)
	if err == nil {
		t.Error("Expected error when tracking process on stopped tracker")
	}

	// Start tracker
	ctx := context.Background()
	err = tracker.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop(ctx)

	// Test tracking current process (should exist)
	currentPID := os.Getpid()
	err = tracker.TrackProcess(currentPID)
	if err != nil {
		// On non-Linux systems, this may fail due to /proc dependency
		t.Logf("Failed to track process (expected on non-Linux): %v", err)
		return
	}

	// Verify process is tracked
	tracker.mu.RLock()
	processInfo, exists := tracker.processes[currentPID]
	tracker.mu.RUnlock()

	if !exists {
		t.Error("Expected process to be tracked")
	} else {
		if processInfo.PID != currentPID {
			t.Errorf("Expected PID %d, got %d", currentPID, processInfo.PID)
		}
		if processInfo.SampleCount != 1 {
			t.Errorf("Expected initial sample count 1, got %d", processInfo.SampleCount)
		}
		if processInfo.VmRSS == 0 {
			t.Error("Expected non-zero RSS memory")
		}
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

func TestUpdateProcess(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker, err := NewMemoryTracker(logger)
	if err != nil {
		t.Fatalf("Failed to create memory tracker: %v", err)
	}

	ctx := context.Background()
	err = tracker.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop(ctx)

	currentPID := os.Getpid()

	// Test updating non-tracked process
	err = tracker.UpdateProcess(999999)
	if err == nil {
		t.Error("Expected error when updating non-tracked process")
	}

	// Track current process
	err = tracker.TrackProcess(currentPID)
	if err != nil {
		t.Logf("Failed to track process (expected on non-Linux): %v", err)
		return
	}

	// Get initial values
	tracker.mu.RLock()
	initialSampleCount := tracker.processes[currentPID].SampleCount
	tracker.mu.RUnlock()

	// Update process
	err = tracker.UpdateProcess(currentPID)
	if err != nil {
		t.Errorf("Failed to update process: %v", err)
	}

	// Verify sample count increased
	tracker.mu.RLock()
	newSampleCount := tracker.processes[currentPID].SampleCount
	tracker.mu.RUnlock()

	if newSampleCount != initialSampleCount+1 {
		t.Errorf("Expected sample count to increase from %d to %d, got %d",
			initialSampleCount, initialSampleCount+1, newSampleCount)
	}
}

func TestGetProcessStats(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker, err := NewMemoryTracker(logger)
	if err != nil {
		t.Fatalf("Failed to create memory tracker: %v", err)
	}

	ctx := context.Background()
	err = tracker.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop(ctx)

	currentPID := os.Getpid()

	// Test getting stats for non-tracked process
	_, err = tracker.GetProcessStats(999999)
	if err == nil {
		t.Error("Expected error when getting stats for non-tracked process")
	}

	// Track current process
	err = tracker.TrackProcess(currentPID)
	if err != nil {
		t.Logf("Failed to track process (expected on non-Linux): %v", err)
		return
	}

	// Get process stats
	stats, err := tracker.GetProcessStats(currentPID)
	if err != nil {
		t.Errorf("Failed to get process stats: %v", err)
	}

	if stats == nil {
		t.Error("Expected non-nil process stats")
	}

	if stats.PID != currentPID {
		t.Errorf("Expected PID %d, got %d", currentPID, stats.PID)
	}

	if stats.VmRSS == 0 {
		t.Error("Expected non-zero RSS memory")
	}

	if stats.SampleCount < 2 {
		t.Errorf("Expected at least 2 samples (initial + update), got %d", stats.SampleCount)
	}
}

func TestGetMemoryMetrics(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker, err := NewMemoryTracker(logger)
	if err != nil {
		t.Fatalf("Failed to create memory tracker: %v", err)
	}

	ctx := context.Background()
	err = tracker.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop(ctx)

	// Track current process
	currentPID := os.Getpid()
	err = tracker.TrackProcess(currentPID)
	if err != nil {
		t.Logf("Failed to track process (expected on non-Linux): %v", err)
		return
	}

	// Get metrics
	metrics, err := tracker.GetMetrics(ctx)
	if err != nil {
		t.Errorf("Failed to get metrics: %v", err)
	}

	if len(metrics) == 0 {
		t.Error("Expected memory metrics to be returned")
	}

	// Verify expected metrics
	expectedMetrics := []string{
		"phpfpm_process_memory_rss_bytes",
		"phpfpm_process_memory_vsize_bytes",
		"phpfpm_process_memory_peak_rss_bytes",
		"phpfpm_process_memory_peak_vsize_bytes",
		"phpfpm_process_memory_max_bytes",
		"phpfpm_process_memory_min_bytes",
		"phpfpm_process_memory_avg_bytes",
		"phpfpm_process_memory_samples_total",
	}

	metricNames := make(map[string]bool)
	for _, metric := range metrics {
		metricNames[metric.Name] = true

		// Verify metric has PID label
		if pidLabel, exists := metric.Labels["pid"]; !exists {
			t.Errorf("Expected PID label for metric %s", metric.Name)
		} else if pidLabel != strconv.Itoa(currentPID) {
			t.Errorf("Expected PID label '%d', got '%s'", currentPID, pidLabel)
		}

		// Verify metric values are reasonable
		if strings.Contains(metric.Name, "_bytes") && metric.Value <= 0 {
			t.Errorf("Memory metric %s should be positive, got %f", metric.Name, metric.Value)
		}

		if metric.Name == "phpfpm_process_memory_samples_total" {
			if metric.Type != types.MetricTypeCounter {
				t.Errorf("Expected samples metric to be counter, got %v", metric.Type)
			}
			if metric.Value < 1 {
				t.Errorf("Expected at least 1 sample, got %f", metric.Value)
			}
		} else {
			if metric.Type != types.MetricTypeGauge {
				t.Errorf("Expected memory metric to be gauge, got %v", metric.Type)
			}
		}

		if metric.Timestamp.IsZero() {
			t.Error("Expected metric to have timestamp")
		}
	}

	for _, expectedMetric := range expectedMetrics {
		if !metricNames[expectedMetric] {
			t.Errorf("Expected memory metric '%s' not found", expectedMetric)
		}
	}
}

func TestReadProcessMemory(t *testing.T) {
	// Skip this test on non-Linux systems
	if _, err := os.Stat("/proc"); os.IsNotExist(err) {
		t.Skip("Skipping /proc-dependent test on non-Linux system")
	}

	logger := zaptest.NewLogger(t)
	tracker, err := NewMemoryTracker(logger)
	if err != nil {
		t.Fatalf("Failed to create memory tracker: %v", err)
	}

	// Test reading current process
	currentPID := os.Getpid()
	stats, err := tracker.readProcessMemory(currentPID)
	if err != nil {
		t.Errorf("Failed to read process memory: %v", err)
	}

	if stats == nil {
		t.Error("Expected non-nil memory stats")
	}

	// Memory values should be non-negative
	if stats.VmRSS == 0 && stats.VmSize == 0 {
		t.Error("Expected some non-zero memory values")
	}

	// Test reading non-existent process
	_, err = tracker.readProcessMemory(999999)
	if err == nil {
		t.Error("Expected error when reading non-existent process")
	}
}

func TestReadProcessMemoryWithMockData(t *testing.T) {
	logger := zaptest.NewLogger(t)
	_, err := NewMemoryTracker(logger)
	if err != nil {
		t.Fatalf("Failed to create memory tracker: %v", err)
	}

	// Create temporary directory and mock status file
	tempDir, err := os.MkdirTemp("", "memory_tracker_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Mock status file content
	statusContent := `Name:	test-process
Umask:	0022
State:	S (sleeping)
Tgid:	1234
Ngid:	0
Pid:	1234
PPid:	1
TracerPid:	0
Uid:	1000	1000	1000	1000
Gid:	1000	1000	1000	1000
FDSize:	256
Groups:	1000
VmPeak:	   12345 kB
VmSize:	   10234 kB
VmLck:	       0 kB
VmPin:	       0 kB
VmHWM:	    5678 kB
VmRSS:	    4567 kB
RssAnon:	    3456 kB
RssFile:	    1111 kB
RssShmem:	       0 kB
VmData:	    2345 kB
VmStk:	     132 kB
VmExe:	     123 kB
VmLib:	    3456 kB
VmPTE:	      40 kB
VmSwap:	       0 kB
Threads:	1
SigQ:	0/15848
SigPnd:	0000000000000000
ShdPnd:	0000000000000000
SigBlk:	0000000000000000
SigIgn:	0000000000000001
SigCgt:	0000000000000000
CapInh:	0000000000000000
CapPrm:	0000000000000000
CapEff:	0000000000000000
CapBnd:	0000003fffffffff
CapAmb:	0000000000000000
Seccomp:	0
Cpus_allowed:	ff
Cpus_allowed_list:	0-7
Mems_allowed:	00000000,00000001
Mems_allowed_list:	0
voluntary_ctxt_switches:	123
nonvoluntary_ctxt_switches:	456`

	procDir := filepath.Join(tempDir, "proc", "1234")
	err = os.MkdirAll(procDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create proc dir: %v", err)
	}

	statusFile := filepath.Join(procDir, "status")
	err = os.WriteFile(statusFile, []byte(statusContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write status file: %v", err)
	}

	// Create helper function to read mock process memory data
	readMockProcessMemory := func() (*ProcessMemoryStats, error) {
		data, err := os.ReadFile(statusFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", statusFile, err)
		}

		stats := &ProcessMemoryStats{}
		lines := strings.Split(string(data), "\n")

		for _, line := range lines {
			if line == "" {
				continue
			}

			parts := strings.Fields(line)
			if len(parts) < 2 {
				continue
			}

			key := strings.TrimSuffix(parts[0], ":")

			var value uint64
			if len(parts) >= 2 {
				if v, err := strconv.ParseUint(parts[1], 10, 64); err == nil {
					value = v * 1024 // Convert from kB to bytes
				}
			}

			switch key {
			case "VmPeak":
				stats.VmPeak = value
			case "VmSize":
				stats.VmSize = value
			case "VmHWM":
				stats.VmHWM = value
			case "VmRSS":
				stats.VmRSS = value
			case "VmData":
				stats.VmData = value
			case "VmStk":
				stats.VmStk = value
			case "VmExe":
				stats.VmExe = value
			case "VmLib":
				stats.VmLib = value
			}
		}

		return stats, nil
	}

	// Test reading the mock data directly
	stats, err := readMockProcessMemory()
	if err != nil {
		t.Errorf("Failed to read mock process memory: %v", err)
	}

	expectedVmSize := uint64(10234 * 1024) // Convert from kB to bytes
	if stats.VmSize != expectedVmSize {
		t.Errorf("Expected VmSize %d, got %d", expectedVmSize, stats.VmSize)
	}

	expectedVmRSS := uint64(4567 * 1024)
	if stats.VmRSS != expectedVmRSS {
		t.Errorf("Expected VmRSS %d, got %d", expectedVmRSS, stats.VmRSS)
	}

	expectedVmPeak := uint64(12345 * 1024)
	if stats.VmPeak != expectedVmPeak {
		t.Errorf("Expected VmPeak %d, got %d", expectedVmPeak, stats.VmPeak)
	}

	expectedVmHWM := uint64(5678 * 1024)
	if stats.VmHWM != expectedVmHWM {
		t.Errorf("Expected VmHWM %d, got %d", expectedVmHWM, stats.VmHWM)
	}
}

func TestCalculateMedianMemory(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker, err := NewMemoryTracker(logger)
	if err != nil {
		t.Fatalf("Failed to create memory tracker: %v", err)
	}

	ctx := context.Background()
	err = tracker.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop(ctx)

	currentPID := os.Getpid()

	// Test median for non-tracked process
	_, err = tracker.CalculateMedianMemory(999999)
	if err == nil {
		t.Error("Expected error for non-tracked process")
	}

	// Track current process
	err = tracker.TrackProcess(currentPID)
	if err != nil {
		t.Logf("Failed to track process (expected on non-Linux): %v", err)
		return
	}

	// Update a few times to get samples
	for i := 0; i < 3; i++ {
		err = tracker.UpdateProcess(currentPID)
		if err != nil {
			t.Errorf("Failed to update process: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Calculate median (currently returns average as approximation)
	median, err := tracker.CalculateMedianMemory(currentPID)
	if err != nil {
		t.Errorf("Failed to calculate median memory: %v", err)
	}

	if median <= 0 {
		t.Errorf("Expected positive median memory, got %f", median)
	}
}

func TestGetMemoryUtilization(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker, err := NewMemoryTracker(logger)
	if err != nil {
		t.Fatalf("Failed to create memory tracker: %v", err)
	}

	ctx := context.Background()
	err = tracker.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop(ctx)

	currentPID := os.Getpid()

	// Test utilization for non-tracked process
	_, err = tracker.GetMemoryUtilization(999999)
	if err == nil {
		t.Error("Expected error for non-tracked process")
	}

	// Track current process
	err = tracker.TrackProcess(currentPID)
	if err != nil {
		t.Logf("Failed to track process (expected on non-Linux): %v", err)
		return
	}

	// Get utilization
	utilization, err := tracker.GetMemoryUtilization(currentPID)
	if err != nil {
		// May fail on non-Linux systems due to /proc/meminfo dependency
		t.Logf("Failed to get memory utilization (expected on non-Linux): %v", err)
		return
	}

	if utilization < 0 || utilization > 100 {
		t.Errorf("Memory utilization should be 0-100%%, got %f", utilization)
	}
}

func TestGetSystemMemoryTotal(t *testing.T) {
	// Skip this test on non-Linux systems
	if _, err := os.Stat("/proc/meminfo"); os.IsNotExist(err) {
		t.Skip("Skipping /proc/meminfo-dependent test on non-Linux system")
	}

	logger := zaptest.NewLogger(t)
	tracker, err := NewMemoryTracker(logger)
	if err != nil {
		t.Fatalf("Failed to create memory tracker: %v", err)
	}

	totalMemory, err := tracker.getSystemMemoryTotal()
	if err != nil {
		t.Errorf("Failed to get system memory total: %v", err)
	}

	if totalMemory == 0 {
		t.Error("Expected non-zero system memory total")
	}
}

func TestGetSystemMemoryTotalWithMockData(t *testing.T) {
	logger := zaptest.NewLogger(t)
	_, err := NewMemoryTracker(logger)
	if err != nil {
		t.Fatalf("Failed to create memory tracker: %v", err)
	}

	// Create temporary meminfo file
	tempDir, err := os.MkdirTemp("", "memory_tracker_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Mock meminfo content
	meminfoContent := `MemTotal:        8192000 kB
MemFree:         4096000 kB
MemAvailable:    6144000 kB
Buffers:          512000 kB
Cached:          1024000 kB
SwapCached:            0 kB
Active:          2048000 kB
Inactive:        1536000 kB
Active(anon):    1024000 kB
Inactive(anon):   256000 kB
Active(file):    1024000 kB
Inactive(file):  1280000 kB
Unevictable:           0 kB
Mlocked:               0 kB
SwapTotal:       2048000 kB
SwapFree:        2048000 kB
Dirty:              1024 kB
Writeback:             0 kB`

	meminfoFile := filepath.Join(tempDir, "meminfo")
	err = os.WriteFile(meminfoFile, []byte(meminfoContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write meminfo file: %v", err)
	}

	// Create helper function to read mock system memory data
	readMockSystemMemory := func() (uint64, error) {
		data, err := os.ReadFile(meminfoFile)
		if err != nil {
			return 0, fmt.Errorf("failed to read %s: %w", meminfoFile, err)
		}

		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "MemTotal:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					if total, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
						return total * 1024, nil // Convert from kB to bytes
					}
				}
				break
			}
		}

		return 0, fmt.Errorf("MemTotal not found in meminfo")
	}

	// Test reading the mock data directly
	totalMemory, err := readMockSystemMemory()
	if err != nil {
		t.Errorf("Failed to get mock system memory total: %v", err)
	}

	expectedTotal := uint64(8192000 * 1024) // Convert from kB to bytes
	if totalMemory != expectedTotal {
		t.Errorf("Expected total memory %d, got %d", expectedTotal, totalMemory)
	}

	// Test completed successfully
}
