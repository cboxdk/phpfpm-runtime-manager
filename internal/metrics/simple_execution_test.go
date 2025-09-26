package metrics

import (
	"net/http"
	"testing"

	"go.uber.org/zap/zaptest"
)

// TestSimplePHPFPMExecutionMonitoring tests basic execution monitoring without complex dependencies
func TestSimplePHPFPMExecutionMonitoring(t *testing.T) {
	t.Run("ValidateJSONFullFormatProcessing", func(t *testing.T) {
		// Test that we correctly process PHP-FPM status data that uses json&full format
		mockStatus := &PHPFPMStatus{
			Pool:            "test-pool",
			ProcessManager:  "dynamic",
			StartSince:      3600,
			AcceptedConn:    1000,
			ListenQueue:     5,
			MaxListenQueue:  10,
			IdleProcesses:   2,
			ActiveProcesses: 3,
			TotalProcesses:  5,
			SlowRequests:    1,
			Processes: []PHPFPMProcess{
				{PID: 1001, State: ProcessStateRunning, RequestDuration: 100, LastRequestMemory: 64 * 1024 * 1024, LastRequestCPU: 5.0},
				{PID: 1002, State: ProcessStateIdle, RequestDuration: 0, LastRequestMemory: 32 * 1024 * 1024, LastRequestCPU: 0.1},
				{PID: 1003, State: ProcessStateReading, RequestDuration: 50, LastRequestMemory: 48 * 1024 * 1024, LastRequestCPU: 3.0},
				{PID: 1004, State: ProcessStateIdle, RequestDuration: 0, LastRequestMemory: 28 * 1024 * 1024, LastRequestCPU: 0.1},
				{PID: 1005, State: ProcessStateFinishing, RequestDuration: 200, LastRequestMemory: 72 * 1024 * 1024, LastRequestCPU: 8.0},
			},
		}

		logger := zaptest.NewLogger(t)
		client := &PHPFPMClient{
			client: &http.Client{},
			logger: logger,
		}

		// Test that we can process the status data properly
		metrics, err := client.ProcessWorkerMetrics(mockStatus)
		if err != nil {
			t.Fatalf("Failed to process worker metrics: %v", err)
		}

		// Validate that we process individual processes (not aggregated data)
		expectedActive := 3 // Running, Reading, Finishing processes
		expectedIdle := 2   // Idle processes
		expectedTotal := 5

		if metrics.ActiveWorkers != expectedActive {
			t.Errorf("Expected %d active workers, got %d", expectedActive, metrics.ActiveWorkers)
		}
		if metrics.IdleWorkers != expectedIdle {
			t.Errorf("Expected %d idle workers, got %d", expectedIdle, metrics.IdleWorkers)
		}
		if metrics.TotalWorkers != expectedTotal {
			t.Errorf("Expected %d total workers, got %d", expectedTotal, metrics.TotalWorkers)
		}

		t.Log("✅ Successfully processed PHP-FPM status with individual process analysis")
	})

	t.Run("ProcessIndividualWorkers", func(t *testing.T) {
		// Test that we process individual workers, not aggregated data
		mockStatus := &PHPFPMStatus{
			Pool: "test-pool",
			// Aggregated data (potentially unreliable)
			IdleProcesses:   10, // Wrong value
			ActiveProcesses: 20, // Wrong value
			TotalProcesses:  30, // Wrong value
			ListenQueue:     0,
			Processes: []PHPFPMProcess{
				{PID: 1001, State: ProcessStateRunning, RequestDuration: 100, LastRequestMemory: 64 * 1024 * 1024, LastRequestCPU: 5.0},
				{PID: 1002, State: ProcessStateIdle, RequestDuration: 0, LastRequestMemory: 32 * 1024 * 1024, LastRequestCPU: 0.1},
				{PID: 1003, State: ProcessStateReading, RequestDuration: 50, LastRequestMemory: 48 * 1024 * 1024, LastRequestCPU: 3.0},
				{PID: 1004, State: ProcessStateIdle, RequestDuration: 0, LastRequestMemory: 28 * 1024 * 1024, LastRequestCPU: 0.1},
				{PID: 1005, State: ProcessStateFinishing, RequestDuration: 200, LastRequestMemory: 72 * 1024 * 1024, LastRequestCPU: 8.0},
			},
		}

		logger := zaptest.NewLogger(t)
		client := &PHPFPMClient{
			client: &http.Client{},
			logger: logger,
		}

		metrics, err := client.ProcessWorkerMetrics(mockStatus)
		if err != nil {
			t.Fatalf("Failed to process worker metrics: %v", err)
		}

		// Validate that we correctly identify worker states from INDIVIDUAL processes, not aggregated data
		expectedActive := 3 // Running, Reading, Finishing processes
		expectedIdle := 2   // Idle processes
		expectedTotal := 5

		if metrics.ActiveWorkers != expectedActive {
			t.Errorf("Expected %d active workers, got %d", expectedActive, metrics.ActiveWorkers)
		}
		if metrics.IdleWorkers != expectedIdle {
			t.Errorf("Expected %d idle workers, got %d", expectedIdle, metrics.IdleWorkers)
		}
		if metrics.TotalWorkers != expectedTotal {
			t.Errorf("Expected %d total workers, got %d", expectedTotal, metrics.TotalWorkers)
		}

		// Validate that we IGNORE aggregated data and use individual process data
		if metrics.TotalWorkers == int(mockStatus.TotalProcesses) {
			t.Error("Should use individual process count, not aggregated TotalProcesses")
		}
		if metrics.ActiveWorkers == int(mockStatus.ActiveProcesses) {
			t.Error("Should use individual process state analysis, not aggregated ActiveProcesses")
		}

		t.Logf("✅ Correctly processed individual workers: %d active, %d idle, %d total",
			metrics.ActiveWorkers, metrics.IdleWorkers, metrics.TotalWorkers)
	})

	t.Run("ExecutionMetricsAccuracy", func(t *testing.T) {
		// Test accuracy of execution-specific metrics
		mockStatus := &PHPFPMStatus{
			Pool:        "execution-test",
			ListenQueue: 10, // 10 jobs in queue
			Processes: []PHPFPMProcess{
				// 2 active workers, each taking 100ms per request
				{PID: 3001, State: ProcessStateRunning, RequestDuration: 100, LastRequestMemory: 64 * 1024 * 1024, LastRequestCPU: 5.0},
				{PID: 3002, State: ProcessStateRunning, RequestDuration: 100, LastRequestMemory: 64 * 1024 * 1024, LastRequestCPU: 5.0},
				// 1 idle worker
				{PID: 3003, State: ProcessStateIdle, RequestDuration: 0, LastRequestMemory: 32 * 1024 * 1024, LastRequestCPU: 0.0},
			},
		}

		logger := zaptest.NewLogger(t)
		client := &PHPFPMClient{
			client: &http.Client{},
			logger: logger,
		}

		metrics, err := client.ProcessWorkerMetrics(mockStatus)
		if err != nil {
			t.Fatalf("Failed to process worker metrics: %v", err)
		}

		// Validate processing velocity calculation
		// With 2 active workers averaging 100ms per request = 20 RPS
		expectedVelocity := 20.0
		tolerance := 0.1
		if absFloat(metrics.ProcessingVelocity-expectedVelocity) > tolerance {
			t.Errorf("Expected processing velocity ~%.1f, got %.1f", expectedVelocity, metrics.ProcessingVelocity)
		}

		// Validate queue processing time estimation
		// 10 jobs / 20 RPS = 0.5 seconds
		expectedProcessTimeMs := 500.0
		actualProcessTimeMs := float64(metrics.EstimatedProcessTime.Nanoseconds()) / 1e6
		if absFloat(actualProcessTimeMs-expectedProcessTimeMs) > 50.0 {
			t.Errorf("Expected process time ~%.0fms, got %.0fms", expectedProcessTimeMs, actualProcessTimeMs)
		}

		t.Logf("✅ Execution metrics: %.1f RPS velocity, %.0fms process time",
			metrics.ProcessingVelocity, actualProcessTimeMs)
	})

	t.Run("ScalingRelevantMetrics", func(t *testing.T) {
		// Test that we collect metrics essential for scaling decisions
		mockStatus := &PHPFPMStatus{
			Pool:        "scaling-test",
			ListenQueue: 20,
			Processes: []PHPFPMProcess{
				{PID: 4001, State: ProcessStateRunning, RequestDuration: 200, LastRequestMemory: 96 * 1024 * 1024, LastRequestCPU: 15.0},
				{PID: 4002, State: ProcessStateRunning, RequestDuration: 150, LastRequestMemory: 80 * 1024 * 1024, LastRequestCPU: 12.0},
				{PID: 4003, State: ProcessStateRunning, RequestDuration: 180, LastRequestMemory: 88 * 1024 * 1024, LastRequestCPU: 14.0},
				{PID: 4004, State: ProcessStateIdle, RequestDuration: 0, LastRequestMemory: 32 * 1024 * 1024, LastRequestCPU: 0.5},
				{PID: 4005, State: ProcessStateIdle, RequestDuration: 0, LastRequestMemory: 28 * 1024 * 1024, LastRequestCPU: 0.3},
			},
		}

		logger := zaptest.NewLogger(t)
		client := &PHPFPMClient{
			client: &http.Client{},
			logger: logger,
		}

		metrics, err := client.ProcessWorkerMetrics(mockStatus)
		if err != nil {
			t.Fatalf("Failed to process worker metrics: %v", err)
		}

		// Validate memory metrics for memory-aware scaling
		totalMemory := int64(0)
		activeMemory := int64(0)
		activeWorkers := 0

		for _, proc := range metrics.ProcessMetrics {
			totalMemory += proc.LastRequestMemory
			if proc.IsActive {
				activeMemory += proc.LastRequestMemory
				activeWorkers++
			}
		}

		avgMemoryPerWorker := float64(totalMemory) / float64(len(metrics.ProcessMetrics)) / 1024 / 1024
		avgActiveMemory := float64(activeMemory) / float64(activeWorkers) / 1024 / 1024

		// Validate that we track memory usage per worker
		if avgMemoryPerWorker == 0 {
			t.Error("Expected non-zero average memory per worker")
		}
		if avgActiveMemory == 0 {
			t.Error("Expected non-zero average active worker memory")
		}

		// Validate CPU metrics for CPU-aware scaling
		totalCPU := 0.0
		activeCPU := 0.0
		activeCPUWorkers := 0

		for _, proc := range metrics.ProcessMetrics {
			totalCPU += proc.LastRequestCPU
			if proc.IsActive {
				activeCPU += proc.LastRequestCPU
				activeCPUWorkers++
			}
		}

		avgCPUPerWorker := totalCPU / float64(len(metrics.ProcessMetrics))
		avgActiveCPU := activeCPU / float64(activeCPUWorkers)

		if avgCPUPerWorker == 0 {
			t.Error("Expected non-zero average CPU per worker")
		}
		if avgActiveCPU == 0 {
			t.Error("Expected non-zero average active worker CPU")
		}

		t.Logf("✅ Scaling metrics: AvgMem=%.1fMB, ActiveMem=%.1fMB, AvgCPU=%.2f%%, ActiveCPU=%.2f%%",
			avgMemoryPerWorker, avgActiveMemory, avgCPUPerWorker, avgActiveCPU)
	})
}

// absFloat returns the absolute value of x
func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
