package metrics

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/platform"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/testutil"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/types"
)

// TestMemoryTrackerCrossPlatform tests memory tracker functionality across platforms
func TestMemoryTrackerCrossPlatform(t *testing.T) {
	// Skip test in containerized environment where platform detection might not work properly
	if os.Getenv("DOCKER_TEST") != "" {
		t.Skip("Skipping cross-platform memory tracker test in Docker environment")
	}
	suite := testutil.PlatformTestSuite{
		Tests: []testutil.PlatformTest{
			{
				Name:     "memory_tracker_basic_functionality",
				TestFunc: testMemoryTrackerBasicFunctionality,
			},
			{
				Name:     "memory_tracker_process_tracking",
				TestFunc: testMemoryTrackerProcessTracking,
			},
			{
				Name:      "memory_tracker_linux_specific",
				Platforms: []string{"linux"},
				Features:  []string{platform.FeatureProcFS},
				TestFunc:  testMemoryTrackerLinuxSpecific,
			},
			{
				Name:     "memory_tracker_cross_platform_consistency",
				TestFunc: testMemoryTrackerCrossPlatformConsistency,
			},
			{
				Name:     "memory_tracker_error_handling",
				TestFunc: testMemoryTrackerErrorHandling,
			},
			{
				Name:     "memory_tracker_concurrent_access",
				TestFunc: testMemoryTrackerConcurrentAccess,
			},
		},
	}

	testutil.RunPlatformTests(t, suite)
}

// testMemoryTrackerBasicFunctionality tests basic memory tracker operations
func testMemoryTrackerBasicFunctionality(t *testing.T, provider platform.Provider) {
	logger := zaptest.NewLogger(t)
	tracker := NewMemoryTrackerWithPlatform(logger, provider)

	ctx := context.Background()

	// Test Start
	err := tracker.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start memory tracker: %v", err)
	}

	// Test double start (should fail)
	err = tracker.Start(ctx)
	if err == nil {
		t.Error("Starting already running tracker should fail")
	}

	// Test GetMetrics without processes
	metrics, err := tracker.GetMetrics(ctx)
	if err != nil {
		t.Fatalf("GetMetrics failed: %v", err)
	}

	if len(metrics) != 0 {
		t.Errorf("Expected 0 metrics without tracked processes, got %d", len(metrics))
	}

	// Test Stop
	err = tracker.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop memory tracker: %v", err)
	}

	// Test double stop (should not fail)
	err = tracker.Stop(ctx)
	if err != nil {
		t.Errorf("Stopping already stopped tracker should not fail: %v", err)
	}
}

// testMemoryTrackerProcessTracking tests process tracking functionality
func testMemoryTrackerProcessTracking(t *testing.T, provider platform.Provider) {
	// Setup mock scenario with test processes
	scenario := testutil.MockMemoryScenario{
		TotalMemoryMB:   4096,
		ProcessMemoryMB: 128,
		ProcessPID:      100,
		ProcessName:     "test-php-fpm",
	}
	if err := testutil.SetupMockMemoryScenario(provider, scenario); err != nil {
		t.Fatalf("Failed to setup mock scenario: %v", err)
	}

	logger := zaptest.NewLogger(t)
	tracker := NewMemoryTrackerWithPlatform(logger, provider)

	ctx := context.Background()

	// Start tracker
	err := tracker.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start memory tracker: %v", err)
	}
	defer tracker.Stop(ctx)

	// Track a test process
	testPID := 100
	err = tracker.TrackProcess(testPID)
	if err != nil {
		t.Fatalf("Failed to track process %d: %v", testPID, err)
	}

	// Get process stats
	stats, err := tracker.GetProcessStats(testPID)
	if err != nil {
		t.Fatalf("Failed to get process stats for %d: %v", testPID, err)
	}

	// Validate process stats
	testutil.AssertProcessInfo(t, &platform.ProcessInfo{
		PID:           stats.PID,
		Name:          "test-php-fpm", // From scenario
		VirtualBytes:  stats.VmSize,
		ResidentBytes: stats.VmRSS,
	})

	if stats.PID != testPID {
		t.Errorf("Expected PID %d, got %d", testPID, stats.PID)
	}

	if stats.SampleCount != 2 {
		t.Errorf("Expected sample count 2, got %d", stats.SampleCount)
	}

	// Update process (simulate monitoring)
	err = tracker.UpdateProcess(testPID)
	if err != nil {
		t.Fatalf("Failed to update process %d: %v", testPID, err)
	}

	// Get updated stats
	updatedStats, err := tracker.GetProcessStats(testPID)
	if err != nil {
		t.Fatalf("Failed to get updated process stats: %v", err)
	}

	if updatedStats.SampleCount != 4 {
		t.Errorf("Expected sample count 4 after update, got %d", updatedStats.SampleCount)
	}

	// Test GetMetrics with tracked process
	metrics, err := tracker.GetMetrics(ctx)
	if err != nil {
		t.Fatalf("GetMetrics failed: %v", err)
	}

	if len(metrics) == 0 {
		t.Error("Expected metrics for tracked process")
	}

	// Validate metric names and types
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

	foundMetrics := make(map[string]bool)
	for _, metric := range metrics {
		foundMetrics[metric.Name] = true

		// Validate metric has PID label
		if pidLabel, exists := metric.Labels["pid"]; !exists {
			t.Errorf("Metric %s missing PID label", metric.Name)
		} else if pidLabel != "100" {
			t.Errorf("Metric %s has wrong PID label: %s", metric.Name, pidLabel)
		}

		// Validate metric values are reasonable
		if metric.Value < 0 {
			t.Errorf("Metric %s has negative value: %f", metric.Name, metric.Value)
		}
	}

	for _, expectedMetric := range expectedMetrics {
		if !foundMetrics[expectedMetric] {
			t.Errorf("Expected metric %s not found", expectedMetric)
		}
	}

	// Test UntrackProcess
	tracker.UntrackProcess(testPID)

	// Verify process is no longer tracked
	_, err = tracker.GetProcessStats(testPID)
	if err == nil {
		t.Error("GetProcessStats should fail for untracked process")
	}
}

// testMemoryTrackerLinuxSpecific tests Linux-specific memory tracker functionality
func testMemoryTrackerLinuxSpecific(t *testing.T, provider platform.Provider) {
	if provider.Platform() != "linux" && provider.Platform() != "mock" {
		t.Skip("Linux-specific test")
	}

	// For mock provider, use default mock data
	// For real Linux systems, use actual /proc filesystem

	logger := zaptest.NewLogger(t)
	tracker := NewMemoryTrackerWithPlatform(logger, provider)

	ctx := context.Background()
	err := tracker.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop(ctx)

	// Track process (use PID 100 which exists in mock data)
	testPID := 100
	if provider.Platform() == "linux" {
		// For real Linux, we'd need to find an actual running process
		// For now, skip detailed testing on real Linux
		t.Skip("Real Linux testing not implemented yet")
	}

	err = tracker.TrackProcess(testPID)
	if err != nil {
		t.Fatalf("Failed to track process %d: %v", testPID, err)
	}

	// Test memory reading
	stats, err := tracker.GetProcessStats(testPID)
	if err != nil {
		t.Fatalf("Failed to get process stats: %v", err)
	}

	// For mock provider, verify we get reasonable values
	if provider.Platform() == "mock" {
		// Based on mock.go default data for PID 100
		expectedRSS := uint64(64) * 1024 * 1024 // 64MB in bytes
		if stats.VmRSS != expectedRSS {
			t.Errorf("Expected VmRSS %d, got %d", expectedRSS, stats.VmRSS)
		}
		expectedVSize := uint64(128) * 1024 * 1024 // 128MB in bytes
		if stats.VmSize != expectedVSize {
			t.Errorf("Expected VmSize %d, got %d", expectedVSize, stats.VmSize)
		}
	}

	// Test system memory utilization
	utilization, err := tracker.GetMemoryUtilization(100)
	if err != nil {
		t.Fatalf("Failed to get memory utilization: %v", err)
	}

	if utilization < 0 || utilization > 100 {
		t.Errorf("Memory utilization should be 0-100%%, got %f", utilization)
	}

	// Test median memory calculation
	median, err := tracker.CalculateMedianMemory(100)
	if err != nil {
		t.Fatalf("Failed to calculate median memory: %v", err)
	}

	if median <= 0 {
		t.Error("Median memory should be positive")
	}
}

// testMemoryTrackerCrossPlatformConsistency tests consistency across platforms
func testMemoryTrackerCrossPlatformConsistency(t *testing.T, provider platform.Provider) {
	logger := zaptest.NewLogger(t)
	tracker := NewMemoryTrackerWithPlatform(logger, provider)

	ctx := context.Background()
	err := tracker.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop(ctx)

	// Test consistent behavior across multiple calls
	for i := 0; i < 5; i++ {
		metrics, err := tracker.GetMetrics(ctx)
		if err != nil {
			t.Fatalf("GetMetrics call %d failed: %v", i, err)
		}

		// Should always return empty metrics when no processes are tracked
		if len(metrics) != 0 {
			t.Errorf("Call %d: expected 0 metrics, got %d", i, len(metrics))
		}
	}

	// Test platform provider consistency
	if tracker.provider != provider {
		t.Error("Tracker should use the provided platform provider")
	}
}

// testMemoryTrackerErrorHandling tests error handling scenarios
func testMemoryTrackerErrorHandling(t *testing.T, provider platform.Provider) {
	logger := zaptest.NewLogger(t)
	tracker := NewMemoryTrackerWithPlatform(logger, provider)

	ctx := context.Background()

	// Test operations before starting
	err := tracker.TrackProcess(100)
	if err == nil {
		t.Error("TrackProcess should fail when tracker is not running")
	}

	err = tracker.UpdateProcess(100)
	if err == nil {
		t.Error("UpdateProcess should fail when tracker is not running")
	}

	// Start tracker
	err = tracker.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop(ctx)

	// Test tracking non-existent process
	nonExistentPID := 999999
	err = tracker.TrackProcess(nonExistentPID)
	if err == nil {
		t.Error("TrackProcess should fail for non-existent process")
	}

	// Test updating non-tracked process
	err = tracker.UpdateProcess(1005)
	if err == nil {
		t.Error("UpdateProcess should fail for non-tracked process")
	}

	// Test getting stats for non-tracked process
	_, err = tracker.GetProcessStats(1005)
	if err == nil {
		t.Error("GetProcessStats should fail for non-tracked process")
	}
}

// testMemoryTrackerConcurrentAccess tests concurrent access to memory tracker
func testMemoryTrackerConcurrentAccess(t *testing.T, provider platform.Provider) {
	// Setup mock scenario
	scenario := testutil.MockMemoryScenario{
		TotalMemoryMB:   4096,
		ProcessMemoryMB: 64,
		ProcessPID:      1,
		ProcessName:     "test-process",
	}
	if err := testutil.SetupMockMemoryScenario(provider, scenario); err != nil {
		t.Fatalf("Failed to setup mock scenario: %v", err)
	}

	logger := zaptest.NewLogger(t)
	tracker := NewMemoryTrackerWithPlatform(logger, provider)

	ctx := context.Background()
	err := tracker.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start tracker: %v", err)
	}
	defer tracker.Stop(ctx)

	// Track a process
	testPID := 1
	err = tracker.TrackProcess(testPID)
	if err != nil {
		t.Fatalf("Failed to track process: %v", err)
	}

	// Test concurrent access
	done := make(chan bool, 3)

	// Goroutine 1: Continuously update process
	go func() {
		for i := 0; i < 10; i++ {
			tracker.UpdateProcess(testPID)
			time.Sleep(10 * time.Millisecond)
		}
		done <- true
	}()

	// Goroutine 2: Continuously get process stats
	go func() {
		for i := 0; i < 10; i++ {
			tracker.GetProcessStats(testPID)
			time.Sleep(10 * time.Millisecond)
		}
		done <- true
	}()

	// Goroutine 3: Continuously get metrics
	go func() {
		for i := 0; i < 10; i++ {
			tracker.GetMetrics(ctx)
			time.Sleep(10 * time.Millisecond)
		}
		done <- true
	}()

	// Wait for all goroutines to complete
	for i := 0; i < 3; i++ {
		<-done
	}

	// Verify final state is consistent
	stats, err := tracker.GetProcessStats(testPID)
	if err != nil {
		t.Fatalf("Failed to get final process stats: %v", err)
	}

	if stats.SampleCount < 10 {
		t.Errorf("Expected at least 10 samples, got %d", stats.SampleCount)
	}
}

// NewMemoryTrackerWithPlatform creates a memory tracker with a specific platform provider
func NewMemoryTrackerWithPlatform(logger *zap.Logger, provider platform.Provider) *MemoryTrackerWithPlatform {
	return &MemoryTrackerWithPlatform{
		logger:    logger.Sugar(),
		provider:  provider,
		processes: make(map[int]*ProcessMemoryInfo),
	}
}

// MemoryTrackerWithPlatform is a version of MemoryTracker that accepts a platform provider
type MemoryTrackerWithPlatform struct {
	logger   *zap.SugaredLogger
	provider platform.Provider

	// Process tracking
	processes map[int]*ProcessMemoryInfo
	mu        sync.RWMutex

	// State
	running bool
}

// Implement the same interface as MemoryTracker but use platform abstraction
func (m *MemoryTrackerWithPlatform) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return fmt.Errorf("memory tracker is already running")
	}
	m.running = true
	m.mu.Unlock()

	m.logger.Info("Starting memory tracker with platform provider")
	return nil
}

func (m *MemoryTrackerWithPlatform) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return nil
	}
	m.running = false
	m.mu.Unlock()

	m.logger.Info("Stopping memory tracker")
	return nil
}

func (m *MemoryTrackerWithPlatform) TrackProcess(pid int) error {
	if !m.running {
		return fmt.Errorf("memory tracker is not running")
	}

	ctx := context.Background()
	processInfo, err := m.provider.Process().GetProcessInfo(ctx, pid)
	if err != nil {
		return fmt.Errorf("failed to get process memory info: %w", err)
	}

	m.mu.Lock()
	m.processes[pid] = &ProcessMemoryInfo{
		PID:            pid,
		VmSize:         processInfo.VirtualBytes,
		VmRSS:          processInfo.ResidentBytes,
		VmPeak:         processInfo.VirtualBytes,
		VmHWM:          processInfo.ResidentBytes,
		LastUpdate:     time.Now(),
		MaxMemoryUsage: processInfo.ResidentBytes,
		MinMemoryUsage: processInfo.ResidentBytes,
		SampleCount:    1,
		TotalMemory:    processInfo.ResidentBytes,
	}
	m.mu.Unlock()

	m.logger.Debugf("Started tracking process memory for PID %d", pid)
	return nil
}

func (m *MemoryTrackerWithPlatform) UntrackProcess(pid int) {
	m.mu.Lock()
	delete(m.processes, pid)
	m.mu.Unlock()

	m.logger.Debugf("Stopped tracking process memory for PID %d", pid)
}

func (m *MemoryTrackerWithPlatform) UpdateProcess(pid int) error {
	m.mu.RLock()
	processInfo, exists := m.processes[pid]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("process %d is not being tracked", pid)
	}

	ctx := context.Background()
	currentInfo, err := m.provider.Process().GetProcessInfo(ctx, pid)
	if err != nil {
		return fmt.Errorf("failed to get current memory info: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Update current values
	processInfo.VmSize = currentInfo.VirtualBytes
	processInfo.VmRSS = currentInfo.ResidentBytes
	processInfo.LastUpdate = time.Now()

	// Update statistics
	processInfo.SampleCount++
	processInfo.TotalMemory += currentInfo.ResidentBytes

	if currentInfo.ResidentBytes > processInfo.MaxMemoryUsage {
		processInfo.MaxMemoryUsage = currentInfo.ResidentBytes
	}

	if currentInfo.ResidentBytes < processInfo.MinMemoryUsage {
		processInfo.MinMemoryUsage = currentInfo.ResidentBytes
	}

	return nil
}

func (m *MemoryTrackerWithPlatform) GetProcessStats(pid int) (*ProcessMemoryInfo, error) {
	// Update the process first
	if err := m.UpdateProcess(pid); err != nil {
		return nil, err
	}

	m.mu.RLock()
	processInfo, exists := m.processes[pid]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("process %d is not being tracked", pid)
	}

	// Return a copy to avoid concurrent access issues
	return &ProcessMemoryInfo{
		PID:            processInfo.PID,
		VmSize:         processInfo.VmSize,
		VmRSS:          processInfo.VmRSS,
		VmPeak:         processInfo.VmPeak,
		VmHWM:          processInfo.VmHWM,
		LastUpdate:     processInfo.LastUpdate,
		MaxMemoryUsage: processInfo.MaxMemoryUsage,
		MinMemoryUsage: processInfo.MinMemoryUsage,
		SampleCount:    processInfo.SampleCount,
		TotalMemory:    processInfo.TotalMemory,
	}, nil
}

func (m *MemoryTrackerWithPlatform) GetMetrics(ctx context.Context) ([]types.Metric, error) {
	var metrics []types.Metric
	timestamp := time.Now()

	// Collect PIDs first to avoid holding lock during UpdateProcess calls
	m.mu.RLock()
	pids := make([]int, 0, len(m.processes))
	for pid := range m.processes {
		pids = append(pids, pid)
	}
	m.mu.RUnlock()

	// Update all processes and collect metrics
	for _, pid := range pids {
		// Update process memory info
		if err := m.UpdateProcess(pid); err != nil {
			m.logger.Errorf("Failed to update process memory for PID %d: %v", pid, err)
			continue
		}

		// Get updated info
		updatedInfo, err := m.GetProcessStats(pid)
		if err != nil {
			m.logger.Errorf("Failed to get process stats for PID %d: %v", pid, err)
			continue
		}

		labels := map[string]string{
			"pid": fmt.Sprintf("%d", pid),
		}

		// Add metrics (same as original MemoryTracker)
		metrics = append(metrics, types.Metric{
			Name:      "phpfpm_process_memory_rss_bytes",
			Value:     float64(updatedInfo.VmRSS),
			Type:      types.MetricTypeGauge,
			Labels:    labels,
			Timestamp: timestamp,
		})

		// Add other metrics...
		metrics = append(metrics, types.Metric{
			Name:      "phpfpm_process_memory_vsize_bytes",
			Value:     float64(updatedInfo.VmSize),
			Type:      types.MetricTypeGauge,
			Labels:    labels,
			Timestamp: timestamp,
		})

		metrics = append(metrics, types.Metric{
			Name:      "phpfpm_process_memory_peak_rss_bytes",
			Value:     float64(updatedInfo.VmHWM),
			Type:      types.MetricTypeGauge,
			Labels:    labels,
			Timestamp: timestamp,
		})

		metrics = append(metrics, types.Metric{
			Name:      "phpfpm_process_memory_peak_vsize_bytes",
			Value:     float64(updatedInfo.VmPeak),
			Type:      types.MetricTypeGauge,
			Labels:    labels,
			Timestamp: timestamp,
		})

		metrics = append(metrics, types.Metric{
			Name:      "phpfpm_process_memory_max_bytes",
			Value:     float64(updatedInfo.MaxMemoryUsage),
			Type:      types.MetricTypeGauge,
			Labels:    labels,
			Timestamp: timestamp,
		})

		metrics = append(metrics, types.Metric{
			Name:      "phpfpm_process_memory_min_bytes",
			Value:     float64(updatedInfo.MinMemoryUsage),
			Type:      types.MetricTypeGauge,
			Labels:    labels,
			Timestamp: timestamp,
		})

		// Average memory usage
		if updatedInfo.SampleCount > 0 {
			avgMemory := float64(updatedInfo.TotalMemory) / float64(updatedInfo.SampleCount)
			metrics = append(metrics, types.Metric{
				Name:      "phpfpm_process_memory_avg_bytes",
				Value:     avgMemory,
				Type:      types.MetricTypeGauge,
				Labels:    labels,
				Timestamp: timestamp,
			})
		}

		// Sample count
		metrics = append(metrics, types.Metric{
			Name:      "phpfpm_process_memory_samples_total",
			Value:     float64(updatedInfo.SampleCount),
			Type:      types.MetricTypeCounter,
			Labels:    labels,
			Timestamp: timestamp,
		})
	}

	return metrics, nil
}

func (m *MemoryTrackerWithPlatform) GetMemoryUtilization(pid int) (float64, error) {
	processInfo, err := m.GetProcessStats(pid)
	if err != nil {
		return 0, err
	}

	// Get system memory info from platform provider
	ctx := context.Background()
	sysInfo, err := m.provider.Memory().GetMemoryInfo(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get system memory: %w", err)
	}

	if sysInfo.TotalBytes == 0 {
		return 0, fmt.Errorf("invalid system memory total")
	}

	utilization := (float64(processInfo.VmRSS) / float64(sysInfo.TotalBytes)) * 100
	return utilization, nil
}

func (m *MemoryTrackerWithPlatform) CalculateMedianMemory(pid int) (float64, error) {
	processInfo, err := m.GetProcessStats(pid)
	if err != nil {
		return 0, err
	}

	if processInfo.SampleCount == 0 {
		return 0, fmt.Errorf("no samples available")
	}

	// For now, return the average as an approximation
	avgMemory := float64(processInfo.TotalMemory) / float64(processInfo.SampleCount)
	return avgMemory, nil
}
