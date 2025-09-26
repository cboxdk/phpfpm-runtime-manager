package autoscaler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/config"
	"go.uber.org/zap"
)

// Pool configuration ratio constants
const (
	// Worker calculation ratios
	StartServersRatio    = 0.25  // 25% of max workers for start_servers
	MinSpareServersRatio = 0.125 // 12.5% of max workers for min_spare_servers
	MaxSpareServersRatio = 0.5   // 50% of max workers for max_spare_servers

	// Minimum values
	MinStartServers = 1
	MinSpareServers = 1
	MaxSpareServers = 2
)

// SupervisorPHPFPMManager implements PHPFPMManager interface using the supervisor
type SupervisorPHPFPMManager struct {
	supervisor PHPFPMSupervisor
	logger     *zap.Logger
}

// PHPFPMSupervisor defines the interface we need from the supervisor
type PHPFPMSupervisor interface {
	ScalePool(ctx context.Context, pool string, workers int) error
	ReloadPool(ctx context.Context, pool string) error
	GetPoolStatus(pool string) (*PoolStatusInfo, error)
	GetPoolScale(pool string) (int, error)
}

// PoolStatusInfo represents pool status information
type PoolStatusInfo struct {
	Name           string
	Status         string
	Health         string
	CurrentWorkers int
	ActiveWorkers  int
	IdleWorkers    int
	QueueDepth     int
	LastScaleTime  time.Time
	ConfigPath     string
}

// NewSupervisorPHPFPMManager creates a new supervisor-based PHP-FPM manager.
// Returns an error if required parameters are nil instead of panicking.
func NewSupervisorPHPFPMManager(supervisor PHPFPMSupervisor, logger *zap.Logger) (*SupervisorPHPFPMManager, error) {
	if supervisor == nil {
		return nil, fmt.Errorf("supervisor cannot be nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}

	return &SupervisorPHPFPMManager{
		supervisor: supervisor,
		logger:     logger,
	}, nil
}

// ScalePool implements the PHPFPMManager interface for scaling pools
func (s *SupervisorPHPFPMManager) ScalePool(poolName string, targetWorkers int) error {
	// Input validation
	if poolName == "" {
		return fmt.Errorf("pool name cannot be empty")
	}
	if targetWorkers <= 0 {
		return fmt.Errorf("target workers must be positive, got: %d", targetWorkers)
	}

	ctx := context.Background()

	s.logger.Info("Scaling PHP-FPM pool via supervisor",
		zap.String("pool", poolName),
		zap.Int("target_workers", targetWorkers))

	// Update pool configuration with new worker count before scaling
	if err := s.updatePoolConfigFile(poolName, targetWorkers); err != nil {
		s.logger.Error("Failed to update pool configuration file",
			zap.String("pool", poolName),
			zap.Error(err))
		// Continue with scaling - supervisor might handle it internally
	}

	// Use supervisor's ScalePool method
	if err := s.supervisor.ScalePool(ctx, poolName, targetWorkers); err != nil {
		return fmt.Errorf("supervisor scaling failed for pool %s: %w", poolName, err)
	}

	s.logger.Info("Successfully scaled pool via supervisor",
		zap.String("pool", poolName),
		zap.Int("target_workers", targetWorkers))

	return nil
}

// ReloadPool implements the PHPFPMManager interface for reloading pools
func (s *SupervisorPHPFPMManager) ReloadPool(poolName string) error {
	// Input validation
	if poolName == "" {
		return fmt.Errorf("pool name cannot be empty")
	}

	ctx := context.Background()

	s.logger.Info("Reloading PHP-FPM pool configuration via supervisor",
		zap.String("pool", poolName))

	if err := s.supervisor.ReloadPool(ctx, poolName); err != nil {
		return fmt.Errorf("supervisor reload failed for pool %s: %w", poolName, err)
	}

	s.logger.Info("Successfully reloaded pool configuration",
		zap.String("pool", poolName))

	return nil
}

// updatePoolConfigFile updates the PHP-FPM pool configuration file with new worker settings
func (s *SupervisorPHPFPMManager) updatePoolConfigFile(poolName string, targetWorkers int) error {
	// This is a simplified implementation - in production you might want more sophisticated config management
	configPath := s.getPoolConfigPath(poolName)
	if configPath == "" {
		s.logger.Debug("No specific config path found, relying on supervisor's internal config generation",
			zap.String("pool", poolName))
		return nil // Let supervisor handle it
	}

	// Calculate optimal worker ratios using constants
	startServers := max(MinStartServers, int(float64(targetWorkers)*StartServersRatio))
	minSpareServers := max(MinSpareServers, int(float64(targetWorkers)*MinSpareServersRatio))
	maxSpareServers := max(MaxSpareServers, int(float64(targetWorkers)*MaxSpareServersRatio))

	// Read existing config
	content, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	// Update worker settings
	configStr := string(content)
	configStr = s.updateConfigValue(configStr, "pm.max_children", fmt.Sprintf("%d", targetWorkers))
	configStr = s.updateConfigValue(configStr, "pm.start_servers", fmt.Sprintf("%d", startServers))
	configStr = s.updateConfigValue(configStr, "pm.min_spare_servers", fmt.Sprintf("%d", minSpareServers))
	configStr = s.updateConfigValue(configStr, "pm.max_spare_servers", fmt.Sprintf("%d", maxSpareServers))

	// Create secure backup before updating
	backupPath := configPath + ".backup." + time.Now().Format("20060102-150405")
	if err := s.atomicWriteFile(backupPath, content, 0644); err != nil {
		s.logger.Warn("Failed to create config backup",
			zap.String("backup_path", backupPath),
			zap.Error(err))
	}

	// Write updated config atomically
	if err := s.atomicWriteFile(configPath, []byte(configStr), 0644); err != nil {
		return fmt.Errorf("failed to write updated config atomically: %w", err)
	}

	s.logger.Debug("Updated pool configuration file",
		zap.String("config_path", configPath),
		zap.String("pool", poolName),
		zap.Int("max_children", targetWorkers),
		zap.Int("start_servers", startServers),
		zap.Int("min_spare_servers", minSpareServers),
		zap.Int("max_spare_servers", maxSpareServers))

	// Clean up old backup files (keep last 7 days)
	go s.cleanupOldBackups(configPath, 7)

	return nil
}

// updateConfigValue updates a specific configuration value in PHP-FPM config
func (s *SupervisorPHPFPMManager) updateConfigValue(config, key, value string) string {
	lines := strings.Split(config, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, key+" ") || strings.HasPrefix(trimmed, key+"=") {
			// Found existing setting, update it
			lines[i] = fmt.Sprintf("%s = %s", key, value)
			return strings.Join(lines, "\n")
		}
	}

	// Setting not found, add it after the pool section header
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			// Found pool section, add setting after it
			newLines := make([]string, len(lines)+1)
			copy(newLines, lines[:i+1])
			newLines[i+1] = fmt.Sprintf("%s = %s", key, value)
			copy(newLines[i+2:], lines[i+1:])
			return strings.Join(newLines, "\n")
		}
	}

	// Fallback: append at end
	return config + fmt.Sprintf("\n%s = %s", key, value)
}

// getPoolConfigPath attempts to determine the config path for a pool
func (s *SupervisorPHPFPMManager) getPoolConfigPath(poolName string) string {
	// Common PHP-FPM pool config locations
	commonPaths := []string{
		fmt.Sprintf("/opt/phpfpm-runtime-manager/pools.d/%s.conf", poolName),
		fmt.Sprintf("/usr/local/etc/php-fpm.d/%s.conf", poolName),
		fmt.Sprintf("/opt/homebrew/etc/php-fpm.d/%s.conf", poolName),
		fmt.Sprintf("/etc/php/*/fpm/pool.d/%s.conf", poolName),
	}

	for _, path := range commonPaths {
		// Handle glob patterns for PHP version paths
		if strings.Contains(path, "*") {
			matches, err := filepath.Glob(path)
			if err == nil && len(matches) > 0 {
				if _, err := os.Stat(matches[0]); err == nil {
					return matches[0]
				}
			}
		} else {
			if _, err := os.Stat(path); err == nil {
				return path
			}
		}
	}

	return "" // No config file found, let supervisor handle it
}

// ConfigurePools configures multiple pools with optimal settings
func (s *SupervisorPHPFPMManager) ConfigurePools(poolConfigs []config.PoolConfig) error {
	s.logger.Info("Configuring pools for autoscaling",
		zap.Int("pool_count", len(poolConfigs)))

	for _, poolConfig := range poolConfigs {
		// Set up each pool with autoscaling-friendly defaults
		if err := s.configurePool(poolConfig); err != nil {
			s.logger.Error("Failed to configure pool",
				zap.String("pool", poolConfig.Name),
				zap.Error(err))
			continue
		}
	}

	return nil
}

// configurePool configures a single pool for optimal autoscaling
func (s *SupervisorPHPFPMManager) configurePool(poolConfig config.PoolConfig) error {
	// Start with a conservative baseline
	initialWorkers := max(2, poolConfig.MaxWorkers/4)

	s.logger.Debug("Configuring pool for autoscaling",
		zap.String("pool", poolConfig.Name),
		zap.Int("initial_workers", initialWorkers),
		zap.Int("max_workers", poolConfig.MaxWorkers))

	// Update the pool configuration file if it exists
	if err := s.updatePoolConfigFile(poolConfig.Name, initialWorkers); err != nil {
		s.logger.Warn("Could not update pool config file, relying on supervisor",
			zap.String("pool", poolConfig.Name),
			zap.Error(err))
	}

	return nil
}

// GetPoolMetrics retrieves current pool metrics for autoscaling decisions
func (s *SupervisorPHPFPMManager) GetPoolMetrics(poolName string) (*PoolMetrics, error) {
	// Try to get status from supervisor
	status, err := s.supervisor.GetPoolStatus(poolName)
	if err != nil {
		return nil, fmt.Errorf("failed to get pool status: %w", err)
	}

	currentScale, err := s.supervisor.GetPoolScale(poolName)
	if err != nil {
		s.logger.Warn("Could not get current scale, using status info",
			zap.String("pool", poolName),
			zap.Error(err))
		currentScale = status.CurrentWorkers
	}

	return &PoolMetrics{
		PoolName:      poolName,
		TotalWorkers:  currentScale,
		ActiveWorkers: status.ActiveWorkers,
		IdleWorkers:   status.IdleWorkers,
		QueueDepth:    status.QueueDepth,
		LastScaleTime: status.LastScaleTime,
		Health:        status.Health,
		Status:        status.Status,
	}, nil
}

// PoolMetrics represents current pool metrics
type PoolMetrics struct {
	PoolName      string
	TotalWorkers  int
	ActiveWorkers int
	IdleWorkers   int
	QueueDepth    int
	LastScaleTime time.Time
	Health        string
	Status        string
}

// atomicWriteFile writes data to a file atomically by writing to a temporary file first,
// then renaming it to the target location. This prevents partial writes and race conditions.
func (s *SupervisorPHPFPMManager) atomicWriteFile(filename string, data []byte, perm os.FileMode) error {
	// Create temporary file in the same directory as the target
	dir := filepath.Dir(filename)
	tmpFile, err := os.CreateTemp(dir, ".tmp-phpfpm-config-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Ensure cleanup of temporary file on any error
	defer func() {
		tmpFile.Close()
		if err != nil {
			os.Remove(tmpPath)
		}
	}()

	// Write data to temporary file
	if _, err = tmpFile.Write(data); err != nil {
		return fmt.Errorf("failed to write to temporary file: %w", err)
	}

	// Sync to ensure data is written to disk
	if err = tmpFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync temporary file: %w", err)
	}

	// Close temporary file before rename
	if err = tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temporary file: %w", err)
	}

	// Set proper permissions
	if err = os.Chmod(tmpPath, perm); err != nil {
		return fmt.Errorf("failed to set permissions on temporary file: %w", err)
	}

	// Atomic rename to final location
	if err = os.Rename(tmpPath, filename); err != nil {
		return fmt.Errorf("failed to rename temporary file to target: %w", err)
	}

	return nil
}

// cleanupOldBackups removes backup files older than the specified retention period
func (s *SupervisorPHPFPMManager) cleanupOldBackups(configPath string, retentionDays int) {
	if retentionDays <= 0 {
		return // No cleanup if retention is disabled
	}

	dir := filepath.Dir(configPath)
	baseName := filepath.Base(configPath)
	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	// Find backup files for this config
	pattern := baseName + ".backup.*"
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		s.logger.Debug("Failed to glob backup files", zap.Error(err))
		return
	}

	for _, backupPath := range matches {
		info, err := os.Stat(backupPath)
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			if err := os.Remove(backupPath); err != nil {
				s.logger.Debug("Failed to remove old backup",
					zap.String("backup_path", backupPath),
					zap.Error(err))
			} else {
				s.logger.Debug("Removed old backup file",
					zap.String("backup_path", backupPath))
			}
		}
	}
}
