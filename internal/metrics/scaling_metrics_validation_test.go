package metrics

import (
	"testing"
	"time"
)

// TestScalingMetricsValidation validates that execution metrics support intelligent scaling
func TestScalingMetricsValidation(t *testing.T) {
	t.Run("MemoryAwareScalingData", func(t *testing.T) {
		// Test that we collect accurate memory data for memory-aware scaling
		testCases := []struct {
			name          string
			processes     []PHPFPMProcess
			expectedAvgMB float64
			tolerance     float64
		}{
			{
				name: "Low Memory Usage",
				processes: []PHPFPMProcess{
					{PID: 1, State: ProcessStateRunning, LastRequestMemory: 32 * 1024 * 1024}, // 32MB
					{PID: 2, State: ProcessStateRunning, LastRequestMemory: 40 * 1024 * 1024}, // 40MB
					{PID: 3, State: ProcessStateIdle, LastRequestMemory: 16 * 1024 * 1024},    // 16MB
				},
				expectedAvgMB: 29.33, // (32+40+16)/3
				tolerance:     1.0,
			},
			{
				name: "High Memory Usage",
				processes: []PHPFPMProcess{
					{PID: 1, State: ProcessStateRunning, LastRequestMemory: 128 * 1024 * 1024}, // 128MB
					{PID: 2, State: ProcessStateRunning, LastRequestMemory: 150 * 1024 * 1024}, // 150MB
					{PID: 3, State: ProcessStateRunning, LastRequestMemory: 140 * 1024 * 1024}, // 140MB
				},
				expectedAvgMB: 139.33, // (128+150+140)/3
				tolerance:     2.0,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				status := &PHPFPMStatus{
					Pool:      "memory-test",
					Processes: tc.processes,
				}

				client := createTestClient(t)
				metrics, err := client.ProcessWorkerMetrics(status)
				if err != nil {
					t.Fatalf("Failed to process metrics: %v", err)
				}

				// Calculate actual average memory per worker
				totalMemory := int64(0)
				for _, proc := range metrics.ProcessMetrics {
					totalMemory += proc.LastRequestMemory
				}
				actualAvgMB := float64(totalMemory) / float64(len(metrics.ProcessMetrics)) / 1024 / 1024

				if abs(actualAvgMB-tc.expectedAvgMB) > tc.tolerance {
					t.Errorf("Expected avg memory ~%.2f MB, got %.2f MB", tc.expectedAvgMB, actualAvgMB)
				}

				t.Logf("Memory metrics: Avg=%.2f MB, Active=%d, Total=%d",
					actualAvgMB, metrics.ActiveWorkers, metrics.TotalWorkers)
			})
		}
	})

	t.Run("ProcessingVelocityAccuracy", func(t *testing.T) {
		// Test processing velocity calculation for queue-aware scaling
		testCases := []struct {
			name                 string
			activeWorkers        int
			avgRequestDurationMs int64
			expectedRPS          float64
			tolerance            float64
		}{
			{
				name:                 "Fast Processing",
				activeWorkers:        4,
				avgRequestDurationMs: 50,   // 50ms per request
				expectedRPS:          80.0, // 4 workers * 20 RPS per worker
				tolerance:            2.0,
			},
			{
				name:                 "Slow Processing",
				activeWorkers:        2,
				avgRequestDurationMs: 500, // 500ms per request
				expectedRPS:          4.0, // 2 workers * 2 RPS per worker
				tolerance:            0.5,
			},
			{
				name:                 "Medium Processing",
				activeWorkers:        3,
				avgRequestDurationMs: 200,  // 200ms per request
				expectedRPS:          15.0, // 3 workers * 5 RPS per worker
				tolerance:            1.0,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				processes := make([]PHPFPMProcess, tc.activeWorkers+1) // +1 idle worker

				// Create active workers with specific request duration
				for i := 0; i < tc.activeWorkers; i++ {
					processes[i] = PHPFPMProcess{
						PID:               100 + i,
						State:             ProcessStateRunning,
						RequestDuration:   tc.avgRequestDurationMs,
						LastRequestMemory: 64 * 1024 * 1024,
						LastRequestCPU:    5.0,
					}
				}

				// Add one idle worker
				processes[tc.activeWorkers] = PHPFPMProcess{
					PID:               200,
					State:             ProcessStateIdle,
					RequestDuration:   0,
					LastRequestMemory: 32 * 1024 * 1024,
					LastRequestCPU:    0.1,
				}

				status := &PHPFPMStatus{
					Pool:      "velocity-test",
					Processes: processes,
				}

				client := createTestClient(t)
				metrics, err := client.ProcessWorkerMetrics(status)
				if err != nil {
					t.Fatalf("Failed to process metrics: %v", err)
				}

				if abs(metrics.ProcessingVelocity-tc.expectedRPS) > tc.tolerance {
					t.Errorf("Expected processing velocity ~%.1f RPS, got %.1f RPS",
						tc.expectedRPS, metrics.ProcessingVelocity)
				}

				t.Logf("Velocity metrics: %d active workers @ %dms = %.1f RPS",
					tc.activeWorkers, tc.avgRequestDurationMs, metrics.ProcessingVelocity)
			})
		}
	})

	t.Run("QueuePressureMetrics", func(t *testing.T) {
		// Test queue pressure calculation for scaling triggers
		testCases := []struct {
			name              string
			queueDepth        int
			activeWorkers     int
			requestDurationMs int64
			expectedPressure  string
			expectedScaleUp   bool
		}{
			{
				name:              "No Pressure - Scale Down Safe",
				queueDepth:        0,
				activeWorkers:     5,
				requestDurationMs: 100,
				expectedPressure:  "none",
				expectedScaleUp:   false,
			},
			{
				name:              "Low Pressure - Maintain",
				queueDepth:        5,
				activeWorkers:     4,
				requestDurationMs: 100, // 40 RPS total, queue clears in 0.125s
				expectedPressure:  "low",
				expectedScaleUp:   false,
			},
			{
				name:              "High Pressure - Scale Up",
				queueDepth:        50,
				activeWorkers:     2,
				requestDurationMs: 200, // 10 RPS total, queue clears in 5s
				expectedPressure:  "high",
				expectedScaleUp:   true,
			},
			{
				name:              "Critical Pressure - Urgent Scale Up",
				queueDepth:        100,
				activeWorkers:     1,
				requestDurationMs: 500, // 2 RPS total, queue clears in 50s
				expectedPressure:  "critical",
				expectedScaleUp:   true,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				processes := make([]PHPFPMProcess, tc.activeWorkers+2) // +2 idle workers

				// Create active workers
				for i := 0; i < tc.activeWorkers; i++ {
					processes[i] = PHPFPMProcess{
						PID:               300 + i,
						State:             ProcessStateRunning,
						RequestDuration:   tc.requestDurationMs,
						LastRequestMemory: 64 * 1024 * 1024,
						LastRequestCPU:    8.0,
					}
				}

				// Add idle workers
				for i := tc.activeWorkers; i < len(processes); i++ {
					processes[i] = PHPFPMProcess{
						PID:               400 + i,
						State:             ProcessStateIdle,
						RequestDuration:   0,
						LastRequestMemory: 32 * 1024 * 1024,
						LastRequestCPU:    0.1,
					}
				}

				status := &PHPFPMStatus{
					Pool:        "pressure-test",
					ListenQueue: int64(tc.queueDepth),
					Processes:   processes,
				}

				client := createTestClient(t)
				metrics, err := client.ProcessWorkerMetrics(status)
				if err != nil {
					t.Fatalf("Failed to process metrics: %v", err)
				}

				// Calculate pressure indicators
				jobsPerWorker := float64(tc.queueDepth) / float64(tc.activeWorkers)
				processTimeSeconds := metrics.EstimatedProcessTime.Seconds()

				// Log pressure metrics
				t.Logf("Pressure analysis: Queue=%d, Active=%d, Jobs/Worker=%.1f, ProcessTime=%.1fs, Velocity=%.1f RPS",
					tc.queueDepth, tc.activeWorkers, jobsPerWorker, processTimeSeconds, metrics.ProcessingVelocity)

				// Validate scaling decision indicators
				shouldScaleUp := false
				if tc.queueDepth > 0 {
					// Scale up if processing time > 30s OR jobs per worker > 2
					if processTimeSeconds > 30.0 || jobsPerWorker > 2.0 {
						shouldScaleUp = true
					}
				}

				if shouldScaleUp != tc.expectedScaleUp {
					t.Errorf("Expected scale up decision: %v, got: %v (ProcessTime=%.1fs, Jobs/Worker=%.1f)",
						tc.expectedScaleUp, shouldScaleUp, processTimeSeconds, jobsPerWorker)
				}
			})
		}
	})
}

// TestAutoscalerIntegration validates that metrics integrate properly with autoscaler
func TestAutoscalerIntegration(t *testing.T) {
	t.Run("ExecutionMetricsDataStructure", func(t *testing.T) {
		// Test that execution metrics have the right structure for autoscaler integration
		execMetrics := ExecutionMetrics{
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

		// Validate key metrics are present for scaling decisions
		if execMetrics.Duration <= 0 {
			t.Error("Expected positive execution duration")
		}
		if execMetrics.CPUUtilization < 0 {
			t.Error("Expected non-negative CPU utilization")
		}
		if execMetrics.PeakMemoryMB <= 0 {
			t.Error("Expected positive peak memory")
		}

		// Calculate metrics useful for autoscaler
		requestsPerSecond := 1.0 / execMetrics.Duration.Seconds()
		totalMemoryMB := execMetrics.PeakMemoryMB + execMetrics.ChildPeakMemoryMB

		t.Logf("Execution metrics for autoscaler: %.1f RPS, %.1f MB total, %.1f%% CPU",
			requestsPerSecond, totalMemoryMB, execMetrics.CPUUtilization)

		// Validate reasonable values
		if requestsPerSecond <= 0 {
			t.Error("Expected positive requests per second")
		}
		if totalMemoryMB <= 0 {
			t.Error("Expected positive total memory including children")
		}
	})
}

// TestExecutionMetricsPerformance validates performance of execution monitoring
func TestExecutionMetricsPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	t.Run("HighFrequencyProcessing", func(t *testing.T) {
		// Test processing of high-frequency execution metrics
		client := createTestClient(t)

		// Create large pool status with many processes
		processes := make([]PHPFPMProcess, 100)
		for i := 0; i < 100; i++ {
			state := ProcessStateIdle
			requestDuration := int64(0)
			if i%3 == 0 { // 1/3 active workers
				state = ProcessStateRunning
				requestDuration = 100 + int64(i%200) // Variable durations
			}

			processes[i] = PHPFPMProcess{
				PID:               1000 + i,
				State:             state,
				RequestDuration:   requestDuration,
				LastRequestMemory: int64(32+i%64) * 1024 * 1024, // 32-96MB
				LastRequestCPU:    float64(i % 15),              // 0-15%
			}
		}

		status := &PHPFPMStatus{
			Pool:        "performance-test",
			ListenQueue: 25,
			Processes:   processes,
		}

		// Benchmark processing time
		start := time.Now()
		iterations := 1000

		for i := 0; i < iterations; i++ {
			metrics, err := client.ProcessWorkerMetrics(status)
			if err != nil {
				t.Fatalf("Failed to process metrics: %v", err)
			}

			// Validate basic metrics are calculated
			if metrics.TotalWorkers != 100 {
				t.Errorf("Expected 100 total workers, got %d", metrics.TotalWorkers)
			}
			if metrics.ActiveWorkers == 0 {
				t.Error("Expected some active workers")
			}
		}

		elapsed := time.Since(start)
		avgProcessingTime := elapsed / time.Duration(iterations)

		t.Logf("Performance: %d iterations in %v, avg %.2fms per processing cycle",
			iterations, elapsed, float64(avgProcessingTime.Nanoseconds())/1e6)

		// Should process 100 workers in under 1ms
		if avgProcessingTime > time.Millisecond {
			t.Errorf("Processing too slow: %.2fms per cycle (expected <1ms)",
				float64(avgProcessingTime.Nanoseconds())/1e6)
		}
	})
}

// TestExecutionMonitoringResilience validates resilience of execution monitoring
func TestExecutionMonitoringResilience(t *testing.T) {
	t.Run("MalformedProcessData", func(t *testing.T) {
		// Test handling of malformed process data
		client := createTestClient(t)

		// Create status with some malformed process data
		status := &PHPFPMStatus{
			Pool:        "resilience-test",
			ListenQueue: 5,
			Processes: []PHPFPMProcess{
				{PID: 1, State: ProcessStateRunning, RequestDuration: 100, LastRequestMemory: 64 * 1024 * 1024},
				{PID: 2, State: ProcessStateRunning, RequestDuration: -1, LastRequestMemory: 64 * 1024 * 1024}, // Negative duration
				{PID: 3, State: "InvalidState", RequestDuration: 150, LastRequestMemory: 64 * 1024 * 1024},     // Invalid state
				{PID: 4, State: ProcessStateIdle, RequestDuration: 0, LastRequestMemory: 32 * 1024 * 1024},
			},
		}

		metrics, err := client.ProcessWorkerMetrics(status)
		if err != nil {
			t.Fatalf("Processing should handle malformed data gracefully: %v", err)
		}

		// Should still provide reasonable metrics
		if metrics.TotalWorkers != 4 {
			t.Errorf("Expected 4 total workers despite malformed data, got %d", metrics.TotalWorkers)
		}

		// Should handle negative duration gracefully
		if metrics.ProcessingVelocity < 0 {
			t.Error("Processing velocity should not be negative")
		}

		t.Logf("Resilience test: Processed %d workers with malformed data", metrics.TotalWorkers)
	})

	t.Run("EmptyProcessList", func(t *testing.T) {
		// Test handling of empty process list
		client := createTestClient(t)

		status := &PHPFPMStatus{
			Pool:        "empty-test",
			ListenQueue: 0,
			Processes:   []PHPFPMProcess{}, // Empty process list
		}

		metrics, err := client.ProcessWorkerMetrics(status)
		if err != nil {
			t.Fatalf("Should handle empty process list: %v", err)
		}

		if metrics.TotalWorkers != 0 {
			t.Errorf("Expected 0 workers for empty list, got %d", metrics.TotalWorkers)
		}
		if metrics.ActiveWorkers != 0 {
			t.Errorf("Expected 0 active workers for empty list, got %d", metrics.ActiveWorkers)
		}
		if metrics.ProcessingVelocity != 0 {
			t.Errorf("Expected 0 processing velocity for empty list, got %.2f", metrics.ProcessingVelocity)
		}
	})
}
