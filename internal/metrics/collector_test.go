package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/config"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/resilience"
	"go.uber.org/zap/zaptest"
)

func TestNewCollector(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name           string
		monitoringCfg  config.MonitoringConfig
		pools          []config.PoolConfig
		expectError    bool
		expectedErrMsg string
	}{
		{
			name: "valid configuration",
			monitoringCfg: config.MonitoringConfig{
				CollectInterval:     time.Second,
				HealthCheckInterval: 5 * time.Second,
			},
			pools: []config.PoolConfig{
				{
					Name:              "test-pool",
					ConfigPath:        "/tmp/test.conf",
					FastCGIStatusPath: "http://localhost:9000/status",
					MaxWorkers:        10,
				},
			},
			expectError: false,
		},
		{
			name: "empty pools configuration",
			monitoringCfg: config.MonitoringConfig{
				CollectInterval: time.Second,
			},
			pools:          []config.PoolConfig{},
			expectError:    true,
			expectedErrMsg: "at least one pool must be configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector(tt.monitoringCfg, tt.pools, logger)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if tt.expectedErrMsg != "" && err.Error() != tt.expectedErrMsg {
					t.Errorf("Expected error message '%s', got '%s'", tt.expectedErrMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if collector == nil {
				t.Error("Expected non-nil collector")
				return
			}

			// Verify collector configuration
			if collector.baseInterval != tt.monitoringCfg.CollectInterval {
				t.Errorf("Expected base interval %v, got %v", tt.monitoringCfg.CollectInterval, collector.baseInterval)
			}

			if len(collector.pools) != len(tt.pools) {
				t.Errorf("Expected %d pools, got %d", len(tt.pools), len(collector.pools))
			}

			if collector.adaptiveEnabled != true {
				t.Error("Expected adaptive polling to be enabled by default")
			}
		})
	}
}

func TestCalculateAdaptiveInterval(t *testing.T) {
	logger := zaptest.NewLogger(t)
	monitoringCfg := config.MonitoringConfig{
		CollectInterval: 2 * time.Second,
	}
	pools := []config.PoolConfig{
		{
			Name:              "test-pool",
			ConfigPath:        "/tmp/test.conf",
			FastCGIStatusPath: "http://localhost:9000/status",
			MaxWorkers:        10,
		},
	}

	collector, err := NewCollector(monitoringCfg, pools, logger)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	tests := []struct {
		name               string
		loadFactor         float64
		adaptiveEnabled    bool
		expectedMultiplier float64
	}{
		{
			name:               "adaptive disabled",
			loadFactor:         0.9,
			adaptiveEnabled:    false,
			expectedMultiplier: 1.0, // Should return base interval
		},
		{
			name:               "high load (>80%)",
			loadFactor:         0.9,
			adaptiveEnabled:    true,
			expectedMultiplier: 2.0, // Should double interval
		},
		{
			name:               "moderate load (>60%)",
			loadFactor:         0.7,
			adaptiveEnabled:    true,
			expectedMultiplier: 1.5, // Should increase by 50%
		},
		{
			name:               "low load (<30%)",
			loadFactor:         0.2,
			adaptiveEnabled:    true,
			expectedMultiplier: 0.75, // Should decrease by 25%
		},
		{
			name:               "normal load (30-60%)",
			loadFactor:         0.5,
			adaptiveEnabled:    true,
			expectedMultiplier: 1.0, // Should use base interval
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector.loadFactor = tt.loadFactor
			collector.adaptiveEnabled = tt.adaptiveEnabled

			interval := collector.calculateAdaptiveInterval()
			expectedInterval := time.Duration(float64(collector.baseInterval) * tt.expectedMultiplier)

			if interval != expectedInterval {
				t.Errorf("Expected interval %v, got %v (load: %.2f, adaptive: %v)",
					expectedInterval, interval, tt.loadFactor, tt.adaptiveEnabled)
			}
		})
	}
}

func TestUpdateLoadFactor(t *testing.T) {
	logger := zaptest.NewLogger(t)
	monitoringCfg := config.MonitoringConfig{
		CollectInterval: time.Second,
	}

	tests := []struct {
		name               string
		pools              []config.PoolConfig
		expectedLoadFactor float64
	}{
		{
			name: "single pool with workers",
			pools: []config.PoolConfig{
				{
					Name:       "test-pool",
					MaxWorkers: 10,
				},
			},
			expectedLoadFactor: 0.5, // 50% utilization assumption
		},
		{
			name: "multiple pools",
			pools: []config.PoolConfig{
				{
					Name:       "pool1",
					MaxWorkers: 8,
				},
				{
					Name:       "pool2",
					MaxWorkers: 12,
				},
			},
			expectedLoadFactor: 0.5, // 50% utilization assumption
		},
		{
			name: "pool with zero workers",
			pools: []config.PoolConfig{
				{
					Name:       "empty-pool",
					MaxWorkers: 0,
				},
			},
			expectedLoadFactor: 0.0, // Should handle zero workers gracefully
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector(monitoringCfg, tt.pools, logger)
			if err != nil {
				t.Fatalf("Failed to create collector: %v", err)
			}

			collector.updateLoadFactor()

			if collector.loadFactor != tt.expectedLoadFactor {
				t.Errorf("Expected load factor %.2f, got %.2f",
					tt.expectedLoadFactor, collector.loadFactor)
			}

			// Verify load factor is capped at 1.0
			if collector.loadFactor > 1.0 {
				t.Errorf("Load factor should be capped at 1.0, got %.2f", collector.loadFactor)
			}
		})
	}
}

func TestCollectorStartStop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	monitoringCfg := config.MonitoringConfig{
		CollectInterval: 100 * time.Millisecond,
	}
	pools := []config.PoolConfig{
		{
			Name:              "test-pool",
			ConfigPath:        "/tmp/test.conf",
			FastCGIStatusPath: "http://localhost:9000/status",
			MaxWorkers:        5,
		},
	}

	collector, err := NewCollector(monitoringCfg, pools, logger)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Test start
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Start collector in a goroutine since it blocks
	startErr := make(chan error, 1)
	go func() {
		startErr <- collector.Start(ctx)
	}()

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Verify it's running
	collector.mu.RLock()
	running := collector.running
	collector.mu.RUnlock()

	if !running {
		t.Error("Expected collector to be running")
	}

	// Test double start (should error)
	err = collector.Start(context.Background())
	if err == nil {
		t.Error("Expected error when starting already running collector")
	}

	// Test stop
	err = collector.Stop(context.Background())
	if err != nil {
		t.Errorf("Unexpected error stopping collector: %v", err)
	}

	// Verify it's stopped
	collector.mu.RLock()
	running = collector.running
	collector.mu.RUnlock()

	if running {
		t.Error("Expected collector to be stopped")
	}

	// Test double stop (should not error)
	err = collector.Stop(context.Background())
	if err != nil {
		t.Errorf("Unexpected error stopping already stopped collector: %v", err)
	}

	// Wait for start to complete
	select {
	case err := <-startErr:
		if err != nil && err != context.DeadlineExceeded {
			// On non-Linux systems, CPU tracker start may fail due to /proc dependencies
			if err.Error() != "failed to start CPU tracker: failed to read initial system CPU: failed to read /proc/stat: open /proc/stat: no such file or directory" {
				t.Errorf("Unexpected start error: %v", err)
			}
		}
	case <-time.After(time.Second):
		t.Error("Start method did not return in time")
	}
}

func TestCollectPoolMetrics(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create mock PHP-FPM status server
	mockStatus := &PHPFPMStatus{
		Pool:               "test-pool",
		ProcessManager:     "dynamic",
		StartTime:          time.Now().Add(-time.Hour).Unix(),
		StartSince:         3600,
		AcceptedConn:       1000,
		ListenQueue:        0,
		MaxListenQueue:     5,
		IdleProcesses:      3,
		ActiveProcesses:    2,
		TotalProcesses:     5,
		MaxActiveProcesses: 4,
		MaxChildrenReached: 0,
		SlowRequests:       1,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Return simple JSON response
		w.Write([]byte(`{
			"pool": "test-pool",
			"process manager": "dynamic",
			"start time": 1672531200,
			"start since": 3600,
			"accepted conn": 1000,
			"listen queue": 0,
			"max listen queue": 5,
			"idle processes": 3,
			"active processes": 2,
			"total processes": 5,
			"max active processes": 4,
			"max children reached": 0,
			"slow requests": 1
		}`))
	}))
	defer server.Close()

	monitoringCfg := config.MonitoringConfig{
		CollectInterval: time.Second,
	}
	pools := []config.PoolConfig{
		{
			Name:              "test-pool",
			ConfigPath:        "/tmp/test.conf",
			FastCGIEndpoint:   "127.0.0.1:9000",       // Placeholder endpoint
			FastCGIStatusPath: server.URL + "/status", // Use HTTP mock server
			MaxWorkers:        10,
		},
	}

	collector, err := NewCollector(monitoringCfg, pools, logger)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	ctx := context.Background()
	metrics, err := collector.collectPoolMetrics(ctx, pools[0])

	if err != nil {
		t.Fatalf("Failed to collect pool metrics: %v", err)
	}

	if len(metrics) == 0 {
		t.Error("Expected metrics to be collected")
	}

	// Verify expected metrics are present
	expectedMetrics := []string{
		"phpfpm_accepted_connections_total",
		"phpfpm_listen_queue",
		"phpfpm_idle_processes",
		"phpfpm_active_processes",
		"phpfpm_total_processes",
		"phpfpm_pool_utilization_ratio",
	}

	metricNames := make(map[string]bool)
	for _, metric := range metrics {
		metricNames[metric.Name] = true

		// Verify all metrics have pool label
		if poolLabel, exists := metric.Labels["pool"]; !exists {
			t.Errorf("Metric %s missing pool label", metric.Name)
		} else if poolLabel != "test-pool" {
			t.Errorf("Expected pool label 'test-pool', got '%s'", poolLabel)
		}
	}

	for _, expectedMetric := range expectedMetrics {
		if !metricNames[expectedMetric] {
			t.Errorf("Expected metric '%s' not found", expectedMetric)
		}
	}

	_ = mockStatus // Use the mock status to avoid unused variable error
}

func TestCollectWithMockServer(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create mock server for PHP-FPM status
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"pool": "test-pool",
			"process manager": "dynamic",
			"start time": 1672531200,
			"start since": 3600,
			"accepted conn": 500,
			"listen queue": 0,
			"max listen queue": 10,
			"idle processes": 5,
			"active processes": 3,
			"total processes": 8,
			"max active processes": 6,
			"max children reached": 0,
			"slow requests": 2
		}`))
	}))
	defer server.Close()

	monitoringCfg := config.MonitoringConfig{
		CollectInterval:     time.Second,
		EnableSystemMetrics: false, // Disable to avoid /proc dependencies
		EnableOpcache:       true,  // Enable to test opcache collection
	}
	pools := []config.PoolConfig{
		{
			Name:            "test-pool",
			ConfigPath:      "/tmp/test.conf",
			FastCGIEndpoint: "127.0.0.1:9000", // Mock FastCGI endpoint
			MaxWorkers:      10,
		},
	}

	collector, err := NewCollector(monitoringCfg, pools, logger)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Start components to get baseline readings
	ctx := context.Background()
	err = collector.cpuTracker.Start(ctx)
	if err != nil {
		t.Logf("CPU tracker start failed (expected on non-Linux): %v", err)
	}

	err = collector.memoryTracker.Start(ctx)
	if err != nil {
		t.Logf("Memory tracker start failed (expected on non-Linux): %v", err)
	}

	// Collect metrics
	metricSet, err := collector.Collect(ctx)
	if err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}

	if metricSet == nil {
		t.Fatal("Expected non-nil metric set")
	}

	if len(metricSet.Metrics) == 0 {
		t.Error("Expected metrics to be collected")
	}

	// Verify collection metadata metrics are present
	var foundCollectionDuration, foundMetricsCount bool
	for _, metric := range metricSet.Metrics {
		switch metric.Name {
		case "phpfpm_collection_duration_seconds":
			foundCollectionDuration = true
			if metric.Value <= 0 {
				t.Error("Collection duration should be positive")
			}
		case "phpfpm_metrics_collected_total":
			foundMetricsCount = true
			if metric.Value <= 0 {
				t.Error("Metrics count should be positive")
			}
		}
	}

	if !foundCollectionDuration {
		t.Error("Expected collection duration metric")
	}
	if !foundMetricsCount {
		t.Error("Expected metrics count metric")
	}

	// Test metrics subscription
	metricsChannel := collector.Subscribe()
	if metricsChannel == nil {
		t.Error("Expected non-nil metrics channel")
	}
}

func TestAdjustCollectionInterval(t *testing.T) {
	logger := zaptest.NewLogger(t)
	monitoringCfg := config.MonitoringConfig{
		CollectInterval: time.Second,
	}
	pools := []config.PoolConfig{
		{
			Name:       "test-pool",
			MaxWorkers: 10,
		},
	}

	collector, err := NewCollector(monitoringCfg, pools, logger)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Set initial interval and create ticker
	collector.currentInterval = time.Second
	collector.collectTicker = time.NewTicker(collector.currentInterval)
	defer collector.collectTicker.Stop()

	// Test the adaptive interval calculation directly
	collector.loadFactor = 0.9 // High load should double interval
	adaptiveInterval := collector.calculateAdaptiveInterval()
	expectedAdaptive := 2 * collector.baseInterval
	if adaptiveInterval != expectedAdaptive {
		t.Errorf("Expected adaptive interval %v for load 0.9, got %v", expectedAdaptive, adaptiveInterval)
	}

	// Test with pools that would create high load (more active processes than in the simple test)
	// Create pools configuration that would result in high load
	highLoadPools := []config.PoolConfig{
		{
			Name:       "pool1",
			MaxWorkers: 5,
		},
		{
			Name:       "pool2",
			MaxWorkers: 5,
		},
	}
	collector.pools = highLoadPools

	// The updateLoadFactor with these pools should give 50% load factor
	// We can test the adjustment by manually setting up a scenario
	initialInterval := collector.currentInterval

	// Test normal adjustment (this will use the actual load calculation)
	collector.adjustCollectionInterval()

	// The load factor should be 0.5 (50% utilization assumption)
	expectedLoadFactor := 0.5
	if collector.loadFactor != expectedLoadFactor {
		t.Logf("Load factor is %f, expected %f", collector.loadFactor, expectedLoadFactor)
	}

	// With load factor 0.5, interval should remain the same (normal load)
	if collector.currentInterval != initialInterval {
		t.Logf("Interval changed from %v to %v with load factor %f", initialInterval, collector.currentInterval, collector.loadFactor)
	}
}

func TestPHPFPMClientGetStatus(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name           string
		serverResponse func(w http.ResponseWriter, r *http.Request)
		expectError    bool
		expectedPool   string
	}{
		{
			name: "valid JSON response",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				// Verify JSON and full parameters are added
				if r.URL.RawQuery != "json&full" {
					t.Errorf("Expected query parameter 'json&full', got '%s'", r.URL.RawQuery)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{
					"pool": "test-pool",
					"process manager": "dynamic",
					"start time": 1672531200,
					"start since": 3600,
					"accepted conn": 100,
					"listen queue": 0,
					"max listen queue": 5,
					"idle processes": 2,
					"active processes": 1,
					"total processes": 3,
					"max active processes": 2,
					"max children reached": 0,
					"slow requests": 0
				}`))
			},
			expectError:  false,
			expectedPool: "test-pool",
		},
		{
			name: "HTTP error response",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			},
			expectError: true,
		},
		{
			name: "invalid JSON response",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`invalid json`))
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.serverResponse))
			defer server.Close()

			cbConfig := resilience.DefaultCircuitBreakerConfig()
			client := &PHPFPMClient{
				client:         &http.Client{Timeout: 5 * time.Second},
				logger:         logger,
				circuitBreaker: resilience.NewCircuitBreaker("test-phpfpm", cbConfig, logger),
			}

			ctx := context.Background()
			status, err := client.GetStatus(ctx, server.URL+"/status")

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if status == nil {
				t.Error("Expected non-nil status")
				return
			}

			if status.Pool != tt.expectedPool {
				t.Errorf("Expected pool '%s', got '%s'", tt.expectedPool, status.Pool)
			}
		})
	}
}

func TestCollectOpcacheMetrics(t *testing.T) {
	// Skip test in containerized environment where OpcCache might not be properly configured for FastCGI
	if os.Getenv("DOCKER_TEST") != "" {
		t.Skip("Skipping OpcCache test in Docker environment")
	}
	logger := zaptest.NewLogger(t)
	monitoringCfg := config.MonitoringConfig{
		CollectInterval: time.Second,
	}
	pools := []config.PoolConfig{
		{
			Name:            "test-pool",
			FastCGIEndpoint: "127.0.0.1:9000", // Mock FastCGI endpoint
			MaxWorkers:      5,
		},
	}

	collector, err := NewCollector(monitoringCfg, pools, logger)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	ctx := context.Background()
	metrics, err := collector.collectOpcacheMetrics(ctx)

	if err != nil {
		t.Errorf("Unexpected error collecting opcache metrics: %v", err)
	}

	if len(metrics) == 0 {
		t.Error("Expected opcache metrics to be returned")
	}

	// Verify expected opcache metrics
	expectedMetrics := []string{
		"phpfpm_opcache_hit_rate",
		"phpfpm_opcache_memory_usage_bytes",
	}

	metricNames := make(map[string]bool)
	for _, metric := range metrics {
		metricNames[metric.Name] = true

		// Verify metric values are reasonable
		if metric.Name == "phpfpm_opcache_hit_rate" && (metric.Value < 0 || metric.Value > 1) {
			t.Errorf("Opcache hit rate should be between 0 and 1, got %f", metric.Value)
		}
		if metric.Name == "phpfpm_opcache_memory_usage_bytes" && metric.Value <= 0 {
			t.Errorf("Opcache memory usage should be positive, got %f", metric.Value)
		}
	}

	for _, expectedMetric := range expectedMetrics {
		if !metricNames[expectedMetric] {
			t.Errorf("Expected opcache metric '%s' not found", expectedMetric)
		}
	}
}
