package autoscaler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/config"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/metrics"
	"go.uber.org/zap"
)

// Emergency scaling configuration constants
const (
	// Utilization thresholds
	FullUtilizationThreshold = 100.0 // 100% utilization triggers emergency scaling
	NearCapacityThreshold    = 95.0  // 95% utilization + queue triggers proactive scaling
	AggressiveThreshold      = 90.0  // 90% utilization + queue triggers aggressive scaling

	// Scaling factors
	AggressiveScaleFactor = 0.5  // 50% increase for emergency conditions
	ProactiveScaleFactor  = 0.25 // 25% increase for proactive scaling

	// Cooldown periods
	EmergencyCooldown = 30 * time.Second // Shorter cooldown for emergency scaling

	// Minimum scaling increments
	MinAggressiveScaleUp = 2 // Minimum 2 workers for aggressive scaling
	MinProactiveScaleUp  = 1 // Minimum 1 worker for proactive scaling
)

// PHPFPMExecutor interface for supervisor integration
type PHPFPMExecutor interface {
	ScalePool(poolName string, targetWorkers int) error
	ReloadPool(poolName string) error
}

// Manager orchestrates intelligent autoscaling across multiple PHP-FPM pools
type Manager struct {
	pools     map[string]*PoolScaler
	config    config.Config
	logger    *zap.Logger
	collector *metrics.Collector

	// PHP-FPM integration
	phpfpmManager *SupervisorPHPFPMManager

	// Control
	ctx       context.Context
	cancel    context.CancelFunc
	isRunning bool
	mu        sync.RWMutex

	// Scaling decisions
	scalingInterval    time.Duration
	lastGlobalDecision time.Time
	globalCooldown     time.Duration
}

// PoolScaler manages scaling for a single PHP-FPM pool
type PoolScaler struct {
	poolConfig config.PoolConfig
	scaler     *IntelligentScaler
	collector  *BaselineCollector
	logger     *zap.Logger

	// Current state
	currentWorkers int
	targetWorkers  int
	lastScaleTime  time.Time
	lastDecision   string

	// Metrics integration
	metricsChannel chan *metrics.WorkerProcessMetrics
}

// NewManager creates a new autoscaler manager
func NewManager(cfg config.Config, metricsCollector *metrics.Collector, logger *zap.Logger) (*Manager, error) {
	return NewManagerWithSupervisor(cfg, metricsCollector, nil, logger)
}

// NewManagerWithSupervisor creates a new autoscaler manager with supervisor integration.
//
// This constructor creates a fully integrated autoscaling system that can:
//   - Analyze optimal worker configurations based on system metrics
//   - Update PHP-FPM configuration files dynamically
//   - Perform hot reloads without process restarts
//   - Execute emergency scaling for queue buildup and 100% utilization
//
// Parameters:
//   - cfg: Application configuration including pool definitions
//   - metricsCollector: Metrics collection system for monitoring worker performance
//   - supervisor: PHP-FPM supervisor for pool management (can be nil for testing)
//   - logger: Structured logger for operational visibility
//
// Returns:
//   - *Manager: Configured manager ready for autoscaling operations
//   - error: Configuration validation errors if any
func NewManagerWithSupervisor(cfg config.Config, metricsCollector *metrics.Collector, supervisor PHPFPMSupervisor, logger *zap.Logger) (*Manager, error) {
	// Input validation
	if metricsCollector == nil {
		return nil, fmt.Errorf("metrics collector cannot be nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}
	if len(cfg.Pools) == 0 {
		return nil, fmt.Errorf("at least one pool must be configured")
	}
	ctx, cancel := context.WithCancel(context.Background())

	manager := &Manager{
		pools:           make(map[string]*PoolScaler),
		config:          cfg,
		logger:          logger,
		collector:       metricsCollector,
		ctx:             ctx,
		cancel:          cancel,
		scalingInterval: DefaultScalingInterval, // Make scaling decisions
		globalCooldown:  DefaultGlobalCooldown,  // Global cooldown between major scaling actions
	}

	// Set up PHP-FPM supervisor integration if provided
	if supervisor != nil {
		phpfpmManager, err := NewSupervisorPHPFPMManager(supervisor, logger.With(zap.String("component", "phpfpm_manager")))
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create PHP-FPM manager: %w", err)
		}
		manager.phpfpmManager = phpfpmManager
		logger.Info("PHP-FPM supervisor integration enabled")
	}

	// Initialize pool scalers
	for _, poolConfig := range cfg.Pools {
		poolScaler, err := manager.createPoolScaler(poolConfig)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create scaler for pool %s: %w", poolConfig.Name, err)
		}
		manager.pools[poolConfig.Name] = poolScaler
	}

	return manager, nil
}

// createPoolScaler creates an intelligent scaler for a single pool
func (m *Manager) createPoolScaler(poolConfig config.PoolConfig) (*PoolScaler, error) {
	// Create intelligent scaler
	scaler, err := NewIntelligentScaler(poolConfig.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to create intelligent scaler: %w", err)
	}

	// Create baseline collector
	collector := NewBaselineCollector(scaler, m.collector, m.logger.With(zap.String("pool", poolConfig.Name)))

	poolScaler := &PoolScaler{
		poolConfig:     poolConfig,
		scaler:         scaler,
		collector:      collector,
		logger:         m.logger.With(zap.String("pool", poolConfig.Name)),
		metricsChannel: make(chan *metrics.WorkerProcessMetrics, 100), // Buffered channel
	}

	// Start with learning phase - 1 worker initially
	poolScaler.currentWorkers = 1
	poolScaler.targetWorkers = 1

	return poolScaler, nil
}

// Start begins intelligent autoscaling for all pools
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isRunning {
		return fmt.Errorf("autoscaler manager is already running")
	}

	m.logger.Info("Starting intelligent autoscaler manager",
		zap.Int("pools", len(m.pools)),
		zap.Duration("scaling_interval", m.scalingInterval),
	)

	// Start each pool scaler
	for poolName, poolScaler := range m.pools {
		m.logger.Info("Starting intelligent scaling for pool",
			zap.String("pool", poolName),
			zap.Bool("is_learning", poolScaler.scaler.baseline.IsLearning),
		)

		// Start baseline collection
		if err := poolScaler.collector.Start(m.ctx); err != nil {
			return fmt.Errorf("failed to start baseline collector for pool %s: %w", poolName, err)
		}

		// Start metrics integration
		poolScaler.collector.IntegrateWithMetrics(poolScaler.metricsChannel)
	}

	m.isRunning = true

	// Start main scaling loop
	go m.runScalingLoop()

	return nil
}

// Stop halts all autoscaling operations with graceful shutdown
func (m *Manager) Stop() error {
	return m.StopWithTimeout(30 * time.Second)
}

// StopWithTimeout halts all autoscaling operations with a specified timeout
func (m *Manager) StopWithTimeout(timeout time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isRunning {
		return nil
	}

	m.logger.Info("Stopping intelligent autoscaler manager",
		zap.Duration("timeout", timeout))

	// Mark as stopping to prevent new operations
	m.isRunning = false

	// Create a context for shutdown timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), timeout)
	defer shutdownCancel()

	// Stop all pool collectors with timeout
	var wg sync.WaitGroup
	for poolName, poolScaler := range m.pools {
		wg.Add(1)
		go func(name string, scaler *PoolScaler) {
			defer wg.Done()

			// Stop collector
			if err := scaler.collector.Stop(); err != nil {
				m.logger.Warn("Failed to stop baseline collector",
					zap.String("pool", name),
					zap.Error(err),
				)
			}

			// Close metrics channel safely
			select {
			case <-scaler.metricsChannel:
				// Drain any remaining messages
			default:
			}
			close(scaler.metricsChannel)

		}(poolName, poolScaler)
	}

	// Wait for collectors to stop with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.logger.Info("All pool collectors stopped successfully")
	case <-shutdownCtx.Done():
		m.logger.Warn("Timeout waiting for pool collectors to stop")
	}

	// Cancel main context to stop scaling loop
	m.cancel()

	m.logger.Info("Intelligent autoscaler manager stopped")
	return nil
}

// runScalingLoop executes the main intelligent scaling logic
func (m *Manager) runScalingLoop() {
	ticker := time.NewTicker(m.scalingInterval)
	defer ticker.Stop()

	defer func() {
		if r := recover(); r != nil {
			m.logger.Error("Scaling loop panic", zap.Any("error", r))
		}
	}()

	for {
		select {
		case <-m.ctx.Done():
			m.logger.Debug("Scaling loop stopping due to context cancellation")
			return
		case <-ticker.C:
			m.executeScalingDecisions()
		}
	}
}

// executeScalingDecisions makes scaling decisions for all pools
func (m *Manager) executeScalingDecisions() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.isGlobalCooldownExpired() {
		return
	}

	scalingDecisions, decisionReasons := m.calculateScalingDecisions()

	// Execute scaling decisions
	if len(scalingDecisions) > 0 {
		m.executePoolScaling(scalingDecisions, decisionReasons)
		m.lastGlobalDecision = time.Now()
	}
}

// isGlobalCooldownExpired checks if the global cooldown period has passed
func (m *Manager) isGlobalCooldownExpired() bool {
	now := time.Now()
	return now.Sub(m.lastGlobalDecision) >= m.globalCooldown
}

// calculateScalingDecisions calculates target worker counts for all pools
// Returns scaling decisions and their reasons for pools that need scaling
func (m *Manager) calculateScalingDecisions() (map[string]int, map[string]string) {
	scalingDecisions := make(map[string]int)
	decisionReasons := make(map[string]string)

	// Calculate target workers for each pool
	for poolName, poolScaler := range m.pools {
		targetWorkers, err := poolScaler.scaler.CalculateTargetWorkers(m.ctx)
		if err != nil {
			m.logger.Warn("Failed to calculate target workers",
				zap.String("pool", poolName),
				zap.Error(err),
			)
			continue
		}

		// Determine if scaling is needed
		if targetWorkers != poolScaler.currentWorkers {
			scalingDecisions[poolName] = targetWorkers
			decisionReasons[poolName] = m.determineScalingReason(poolScaler)

			m.logScalingDecision(poolName, poolScaler, targetWorkers, decisionReasons[poolName])
		}
	}

	return scalingDecisions, decisionReasons
}

// determineScalingReason generates a human-readable reason for the scaling decision
func (m *Manager) determineScalingReason(poolScaler *PoolScaler) string {
	if poolScaler.scaler.baseline.IsLearning {
		return "learning_phase"
	}
	return fmt.Sprintf("confidence_%.2f", poolScaler.scaler.baseline.ConfidenceScore)
}

// logScalingDecision logs detailed information about a scaling decision
func (m *Manager) logScalingDecision(poolName string, poolScaler *PoolScaler, targetWorkers int, reason string) {
	poolScaler.logger.Info("Scaling decision made",
		zap.String("pool", poolName),
		zap.Int("current_workers", poolScaler.currentWorkers),
		zap.Int("target_workers", targetWorkers),
		zap.String("reason", reason),
		zap.Bool("is_learning", poolScaler.scaler.baseline.IsLearning),
		zap.Float64("confidence", poolScaler.scaler.baseline.ConfidenceScore),
	)
}

// executePoolScaling executes the actual scaling operations
func (m *Manager) executePoolScaling(decisions map[string]int, reasons map[string]string) {
	for poolName, targetWorkers := range decisions {
		poolScaler := m.pools[poolName]
		currentWorkers := poolScaler.currentWorkers

		// Update pool scaler state
		poolScaler.targetWorkers = targetWorkers
		poolScaler.lastScaleTime = time.Now()
		poolScaler.lastDecision = reasons[poolName]

		// In a real implementation, this would trigger actual PHP-FPM pool scaling
		// For now, we just update the internal state
		poolScaler.currentWorkers = targetWorkers

		// Update the scaler's internal state
		poolScaler.scaler.currentWorkers = targetWorkers

		poolScaler.logger.Info("Executed scaling action",
			zap.Int("from_workers", currentWorkers),
			zap.Int("to_workers", targetWorkers),
			zap.String("reason", reasons[poolName]),
		)

		// Execute scaling action via PHP-FPM manager
		if m.phpfpmManager == nil {
			poolScaler.logger.Warn("No PHP-FPM manager available for scaling execution",
				zap.String("pool", poolName),
			)
			continue
		}

		// Attempt scaling with detailed error context
		if err := m.phpfpmManager.ScalePool(poolName, targetWorkers); err != nil {
			poolScaler.logger.Error("Failed to execute scaling action",
				zap.String("pool", poolName),
				zap.Int("from_workers", currentWorkers),
				zap.Int("target_workers", targetWorkers),
				zap.String("reason", reasons[poolName]),
				zap.Error(err),
			)
			continue
		}

		poolScaler.logger.Info("Successfully executed scaling action",
			zap.String("pool", poolName),
			zap.Int("from_workers", currentWorkers),
			zap.Int("to_workers", targetWorkers),
			zap.String("reason", reasons[poolName]),
		)

		// Attempt hot reload with non-blocking error handling
		if err := m.phpfpmManager.ReloadPool(poolName); err != nil {
			poolScaler.logger.Error("Failed to reload pool after scaling",
				zap.String("pool", poolName),
				zap.Error(err),
			)
			// Don't continue here - scaling succeeded, only reload failed
		} else {
			poolScaler.logger.Debug("Pool configuration reloaded successfully",
				zap.String("pool", poolName),
			)
		}
	}
}

// UpdatePoolMetrics feeds metrics to the appropriate pool scaler
func (m *Manager) UpdatePoolMetrics(poolName string, workerMetrics *metrics.WorkerProcessMetrics) {
	m.mu.RLock()
	poolScaler, exists := m.pools[poolName]
	m.mu.RUnlock()

	if !exists {
		m.logger.Warn("Received metrics for unknown pool", zap.String("pool", poolName))
		return
	}

	// Update scaler state
	poolScaler.scaler.currentWorkers = workerMetrics.TotalWorkers
	poolScaler.scaler.activeWorkers = workerMetrics.ActiveWorkers

	// Update queue metrics
	queueMetrics := &QueueMetrics{
		Depth:                workerMetrics.QueueDepth,
		ProcessingVelocity:   workerMetrics.ProcessingVelocity,
		EstimatedProcessTime: workerMetrics.EstimatedProcessTime,
	}
	poolScaler.scaler.UpdateQueueMetrics(queueMetrics)

	// Check for immediate scaling conditions (queue buildup or 100% utilization)
	m.checkEmergencyScaling(poolName, poolScaler, workerMetrics)

	// Feed to baseline collector
	select {
	case poolScaler.metricsChannel <- workerMetrics:
		// Successfully sent
	default:
		// Channel full, skip this update
		m.logger.Debug("Metrics channel full, skipping update", zap.String("pool", poolName))
	}
}

// GetPoolStatus returns the current status of all pools with type safety
func (m *Manager) GetPoolStatus() PoolStatusResponse {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pools := make(map[string]PoolDetail)

	for poolName, poolScaler := range m.pools {
		baselineStatus := poolScaler.scaler.GetBaselineStatusTyped()
		collectorStatus := poolScaler.collector.GetCollectionStatusTyped()

		poolDetail := PoolDetail{
			CurrentWorkers: poolScaler.currentWorkers,
			TargetWorkers:  poolScaler.targetWorkers,
			LastScaleTime:  poolScaler.lastScaleTime,
			LastDecision:   poolScaler.lastDecision,
			Baseline:       baselineStatus,
			Collection:     collectorStatus,
		}

		pools[poolName] = poolDetail
	}

	return PoolStatusResponse{
		IsRunning:       m.isRunning,
		ScalingInterval: m.scalingInterval,
		GlobalCooldown:  m.globalCooldown,
		Pools:           pools,
	}
}

// GetPoolStatusLegacy returns the status in the old map format for backward compatibility
func (m *Manager) GetPoolStatusLegacy() map[string]interface{} {
	status := m.GetPoolStatus()

	// Convert to legacy format for backward compatibility
	legacyStatus := map[string]interface{}{
		"is_running":       status.IsRunning,
		"scaling_interval": status.ScalingInterval.String(),
		"global_cooldown":  status.GlobalCooldown.String(),
		"pools":            make(map[string]interface{}),
	}

	pools := legacyStatus["pools"].(map[string]interface{})
	for poolName, poolDetail := range status.Pools {
		pools[poolName] = map[string]interface{}{
			"current_workers": poolDetail.CurrentWorkers,
			"target_workers":  poolDetail.TargetWorkers,
			"last_scale_time": poolDetail.LastScaleTime,
			"last_decision":   poolDetail.LastDecision,
			"baseline":        poolDetail.Baseline,
			"collection":      poolDetail.Collection,
		}
	}

	return legacyStatus
}

// GetPoolScaler returns the scaler for a specific pool (for testing/debugging)
func (m *Manager) GetPoolScaler(poolName string) (*PoolScaler, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	scaler, exists := m.pools[poolName]
	return scaler, exists
}

// checkEmergencyScaling performs immediate scaling for critical conditions
func (m *Manager) checkEmergencyScaling(poolName string, poolScaler *PoolScaler, workerMetrics *metrics.WorkerProcessMetrics) {
	// Input validation
	if poolScaler == nil || workerMetrics == nil {
		m.logger.Error("Invalid parameters for emergency scaling check",
			zap.String("pool", poolName),
			zap.Bool("poolScaler_nil", poolScaler == nil),
			zap.Bool("workerMetrics_nil", workerMetrics == nil))
		return
	}

	// Calculate utilization safely (avoid division by zero)
	var utilization float64
	if workerMetrics.TotalWorkers > 0 {
		utilization = float64(workerMetrics.ActiveWorkers) / float64(workerMetrics.TotalWorkers) * 100
	} else {
		// If no workers exist, treat as 0% utilization
		utilization = 0.0
	}

	// Check emergency conditions using constants
	emergencyCondition := m.evaluateEmergencyCondition(workerMetrics, utilization)
	if emergencyCondition == nil {
		return // No emergency condition detected
	}

	// Check cooldown to prevent rapid scaling
	if !m.isCooldownExpired(poolScaler.lastScaleTime) {
		return
	}

	// Calculate target workers
	targetWorkers := m.calculateEmergencyTarget(workerMetrics, emergencyCondition, poolScaler.poolConfig.MaxWorkers)
	if targetWorkers <= workerMetrics.TotalWorkers {
		return // No scaling needed
	}

	// Execute emergency scaling
	m.executeEmergencyScaling(poolName, poolScaler, workerMetrics, targetWorkers, emergencyCondition, utilization)
}

// EmergencyCondition represents the type and severity of emergency scaling needed
type EmergencyCondition struct {
	Type        string  // "aggressive" or "proactive"
	ScaleFactor float64 // scaling factor to apply
	MinIncrease int     // minimum worker increase
	Reason      string  // reason template for logging
}

// evaluateEmergencyCondition determines if emergency scaling is needed and what type
func (m *Manager) evaluateEmergencyCondition(workerMetrics *metrics.WorkerProcessMetrics, utilization float64) *EmergencyCondition {
	hasQueue := workerMetrics.QueueDepth > 0
	isFullyUtilized := utilization >= FullUtilizationThreshold
	isNearCapacity := utilization >= NearCapacityThreshold && hasQueue
	isHighUtilizationWithQueue := hasQueue && utilization >= AggressiveThreshold

	if isFullyUtilized || isHighUtilizationWithQueue {
		return &EmergencyCondition{
			Type:        "aggressive",
			ScaleFactor: AggressiveScaleFactor,
			MinIncrease: MinAggressiveScaleUp,
			Reason:      "emergency_scale",
		}
	}

	if isNearCapacity {
		return &EmergencyCondition{
			Type:        "proactive",
			ScaleFactor: ProactiveScaleFactor,
			MinIncrease: MinProactiveScaleUp,
			Reason:      "proactive_scale",
		}
	}

	return nil // No emergency condition
}

// isCooldownExpired checks if enough time has passed since last scaling
func (m *Manager) isCooldownExpired(lastScaleTime time.Time) bool {
	return time.Since(lastScaleTime) >= EmergencyCooldown
}

// calculateEmergencyTarget calculates the target number of workers for emergency scaling
func (m *Manager) calculateEmergencyTarget(workerMetrics *metrics.WorkerProcessMetrics, condition *EmergencyCondition, maxWorkers int) int {
	currentWorkers := workerMetrics.TotalWorkers
	scaleUp := max(condition.MinIncrease, int(float64(currentWorkers)*condition.ScaleFactor))
	return min(currentWorkers+scaleUp, maxWorkers)
}

// executeEmergencyScaling performs the actual emergency scaling operation
func (m *Manager) executeEmergencyScaling(
	poolName string,
	poolScaler *PoolScaler,
	workerMetrics *metrics.WorkerProcessMetrics,
	targetWorkers int,
	condition *EmergencyCondition,
	utilization float64,
) {
	currentWorkers := workerMetrics.TotalWorkers
	reason := fmt.Sprintf("%s_queue=%d_util=%.1f%%", condition.Reason, workerMetrics.QueueDepth, utilization)

	m.logger.Warn("Emergency scaling triggered",
		zap.String("pool", poolName),
		zap.String("type", condition.Type),
		zap.Int("current_workers", currentWorkers),
		zap.Int("target_workers", targetWorkers),
		zap.Int("queue_depth", workerMetrics.QueueDepth),
		zap.Float64("utilization_percent", utilization),
		zap.String("reason", reason),
	)

	// Execute emergency scaling with improved error handling
	if m.phpfpmManager == nil {
		m.logger.Error("No PHP-FPM manager available for emergency scaling",
			zap.String("pool", poolName))
		return
	}

	// Attempt scaling
	if err := m.phpfpmManager.ScalePool(poolName, targetWorkers); err != nil {
		m.logger.Error("Emergency scaling failed",
			zap.String("pool", poolName),
			zap.String("type", condition.Type),
			zap.Int("target_workers", targetWorkers),
			zap.Error(err),
		)
		return
	}

	// Update pool scaler state on successful scaling
	poolScaler.lastScaleTime = time.Now()
	poolScaler.targetWorkers = targetWorkers
	poolScaler.lastDecision = reason

	// Attempt immediate hot reload for emergency scaling
	if err := m.phpfpmManager.ReloadPool(poolName); err != nil {
		m.logger.Error("Failed to reload pool after emergency scaling",
			zap.String("pool", poolName),
			zap.Error(err),
		)
		// Note: We don't return here as scaling succeeded, only reload failed
	}

	m.logger.Info("Emergency scaling executed successfully",
		zap.String("pool", poolName),
		zap.String("type", condition.Type),
		zap.Int("scaled_from", currentWorkers),
		zap.Int("scaled_to", targetWorkers),
		zap.String("reason", reason),
	)
}

// SetScalingInterval updates the scaling decision interval
func (m *Manager) SetScalingInterval(interval time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.scalingInterval = interval
	m.logger.Info("Updated scaling interval", zap.Duration("new_interval", interval))
}
