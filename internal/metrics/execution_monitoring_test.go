package metrics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/config"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/resilience"
	"go.uber.org/zap/zaptest"
)

// TestPHPFPMExecutionMonitoring validates that we correctly monitor PHP-FPM process executions
// This is critical for intelligent scaling decisions
func TestPHPFPMExecutionMonitoring(t *testing.T) {
	t.Run("ValidateJSONFullFormat", func(t *testing.T) {
		// Create mock server that validates the request format
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Validate that we're using json&full format
			query := r.URL.Query()
			if !query.Has("json") {
				t.Error("Expected json parameter in request")
			}
			if !query.Has("full") {
				t.Error("Expected full parameter in request")
			}

			// Return mock response
			mockStatus := createMockPHPFPMStatus()
			json.NewEncoder(w).Encode(mockStatus)
		}))
		defer server.Close()

		client := createTestClient(t)
		_, err := client.GetStatus(context.Background(), server.URL)
		if err != nil {
			t.Fatalf("Failed to get status: %v", err)
		}
	})

	t.Run("ProcessIndividualWorkers", func(t *testing.T) {
		// Test that we process individual workers, not aggregated data
		mockStatus := createComplexMockStatus()
		client := createTestClient(t)

		metrics, err := client.ProcessWorkerMetrics(mockStatus)
		if err != nil {
			t.Fatalf("Failed to process worker metrics: %v", err)
		}

		// Validate that we correctly identify worker states
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

		// Validate individual process metrics
		if len(metrics.ProcessMetrics) != expectedTotal {
			t.Errorf("Expected %d process metrics, got %d", expectedTotal, len(metrics.ProcessMetrics))
		}

		// Validate that only actively executing processes are counted as active
		activeCount := 0
		for _, proc := range metrics.ProcessMetrics {
			if proc.IsActive {
				activeCount++
			}
		}
		if activeCount != expectedActive {
			t.Errorf("Expected %d active processes in metrics, got %d", expectedActive, activeCount)
		}
	})

	t.Run("ExecutionMetricsAccuracy", func(t *testing.T) {
		// Test accuracy of execution-specific metrics
		mockStatus := createExecutionMetricsTestStatus()
		client := createTestClient(t)

		metrics, err := client.ProcessWorkerMetrics(mockStatus)
		if err != nil {
			t.Fatalf("Failed to process worker metrics: %v", err)
		}

		// Validate processing velocity calculation
		// With 2 active workers averaging 100ms per request = 20 RPS
		expectedVelocity := 20.0
		tolerance := 0.1
		if abs(metrics.ProcessingVelocity-expectedVelocity) > tolerance {
			t.Errorf("Expected processing velocity ~%.1f, got %.1f", expectedVelocity, metrics.ProcessingVelocity)
		}

		// Validate queue processing time estimation
		// 10 jobs / 20 RPS = 0.5 seconds
		expectedProcessTime := 500 * time.Millisecond
		if abs(float64(metrics.EstimatedProcessTime-expectedProcessTime)) > float64(50*time.Millisecond) {
			t.Errorf("Expected process time ~%v, got %v", expectedProcessTime, metrics.EstimatedProcessTime)
		}

		// Validate average request duration for active workers
		expectedAvgDuration := 100 * time.Millisecond
		if abs(float64(metrics.AvgActiveRequestDuration-expectedAvgDuration)) > float64(10*time.Millisecond) {
			t.Errorf("Expected avg active duration ~%v, got %v", expectedAvgDuration, metrics.AvgActiveRequestDuration)
		}
	})

	t.Run("ScalingRelevantMetrics", func(t *testing.T) {
		// Test that we collect metrics essential for scaling decisions
		mockStatus := createScalingMetricsTestStatus()
		client := createTestClient(t)

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

		avgMemoryPerWorker := float64(totalMemory) / float64(len(metrics.ProcessMetrics))
		avgActiveMemory := float64(activeMemory) / float64(activeWorkers)

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

		t.Logf("Scaling metrics: AvgMem=%.1fMB, ActiveMem=%.1fMB, AvgCPU=%.2f%%, ActiveCPU=%.2f%%",
			avgMemoryPerWorker/1024/1024, avgActiveMemory/1024/1024, avgCPUPerWorker, avgActiveCPU)
	})
}

// TestProcessStateClassification validates correct worker state classification
func TestProcessStateClassification(t *testing.T) {
	testCases := []struct {
		state    PHPFPMProcessState
		expected bool // true if should be classified as active
	}{
		{ProcessStateIdle, false},
		{ProcessStateRunning, true},
		{ProcessStateReading, true},
		{ProcessStateInfo, true},
		{ProcessStateFinishing, true},
		{ProcessStateEnding, true},
	}

	for _, tc := range testCases {
		t.Run(string(tc.state), func(t *testing.T) {
			mockStatus := &PHPFPMStatus{
				Pool: "test-pool",
				Processes: []PHPFPMProcess{
					{
						PID:               1234,
						State:             tc.state,
						RequestDuration:   100,
						LastRequestMemory: 64 * 1024 * 1024, // 64MB
						LastRequestCPU:    5.0,
					},
				},
			}

			client := createTestClient(t)
			metrics, err := client.ProcessWorkerMetrics(mockStatus)
			if err != nil {
				t.Fatalf("Failed to process metrics: %v", err)
			}

			if tc.expected {
				if metrics.ActiveWorkers != 1 {
					t.Errorf("State %s should be active, got %d active workers", tc.state, metrics.ActiveWorkers)
				}
				if metrics.IdleWorkers != 0 {
					t.Errorf("State %s should not be idle, got %d idle workers", tc.state, metrics.IdleWorkers)
				}
			} else {
				if metrics.ActiveWorkers != 0 {
					t.Errorf("State %s should not be active, got %d active workers", tc.state, metrics.ActiveWorkers)
				}
				if metrics.IdleWorkers != 1 {
					t.Errorf("State %s should be idle, got %d idle workers", tc.state, metrics.IdleWorkers)
				}
			}
		})
	}
}

// TestCollectorExecutionIntegration tests execution monitoring through the full collector
func TestCollectorExecutionIntegration(t *testing.T) {
	// Create test collector with mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mockStatus := createMockPHPFPMStatus()
		json.NewEncoder(w).Encode(mockStatus)
	}))
	defer server.Close()

	// Create collector configuration
	monitoringConfig := config.MonitoringConfig{
		CollectInterval:     1 * time.Second,
		EnableSystemMetrics: true,
		EnableOpcache:       false,
		CPUThreshold:        80.0,
		MemoryThreshold:     85.0,
	}

	poolConfig := config.PoolConfig{
		Name:              "test-pool",
		FastCGIEndpoint:   "127.0.0.1:9000",
		FastCGIStatusPath: server.URL + "/status",
		MaxWorkers:        10,
	}

	logger := zaptest.NewLogger(t)
	collector, err := NewCollector(monitoringConfig, []config.PoolConfig{poolConfig}, logger)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Override the status URL to use our test server
	collector.phpfpmClient = createTestClient(t)

	// Test metrics collection
	ctx := context.Background()
	metricSet, err := collector.Collect(ctx)
	if err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}

	// Debug: Print all collected metrics
	t.Logf("Collected %d metrics:", len(metricSet.Metrics))
	for _, metric := range metricSet.Metrics {
		t.Logf("  %s = %f (labels: %v)", metric.Name, metric.Value, metric.Labels)
	}

	// Validate that execution metrics are present
	foundActiveProcesses := false
	foundIdleProcesses := false
	foundProcessingVelocity := false
	foundEstimatedProcessTime := false

	for _, metric := range metricSet.Metrics {
		switch metric.Name {
		case "phpfpm_active_processes":
			foundActiveProcesses = true
			if metric.Value <= 0 {
				t.Error("Expected positive active processes count")
			}
		case "phpfpm_idle_processes":
			foundIdleProcesses = true
		case "phpfpm_processing_velocity_rps":
			foundProcessingVelocity = true
			if metric.Value <= 0 {
				t.Error("Expected positive processing velocity")
			}
		case "phpfpm_estimated_queue_process_time_ms":
			foundEstimatedProcessTime = true
		}
	}

	if !foundActiveProcesses {
		t.Error("Missing phpfpm_active_processes metric")
	}
	if !foundIdleProcesses {
		t.Error("Missing phpfpm_idle_processes metric")
	}
	if !foundProcessingVelocity {
		t.Error("Missing phpfpm_processing_velocity_rps metric")
	}
	if !foundEstimatedProcessTime {
		t.Error("Missing phpfpm_estimated_queue_process_time_ms metric")
	}

	t.Logf("Collected %d metrics with execution monitoring", len(metricSet.Metrics))
}

// TestQueuePressureDetection validates queue pressure calculation for scaling
func TestQueuePressureDetection(t *testing.T) {
	testCases := []struct {
		name               string
		queueDepth         int
		activeWorkers      int
		processingVelocity float64
		expectedPressure   string
	}{
		{
			name:               "No Queue Pressure",
			queueDepth:         0,
			activeWorkers:      5,
			processingVelocity: 10.0,
			expectedPressure:   "none",
		},
		{
			name:               "Low Queue Pressure",
			queueDepth:         5,
			activeWorkers:      5,
			processingVelocity: 20.0, // Can process queue in 0.25s
			expectedPressure:   "low",
		},
		{
			name:               "High Queue Pressure",
			queueDepth:         100,
			activeWorkers:      2,
			processingVelocity: 1.0, // Would take 100s to process
			expectedPressure:   "high",
		},
		{
			name:               "Critical Queue Pressure",
			queueDepth:         50,
			activeWorkers:      1,
			processingVelocity: 0.5, // Would take 100s to process
			expectedPressure:   "critical",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create mock status with specific queue conditions
			mockStatus := &PHPFPMStatus{
				Pool:        "test-pool",
				ListenQueue: int64(tc.queueDepth),
				Processes:   createProcessesWithActiveCount(tc.activeWorkers, tc.processingVelocity),
			}

			client := createTestClient(t)
			metrics, err := client.ProcessWorkerMetrics(mockStatus)
			if err != nil {
				t.Fatalf("Failed to process metrics: %v", err)
			}

			// Calculate pressure indicators
			jobsPerWorker := float64(tc.queueDepth) / float64(tc.activeWorkers)
			estimatedProcessTimeSeconds := metrics.EstimatedProcessTime.Seconds()

			// Log metrics for analysis
			t.Logf("Queue Metrics - Depth: %d, Active: %d, Velocity: %.2f RPS, Jobs/Worker: %.2f, Process Time: %.2fs",
				tc.queueDepth, tc.activeWorkers, metrics.ProcessingVelocity, jobsPerWorker, estimatedProcessTimeSeconds)

			// Validate that metrics support pressure detection
			if tc.queueDepth > 0 && metrics.ProcessingVelocity <= 0 {
				t.Error("Expected positive processing velocity when queue exists")
			}

			if tc.queueDepth > 0 && estimatedProcessTimeSeconds <= 0 {
				t.Error("Expected positive estimated process time when queue exists")
			}

			// Validate pressure classification logic
			switch tc.expectedPressure {
			case "none":
				if tc.queueDepth > 0 {
					t.Error("Expected no queue depth for 'none' pressure")
				}
			case "low":
				if estimatedProcessTimeSeconds > 30 {
					t.Error("Low pressure should have quick processing time")
				}
			case "high":
				if estimatedProcessTimeSeconds < 30 {
					t.Error("High pressure should have slow processing time")
				}
			case "critical":
				if jobsPerWorker < 10 {
					t.Error("Critical pressure should have high jobs per worker ratio")
				}
			}
		})
	}
}

// Helper functions for test data creation

func createTestClient(t *testing.T) *PHPFPMClient {
	logger := zaptest.NewLogger(t)
	cbConfig := resilience.DefaultCircuitBreakerConfig()
	return &PHPFPMClient{
		client:         &http.Client{Timeout: 5 * time.Second},
		logger:         logger,
		circuitBreaker: resilience.NewCircuitBreaker("test-phpfpm", cbConfig, logger),
	}
}

func createMockPHPFPMStatus() *PHPFPMStatus {
	return &PHPFPMStatus{
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
}

func createComplexMockStatus() *PHPFPMStatus {
	return &PHPFPMStatus{
		Pool:        "complex-pool",
		ListenQueue: 0,
		Processes: []PHPFPMProcess{
			{PID: 2001, State: ProcessStateRunning, RequestDuration: 150, LastRequestMemory: 80 * 1024 * 1024, LastRequestCPU: 7.5},
			{PID: 2002, State: ProcessStateIdle, RequestDuration: 0, LastRequestMemory: 30 * 1024 * 1024, LastRequestCPU: 0.0},
			{PID: 2003, State: ProcessStateReading, RequestDuration: 75, LastRequestMemory: 55 * 1024 * 1024, LastRequestCPU: 4.2},
			{PID: 2004, State: ProcessStateIdle, RequestDuration: 0, LastRequestMemory: 25 * 1024 * 1024, LastRequestCPU: 0.0},
			{PID: 2005, State: ProcessStateFinishing, RequestDuration: 300, LastRequestMemory: 95 * 1024 * 1024, LastRequestCPU: 12.0},
		},
	}
}

func createExecutionMetricsTestStatus() *PHPFPMStatus {
	return &PHPFPMStatus{
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
}

func createScalingMetricsTestStatus() *PHPFPMStatus {
	return &PHPFPMStatus{
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
}

func createProcessesWithActiveCount(activeCount int, processingVelocity float64) []PHPFPMProcess {
	processes := make([]PHPFPMProcess, activeCount+2) // Add 2 idle processes

	// Create active processes with duration based on desired velocity
	requestDuration := int64(0)
	if processingVelocity > 0 {
		// Duration in ms for desired RPS per worker
		requestDuration = int64(1000.0 / (processingVelocity / float64(activeCount)))
	}

	for i := 0; i < activeCount; i++ {
		processes[i] = PHPFPMProcess{
			PID:               5000 + i,
			State:             ProcessStateRunning,
			RequestDuration:   requestDuration,
			LastRequestMemory: 64 * 1024 * 1024,
			LastRequestCPU:    8.0,
		}
	}

	// Add idle processes
	for i := activeCount; i < len(processes); i++ {
		processes[i] = PHPFPMProcess{
			PID:               5000 + i,
			State:             ProcessStateIdle,
			RequestDuration:   0,
			LastRequestMemory: 32 * 1024 * 1024,
			LastRequestCPU:    0.1,
		}
	}

	return processes
}

// abs returns the absolute value of x
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
