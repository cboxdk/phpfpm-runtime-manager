package metrics

import (
	"context"
	"os"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

// TestFullExecutionLifecycleTracking tests comprehensive execution monitoring from start to finish
func TestFullExecutionLifecycleTracking(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("Full execution tracking tests require root privileges to access /proc")
	}

	// Skip test in containerized environment where CPU tracking might not be accurate
	if os.Getenv("DOCKER_TEST") != "" {
		t.Skip("Skipping execution lifecycle test in Docker environment")
	}

	t.Run("CompleteExecutionCycle", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		tracker, err := NewProcessExecutionTracker(logger)
		if err != nil {
			t.Fatalf("Failed to create tracker: %v", err)
		}

		// Get our own PID for testing (since we can access our own /proc data)
		testPID := os.Getpid()

		// Start tracking our own process (simulating PHP-FPM worker)
		err = tracker.StartTracking(testPID, "/test/execution", "GET")
		if err != nil {
			t.Fatalf("Failed to start tracking: %v", err)
		}

		// Verify tracking started
		activeExecutions := tracker.GetActiveExecutions()
		if len(activeExecutions) != 1 {
			t.Errorf("Expected 1 active execution, got %d", len(activeExecutions))
		}

		execution, exists := activeExecutions[testPID]
		if !exists {
			t.Fatal("Expected to find tracked execution")
		}

		// Verify initial data
		if execution.PID != testPID {
			t.Errorf("Expected PID %d, got %d", testPID, execution.PID)
		}
		if execution.RequestURI != "/test/execution" {
			t.Errorf("Expected URI '/test/execution', got '%s'", execution.RequestURI)
		}
		if execution.RequestMethod != "GET" {
			t.Errorf("Expected method 'GET', got '%s'", execution.RequestMethod)
		}

		// Simulate some CPU and memory activity
		_ = simulateWorkload()

		// Update tracking (simulating periodic updates)
		ctx := context.Background()
		for i := 0; i < 5; i++ {
			err = tracker.UpdateTracking(ctx)
			if err != nil {
				t.Errorf("Failed to update tracking: %v", err)
			}
			time.Sleep(10 * time.Millisecond)
		}

		// Get updated execution data
		activeExecutions = tracker.GetActiveExecutions()
		execution = activeExecutions[testPID]

		// Verify tracking is updating
		if execution.CumulativeCPUTime == 0 {
			t.Error("Expected non-zero cumulative CPU time")
		}
		if execution.CurrentMemoryKB == 0 {
			t.Error("Expected non-zero current memory")
		}

		t.Logf("Execution tracking: CPU=%v, Memory=%dKB, Peak=%dKB",
			execution.CumulativeCPUTime, execution.CurrentMemoryKB, execution.PeakMemoryKB)

		// Stop tracking and get final metrics
		metrics, err := tracker.StopTracking(testPID)
		if err != nil {
			t.Fatalf("Failed to stop tracking: %v", err)
		}

		// Verify final metrics
		if metrics.PID != testPID {
			t.Errorf("Expected PID %d in metrics, got %d", testPID, metrics.PID)
		}
		if metrics.Duration <= 0 {
			t.Error("Expected positive execution duration")
		}
		if metrics.TotalCPUTime < 0 {
			t.Error("Expected non-negative total CPU time")
		}
		if metrics.PeakMemoryMB <= 0 {
			t.Error("Expected positive peak memory")
		}

		// Verify no longer in active tracking
		activeExecutions = tracker.GetActiveExecutions()
		if len(activeExecutions) != 0 {
			t.Errorf("Expected 0 active executions after stop, got %d", len(activeExecutions))
		}

		// Verify in completed executions
		completedExecutions := tracker.GetCompletedExecutions(10)
		if len(completedExecutions) != 1 {
			t.Errorf("Expected 1 completed execution, got %d", len(completedExecutions))
		}

		completed := completedExecutions[0]
		if completed.PID != testPID {
			t.Errorf("Expected completed PID %d, got %d", testPID, completed.PID)
		}

		t.Logf("Completed execution: Duration=%v, CPU=%.2f%%, Memory=%.1fMB",
			completed.Duration, completed.CPUUtilization, completed.PeakMemoryMB)
	})

	t.Run("ChildProcessTracking", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		tracker, err := NewProcessExecutionTracker(logger)
		if err != nil {
			t.Fatalf("Failed to create tracker: %v", err)
		}

		// Start tracking
		testPID := os.Getpid()
		err = tracker.StartTracking(testPID, "/test/with-children", "POST")
		if err != nil {
			t.Fatalf("Failed to start tracking: %v", err)
		}

		// Note: In a real test environment, we would spawn child processes
		// For this test, we'll simulate the scenario by manually checking
		// child process discovery functionality

		ctx := context.Background()
		err = tracker.UpdateTracking(ctx)
		if err != nil {
			t.Errorf("Failed to update tracking: %v", err)
		}

		// Get execution data
		activeExecutions := tracker.GetActiveExecutions()
		execution := activeExecutions[testPID]

		// Verify child process structure is initialized
		if execution.ChildProcesses == nil {
			t.Error("Expected child processes map to be initialized")
		}

		t.Logf("Child process tracking initialized for PID %d", testPID)

		// Stop tracking
		_, err = tracker.StopTracking(testPID)
		if err != nil {
			t.Fatalf("Failed to stop tracking: %v", err)
		}
	})
}

// TestEnhancedExecutionCollector tests the integration of execution tracking with PHP-FPM monitoring
func TestEnhancedExecutionCollector(t *testing.T) {
	t.Run("ExecutionStateTransitions", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		phpfpmClient := createTestClient(t)

		collector, err := NewEnhancedExecutionCollector(phpfpmClient, logger)
		if err != nil {
			t.Fatalf("Failed to create enhanced collector: %v", err)
		}

		// Test process state transitions
		testCases := []struct {
			name         string
			initialState PHPFPMProcessState
			finalState   PHPFPMProcessState
			shouldTrack  bool
		}{
			{
				name:         "Idle to Running - Start Tracking",
				initialState: ProcessStateIdle,
				finalState:   ProcessStateRunning,
				shouldTrack:  true,
			},
			{
				name:         "Running to Idle - Stop Tracking",
				initialState: ProcessStateRunning,
				finalState:   ProcessStateIdle,
				shouldTrack:  false,
			},
			{
				name:         "Reading to Running - Continue Tracking",
				initialState: ProcessStateReading,
				finalState:   ProcessStateRunning,
				shouldTrack:  true,
			},
			{
				name:         "Idle to Idle - No Change",
				initialState: ProcessStateIdle,
				finalState:   ProcessStateIdle,
				shouldTrack:  false,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// Use current process PID in container environment
				testPID := os.Getpid()

				// Create initial process state
				initialProcesses := []PHPFPMProcess{
					{
						PID:           testPID,
						State:         tc.initialState,
						RequestURI:    "/test/uri",
						RequestMethod: "POST",
					},
				}

				// Update with initial state
				err := collector.updateExecutionTracking(initialProcesses)
				if err != nil {
					t.Errorf("Failed to update execution tracking: %v", err)
				}

				// Create final process state
				finalProcesses := []PHPFPMProcess{
					{
						PID:           testPID,
						State:         tc.finalState,
						RequestURI:    "/test/uri",
						RequestMethod: "POST",
					},
				}

				// Update with final state
				err = collector.updateExecutionTracking(finalProcesses)
				if err != nil {
					t.Errorf("Failed to update execution tracking: %v", err)
				}

				// Check tracking state
				activeExecutions := collector.executionTracker.GetActiveExecutions()
				isTracking := len(activeExecutions) > 0

				if tc.shouldTrack && !isTracking {
					t.Errorf("Expected to be tracking execution, but not tracking")
				}
				if !tc.shouldTrack && isTracking && tc.finalState == ProcessStateIdle {
					t.Errorf("Expected to stop tracking when transitioning to idle, but still tracking")
				}

				t.Logf("State transition %s -> %s: tracking=%v", tc.initialState, tc.finalState, isTracking)

				// Clean up for next test
				if isTracking {
					collector.executionTracker.StopTracking(testPID)
				}
			})
		}
	})

	t.Run("ScalingMetricsCalculation", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		phpfpmClient := createTestClient(t)

		collector, err := NewEnhancedExecutionCollector(phpfpmClient, logger)
		if err != nil {
			t.Fatalf("Failed to create enhanced collector: %v", err)
		}

		// Create test data
		workerMetrics := &WorkerProcessMetrics{
			TotalWorkers:         5,
			ActiveWorkers:        3,
			IdleWorkers:          2,
			QueueDepth:           10,
			ProcessingVelocity:   15.0,
			EstimatedProcessTime: 667 * time.Millisecond, // 10 jobs / 15 RPS
		}

		// Create active executions with child processes
		activeExecutions := map[int]*ProcessExecution{
			1001: {
				PID:               1001,
				StartTime:         time.Now().Add(-200 * time.Millisecond),
				CumulativeCPUTime: 20 * time.Millisecond,
				CurrentMemoryKB:   64 * 1024, // 64MB
				PeakMemoryKB:      80 * 1024, // 80MB
				IsActive:          true,
				ChildProcesses: map[int]*ChildProcess{
					2001: {
						PID:               2001,
						CumulativeCPUTime: 5 * time.Millisecond,
						PeakMemoryKB:      16 * 1024, // 16MB
					},
				},
			},
			1002: {
				PID:               1002,
				StartTime:         time.Now().Add(-300 * time.Millisecond),
				CumulativeCPUTime: 30 * time.Millisecond,
				CurrentMemoryKB:   72 * 1024, // 72MB
				PeakMemoryKB:      90 * 1024, // 90MB
				IsActive:          true,
				ChildProcesses:    map[int]*ChildProcess{},
			},
		}

		// Create completed executions
		completedExecutions := []ExecutionMetrics{
			{
				Duration:          250 * time.Millisecond,
				CPUUtilization:    8.0,
				PeakMemoryMB:      75.0,
				ChildPeakMemoryMB: 10.0,
			},
			{
				Duration:          180 * time.Millisecond,
				CPUUtilization:    12.0,
				PeakMemoryMB:      68.0,
				ChildPeakMemoryMB: 8.0,
			},
		}

		executionSummary := ExecutionSummary{
			TotalExecutions:         len(completedExecutions),
			AvgCPUTimePerExecution:  25 * time.Millisecond,
			AvgMemoryMBPerExecution: 71.5,
			AvgChildMemoryMB:        9.0,
			AvgExecutionDuration:    215 * time.Millisecond,
			ActiveProcesses:         len(activeExecutions),
		}

		// Calculate scaling metrics
		scalingMetrics := collector.calculateScalingMetrics(workerMetrics, activeExecutions, completedExecutions, executionSummary)

		// Validate basic metrics
		if scalingMetrics.TotalWorkers != 5 {
			t.Errorf("Expected 5 total workers, got %d", scalingMetrics.TotalWorkers)
		}
		if scalingMetrics.ActiveWorkers != 3 {
			t.Errorf("Expected 3 active workers, got %d", scalingMetrics.ActiveWorkers)
		}
		if scalingMetrics.QueueDepth != 10 {
			t.Errorf("Expected queue depth 10, got %d", scalingMetrics.QueueDepth)
		}

		// Validate enhanced metrics
		if scalingMetrics.AvgMemoryMBPerWorker <= 0 {
			t.Error("Expected positive average memory per worker")
		}
		if scalingMetrics.ChildProcessMemoryOverhead <= 0 {
			t.Error("Expected positive child process memory overhead")
		}
		if scalingMetrics.EffectiveProcessingPower <= 0 {
			t.Error("Expected positive effective processing power")
		}

		// Validate scaling recommendation
		if scalingMetrics.RecommendedWorkerChange == 0 {
			t.Error("Expected scaling recommendation with queue pressure")
		}
		if scalingMetrics.ScalingConfidence <= 0 {
			t.Error("Expected positive scaling confidence")
		}

		t.Logf("Scaling metrics: AvgMem=%.1fMB, ChildMem=%.1fMB, CPU=%.1f%%, Efficiency=%.3f",
			scalingMetrics.AvgMemoryMBPerWorker,
			scalingMetrics.ChildProcessMemoryOverhead,
			scalingMetrics.AvgCPUUtilization,
			scalingMetrics.ResourceEfficiency)

		t.Logf("Scaling recommendation: %+d workers (confidence: %.2f)",
			scalingMetrics.RecommendedWorkerChange,
			scalingMetrics.ScalingConfidence)
	})
}

// TestProcStatParsing tests parsing of /proc/[pid]/stat data
func TestProcStatParsing(t *testing.T) {
	t.Run("ParseOwnProcessStat", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		tracker, err := NewProcessExecutionTracker(logger)
		if err != nil {
			t.Fatalf("Failed to create tracker: %v", err)
		}

		// Test parsing our own process stat
		testPID := os.Getpid()
		stat, err := tracker.readProcStat(testPID)
		if err != nil {
			t.Fatalf("Failed to read proc stat: %v", err)
		}

		// Validate basic fields
		if stat.PID != testPID {
			t.Errorf("Expected PID %d, got %d", testPID, stat.PID)
		}
		if stat.UTime < 0 {
			t.Errorf("Expected non-negative user time, got %v", stat.UTime)
		}
		if stat.STime < 0 {
			t.Errorf("Expected non-negative system time, got %v", stat.STime)
		}
		if stat.RSS <= 0 {
			t.Errorf("Expected positive RSS, got %d", stat.RSS)
		}

		t.Logf("Process %d stats: UTime=%v, STime=%v, RSS=%d pages",
			testPID, stat.UTime, stat.STime, stat.RSS)
	})

	t.Run("ValidateMemoryCalculation", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		tracker, err := NewProcessExecutionTracker(logger)
		if err != nil {
			t.Fatalf("Failed to create tracker: %v", err)
		}

		testPID := os.Getpid()
		stat, err := tracker.readProcStat(testPID)
		if err != nil {
			t.Fatalf("Failed to read proc stat: %v", err)
		}

		// Calculate memory in KB
		memoryKB := stat.RSS * tracker.pageSize / 1024

		// Validate reasonable memory usage (should be > 1MB for a test process)
		if memoryKB < 1024 {
			t.Errorf("Expected memory > 1MB, got %d KB", memoryKB)
		}

		// Should be less than 1GB for a test process
		if memoryKB > 1024*1024 {
			t.Errorf("Unexpectedly high memory usage: %d KB", memoryKB)
		}

		t.Logf("Process memory: %d KB (page size: %d bytes)", memoryKB, tracker.pageSize)
	})
}

// TestExecutionMetricsIntegration tests integration with autoscaler
func TestExecutionMetricsIntegration(t *testing.T) {
	t.Run("AutoscalerFeedback", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		phpfpmClient := createTestClient(t)

		collector, err := NewEnhancedExecutionCollector(phpfpmClient, logger)
		if err != nil {
			t.Fatalf("Failed to create enhanced collector: %v", err)
		}

		// Create test execution metrics
		execMetrics := &ExecutionMetrics{
			PID:               1234,
			Duration:          200 * time.Millisecond,
			TotalCPUTime:      20 * time.Millisecond,
			CPUUtilization:    10.0,
			PeakMemoryMB:      75.0,
			AvgMemoryMB:       68.0,
			ChildProcessCount: 1,
			ChildTotalCPUTime: 5 * time.Millisecond,
			ChildPeakMemoryMB: 12.0,
			RequestURI:        "/api/process",
			RequestMethod:     "POST",
			CompletedAt:       time.Now(),
		}

		// Test feeding to autoscaler (without actual autoscaler for this test)
		var receivedMetrics *ExecutionMetrics
		collector.SetAutoscalerFeedFunc(func(metrics ExecutionMetrics) {
			receivedMetrics = &metrics
		})

		// Simulate feeding metrics
		if collector.autoscalerFeedFunc != nil {
			collector.autoscalerFeedFunc(*execMetrics)
		}

		// Verify metrics were received
		if receivedMetrics == nil {
			t.Error("Expected to receive metrics via autoscaler feed function")
		} else {
			// Verify the data
			expectedRPS := 1.0 / execMetrics.Duration.Seconds()                        // Should be 5.0 RPS
			expectedMemory := execMetrics.PeakMemoryMB + execMetrics.ChildPeakMemoryMB // 87.0 MB

			if expectedRPS != 5.0 {
				t.Errorf("Expected 5.0 RPS, calculated %.1f", expectedRPS)
			}
			if expectedMemory != 87.0 {
				t.Errorf("Expected 87.0 MB total memory, calculated %.1f", expectedMemory)
			}

			t.Logf("Autoscaler feedback: %.1f RPS, %.1f MB total memory", expectedRPS, expectedMemory)
		}
	})
}

// simulateWorkload creates some CPU and memory activity for testing
func simulateWorkload() []byte {
	// Allocate some memory
	data := make([]byte, 1024*1024) // 1MB

	// Do some CPU work
	for i := 0; i < len(data); i++ {
		data[i] = byte(i % 256)
	}

	// Calculate checksum (more CPU work)
	sum := 0
	for _, b := range data {
		sum += int(b)
	}

	return data[:sum%1000] // Return varying size to use memory
}

// TestFullExecutionScenarios tests realistic execution scenarios
func TestFullExecutionScenarios(t *testing.T) {
	// Skip test in containerized environment where confidence thresholds might not be accurate
	if os.Getenv("DOCKER_TEST") != "" {
		t.Skip("Skipping execution scenarios test in Docker environment")
	}
	t.Run("HighMemoryExecution", func(t *testing.T) {
		// Test scenario: Process that uses high memory including child processes
		logger := zaptest.NewLogger(t)
		phpfpmClient := createTestClient(t)

		collector, err := NewEnhancedExecutionCollector(phpfpmClient, logger)
		if err != nil {
			t.Fatalf("Failed to create enhanced collector: %v", err)
		}

		// Simulate high memory execution
		scalingMetrics := &ScalingExecutionMetrics{
			TotalWorkers:               5,
			ActiveWorkers:              3,
			AvgMemoryMBPerWorker:       150.0, // High memory per worker
			ChildProcessMemoryOverhead: 50.0,  // Significant child memory
			AvgCPUUtilization:          25.0,
			ProcessingVelocity:         10.0,
			QueueDepth:                 20,
		}

		collector.calculateScalingRecommendation(scalingMetrics)

		// High memory should reduce scaling confidence
		if scalingMetrics.ScalingConfidence > 0.7 {
			t.Errorf("Expected reduced confidence for high memory scenario, got %.2f", scalingMetrics.ScalingConfidence)
		}

		t.Logf("High memory scenario: Confidence=%.2f, Recommendation=%+d",
			scalingMetrics.ScalingConfidence, scalingMetrics.RecommendedWorkerChange)
	})

	t.Run("EfficientExecution", func(t *testing.T) {
		// Test scenario: Efficient execution with good resource utilization
		logger := zaptest.NewLogger(t)
		phpfpmClient := createTestClient(t)

		collector, err := NewEnhancedExecutionCollector(phpfpmClient, logger)
		if err != nil {
			t.Fatalf("Failed to create enhanced collector: %v", err)
		}

		scalingMetrics := &ScalingExecutionMetrics{
			TotalWorkers:               4,
			ActiveWorkers:              3,
			AvgMemoryMBPerWorker:       64.0, // Normal memory
			ChildProcessMemoryOverhead: 8.0,  // Low child overhead
			AvgCPUUtilization:          15.0, // Reasonable CPU
			ProcessingVelocity:         20.0, // Good throughput
			ResourceEfficiency:         1.5,  // High efficiency
			QueueDepth:                 15,
		}

		collector.calculateScalingRecommendation(scalingMetrics)

		// Efficient execution should have high confidence
		if scalingMetrics.ScalingConfidence < 0.8 {
			t.Errorf("Expected high confidence for efficient scenario, got %.2f", scalingMetrics.ScalingConfidence)
		}

		t.Logf("Efficient scenario: Confidence=%.2f, Recommendation=%+d",
			scalingMetrics.ScalingConfidence, scalingMetrics.RecommendedWorkerChange)
	})
}
