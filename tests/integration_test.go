package tests

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/app"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/config"
	"go.uber.org/zap/zaptest"
)

// isPHPFPMAvailable checks if PHP-FPM binary is available for integration tests
func isPHPFPMAvailable() bool {
	// Check if php-fpm is in PATH
	if _, err := exec.LookPath("php-fpm"); err == nil {
		return true
	}

	// Check common locations
	commonPaths := []string{
		"/usr/sbin/php-fpm",
		"/usr/bin/php-fpm",
		"/usr/local/sbin/php-fpm",
		"/usr/local/bin/php-fpm",
		"/opt/homebrew/sbin/php-fpm",
	}

	for _, path := range commonPaths {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}

	return false
}

// TestManagerIntegration tests the integration between all components
func TestManagerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check if PHP-FPM binary is available before running integration tests
	if !isPHPFPMAvailable() {
		t.Skip("Skipping integration test: PHP-FPM binary not available")
	}

	logger := zaptest.NewLogger(t)

	// Create temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "integration_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a mock PHP-FPM config file
	configPath := filepath.Join(tmpDir, "test.conf")
	phpfpmConfig := `[global]
pid = ` + tmpDir + `/test.pid
error_log = ` + tmpDir + `/test.log
daemonize = no

[www]
listen = 127.0.0.1:9999
pm = static
pm.max_children = 2
pm.status_path = /status
ping.path = /ping
`
	if err := os.WriteFile(configPath, []byte(phpfpmConfig), 0644); err != nil {
		t.Fatalf("Failed to create PHP-FPM config: %v", err)
	}

	// Create test configuration
	cfg := &config.Config{
		Server: config.ServerConfig{
			BindAddress: "127.0.0.1:9091", // Use different port to avoid conflicts
			MetricsPath: "/metrics",
			Auth: config.AuthConfig{
				Enabled: false, // Disable auth for integration test
			},
		},
		Storage: config.StorageConfig{
			DatabasePath: filepath.Join(tmpDir, "metrics.db"),
			Retention: config.RetentionConfig{
				Raw:    time.Hour,
				Minute: 24 * time.Hour,
				Hour:   7 * 24 * time.Hour,
			},
			Aggregation: config.AggregationConfig{
				Enabled:   true,
				Interval:  time.Minute,
				BatchSize: 100,
			},
		},
		Pools: []config.PoolConfig{
			{
				Name:              "integration-test",
				ConfigPath:        configPath,
				FastCGIStatusPath: "http://127.0.0.1:9999/status",
				MaxWorkers:        2,
				HealthCheck: config.HealthConfig{
					Enabled:  true,
					Interval: 5 * time.Second,
					Timeout:  2 * time.Second,
					Retries:  3,
					Endpoint: "http://127.0.0.1:9999/ping",
				},
				Resources: config.ResourceLimits{
					CPUThreshold:    80.0,
					MemoryThreshold: 85.0,
					MaxMemoryMB:     256,
				},
			},
		},
		Monitoring: config.MonitoringConfig{
			CollectInterval:     2 * time.Second,
			HealthCheckInterval: 3 * time.Second,
			CPUThreshold:        80.0,
			MemoryThreshold:     85.0,
			EnableOpcache:       false, // Disable for test
			EnableSystemMetrics: true,
		},
		Logging: config.LoggingConfig{
			Level:      "debug",
			Format:     "console",
			OutputPath: "stdout",
			Structured: false,
		},
	}

	// Create and start the manager
	manager, err := app.NewManager(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Start the manager in a goroutine
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	managerDone := make(chan error, 1)
	go func() {
		managerDone <- manager.Run(ctx)
	}()

	// Give the manager time to start
	time.Sleep(2 * time.Second)

	// Test health endpoint
	t.Run("health_endpoint", func(t *testing.T) {
		resp, err := http.Get("http://127.0.0.1:9091/health")
		if err != nil {
			t.Errorf("Failed to get health endpoint: %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
	})

	// Test metrics endpoint
	t.Run("metrics_endpoint", func(t *testing.T) {
		resp, err := http.Get("http://127.0.0.1:9091/metrics")
		if err != nil {
			t.Errorf("Failed to get metrics endpoint: %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		// Check Content-Type
		contentType := resp.Header.Get("Content-Type")
		if contentType != "text/plain; version=0.0.4; charset=utf-8" {
			t.Errorf("Expected Prometheus content type, got %s", contentType)
		}
	})

	// Test manager graceful shutdown
	t.Run("graceful_shutdown", func(t *testing.T) {
		// Cancel context to trigger shutdown
		cancel()

		// Wait for manager to shut down
		select {
		case err := <-managerDone:
			if err != nil && err != context.Canceled {
				t.Errorf("Manager returned error: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("Manager did not shut down within timeout")
		}
	})
}

// TestAuthenticationIntegration tests authentication in integration scenario
func TestAuthenticationIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check if PHP-FPM binary is available before running integration tests
	if !isPHPFPMAvailable() {
		t.Skip("Skipping integration test: PHP-FPM binary not available")
	}

	logger := zaptest.NewLogger(t)

	// Create temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "auth_integration_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a mock PHP-FPM config file
	configPath := filepath.Join(tmpDir, "test.conf")
	phpfpmConfig := `[global]
pid = ` + tmpDir + `/test.pid
error_log = ` + tmpDir + `/test.log
daemonize = no

[www]
listen = 127.0.0.1:9998
pm = static
pm.max_children = 1
pm.status_path = /status
ping.path = /ping
`
	if err := os.WriteFile(configPath, []byte(phpfpmConfig), 0644); err != nil {
		t.Fatalf("Failed to create PHP-FPM config: %v", err)
	}

	// Create test configuration with authentication enabled
	cfg := &config.Config{
		Server: config.ServerConfig{
			BindAddress: "127.0.0.1:9092", // Use different port
			MetricsPath: "/metrics",
			Auth: config.AuthConfig{
				Enabled: true,
				Type:    "api_key",
				APIKey:  "test-integration-api-key-32-chars-minimum",
			},
		},
		Storage: config.StorageConfig{
			DatabasePath: filepath.Join(tmpDir, "metrics.db"),
			Retention: config.RetentionConfig{
				Raw:    time.Hour,
				Minute: 24 * time.Hour,
				Hour:   7 * 24 * time.Hour,
			},
		},
		Pools: []config.PoolConfig{
			{
				Name:              "auth-test",
				ConfigPath:        configPath,
				FastCGIStatusPath: "http://127.0.0.1:9998/status",
				MaxWorkers:        1,
				HealthCheck: config.HealthConfig{
					Enabled:  false, // Disable health check for this test
					Interval: 30 * time.Second,
					Timeout:  5 * time.Second,
					Retries:  1,
					Endpoint: "http://127.0.0.1:9998/ping",
				},
			},
		},
		Monitoring: config.MonitoringConfig{
			CollectInterval:     5 * time.Second,
			HealthCheckInterval: 10 * time.Second,
			EnableOpcache:       false,
			EnableSystemMetrics: false, // Disable for minimal test
		},
		Logging: config.LoggingConfig{
			Level:  "warn", // Reduce logging for cleaner test output
			Format: "console",
		},
	}

	// Create and start the manager
	manager, err := app.NewManager(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	go func() {
		manager.Run(ctx)
	}()

	// Give the manager time to start
	time.Sleep(1 * time.Second)

	// Test unauthenticated request
	t.Run("unauthenticated_request", func(t *testing.T) {
		resp, err := http.Get("http://127.0.0.1:9092/metrics")
		if err != nil {
			t.Errorf("Failed to make request: %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", resp.StatusCode)
		}

		// Check WWW-Authenticate header
		authHeader := resp.Header.Get("WWW-Authenticate")
		if authHeader == "" {
			t.Error("Expected WWW-Authenticate header")
		}
	})

	// Test authenticated request with valid API key
	t.Run("authenticated_request", func(t *testing.T) {
		req, err := http.NewRequest("GET", "http://127.0.0.1:9092/metrics", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		req.Header.Set("Authorization", "Bearer test-integration-api-key-32-chars-minimum")

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			t.Errorf("Failed to make authenticated request: %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 for authenticated request, got %d", resp.StatusCode)
		}
	})

	// Test authenticated request with invalid API key
	t.Run("invalid_api_key", func(t *testing.T) {
		req, err := http.NewRequest("GET", "http://127.0.0.1:9092/metrics", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		req.Header.Set("Authorization", "Bearer invalid-api-key")

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			t.Errorf("Failed to make request: %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401 for invalid API key, got %d", resp.StatusCode)
		}
	})

	// Test health endpoint (should be public)
	t.Run("public_health_endpoint", func(t *testing.T) {
		resp, err := http.Get("http://127.0.0.1:9092/health")
		if err != nil {
			t.Errorf("Failed to get health endpoint: %v", err)
			return
		}
		defer resp.Body.Close()

		// Health endpoint should be accessible without authentication
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 for health endpoint, got %d", resp.StatusCode)
		}
	})

	cancel()
}

// TestConfigValidationIntegration tests configuration validation in integration scenario
func TestConfigValidationIntegration(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Test with invalid config path
	t.Run("invalid_config_path", func(t *testing.T) {
		cfg := &config.Config{
			Server: config.ServerConfig{
				BindAddress: "127.0.0.1:9093",
				MetricsPath: "/metrics",
			},
			Pools: []config.PoolConfig{
				{
					Name:              "invalid-test",
					ConfigPath:        "/nonexistent/path.conf",
					FastCGIStatusPath: "http://127.0.0.1:9997/status",
					MaxWorkers:        1,
				},
			},
			Storage: config.StorageConfig{
				DatabasePath: "./test.db",
				Retention: config.RetentionConfig{
					Raw:    time.Hour,
					Minute: 24 * time.Hour,
					Hour:   7 * 24 * time.Hour,
				},
			},
			Monitoring: config.MonitoringConfig{
				CollectInterval:     time.Second,
				HealthCheckInterval: 5 * time.Second,
			},
		}

		_, err := app.NewManager(cfg, logger)
		if err == nil {
			t.Error("Expected error when creating manager with invalid config path")
		}
	})

	// Test with relative config path (should fail)
	t.Run("relative_config_path", func(t *testing.T) {
		cfg := &config.Config{
			Server: config.ServerConfig{
				BindAddress: "127.0.0.1:9094",
				MetricsPath: "/metrics",
			},
			Pools: []config.PoolConfig{
				{
					Name:              "relative-test",
					ConfigPath:        "./relative.conf",
					FastCGIStatusPath: "http://127.0.0.1:9996/status",
					MaxWorkers:        1,
				},
			},
			Storage: config.StorageConfig{
				DatabasePath: "./test.db",
				Retention: config.RetentionConfig{
					Raw:    time.Hour,
					Minute: 24 * time.Hour,
					Hour:   7 * 24 * time.Hour,
				},
			},
			Monitoring: config.MonitoringConfig{
				CollectInterval:     time.Second,
				HealthCheckInterval: 5 * time.Second,
			},
		}

		_, err := app.NewManager(cfg, logger)
		if err == nil {
			t.Error("Expected error when creating manager with relative config path")
		}
	})
}

// BenchmarkManagerStart benchmarks the manager startup time
func BenchmarkManagerStart(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	logger := zaptest.NewLogger(b)

	// Create temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "benchmark_test")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a minimal config
	configPath := filepath.Join(tmpDir, "bench.conf")
	phpfpmConfig := `[global]
pid = ` + tmpDir + `/bench.pid
error_log = ` + tmpDir + `/bench.log
daemonize = no

[www]
listen = 127.0.0.1:9995
pm = static
pm.max_children = 1
`
	if err := os.WriteFile(configPath, []byte(phpfpmConfig), 0644); err != nil {
		b.Fatalf("Failed to create PHP-FPM config: %v", err)
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			BindAddress: "127.0.0.1:9095",
			MetricsPath: "/metrics",
		},
		Storage: config.StorageConfig{
			DatabasePath: filepath.Join(tmpDir, "bench.db"),
			Retention: config.RetentionConfig{
				Raw:    time.Hour,
				Minute: 24 * time.Hour,
				Hour:   7 * 24 * time.Hour,
			},
		},
		Pools: []config.PoolConfig{
			{
				Name:              "benchmark",
				ConfigPath:        configPath,
				FastCGIStatusPath: "http://127.0.0.1:9995/status",
				MaxWorkers:        1,
				HealthCheck: config.HealthConfig{
					Enabled: false, // Disable for benchmark
				},
			},
		},
		Monitoring: config.MonitoringConfig{
			CollectInterval:     10 * time.Second,
			HealthCheckInterval: 30 * time.Second,
			EnableOpcache:       false,
			EnableSystemMetrics: false,
		},
		Logging: config.LoggingConfig{
			Level:  "error", // Minimal logging for benchmark
			Format: "console",
		},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		manager, err := app.NewManager(cfg, logger)
		if err != nil {
			b.Fatalf("Failed to create manager: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

		// Start manager (this is what we're benchmarking)
		go func() {
			manager.Run(ctx)
		}()

		// Give it a moment to start
		time.Sleep(100 * time.Millisecond)

		cancel()

		// Give it time to shutdown
		time.Sleep(100 * time.Millisecond)
	}
}
