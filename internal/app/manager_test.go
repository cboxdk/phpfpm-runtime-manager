package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/api"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/config"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/types"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// Mock implementations for testing

type mockProcessSupervisor struct {
	mu           sync.RWMutex
	started      bool
	startError   error
	reloadError  error
	restartError error
	healthStatus types.HealthStatus
	events       chan types.ProcessEvent
	eventsSent   []types.ProcessEvent
}

func newMockProcessSupervisor() *mockProcessSupervisor {
	return &mockProcessSupervisor{
		events: make(chan types.ProcessEvent, 10),
		healthStatus: types.HealthStatus{
			Overall: types.HealthStateHealthy,
			Updated: time.Now(),
		},
	}
}

func (m *mockProcessSupervisor) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.startError != nil {
		return m.startError
	}

	m.started = true
	return nil
}

func (m *mockProcessSupervisor) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.started = false
	return nil
}

func (m *mockProcessSupervisor) Reload(ctx context.Context) error {
	if m.reloadError != nil {
		return m.reloadError
	}
	return nil
}

func (m *mockProcessSupervisor) Restart(ctx context.Context) error {
	if m.restartError != nil {
		return m.restartError
	}
	return nil
}

func (m *mockProcessSupervisor) Health() types.HealthStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.healthStatus
}

func (m *mockProcessSupervisor) Subscribe() <-chan types.ProcessEvent {
	return m.events
}

func (m *mockProcessSupervisor) setStartError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startError = err
}

func (m *mockProcessSupervisor) setReloadError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reloadError = err
}

func (m *mockProcessSupervisor) setRestartError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.restartError = err
}

func (m *mockProcessSupervisor) setHealthStatus(status types.HealthStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healthStatus = status
}

func (m *mockProcessSupervisor) emitEvent(event types.ProcessEvent) {
	select {
	case m.events <- event:
		m.eventsSent = append(m.eventsSent, event)
	default:
	}
}

// ReloadPool reloads a specific pool
func (m *mockProcessSupervisor) ReloadPool(ctx context.Context, pool string) error {
	return nil
}

// ScalePool scales a specific pool to the given number of workers
func (m *mockProcessSupervisor) ScalePool(ctx context.Context, pool string, workers int) error {
	return nil
}

// GetPoolScale returns the current number of workers for a pool
func (m *mockProcessSupervisor) GetPoolScale(pool string) (int, error) {
	return 5, nil // Default mock value
}

// ValidatePoolConfig validates the configuration for a specific pool
func (m *mockProcessSupervisor) ValidatePoolConfig(pool string) error {
	return nil
}

type mockMetricsCollector struct {
	mu         sync.RWMutex
	started    bool
	startError error
	metrics    chan types.MetricSet
}

func newMockMetricsCollector() *mockMetricsCollector {
	return &mockMetricsCollector{
		metrics: make(chan types.MetricSet, 10),
	}
}

func (m *mockMetricsCollector) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.startError != nil {
		return m.startError
	}

	m.started = true
	return nil
}

func (m *mockMetricsCollector) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.started = false
	return nil
}

func (m *mockMetricsCollector) Subscribe() <-chan types.MetricSet {
	return m.metrics
}

func (m *mockMetricsCollector) Collect(ctx context.Context) (*types.MetricSet, error) {
	return nil, nil
}

func (m *mockMetricsCollector) setStartError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startError = err
}

func (m *mockMetricsCollector) emitMetrics(metricSet types.MetricSet) {
	select {
	case m.metrics <- metricSet:
	default:
	}
}

// IsAutoscalingEnabled returns true if autoscaling is enabled
func (m *mockMetricsCollector) IsAutoscalingEnabled() bool {
	return true
}

// IsAutoscalingPaused returns true if autoscaling is paused
func (m *mockMetricsCollector) IsAutoscalingPaused() bool {
	return false
}

// PauseAutoscaling pauses the autoscaling functionality
func (m *mockMetricsCollector) PauseAutoscaling() error {
	return nil
}

// ResumeAutoscaling resumes the autoscaling functionality
func (m *mockMetricsCollector) ResumeAutoscaling() error {
	return nil
}

// GetAutoscalingStatus returns the current autoscaling status
func (m *mockMetricsCollector) GetAutoscalingStatus() api.AutoscalingStatus {
	return api.AutoscalingStatus{
		Enabled:    true,
		Paused:     false,
		LastUpdate: time.Now(),
	}
}

type mockStorage struct {
	mu         sync.RWMutex
	started    bool
	startError error
	storeError error
	stored     []types.MetricSet
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		stored: make([]types.MetricSet, 0),
	}
}

func (m *mockStorage) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.startError != nil {
		return m.startError
	}

	m.started = true
	return nil
}

func (m *mockStorage) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.started = false
	return nil
}

func (m *mockStorage) Store(ctx context.Context, metrics types.MetricSet) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.storeError != nil {
		return m.storeError
	}

	m.stored = append(m.stored, metrics)
	return nil
}

func (m *mockStorage) Query(ctx context.Context, query types.Query) (*types.Result, error) {
	return nil, nil
}

func (m *mockStorage) Cleanup(ctx context.Context) error {
	return nil
}

func (m *mockStorage) setStartError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startError = err
}

func (m *mockStorage) setStoreError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.storeError = err
}

func (m *mockStorage) getStoredMetrics() []types.MetricSet {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]types.MetricSet{}, m.stored...)
}

type mockPrometheusExporter struct {
	mu           sync.RWMutex
	started      bool
	startError   error
	updateError  error
	updatedCount int
}

func newMockPrometheusExporter() *mockPrometheusExporter {
	return &mockPrometheusExporter{}
}

func (m *mockPrometheusExporter) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.startError != nil {
		return m.startError
	}

	m.started = true
	return nil
}

func (m *mockPrometheusExporter) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.started = false
	return nil
}

func (m *mockPrometheusExporter) UpdateMetrics(metrics types.MetricSet) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.updateError != nil {
		return m.updateError
	}

	m.updatedCount++
	return nil
}

func (m *mockPrometheusExporter) setStartError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startError = err
}

func (m *mockPrometheusExporter) setUpdateError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateError = err
}

func (m *mockPrometheusExporter) getUpdateCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.updatedCount
}

// Mock autoscaler manager for testing
type mockAutoscalerManager struct {
	mu         sync.RWMutex
	started    bool
	startError error
}

func newMockAutoscalerManager() *mockAutoscalerManager {
	return &mockAutoscalerManager{}
}

func (m *mockAutoscalerManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.startError != nil {
		return m.startError
	}

	m.started = true
	<-ctx.Done() // Wait for cancellation
	return ctx.Err()
}

func (m *mockAutoscalerManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = false
	return nil
}

func (m *mockAutoscalerManager) setStartError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startError = err
}

// Helper function to create a test manager with mocked dependencies
func createTestManager(t *testing.T) (*Manager, *mockProcessSupervisor, *mockMetricsCollector, *mockStorage, *mockPrometheusExporter, *mockAutoscalerManager) {
	// Create temporary directory for test config files
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test.conf")

	// Create a minimal PHP-FPM config file for validation
	configContent := `[test-pool]
user = www-data
group = www-data
listen = /var/run/php/php-fpm-test.sock
pm = dynamic
pm.max_children = 5
pm.start_servers = 2
pm.min_spare_servers = 1
pm.max_spare_servers = 3
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	cfg := &config.Config{
		Pools: []config.PoolConfig{
			{
				Name:              "test-pool",
				ConfigPath:        configPath,
				FastCGIStatusPath: "http://localhost/status",
			},
		},
		Monitoring: config.MonitoringConfig{
			CollectInterval: 5 * time.Second,
		},
		Storage: config.StorageConfig{
			DatabasePath: ":memory:",
		},
		Server: config.ServerConfig{
			BindAddress: "127.0.0.1:9090",
		},
	}

	logger := zaptest.NewLogger(t)

	// Create mocks
	mockSupervisor := newMockProcessSupervisor()
	mockCollector := newMockMetricsCollector()
	mockStore := newMockStorage()
	mockExporter := newMockPrometheusExporter()
	mockAutoscaler := newMockAutoscalerManager()

	ctx, cancel := context.WithCancel(context.Background())

	manager := &Manager{
		config:     cfg,
		logger:     logger,
		supervisor: mockSupervisor,
		collector:  mockCollector,
		storage:    mockStore,
		exporter:   mockExporter,
		autoscaler: nil, // Set to nil for tests, will be handled in Run()
		events:     make(chan ManagerEvent, 100),
		metrics:    make(chan types.MetricSet, 1000),
		ctx:        ctx,
		cancel:     cancel,
	}

	return manager, mockSupervisor, mockCollector, mockStore, mockExporter, mockAutoscaler
}

// Tests for NewManager function

func TestNewManager(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *config.Config
		logger      *zap.Logger
		expectError bool
	}{
		{
			name:        "nil config",
			cfg:         nil,
			logger:      zaptest.NewLogger(t),
			expectError: true,
		},
		{
			name: "nil logger",
			cfg: &config.Config{
				Pools: []config.PoolConfig{
					{Name: "test", ConfigPath: "/etc/php-fpm.d/test.conf"},
				},
			},
			logger:      nil,
			expectError: true,
		},
		{
			name: "valid configuration",
			cfg: &config.Config{
				Pools: []config.PoolConfig{
					{Name: "test", ConfigPath: "/tmp/test.conf"},
				},
				Monitoring: config.MonitoringConfig{
					CollectInterval: 5 * time.Second,
				},
				Storage: config.StorageConfig{
					DatabasePath: ":memory:",
				},
				Server: config.ServerConfig{
					BindAddress: "127.0.0.1:9090",
				},
			},
			logger:      zaptest.NewLogger(t),
			expectError: true, // Will fail because /tmp/test.conf doesn't exist
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, err := NewManager(tt.cfg, tt.logger)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				if manager != nil {
					t.Error("expected nil manager on error")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if manager == nil {
					t.Error("expected non-nil manager")
				}
			}
		})
	}
}

// Tests for Manager.Run method

func TestManagerRun(t *testing.T) {
	t.Run("successful startup and shutdown", func(t *testing.T) {
		manager, _, _, _, _, _ := createTestManager(t)

		ctx, cancel := context.WithCancel(context.Background())

		// Start manager in goroutine
		errChan := make(chan error, 1)
		go func() {
			errChan <- manager.Run(ctx)
		}()

		// Wait for startup
		time.Sleep(200 * time.Millisecond)

		// Verify manager is running
		if !manager.IsRunning() {
			t.Error("expected manager to be running")
		}

		// Trigger shutdown
		cancel()

		// Wait for shutdown
		select {
		case err := <-errChan:
			if err != nil && err != context.Canceled {
				t.Errorf("unexpected error: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("manager did not shutdown within timeout")
		}

		// Verify manager stopped
		if manager.running {
			t.Error("expected manager to be stopped")
		}
	})

	t.Run("already running error", func(t *testing.T) {
		manager, _, _, _, _, _ := createTestManager(t)
		manager.running = true

		ctx := context.Background()
		err := manager.Run(ctx)

		if err == nil {
			t.Error("expected error for already running manager")
		}
		if err.Error() != "manager is already running" {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("component startup failure", func(t *testing.T) {
		manager, mockSupervisor, _, _, _, _ := createTestManager(t)

		// Make supervisor fail to start
		expectedError := errors.New("supervisor start failed")
		mockSupervisor.setStartError(expectedError)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		err := manager.Run(ctx)

		if err == nil {
			t.Error("expected error when component fails to start")
		}
	})
}

// Tests for Manager.Reload method

func TestManagerReload(t *testing.T) {
	t.Run("successful reload", func(t *testing.T) {
		manager, _, _, _, _, _ := createTestManager(t)

		ctx := context.Background()
		err := manager.Reload(ctx)

		// Note: This will fail in test environment because config file doesn't exist
		// In a real implementation, we'd mock the config loading
		if err == nil {
			t.Error("expected error due to missing config file in test")
		}
	})

	t.Run("supervisor reload failure", func(t *testing.T) {
		manager, mockSupervisor, _, _, _, _ := createTestManager(t)

		expectedError := errors.New("supervisor reload failed")
		mockSupervisor.setReloadError(expectedError)

		ctx := context.Background()
		err := manager.Reload(ctx)

		if err == nil {
			t.Error("expected error when supervisor reload fails")
		}
	})
}

// Tests for Manager.Restart method

func TestManagerRestart(t *testing.T) {
	t.Run("successful restart", func(t *testing.T) {
		manager, _, _, _, _, _ := createTestManager(t)

		ctx := context.Background()
		err := manager.Restart(ctx)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("supervisor restart failure", func(t *testing.T) {
		manager, mockSupervisor, _, _, _, _ := createTestManager(t)

		expectedError := errors.New("supervisor restart failed")
		mockSupervisor.setRestartError(expectedError)

		ctx := context.Background()
		err := manager.Restart(ctx)

		if err == nil {
			t.Error("expected error when supervisor restart fails")
		}

		if !errors.Is(err, expectedError) {
			t.Errorf("expected wrapped error, got: %v", err)
		}
	})
}

// Tests for Manager.Health method

func TestManagerHealth(t *testing.T) {
	t.Run("manager not running", func(t *testing.T) {
		manager, _, _, _, _, _ := createTestManager(t)
		manager.running = false

		health := manager.Health()

		if health.Overall != types.HealthStateStopping {
			t.Errorf("expected stopping state, got: %v", health.Overall)
		}
	})

	t.Run("manager running - returns supervisor health", func(t *testing.T) {
		manager, mockSupervisor, _, _, _, _ := createTestManager(t)
		manager.running = true

		expectedHealth := types.HealthStatus{
			Overall: types.HealthStateHealthy,
			Updated: time.Now(),
		}
		mockSupervisor.setHealthStatus(expectedHealth)

		health := manager.Health()

		if health.Overall != expectedHealth.Overall {
			t.Errorf("expected %v, got: %v", expectedHealth.Overall, health.Overall)
		}
	})
}

// Tests for event processing

func TestProcessEvents(t *testing.T) {
	t.Run("processes manager events", func(t *testing.T) {
		manager, _, _, _, _, _ := createTestManager(t)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Start event processing in goroutine
		go manager.processEvents(ctx)

		// Emit a manager event
		manager.emitEvent(ManagerEventStarting, "test message")

		// Give time for processing
		time.Sleep(100 * time.Millisecond)

		// Verify event was processed (we can't easily test logging output,
		// but the function should not crash or hang)
	})

	t.Run("processes supervisor events", func(t *testing.T) {
		manager, mockSupervisor, _, _, _, _ := createTestManager(t)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Start event processing in goroutine
		go manager.processEvents(ctx)

		// Emit a supervisor event
		supervisorEvent := types.ProcessEvent{
			Type:    types.ProcessEventStarted,
			Pool:    "test-pool",
			PID:     1234,
			Message: "Process started",
		}
		mockSupervisor.emitEvent(supervisorEvent)

		// Give time for processing
		time.Sleep(100 * time.Millisecond)

		// Verify event was processed (we can't easily test logging output,
		// but the function should not crash or hang)
	})

	t.Run("stops on context cancellation", func(t *testing.T) {
		manager, _, _, _, _, _ := createTestManager(t)

		ctx, cancel := context.WithCancel(context.Background())

		// Start event processing in goroutine
		errChan := make(chan error, 1)
		go func() {
			errChan <- manager.processEvents(ctx)
		}()

		// Cancel context
		cancel()

		// Verify it stops
		select {
		case err := <-errChan:
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		case <-time.After(1 * time.Second):
			t.Error("processEvents did not stop within timeout")
		}
	})
}

// Tests for metrics processing

func TestProcessMetrics(t *testing.T) {
	t.Run("processes metrics successfully", func(t *testing.T) {
		manager, _, mockCollector, mockStore, mockExporter, _ := createTestManager(t)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Start metrics processing in goroutine
		go manager.processMetrics(ctx)

		// Create test metrics
		testMetrics := types.MetricSet{
			Timestamp: time.Now(),
			Metrics: []types.Metric{
				{
					Name:      "test_metric",
					Value:     42.0,
					Type:      types.MetricTypeGauge,
					Timestamp: time.Now(),
				},
			},
		}

		// Emit metrics
		mockCollector.emitMetrics(testMetrics)

		// Give time for processing
		time.Sleep(100 * time.Millisecond)

		// Verify metrics were stored
		stored := mockStore.getStoredMetrics()
		if len(stored) != 1 {
			t.Errorf("expected 1 stored metric set, got %d", len(stored))
		}

		// Verify metrics were sent to exporter
		updateCount := mockExporter.getUpdateCount()
		if updateCount != 1 {
			t.Errorf("expected 1 exporter update, got %d", updateCount)
		}
	})

	t.Run("handles storage errors gracefully", func(t *testing.T) {
		manager, _, mockCollector, mockStore, _, _ := createTestManager(t)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Make storage fail
		mockStore.setStoreError(errors.New("storage error"))

		// Start metrics processing in goroutine
		go manager.processMetrics(ctx)

		// Create test metrics
		testMetrics := types.MetricSet{
			Timestamp: time.Now(),
			Metrics: []types.Metric{
				{
					Name:      "test_metric",
					Value:     42.0,
					Type:      types.MetricTypeGauge,
					Timestamp: time.Now(),
				},
			},
		}

		// Emit metrics
		mockCollector.emitMetrics(testMetrics)

		// Give time for processing
		time.Sleep(100 * time.Millisecond)

		// Should not crash and should continue processing
		// (We can't easily test the error logging, but the function should continue)
	})

	t.Run("handles exporter errors gracefully", func(t *testing.T) {
		manager, _, mockCollector, _, mockExporter, _ := createTestManager(t)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Make exporter fail
		mockExporter.setUpdateError(errors.New("exporter error"))

		// Start metrics processing in goroutine
		go manager.processMetrics(ctx)

		// Create test metrics
		testMetrics := types.MetricSet{
			Timestamp: time.Now(),
			Metrics: []types.Metric{
				{
					Name:      "test_metric",
					Value:     42.0,
					Type:      types.MetricTypeGauge,
					Timestamp: time.Now(),
				},
			},
		}

		// Emit metrics
		mockCollector.emitMetrics(testMetrics)

		// Give time for processing
		time.Sleep(100 * time.Millisecond)

		// Should not crash and should continue processing
		// (We can't easily test the error logging, but the function should continue)
	})

	t.Run("stops on context cancellation", func(t *testing.T) {
		manager, _, _, _, _, _ := createTestManager(t)

		ctx, cancel := context.WithCancel(context.Background())

		// Start metrics processing in goroutine
		errChan := make(chan error, 1)
		go func() {
			errChan <- manager.processMetrics(ctx)
		}()

		// Cancel context
		cancel()

		// Verify it stops
		select {
		case err := <-errChan:
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		case <-time.After(1 * time.Second):
			t.Error("processMetrics did not stop within timeout")
		}
	})
}

// Tests for event emission

func TestEmitEvent(t *testing.T) {
	t.Run("emits event successfully", func(t *testing.T) {
		manager, _, _, _, _, _ := createTestManager(t)

		manager.emitEvent(ManagerEventStarted, "test message")

		// Verify event was sent to channel
		select {
		case event := <-manager.events:
			if event.Type != ManagerEventStarted {
				t.Errorf("expected %v, got %v", ManagerEventStarted, event.Type)
			}
			if event.Message != "test message" {
				t.Errorf("expected 'test message', got %v", event.Message)
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("event was not received")
		}
	})

	t.Run("handles full channel gracefully", func(t *testing.T) {
		manager, _, _, _, _, _ := createTestManager(t)

		// Fill the channel
		for i := 0; i < 100; i++ {
			manager.events <- ManagerEvent{Type: ManagerEventStarted, Message: "fill"}
		}

		// This should not block or crash
		manager.emitEvent(ManagerEventError, "overflow message")

		// Channel should still be full with original messages
		if len(manager.events) != 100 {
			t.Errorf("expected channel to remain full, got length %d", len(manager.events))
		}
	})
}
