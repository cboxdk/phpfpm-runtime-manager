package autoscaler

import (
	"context"
	"time"
)

// PHPFPMInterface defines the contract for PHP-FPM pool management operations.
//
// This interface abstracts PHP-FPM pool management to allow different implementations
// (e.g., direct configuration file management, FPM API, systemd service control).
// It provides the core operations needed for intelligent autoscaling.
type PHPFPMInterface interface {
	// Pool management operations
	GetPoolStatus(ctx context.Context, poolName string) (*PoolStatus, error)
	UpdatePoolConfiguration(ctx context.Context, poolName string, config PoolConfiguration) error
	ReloadPool(ctx context.Context, poolName string) error

	// Worker management operations
	GetWorkerMetrics(ctx context.Context, poolName string) ([]*WorkerMetrics, error)
	GetQueueMetrics(ctx context.Context, poolName string) (*QueueMetrics, error)

	// Configuration operations
	ValidateConfiguration(ctx context.Context, poolName string, config PoolConfiguration) error
	BackupConfiguration(ctx context.Context, poolName string) (string, error)
	RestoreConfiguration(ctx context.Context, poolName string, backupPath string) error
}

// PoolStatus represents the current status of a PHP-FPM pool
type PoolStatus struct {
	Name               string    `json:"name"`
	ProcessManager     string    `json:"process_manager"` // dynamic, static, ondemand
	StartTime          time.Time `json:"start_time"`
	StartSince         int64     `json:"start_since"`
	AcceptedConn       int64     `json:"accepted_conn"`
	ListenQueue        int       `json:"listen_queue"`
	MaxListenQueue     int       `json:"max_listen_queue"`
	ListenQueueLen     int       `json:"listen_queue_len"`
	IdleProcesses      int       `json:"idle_processes"`
	ActiveProcesses    int       `json:"active_processes"`
	TotalProcesses     int       `json:"total_processes"`
	MaxActiveProcesses int       `json:"max_active_processes"`
	MaxChildrenReached int       `json:"max_children_reached"`
	SlowRequests       int64     `json:"slow_requests"`

	// Configuration values
	MaxChildren     int `json:"max_children"`
	StartServers    int `json:"start_servers"`
	MinSpareServers int `json:"min_spare_servers"`
	MaxSpareServers int `json:"max_spare_servers"`

	// Additional metadata
	ConfigFile string    `json:"config_file"`
	LastReload time.Time `json:"last_reload"`
}

// PoolConfiguration represents PHP-FPM pool configuration parameters
type PoolConfiguration struct {
	// Process management
	ProcessManager  string `ini:"pm"`                   // dynamic, static, ondemand
	MaxChildren     int    `ini:"pm.max_children"`      // Maximum number of child processes
	StartServers    int    `ini:"pm.start_servers"`     // Number of child processes created on startup
	MinSpareServers int    `ini:"pm.min_spare_servers"` // Minimum number of idle server processes
	MaxSpareServers int    `ini:"pm.max_spare_servers"` // Maximum number of idle server processes
	MaxRequests     int    `ini:"pm.max_requests"`      // Number of requests each child process should execute before respawning

	// Resource limits
	ProcessIdleTimeout      time.Duration `ini:"pm.process_idle_timeout"`   // Seconds after which an idle process will be killed
	RequestTerminateTimeout time.Duration `ini:"request_terminate_timeout"` // Timeout for serving a single request
	RLimitFiles             int           `ini:"rlimit_files"`              // Maximum number of open file descriptors
	RLimitCore              int           `ini:"rlimit_core"`               // Maximum size of core files

	// Memory limits (if available)
	MemoryLimit string `ini:"php_admin_value[memory_limit]"` // PHP memory limit

	// Logging and monitoring
	SlowLog               string        `ini:"slowlog"`                 // Path to slow log file
	RequestSlowlogTimeout time.Duration `ini:"request_slowlog_timeout"` // Timeout for slow request logging

	// Status and ping
	StatusPath   string `ini:"pm.status_path"` // URI for status page
	PingPath     string `ini:"ping.path"`      // URI for ping page
	PingResponse string `ini:"ping.response"`  // Response for ping requests

	// Security
	ClearEnv      bool `ini:"clear_env"`                 // Clear environment variables
	SecurityLimit bool `ini:"security.limit_extensions"` // Limit script extensions
}

// DefaultPoolConfiguration returns a default pool configuration suitable for autoscaling
func DefaultPoolConfiguration() PoolConfiguration {
	return PoolConfiguration{
		ProcessManager:          "dynamic",
		MaxChildren:             50,
		StartServers:            5,
		MinSpareServers:         2,
		MaxSpareServers:         10,
		MaxRequests:             1000,
		ProcessIdleTimeout:      10 * time.Second,
		RequestTerminateTimeout: 30 * time.Second,
		RLimitFiles:             1024,
		RLimitCore:              0,
		MemoryLimit:             "128M",
		SlowLog:                 "/var/log/php-fpm/slow.log",
		RequestSlowlogTimeout:   5 * time.Second,
		StatusPath:              "/status",
		PingPath:                "/ping",
		PingResponse:            "pong",
		ClearEnv:                true,
		SecurityLimit:           true,
	}
}

// PHPFPMIntegrationManager integrates the intelligent scaler with PHP-FPM operations
type PHPFPMIntegrationManager struct {
	scaler   *IntelligentScaler
	phpfpm   PHPFPMInterface
	poolName string
}

// NewPHPFPMIntegrationManager creates a new PHP-FPM manager with intelligent scaling
func NewPHPFPMIntegrationManager(poolName string, phpfpm PHPFPMInterface) (*PHPFPMIntegrationManager, error) {
	scaler, err := NewIntelligentScaler(poolName)
	if err != nil {
		return nil, err
	}

	return &PHPFPMIntegrationManager{
		scaler:   scaler,
		phpfpm:   phpfpm,
		poolName: poolName,
	}, nil
}

// UpdateConfiguration applies scaling recommendations to PHP-FPM pool configuration
func (m *PHPFPMIntegrationManager) UpdateConfiguration(ctx context.Context) error {
	// Get current status
	status, err := m.phpfpm.GetPoolStatus(ctx, m.poolName)
	if err != nil {
		return err
	}

	// Update scaler with current worker count
	m.scaler.UpdateWorkerCount(status.TotalProcesses, status.ActiveProcesses)

	// Get worker metrics for baseline learning
	workerMetrics, err := m.phpfpm.GetWorkerMetrics(ctx, m.poolName)
	if err != nil {
		return err
	}

	// Update scaler with worker metrics
	for _, metric := range workerMetrics {
		m.scaler.UpdateWorkerMetrics(metric)
	}

	// Get queue metrics
	queueMetrics, err := m.phpfpm.GetQueueMetrics(ctx, m.poolName)
	if err != nil {
		return err
	}

	m.scaler.UpdateQueueMetrics(queueMetrics)

	// Calculate target workers
	targetWorkers, err := m.scaler.CalculateTargetWorkers(ctx)
	if err != nil {
		return err
	}

	// Apply scaling if needed
	if targetWorkers != status.TotalProcesses {
		return m.applyScaling(ctx, targetWorkers)
	}

	return nil
}

// applyScaling applies the calculated scaling decision to PHP-FPM configuration
func (m *PHPFPMIntegrationManager) applyScaling(ctx context.Context, targetWorkers int) error {
	// Calculate optimal configuration ratios
	_, start, minSpare, maxSpare, err := CalculateOptimalWorkers(targetWorkers)
	if err != nil {
		return err
	}

	// Create new configuration
	config := DefaultPoolConfiguration()
	config.MaxChildren = targetWorkers
	config.StartServers = start
	config.MinSpareServers = minSpare
	config.MaxSpareServers = maxSpare

	// Validate configuration
	if err := m.phpfpm.ValidateConfiguration(ctx, m.poolName, config); err != nil {
		return err
	}

	// Backup current configuration
	backupPath, err := m.phpfpm.BackupConfiguration(ctx, m.poolName)
	if err != nil {
		return err
	}

	// Apply new configuration
	if err := m.phpfpm.UpdatePoolConfiguration(ctx, m.poolName, config); err != nil {
		// Restore backup on failure
		m.phpfpm.RestoreConfiguration(ctx, m.poolName, backupPath)
		return err
	}

	// Reload pool to apply changes
	return m.phpfpm.ReloadPool(ctx, m.poolName)
}

// GetScalingStatus returns the current scaling status and metrics
func (m *PHPFPMIntegrationManager) GetScalingStatus(ctx context.Context) (*ScalingStatus, error) {
	status, err := m.phpfpm.GetPoolStatus(ctx, m.poolName)
	if err != nil {
		return nil, err
	}

	queueMetrics, err := m.phpfpm.GetQueueMetrics(ctx, m.poolName)
	if err != nil {
		return nil, err
	}

	return &ScalingStatus{
		PoolName:           m.poolName,
		CurrentWorkers:     status.TotalProcesses,
		ActiveWorkers:      status.ActiveProcesses,
		IdleWorkers:        status.IdleProcesses,
		QueueDepth:         queueMetrics.Depth,
		ProcessingVelocity: queueMetrics.ProcessingVelocity,
		IsLearning:         m.scaler.baseline.IsLearning,
		ConfidenceScore:    m.scaler.baseline.ConfidenceScore,
		SampleCount:        m.scaler.baseline.SampleCount,
		LastScaleTime:      m.scaler.lastScaleTime,
		LastScaleDirection: m.scaler.lastScaleDirection,
	}, nil
}

// ScalingStatus represents the current autoscaling status
type ScalingStatus struct {
	PoolName           string    `json:"pool_name"`
	CurrentWorkers     int       `json:"current_workers"`
	ActiveWorkers      int       `json:"active_workers"`
	IdleWorkers        int       `json:"idle_workers"`
	QueueDepth         int       `json:"queue_depth"`
	ProcessingVelocity float64   `json:"processing_velocity"`
	IsLearning         bool      `json:"is_learning"`
	ConfidenceScore    float64   `json:"confidence_score"`
	SampleCount        int       `json:"sample_count"`
	LastScaleTime      time.Time `json:"last_scale_time"`
	LastScaleDirection int       `json:"last_scale_direction"` // 1 for up, -1 for down, 0 for none
}
