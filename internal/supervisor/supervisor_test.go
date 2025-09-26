package supervisor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/config"
	"go.uber.org/zap/zaptest"
)

// isPHPFPMAvailable checks if PHP-FPM binary is available for tests
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

func TestValidateConfigPath(t *testing.T) {
	// Create a temporary directory and file for testing
	tmpDir, err := os.MkdirTemp("", "supervisor_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a valid config file
	validPath := filepath.Join(tmpDir, "valid.conf")
	if err := os.WriteFile(validPath, []byte("[global]\npid = /tmp/test.pid"), 0644); err != nil {
		t.Fatalf("Failed to create valid config file: %v", err)
	}

	// Create a non-config file
	nonConfigPath := filepath.Join(tmpDir, "invalid.txt")
	if err := os.WriteFile(nonConfigPath, []byte("not a config"), 0644); err != nil {
		t.Fatalf("Failed to create non-config file: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "valid absolute config path",
			path:    validPath,
			wantErr: false,
		},
		{
			name:    "relative path",
			path:    "./relative.conf",
			wantErr: true,
		},
		{
			name:    "path with directory traversal",
			path:    tmpDir + "/../dangerous.conf",
			wantErr: true,
		},
		{
			name:    "path with null byte",
			path:    validPath + "\x00",
			wantErr: true,
		},
		{
			name:    "non-existent file",
			path:    tmpDir + "/nonexistent.conf",
			wantErr: true,
		},
		{
			name:    "directory instead of file",
			path:    tmpDir,
			wantErr: true,
		},
		{
			name:    "wrong file extension",
			path:    nonConfigPath,
			wantErr: true,
		},
		{
			name:    "redundant path elements",
			path:    tmpDir + "/./valid.conf",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfigPath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateConfigPath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewSupervisor(t *testing.T) {
	// Skip if PHP-FPM binary is not available
	if !isPHPFPMAvailable() {
		t.Skip("Skipping test: PHP-FPM binary not available")
	}

	logger := zaptest.NewLogger(t)

	// Create a temporary config file
	tmpDir, err := os.MkdirTemp("", "supervisor_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	validPath := filepath.Join(tmpDir, "valid.conf")
	if err := os.WriteFile(validPath, []byte("[global]\npid = /tmp/test.pid"), 0644); err != nil {
		t.Fatalf("Failed to create valid config file: %v", err)
	}

	tests := []struct {
		name    string
		pools   []config.PoolConfig
		wantErr bool
	}{
		{
			name:    "no pools",
			pools:   []config.PoolConfig{},
			wantErr: true,
		},
		{
			name: "valid pools",
			pools: []config.PoolConfig{
				{
					Name:              "test-pool",
					ConfigPath:        validPath,
					FastCGIEndpoint:   "127.0.0.1:9001",
					FastCGIStatusPath: "/status",
					MaxWorkers:        5,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid config path",
			pools: []config.PoolConfig{
				{
					Name:              "test-pool",
					ConfigPath:        "/nonexistent/path.conf",
					FastCGIEndpoint:   "127.0.0.1:9001",
					FastCGIStatusPath: "/status",
					MaxWorkers:        5,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			supervisor, err := NewSupervisor(tt.pools, logger, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSupervisor() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if supervisor == nil {
					t.Error("Expected supervisor to be created, but got nil")
					return
				}

				// Verify supervisor was initialized correctly
				if len(supervisor.processes) != len(tt.pools) {
					t.Errorf("Expected %d processes, got %d", len(tt.pools), len(supervisor.processes))
				}

				// Verify event channel was created
				if supervisor.events == nil {
					t.Error("Expected event channel to be created")
				}

				// Verify initial state
				for _, pool := range tt.pools {
					process, exists := supervisor.processes[pool.Name]
					if !exists {
						t.Errorf("Expected process for pool %s to exist", pool.Name)
						continue
					}

					if process.Status != PoolStatusStopped {
						t.Errorf("Expected initial status to be stopped, got %v", process.Status)
					}
				}
			}
		})
	}
}

func TestSupervisorEventEmission(t *testing.T) {
	// Skip if PHP-FPM binary is not available
	if !isPHPFPMAvailable() {
		t.Skip("Skipping test: PHP-FPM binary not available")
	}

	logger := zaptest.NewLogger(t)

	// Create a temporary config file
	tmpDir, err := os.MkdirTemp("", "supervisor_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	validPath := filepath.Join(tmpDir, "valid.conf")
	if err := os.WriteFile(validPath, []byte("[global]\npid = /tmp/test.pid"), 0644); err != nil {
		t.Fatalf("Failed to create valid config file: %v", err)
	}

	pools := []config.PoolConfig{
		{
			Name:              "test-pool",
			ConfigPath:        validPath,
			FastCGIStatusPath: "http://localhost:9001/status",
			MaxWorkers:        5,
		},
	}

	supervisor, err := NewSupervisor(pools, logger, nil)
	if err != nil {
		t.Fatalf("Failed to create supervisor: %v", err)
	}

	// Test event emission
	go func() {
		supervisor.emitEvent("test_event", "test-pool", 123, "test message")
	}()

	// Wait for event to be emitted
	select {
	case event := <-supervisor.Subscribe():
		if event.Type != "test_event" {
			t.Errorf("Expected event type 'test_event', got '%s'", event.Type)
		}
		if event.Pool != "test-pool" {
			t.Errorf("Expected pool 'test-pool', got '%s'", event.Pool)
		}
		if event.PID != 123 {
			t.Errorf("Expected PID 123, got %d", event.PID)
		}
		if event.Message != "test message" {
			t.Errorf("Expected message 'test message', got '%s'", event.Message)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for event")
	}
}

func TestSupervisorHealth(t *testing.T) {
	// Skip if PHP-FPM binary is not available
	if !isPHPFPMAvailable() {
		t.Skip("Skipping test: PHP-FPM binary not available")
	}

	logger := zaptest.NewLogger(t)

	// Create a temporary config file
	tmpDir, err := os.MkdirTemp("", "supervisor_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	validPath := filepath.Join(tmpDir, "valid.conf")
	if err := os.WriteFile(validPath, []byte("[global]\npid = /tmp/test.pid"), 0644); err != nil {
		t.Fatalf("Failed to create valid config file: %v", err)
	}

	pools := []config.PoolConfig{
		{
			Name:              "test-pool-1",
			ConfigPath:        validPath,
			FastCGIStatusPath: "http://localhost:9001/status",
			MaxWorkers:        5,
		},
		{
			Name:              "test-pool-2",
			ConfigPath:        validPath,
			FastCGIStatusPath: "http://localhost:9002/status",
			MaxWorkers:        3,
		},
	}

	supervisor, err := NewSupervisor(pools, logger, nil)
	if err != nil {
		t.Fatalf("Failed to create supervisor: %v", err)
	}

	// Test initial health status
	health := supervisor.Health()
	if len(health.Pools) != 2 {
		t.Errorf("Expected 2 pools in health status, got %d", len(health.Pools))
	}

	// All pools should be unknown initially
	for poolName, state := range health.Pools {
		if state != "unknown" {
			t.Errorf("Expected initial health state 'unknown' for pool %s, got '%s'", poolName, state)
		}
	}

	// Overall health should be unhealthy when pools are unknown
	if health.Overall != "unhealthy" {
		t.Errorf("Expected overall health 'unhealthy', got '%s'", health.Overall)
	}
}

func TestSupervisorStopGracefully(t *testing.T) {
	// Skip if PHP-FPM binary is not available
	if !isPHPFPMAvailable() {
		t.Skip("Skipping test: PHP-FPM binary not available")
	}

	logger := zaptest.NewLogger(t)

	// Create a temporary config file
	tmpDir, err := os.MkdirTemp("", "supervisor_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	validPath := filepath.Join(tmpDir, "valid.conf")
	if err := os.WriteFile(validPath, []byte("[global]\npid = /tmp/test.pid"), 0644); err != nil {
		t.Fatalf("Failed to create valid config file: %v", err)
	}

	pools := []config.PoolConfig{
		{
			Name:              "test-pool",
			ConfigPath:        validPath,
			FastCGIStatusPath: "http://localhost:9001/status",
			MaxWorkers:        5,
		},
	}

	supervisor, err := NewSupervisor(pools, logger, nil)
	if err != nil {
		t.Fatalf("Failed to create supervisor: %v", err)
	}

	// Test stopping without starting (should not error)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = supervisor.Stop(ctx)
	if err != nil {
		t.Errorf("Expected no error when stopping non-running supervisor, got: %v", err)
	}

	// Verify supervisor is not running
	if supervisor.running {
		t.Error("Expected supervisor to not be running after stop")
	}
}

// Benchmark tests for performance validation
func BenchmarkValidateConfigPath(b *testing.B) {
	// Create a temporary valid config file
	tmpDir, err := os.MkdirTemp("", "benchmark_test")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	validPath := filepath.Join(tmpDir, "valid.conf")
	if err := os.WriteFile(validPath, []byte("[global]\npid = /tmp/test.pid"), 0644); err != nil {
		b.Fatalf("Failed to create valid config file: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validateConfigPath(validPath)
	}
}

func BenchmarkEmitEvent(b *testing.B) {
	logger := zaptest.NewLogger(b)

	// Create a temporary config file
	tmpDir, err := os.MkdirTemp("", "benchmark_test")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	validPath := filepath.Join(tmpDir, "valid.conf")
	if err := os.WriteFile(validPath, []byte("[global]\npid = /tmp/test.pid"), 0644); err != nil {
		b.Fatalf("Failed to create valid config file: %v", err)
	}

	pools := []config.PoolConfig{
		{
			Name:              "benchmark-pool",
			ConfigPath:        validPath,
			FastCGIStatusPath: "http://localhost:9001/status",
			MaxWorkers:        5,
		},
	}

	supervisor, err := NewSupervisor(pools, logger, nil)
	if err != nil {
		b.Fatalf("Failed to create supervisor: %v", err)
	}

	// Drain events to prevent channel blocking
	go func() {
		for range supervisor.Subscribe() {
			// Consume events
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		supervisor.emitEvent("benchmark_event", "benchmark-pool", i, fmt.Sprintf("Event %d", i))
	}
}
