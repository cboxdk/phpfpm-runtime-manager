package app

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/autoscaler"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/config"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/metrics"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/prometheus"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/storage"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/supervisor"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/telemetry"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/types"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// Manager coordinates all system components
type Manager struct {
	config     *config.Config
	logger     *zap.Logger
	supervisor types.ProcessSupervisor
	collector  types.MetricsCollector
	storage    types.Storage
	exporter   types.PrometheusExporter

	// Autoscaling component
	autoscaler *autoscaler.Manager

	// Telemetry components
	telemetryService *telemetry.Service
	eventEmitter     *telemetry.EventEmitter
	eventStorage     *storage.EventStorage

	// Internal state
	mu         sync.RWMutex
	running    bool
	startTime  time.Time
	lastReload time.Time
	configPath string // Store original config path

	// Event channels
	events  chan ManagerEvent
	metrics chan types.MetricSet

	// Context for graceful shutdown
	ctx    context.Context
	cancel context.CancelFunc
}

// ManagerEvent represents events from the manager
type ManagerEvent struct {
	Type      ManagerEventType `json:"type"`
	Message   string           `json:"message"`
	Timestamp time.Time        `json:"timestamp"`
	Error     error            `json:"error,omitempty"`
}

// ManagerEventType defines types of manager events
type ManagerEventType string

const (
	ManagerEventStarting ManagerEventType = "starting"
	ManagerEventStarted  ManagerEventType = "started"
	ManagerEventStopping ManagerEventType = "stopping"
	ManagerEventStopped  ManagerEventType = "stopped"
	ManagerEventReloaded ManagerEventType = "reloaded"
	ManagerEventError    ManagerEventType = "error"
)

// NewManager creates a new manager instance
func NewManager(cfg *config.Config, logger *zap.Logger) (*Manager, error) {
	return NewManagerWithConfigPath(cfg, "", logger)
}

// NewManagerWithPHPFPMBinary creates a new manager instance with a specific PHP-FPM binary
func NewManagerWithPHPFPMBinary(cfg *config.Config, phpfpmBinary string, logger *zap.Logger) (*Manager, error) {
	return NewManagerWithConfigPathAndBinary(cfg, "", phpfpmBinary, logger)
}

// NewManagerWithConfigPath creates a new manager instance with explicit config path
func NewManagerWithConfigPath(cfg *config.Config, configPath string, logger *zap.Logger) (*Manager, error) {
	return NewManagerWithConfigPathAndBinary(cfg, configPath, "", logger)
}

// NewManagerWithConfigPathAndBinary creates a new manager instance with explicit config path and PHP-FPM binary
func NewManagerWithConfigPathAndBinary(cfg *config.Config, configPath string, phpfpmBinary string, logger *zap.Logger) (*Manager, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration is required")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create storage backend
	sqliteStore, err := storage.NewSQLiteStorage(cfg.Storage, logger.Named("storage"))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create storage: %w", err)
	}

	// Create telemetry service
	telemetryConfig := telemetry.Config{
		Enabled:        cfg.Telemetry.Enabled,
		ServiceName:    cfg.Telemetry.ServiceName,
		ServiceVersion: cfg.Telemetry.ServiceVersion,
		Environment:    cfg.Telemetry.Environment,
		Exporter: telemetry.ExporterConfig{
			Type:     cfg.Telemetry.Exporter.Type,
			Endpoint: cfg.Telemetry.Exporter.Endpoint,
			Headers:  cfg.Telemetry.Exporter.Headers,
		},
		Sampling: telemetry.SamplingConfig{
			Rate: cfg.Telemetry.Sampling.Rate,
		},
	}

	telemetryService, err := telemetry.NewService(telemetryConfig, logger.Named("telemetry"))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create telemetry service: %w", err)
	}

	// Create event storage
	eventStorage := storage.NewEventStorage(sqliteStore.DB(), logger.Named("events"))

	// Create event emitter
	eventEmitter := telemetry.NewEventEmitter(telemetryService, logger.Named("events"), eventStorage)

	// Create metrics collector with PHP-FPM paths for config parsing
	collector, err := metrics.NewCollectorWithPHPFPM(cfg.Monitoring, cfg.Pools, phpfpmBinary, cfg.PHPFPM.GlobalConfigPath, logger.Named("metrics"))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create metrics collector: %w", err)
	}

	// Create process supervisor with event emitter
	var processSupervisor *supervisor.Supervisor
	if phpfpmBinary != "" {
		processSupervisor, err = supervisor.NewSupervisorWithConfigAndBinary(cfg.Pools, phpfpmBinary, cfg.PHPFPM.GlobalConfigPath, logger.Named("supervisor"), eventEmitter)
	} else {
		processSupervisor, err = supervisor.NewSupervisorWithConfigAndBinary(cfg.Pools, "", cfg.PHPFPM.GlobalConfigPath, logger.Named("supervisor"), eventEmitter)
	}
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create supervisor: %w", err)
	}

	// Create Prometheus exporter
	exporter, err := prometheus.NewExporter(cfg.Server, logger.Named("prometheus"))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create prometheus exporter: %w", err)
	}

	// Create autoscaler manager
	autoscalerManager, err := autoscaler.NewManager(*cfg, collector, logger.Named("autoscaler"))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create autoscaler: %w", err)
	}

	m := &Manager{
		config:           cfg,
		logger:           logger,
		supervisor:       processSupervisor,
		collector:        collector,
		storage:          sqliteStore,
		exporter:         exporter,
		autoscaler:       autoscalerManager,
		telemetryService: telemetryService,
		eventEmitter:     eventEmitter,
		eventStorage:     eventStorage,
		configPath:       configPath,
		events:           make(chan ManagerEvent, config.DefaultEventChannelBuffer),
		metrics:          make(chan types.MetricSet, config.DefaultMetricsChannelBuffer),
		ctx:              ctx,
		cancel:           cancel,
	}

	// Set up API components for Prometheus exporter
	exporter.SetAPIComponents(m, processSupervisor, collector, eventStorage, &cfg.Security, "1.0.0")

	return m, nil
}

// Run starts the manager and all its components
func (m *Manager) Run(ctx context.Context) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return fmt.Errorf("manager is already running")
	}
	m.running = true
	m.startTime = time.Now()
	m.mu.Unlock()

	m.emitEvent(ManagerEventStarting, "Starting phpfpm-runtime-manager")

	// Perform pre-flight checks before starting any services
	if err := m.performPreflightChecks(ctx); err != nil {
		m.mu.Lock()
		m.running = false
		m.mu.Unlock()
		m.emitEvent(ManagerEventError, fmt.Sprintf("Pre-flight checks failed: %v", err))
		return fmt.Errorf("pre-flight checks failed: %w", err)
	}

	// Create error group for coordinated startup/shutdown
	g, gCtx := errgroup.WithContext(ctx)

	// Start storage backend
	g.Go(func() error {
		m.logger.Info("Starting storage backend")
		return m.storage.Start(gCtx)
	})

	// Start telemetry service
	g.Go(func() error {
		m.logger.Info("Starting telemetry service")
		if m.telemetryService != nil {
			return m.telemetryService.Start(gCtx)
		}
		return nil
	})

	// Start process supervisor
	g.Go(func() error {
		m.logger.Info("Starting process supervisor")
		return m.supervisor.Start(gCtx)
	})

	// Start metrics collector
	g.Go(func() error {
		m.logger.Info("Starting metrics collector")
		return m.collector.Start(gCtx)
	})

	// Start Prometheus exporter
	g.Go(func() error {
		m.logger.Info("Starting Prometheus exporter")
		return m.exporter.Start(gCtx)
	})

	// Start autoscaler manager (skip if nil for tests)
	if m.autoscaler != nil {
		g.Go(func() error {
			m.logger.Info("Starting autoscaler manager")
			return m.autoscaler.Start(gCtx)
		})
	}

	// Start event processing
	g.Go(func() error {
		return m.processEvents(gCtx)
	})

	// Start metrics processing
	g.Go(func() error {
		return m.processMetrics(gCtx)
	})

	// Wait a moment to ensure all components started
	time.Sleep(config.DefaultStartupDelay)

	m.emitEvent(ManagerEventStarted, "Manager started successfully")
	m.logger.Info("Manager started successfully",
		zap.Int("pools", len(m.config.Pools)),
		zap.Duration("startup_time", time.Since(m.startTime)))

	// Wait for completion or error
	err := g.Wait()

	// The supervisor will have already called its Stop() method during context cancellation
	// Just ensure autoscaler is stopped to prevent new scaling actions
	m.logger.Info("Stopping remaining services")

	if m.autoscaler != nil {
		if stopErr := m.autoscaler.Stop(); stopErr != nil {
			m.logger.Error("Failed to stop autoscaler", zap.Error(stopErr))
		}
	}

	// Clean shutdown
	m.mu.Lock()
	m.running = false
	m.mu.Unlock()

	m.emitEvent(ManagerEventStopped, "Manager stopped")

	if err != nil && err != context.Canceled {
		m.logger.Error("Manager stopped with error", zap.Error(err))
		return err
	}

	m.logger.Info("Manager stopped gracefully")
	return nil
}

// Reload reloads the configuration and restarts components as needed
func (m *Manager) Reload(ctx context.Context) error {
	m.logger.Info("Reloading configuration")

	// Load new configuration
	newConfig, err := config.Load(m.getConfigPath())
	if err != nil {
		m.emitEvent(ManagerEventError, fmt.Sprintf("Failed to reload config: %v", err))
		return fmt.Errorf("failed to reload config: %w", err)
	}

	// Perform zero-downtime reload
	if err := m.supervisor.Reload(ctx); err != nil {
		m.emitEvent(ManagerEventError, fmt.Sprintf("Failed to reload supervisor: %v", err))
		return fmt.Errorf("failed to reload supervisor: %w", err)
	}

	m.mu.Lock()
	m.config = newConfig
	m.lastReload = time.Now()
	m.mu.Unlock()

	m.emitEvent(ManagerEventReloaded, "Configuration reloaded successfully")
	m.logger.Info("Configuration reloaded successfully")

	return nil
}

// Restart performs a zero-downtime restart
func (m *Manager) Restart(ctx context.Context) error {
	m.logger.Info("Performing zero-downtime restart")

	if err := m.supervisor.Restart(ctx); err != nil {
		return fmt.Errorf("failed to restart supervisor: %w", err)
	}

	m.logger.Info("Zero-downtime restart completed")
	return nil
}

// Health returns the current health status
func (m *Manager) Health() types.HealthStatus {
	m.mu.RLock()
	running := m.running
	m.mu.RUnlock()

	if !running {
		return types.HealthStatus{
			Overall: types.HealthStateStopping,
			Updated: time.Now(),
		}
	}

	return m.supervisor.Health()
}

// processEvents handles internal events
func (m *Manager) processEvents(ctx context.Context) error {
	// Subscribe to supervisor events
	supervisorEvents := m.supervisor.Subscribe()

	for {
		select {
		case <-ctx.Done():
			return nil
		case event := <-m.events:
			m.logger.Info("Manager event",
				zap.String("type", string(event.Type)),
				zap.String("message", event.Message),
				zap.Error(event.Error))
		case event := <-supervisorEvents:
			m.logger.Info("Process event",
				zap.String("type", string(event.Type)),
				zap.String("pool", event.Pool),
				zap.Int("pid", event.PID),
				zap.String("message", event.Message),
				zap.Error(event.Error))
		}
	}
}

// processMetrics handles metrics collection and storage
func (m *Manager) processMetrics(ctx context.Context) error {
	// Subscribe to metrics
	metricsChannel := m.collector.Subscribe()

	for {
		select {
		case <-ctx.Done():
			return nil
		case metrics := <-metricsChannel:
			// Store metrics
			if err := m.storage.Store(ctx, metrics); err != nil {
				m.logger.Error("Failed to store metrics", zap.Error(err))
				continue
			}

			// Update Prometheus exporter
			if err := m.exporter.UpdateMetrics(metrics); err != nil {
				m.logger.Error("Failed to update prometheus metrics", zap.Error(err))
			}

			m.logger.Debug("Processed metrics",
				zap.Int("count", len(metrics.Metrics)),
				zap.Time("timestamp", metrics.Timestamp))
		}
	}
}

// emitEvent emits a manager event
func (m *Manager) emitEvent(eventType ManagerEventType, message string) {
	event := ManagerEvent{
		Type:      eventType,
		Message:   message,
		Timestamp: time.Now(),
	}

	select {
	case m.events <- event:
	default:
		// Channel full, log directly
		m.logger.Warn("Event channel full, dropping event",
			zap.String("type", string(eventType)),
			zap.String("message", message))
	}
}

// getConfigPath returns the configuration file path
func (m *Manager) getConfigPath() string {
	if m.configPath != "" {
		return m.configPath
	}
	// Fallback to default path if none provided
	return config.DefaultConfigPath
}

// performPreflightChecks validates system dependencies and configuration before startup
func (m *Manager) performPreflightChecks(ctx context.Context) error {
	m.logger.Info("Performing pre-flight checks")

	// Check for port conflicts with server bind address
	if m.config.Server.BindAddress != "" {
		if err := m.checkBindAddressAvailable(m.config.Server.BindAddress); err != nil {
			return fmt.Errorf("server bind address %s is not available: %w", m.config.Server.BindAddress, err)
		}
		m.logger.Info("Server bind address available", zap.String("bind_address", m.config.Server.BindAddress))
	}

	// Check for port conflicts with PHP-FPM pool addresses
	if err := m.checkPoolAddressesAvailable(); err != nil {
		return fmt.Errorf("%w", err)
	}

	// Validate that all required directories exist and are writable
	if err := m.validateStorageDirectories(); err != nil {
		return fmt.Errorf("storage directory validation failed: %w", err)
	}

	// Ensure required directory structure exists
	if err := m.ensureDirectoryStructure(); err != nil {
		return fmt.Errorf("directory structure creation failed: %w", err)
	}

	// Validate PHP-FPM config file write permissions
	if err := m.validatePHPFPMConfigPermissions(); err != nil {
		return fmt.Errorf("PHP-FPM config validation failed: %w", err)
	}

	m.logger.Info("All pre-flight checks passed successfully")
	return nil
}

// checkBindAddressAvailable checks if a bind address is available for binding
func (m *Manager) checkBindAddressAvailable(bindAddress string) error {
	// Try to listen on the address briefly to see if it's available
	listener, err := net.Listen("tcp", bindAddress)
	if err != nil {
		return fmt.Errorf("address is already in use or cannot be bound: %w", err)
	}
	listener.Close()

	return nil
}

// checkPoolAddressesAvailable checks if all pool FastCGI endpoints are available for binding
func (m *Manager) checkPoolAddressesAvailable() error {
	for _, pool := range m.config.Pools {
		// Skip Unix socket endpoints - they'll be checked during pool creation
		if strings.HasPrefix(pool.FastCGIEndpoint, "unix:") {
			continue
		}

		// Check if TCP endpoint is available
		if err := m.checkBindAddressAvailable(pool.FastCGIEndpoint); err != nil {
			return fmt.Errorf("port %s already in use (pool '%s')",
				pool.FastCGIEndpoint, pool.Name)
		}
		m.logger.Info("Pool FastCGI endpoint available",
			zap.String("pool", pool.Name),
			zap.String("endpoint", pool.FastCGIEndpoint))
	}

	return nil
}

// validateStorageDirectories ensures required directories exist and are accessible
func (m *Manager) validateStorageDirectories() error {
	// Check storage database directory if using file-based storage
	if m.config.Storage.DatabasePath != "" && m.config.Storage.DatabasePath != ":memory:" {
		// Extract directory from database path
		dbDir := filepath.Dir(m.config.Storage.DatabasePath)
		if dbDir != "." && dbDir != "" {
			// Ensure directory exists and is writable
			if _, err := os.Stat(dbDir); os.IsNotExist(err) {
				if err := os.MkdirAll(dbDir, 0755); err != nil {
					return fmt.Errorf("failed to create database directory: %s: %w", dbDir, err)
				}
			}

			// Test write permissions by creating a temporary file
			tempFile := filepath.Join(dbDir, ".write_test")
			if file, err := os.Create(tempFile); err != nil {
				return fmt.Errorf("database directory is not writable: %s: %w", dbDir, err)
			} else {
				file.Close()
				os.Remove(tempFile)
			}
		}
	}

	return nil
}

// validatePHPFPMConfigPermissions checks if PHP-FPM config files can be written
func (m *Manager) validatePHPFPMConfigPermissions() error {
	// Check global config path
	globalConfigPath := m.config.PHPFPM.GlobalConfigPath
	if globalConfigPath != "" {
		if err := m.checkConfigFileWritable(globalConfigPath); err != nil {
			return fmt.Errorf("global config file %s: %w", globalConfigPath, err)
		}
		m.logger.Info("Global PHP-FPM config path verified", zap.String("path", globalConfigPath))
	}

	// Check individual pool config paths
	for _, pool := range m.config.Pools {
		if pool.ConfigPath != "" {
			if err := m.checkConfigFileWritable(pool.ConfigPath); err != nil {
				return fmt.Errorf("pool '%s' config file %s: %w", pool.Name, pool.ConfigPath, err)
			}
			m.logger.Info("Pool config path verified", zap.String("pool", pool.Name), zap.String("path", pool.ConfigPath))
		}
	}

	return nil
}

// ensureDirectoryStructure ensures all required directories exist
func (m *Manager) ensureDirectoryStructure() error {
	// Get the base directory from global config path
	globalConfigPath := m.config.PHPFPM.GlobalConfigPath
	if globalConfigPath == "" {
		return nil // Nothing to do if no global config path
	}

	baseDir := filepath.Dir(globalConfigPath)

	// Create base directory
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("failed to create base directory %s: %w", baseDir, err)
	}

	// Always ensure structured subdirectories exist
	subdirs := []string{"logs", "pids", "sockets", "config"}
	for _, subdir := range subdirs {
		subdirPath := filepath.Join(baseDir, subdir)
		if err := os.MkdirAll(subdirPath, 0755); err != nil {
			return fmt.Errorf("failed to create subdirectory %s: %w", subdirPath, err)
		}
	}

	m.logger.Info("Directory structure verified", zap.String("base_dir", baseDir))
	return nil
}

// checkConfigFileWritable checks if a config file can be written to
func (m *Manager) checkConfigFileWritable(configPath string) error {
	// Skip validation for empty paths
	if configPath == "" {
		return nil
	}

	// Check if file exists
	if _, err := os.Stat(configPath); err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, ensure directory exists and is writable
			dir := filepath.Dir(configPath)

			// Create directory if it doesn't exist
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("failed to create directory: %s: %w", dir, err)
			}

			// Test write permissions by creating a temporary file
			tempFile := filepath.Join(dir, ".write_test_"+filepath.Base(configPath))
			if file, err := os.Create(tempFile); err != nil {
				return fmt.Errorf("directory is not writable: %s: %w", dir, err)
			} else {
				file.Close()
				os.Remove(tempFile)
			}
			return nil
		}
		return fmt.Errorf("cannot access file: %w", err)
	}

	// File exists, check if it's writable
	file, err := os.OpenFile(configPath, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("file is not writable: %w", err)
	}
	file.Close()

	return nil
}

// IsRunning returns true if the manager is currently running
func (m *Manager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}
