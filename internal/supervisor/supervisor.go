package supervisor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/api"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/config"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/telemetry"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/types"
	"go.uber.org/zap"
)

// validatePHPFPMBinary validates that PHP-FPM exists and is executable
func validatePHPFPMBinary() (string, error) {
	phpfpmPath, err := findPHPFPMBinary()
	if err != nil {
		return "", fmt.Errorf("PHP-FPM binary validation failed: %w", err)
	}

	// Check if the binary is executable
	fileInfo, err := os.Stat(phpfpmPath)
	if err != nil {
		return "", fmt.Errorf("cannot access PHP-FPM binary at %s: %w", phpfpmPath, err)
	}

	// Check executable permissions
	if fileInfo.Mode()&0111 == 0 {
		return "", fmt.Errorf("PHP-FPM binary at %s is not executable", phpfpmPath)
	}

	// Try to get version to validate it's actually PHP-FPM
	cmd := exec.Command(phpfpmPath, "-v")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("PHP-FPM binary validation failed (cannot get version): %w", err)
	}

	if !strings.Contains(string(output), "PHP") && !strings.Contains(string(output), "FPM") {
		return "", fmt.Errorf("binary at %s does not appear to be PHP-FPM", phpfpmPath)
	}

	return phpfpmPath, nil
}

// validateCustomPHPFPMBinary validates a user-specified PHP-FPM binary path
func validateCustomPHPFPMBinary(binaryPath string) (string, error) {
	var phpfpmPath string

	// Check if it's an absolute path
	if filepath.IsAbs(binaryPath) {
		phpfpmPath = binaryPath
	} else {
		// It's a relative path or just a binary name, search in PATH
		var err error
		phpfpmPath, err = exec.LookPath(binaryPath)
		if err != nil {
			return "", fmt.Errorf("PHP-FPM binary '%s' not found in PATH: %w", binaryPath, err)
		}
	}

	// Check if the binary exists and is executable
	fileInfo, err := os.Stat(phpfpmPath)
	if err != nil {
		return "", fmt.Errorf("cannot access PHP-FPM binary at %s: %w", phpfpmPath, err)
	}

	// Check executable permissions
	if fileInfo.Mode()&0111 == 0 {
		return "", fmt.Errorf("PHP-FPM binary at %s is not executable", phpfpmPath)
	}

	// Try to get version to validate it's actually PHP-FPM
	cmd := exec.Command(phpfpmPath, "-v")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("PHP-FPM binary validation failed (cannot get version): %w", err)
	}

	if !strings.Contains(string(output), "PHP") && !strings.Contains(string(output), "FPM") {
		return "", fmt.Errorf("binary at %s does not appear to be PHP-FPM", phpfpmPath)
	}

	return phpfpmPath, nil
}

// findPHPFPMBinary finds the PHP-FPM executable across different platforms
func findPHPFPMBinary() (string, error) {
	// Common PHP-FPM binary names to try
	candidates := []string{"php-fpm"}

	// Add platform-specific paths
	switch runtime.GOOS {
	case "darwin":
		// macOS common paths (Homebrew, MacPorts, etc.)
		candidates = append(candidates,
			"/opt/homebrew/sbin/php-fpm",
			"/opt/homebrew/bin/php-fpm",
			"/usr/local/sbin/php-fpm",
			"/usr/local/bin/php-fpm",
			"/opt/local/sbin/php-fpm",
		)
	case "linux":
		// Linux common paths
		candidates = append(candidates,
			"/usr/sbin/php-fpm",
			"/usr/bin/php-fpm",
			"/usr/local/sbin/php-fpm",
			"/usr/local/bin/php-fpm",
		)
	}

	// First try finding in PATH
	if path, err := exec.LookPath("php-fpm"); err == nil {
		return path, nil
	}

	// Try each candidate path
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("php-fpm executable not found in PATH or common locations")
}

// validateConfigPath validates and sanitizes a configuration file path
func validateConfigPath(path string) error {
	// Empty path is valid (means inline config)
	if path == "" {
		return nil
	}

	// Ensure path is absolute
	if !filepath.IsAbs(path) {
		return fmt.Errorf("config path must be absolute: %s", path)
	}

	// Check for directory traversal attacks
	if strings.Contains(path, "..") {
		return fmt.Errorf("config path contains invalid characters: %s", path)
	}

	// Ensure the path doesn't contain null bytes
	if strings.Contains(path, "\x00") {
		return fmt.Errorf("config path contains null bytes: %s", path)
	}

	// Clean the path to remove any redundant elements
	cleanPath := filepath.Clean(path)
	if cleanPath != path {
		return fmt.Errorf("config path contains redundant elements: %s", path)
	}

	// Ensure parent directory exists - create if necessary
	dir := filepath.Dir(path)
	if dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create config directory %s: %w", dir, err)
		}
	}

	// Check if file exists - if not, that's OK (will be created later)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist - that's fine, supervisor will create it
			return nil
		}
		return fmt.Errorf("config path validation failed: %w", err)
	}

	// Ensure it's a regular file
	if !info.Mode().IsRegular() {
		return fmt.Errorf("config path is not a regular file: %s", path)
	}

	// Check file extension for PHP-FPM config
	ext := filepath.Ext(path)
	if ext != ".conf" && ext != ".ini" {
		return fmt.Errorf("config file must have .conf or .ini extension: %s", path)
	}

	return nil
}

// MasterProcess represents the single PHP-FPM master process
type MasterProcess struct {
	Cmd       *exec.Cmd
	PID       int
	StartTime time.Time
	Status    PoolStatus
	mu        sync.RWMutex
}

// Supervisor manages PHP-FPM processes
type Supervisor struct {
	pools  []config.PoolConfig
	logger *zap.Logger

	// Master process management
	masterProcess *MasterProcess
	processes     map[string]*PoolProcess // Keep for compatibility but not used for separate processes
	mu            sync.RWMutex

	// Event handling
	events chan types.ProcessEvent

	// Health monitoring
	healthTicker *time.Ticker

	// Telemetry
	eventEmitter *telemetry.EventEmitter

	// PHP-FPM binary path (validated at startup)
	phpfpmBinary     string
	globalConfigPath string
	running          bool
}

// PoolProcess represents a managed PHP-FPM pool process
type PoolProcess struct {
	Config      config.PoolConfig
	Cmd         *exec.Cmd
	PID         int
	StartTime   time.Time
	Restarts    int
	Status      PoolStatus
	HealthState types.HealthState
	LastHealth  time.Time

	// Process monitoring
	mu sync.RWMutex
}

// PoolStatus represents the status of a pool
type PoolStatus string

const (
	PoolStatusStopped  PoolStatus = "stopped"
	PoolStatusStarting PoolStatus = "starting"
	PoolStatusRunning  PoolStatus = "running"
	PoolStatusStopping PoolStatus = "stopping"
	PoolStatusFailed   PoolStatus = "failed"
)

// NewSupervisor creates a new process supervisor for managing PHP-FPM pools.
//
// The supervisor is responsible for:
//   - Starting and stopping PHP-FPM processes for configured pools
//   - Monitoring process health and status
//   - Handling process lifecycle events and notifications
//   - Coordinating with the telemetry system for operational observability
//
// Parameters:
//   - pools: Configuration for all PHP-FPM pools to be managed
//   - logger: Structured logger for operational logging
//   - eventEmitter: Telemetry event emitter for lifecycle and scaling events
//
// Returns:
//   - *Supervisor: Configured supervisor instance ready for operation
//   - error: Configuration validation error if pools are invalid
//
// The supervisor validates all pool configurations during initialization to fail fast
// on invalid configurations rather than at runtime.
func NewSupervisor(pools []config.PoolConfig, logger *zap.Logger, eventEmitter *telemetry.EventEmitter) (*Supervisor, error) {
	return NewSupervisorWithBinary(pools, "", logger, eventEmitter)
}

// NewSupervisorWithBinary creates a new supervisor with a custom PHP-FPM binary
func NewSupervisorWithBinary(pools []config.PoolConfig, phpfpmBinary string, logger *zap.Logger, eventEmitter *telemetry.EventEmitter) (*Supervisor, error) {
	return NewSupervisorWithConfigAndBinary(pools, phpfpmBinary, "", logger, eventEmitter)
}

// NewSupervisorWithConfigAndBinary creates a new supervisor with custom PHP-FPM binary and global config path
func NewSupervisorWithConfigAndBinary(pools []config.PoolConfig, phpfpmBinary string, globalConfigPath string, logger *zap.Logger, eventEmitter *telemetry.EventEmitter) (*Supervisor, error) {
	if len(pools) == 0 {
		return nil, fmt.Errorf("at least one pool must be configured")
	}

	// Validate PHP-FPM binary at startup
	var phpfpmPath string
	var err error
	if phpfpmBinary != "" {
		phpfpmPath, err = validateCustomPHPFPMBinary(phpfpmBinary)
	} else {
		phpfpmPath, err = validatePHPFPMBinary()
	}
	if err != nil {
		return nil, fmt.Errorf("PHP-FPM validation failed: %w. Please ensure PHP-FPM is installed and accessible", err)
	}
	logger.Info("PHP-FPM binary validated", zap.String("path", phpfpmPath))

	// Validate all pool configurations during initialization
	for _, pool := range pools {
		if err := validateConfigPath(pool.ConfigPath); err != nil {
			return nil, fmt.Errorf("invalid config path for pool %s: %w", pool.Name, err)
		}
	}

	s := &Supervisor{
		pools:            pools,
		logger:           logger,
		processes:        make(map[string]*PoolProcess),
		events:           make(chan types.ProcessEvent, 100),
		eventEmitter:     eventEmitter,
		phpfpmBinary:     phpfpmPath,
		globalConfigPath: globalConfigPath,
	}

	// Initialize pool processes
	for _, pool := range pools {
		s.processes[pool.Name] = &PoolProcess{
			Config:      pool,
			Status:      PoolStatusStopped,
			HealthState: types.HealthStateUnknown,
		}
	}

	return s, nil
}

// Start initializes and starts all configured PHP-FPM pools
func (s *Supervisor) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("supervisor is already running")
	}
	s.running = true
	s.mu.Unlock()

	s.logger.Info("Starting PHP-FPM supervisor", zap.Int("pools", len(s.pools)))

	// Generate PHP-FPM configuration files for ALL pools
	if err := s.generatePHPFPMConfig(""); err != nil {
		s.logger.Error("Config generation failed", zap.Error(err))
		return fmt.Errorf("config generation failed: %w", err)
	}

	// Start ONE PHP-FPM master process with all pools
	if err := s.startMasterProcess(ctx); err != nil {
		s.logger.Error("Failed to start PHP-FPM master", zap.Error(err))
		return fmt.Errorf("failed to start PHP-FPM master: %w", err)
	}

	// Start health monitoring
	s.healthTicker = time.NewTicker(5 * time.Second)
	go s.healthMonitor(ctx)

	// Process monitoring loop
	err := s.monitorLoop(ctx)

	// When context is done (for ANY reason), ensure proper shutdown
	if ctx.Err() != nil {
		s.logger.Info("Context completed, performing graceful shutdown", zap.Error(ctx.Err()))
		// Create a fresh context for shutdown operations since ctx is done
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		if stopErr := s.Stop(shutdownCtx); stopErr != nil {
			s.logger.Error("Failed to stop supervisor during context completion", zap.Error(stopErr))
		}
	}

	return err
}

// Stop gracefully stops all PHP-FPM processes
func (s *Supervisor) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	s.mu.Unlock()

	s.logger.Info("Stopping PHP-FPM supervisor")

	// Stop health monitoring
	if s.healthTicker != nil {
		s.healthTicker.Stop()
	}

	// Stop the master process which will stop all pools
	if err := s.stopMasterProcess(ctx); err != nil {
		s.logger.Error("Failed to stop PHP-FPM master", zap.Error(err))
		return fmt.Errorf("failed to stop PHP-FPM master: %w", err)
	}

	s.logger.Info("PHP-FPM supervisor stopped successfully")

	return nil
}

// Reload performs a zero-downtime reload of configuration
func (s *Supervisor) Reload(ctx context.Context) error {
	s.logger.Info("Performing zero-downtime reload")

	s.mu.RLock()
	processes := make(map[string]*PoolProcess)
	for k, v := range s.processes {
		processes[k] = v
	}
	s.mu.RUnlock()

	// Reload each pool
	for poolName, process := range processes {
		if err := s.reloadPool(ctx, poolName); err != nil {
			s.logger.Error("Failed to reload pool",
				zap.String("pool", poolName),
				zap.Error(err))
			continue
		}

		s.emitEvent(types.ProcessEventReloaded, poolName, process.PID, "Pool reloaded successfully")
	}

	s.logger.Info("Zero-downtime reload completed")
	return nil
}

// Restart performs a zero-downtime restart of processes
func (s *Supervisor) Restart(ctx context.Context) error {
	s.logger.Info("Performing zero-downtime restart")

	s.mu.RLock()
	processes := make(map[string]*PoolProcess)
	for k, v := range s.processes {
		processes[k] = v
	}
	s.mu.RUnlock()

	// Restart each pool
	for poolName := range processes {
		if err := s.restartPool(ctx, poolName); err != nil {
			s.logger.Error("Failed to restart pool",
				zap.String("pool", poolName),
				zap.Error(err))
			continue
		}
	}

	s.logger.Info("Zero-downtime restart completed")
	return nil
}

// Health returns the current health status of all pools
func (s *Supervisor) Health() types.HealthStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := types.HealthStatus{
		Pools:   make(map[string]types.HealthState),
		Updated: time.Now(),
	}

	overallHealthy := true

	for name, process := range s.processes {
		process.mu.RLock()
		healthState := process.HealthState
		process.mu.RUnlock()

		status.Pools[name] = healthState

		if healthState != types.HealthStateHealthy {
			overallHealthy = false
		}
	}

	if overallHealthy {
		status.Overall = types.HealthStateHealthy
	} else {
		status.Overall = types.HealthStateUnhealthy
	}

	return status
}

// Subscribe returns a channel for process events
func (s *Supervisor) Subscribe() <-chan types.ProcessEvent {
	return s.events
}

// startPool starts a specific pool
func (s *Supervisor) startPool(ctx context.Context, poolName string) error {
	s.mu.RLock()
	process, exists := s.processes[poolName]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("pool %s not found", poolName)
	}

	process.mu.Lock()
	defer process.mu.Unlock()

	if process.Status == PoolStatusRunning {
		return fmt.Errorf("pool %s is already running", poolName)
	}

	s.logger.Info("Starting pool", zap.String("pool", poolName))

	process.Status = PoolStatusStarting
	process.HealthState = types.HealthStateStarting

	// Generate PHP-FPM configuration files
	if err := s.generatePHPFPMConfig(poolName); err != nil {
		process.Status = PoolStatusFailed
		process.HealthState = types.HealthStateUnhealthy
		s.emitEvent(types.ProcessEventFailed, poolName, 0, fmt.Sprintf("Config generation failed: %v", err))
		return fmt.Errorf("config generation failed: %w", err)
	}

	// Create command with generated global config
	// Use the pre-validated PHP-FPM binary path
	s.logger.Debug("Starting PHP-FPM process", zap.String("binary", s.phpfpmBinary), zap.String("pool", poolName))

	// Use the global config path - PHP-FPM will include pool configs from there
	cmd := exec.CommandContext(ctx, s.phpfpmBinary, "--fpm-config", s.globalConfigPath, "--nodaemonize")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Create new process group
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		process.Status = PoolStatusFailed
		process.HealthState = types.HealthStateUnhealthy
		s.emitEvent(types.ProcessEventFailed, poolName, 0, fmt.Sprintf("Failed to start: %v", err))
		return fmt.Errorf("failed to start PHP-FPM: %w", err)
	}

	process.Cmd = cmd
	process.PID = cmd.Process.Pid
	process.StartTime = time.Now()
	process.Status = PoolStatusRunning
	process.HealthState = types.HealthStateHealthy

	s.emitEvent(types.ProcessEventStarted, poolName, process.PID, "Pool started successfully")

	s.logger.Info("Pool started successfully",
		zap.String("pool", poolName),
		zap.Int("pid", process.PID))

	return nil
}

// stopPool stops a specific pool
func (s *Supervisor) stopPool(ctx context.Context, poolName string) error {
	s.mu.RLock()
	process, exists := s.processes[poolName]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("pool %s not found", poolName)
	}

	process.mu.Lock()
	defer process.mu.Unlock()

	if process.Status == PoolStatusStopped {
		return nil
	}

	s.logger.Info("Stopping pool", zap.String("pool", poolName))

	process.Status = PoolStatusStopping
	process.HealthState = types.HealthStateStopping

	if process.Cmd == nil || process.Cmd.Process == nil {
		process.Status = PoolStatusStopped
		process.HealthState = types.HealthStateUnknown
		return nil
	}

	// Send SIGTERM for graceful shutdown
	if err := process.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
		s.logger.Error("Failed to send SIGTERM",
			zap.String("pool", poolName),
			zap.Error(err))
	}

	// Wait for graceful shutdown with timeout
	done := make(chan error, 1)
	go func() {
		done <- process.Cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil && err.Error() != "signal: terminated" {
			s.logger.Error("Process exited with error",
				zap.String("pool", poolName),
				zap.Error(err))
		}
	case <-time.After(10 * time.Second):
		// Force kill after timeout
		s.logger.Warn("Timeout waiting for graceful shutdown, force killing",
			zap.String("pool", poolName))
		masterPID := process.Cmd.Process.Pid
		if err := process.Cmd.Process.Kill(); err != nil {
			s.logger.Error("Failed to kill process",
				zap.String("pool", poolName),
				zap.Error(err))
		}
		// Clean up any orphaned workers
		s.killOrphanedWorkers(poolName, masterPID)
	case <-ctx.Done():
		// Context canceled, force kill
		masterPID := process.Cmd.Process.Pid
		if err := process.Cmd.Process.Kill(); err != nil {
			s.logger.Error("Failed to kill process",
				zap.String("pool", poolName),
				zap.Error(err))
		}
		// Clean up any orphaned workers
		s.killOrphanedWorkers(poolName, masterPID)
	}

	process.Status = PoolStatusStopped
	process.HealthState = types.HealthStateUnknown
	process.Cmd = nil
	process.PID = 0

	s.emitEvent(types.ProcessEventStopped, poolName, 0, "Pool stopped")

	s.logger.Info("Pool stopped", zap.String("pool", poolName))

	return nil
}

// reloadPool reloads a specific pool configuration
func (s *Supervisor) reloadPool(ctx context.Context, poolName string) error {
	s.mu.RLock()
	process, exists := s.processes[poolName]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("pool %s not found", poolName)
	}

	process.mu.RLock()
	if process.Status != PoolStatusRunning || process.Cmd == nil {
		process.mu.RUnlock()
		return fmt.Errorf("pool %s is not running", poolName)
	}

	cmd := process.Cmd
	process.mu.RUnlock()

	s.logger.Info("Reloading pool configuration", zap.String("pool", poolName))

	// Send SIGUSR2 for graceful reload
	if err := cmd.Process.Signal(syscall.SIGUSR2); err != nil {
		return fmt.Errorf("failed to send SIGUSR2: %w", err)
	}

	// Wait a moment for the reload to take effect
	time.Sleep(100 * time.Millisecond)

	return nil
}

// restartPool performs a zero-downtime restart of a specific pool
func (s *Supervisor) restartPool(ctx context.Context, poolName string) error {
	if err := s.stopPool(ctx, poolName); err != nil {
		return fmt.Errorf("failed to stop pool: %w", err)
	}

	// Wait a moment before restarting
	time.Sleep(500 * time.Millisecond)

	if err := s.startPool(ctx, poolName); err != nil {
		return fmt.Errorf("failed to start pool: %w", err)
	}

	s.mu.RLock()
	process := s.processes[poolName]
	s.mu.RUnlock()

	s.emitEvent(types.ProcessEventRestarted, poolName, process.PID, "Pool restarted successfully")

	return nil
}

// forceStopAll forcefully stops all processes
func (s *Supervisor) forceStopAll() {
	s.mu.RLock()
	processes := make(map[string]*PoolProcess)
	for k, v := range s.processes {
		processes[k] = v
	}
	s.mu.RUnlock()

	for poolName, process := range processes {
		process.mu.Lock()
		if process.Cmd != nil && process.Cmd.Process != nil {
			// Kill main PHP-FPM master process
			masterPID := process.Cmd.Process.Pid
			if err := process.Cmd.Process.Kill(); err != nil {
				s.logger.Error("Failed to kill master process",
					zap.String("pool", poolName),
					zap.Int("master_pid", masterPID),
					zap.Error(err))
			}

			// Additionally, kill any remaining PHP-FPM worker processes
			s.killOrphanedWorkers(poolName, masterPID)

			process.Status = PoolStatusStopped
			process.HealthState = types.HealthStateUnknown
			process.Cmd = nil
			process.PID = 0
		}
		process.mu.Unlock()
	}
}

// killOrphanedWorkers kills any orphaned PHP-FPM worker processes
func (s *Supervisor) killOrphanedWorkers(poolName string, masterPID int) {
	// Use platform-specific approach to find and kill orphaned workers
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		s.killOrphanedWorkersUnix(poolName, masterPID)
	}
}

// killOrphanedWorkersUnix kills orphaned workers on Unix systems
func (s *Supervisor) killOrphanedWorkersUnix(poolName string, masterPID int) {
	binary := filepath.Base(s.phpfpmBinary)

	// Strategy 1: Try to find processes using config file first
	configPath := s.globalConfigPath
	var pids []string

	// Use pgrep to find PHP-FPM processes using our config file
	cmd := exec.Command("pgrep", "-f", binary+".*"+configPath)
	output, err := cmd.Output()
	if err == nil {
		pids = strings.Split(strings.TrimSpace(string(output)), "\n")
	}

	// Strategy 2: If no processes found by config, find all processes by binary name
	if len(pids) == 0 || (len(pids) == 1 && pids[0] == "") {
		s.logger.Debug("No processes found by config path, trying binary name",
			zap.String("binary", binary))
		cmd = exec.Command("pgrep", binary)
		output, err = cmd.Output()
		if err != nil {
			s.logger.Debug("No orphaned PHP-FPM processes found",
				zap.String("binary", binary))
			return
		}
		pids = strings.Split(strings.TrimSpace(string(output)), "\n")
	}

	// Strategy 3: Also look for processes by exact binary path
	if s.phpfpmBinary != binary {
		cmd = exec.Command("pgrep", "-f", s.phpfpmBinary)
		pathOutput, pathErr := cmd.Output()
		if pathErr == nil {
			pathPids := strings.Split(strings.TrimSpace(string(pathOutput)), "\n")
			// Merge with existing PIDs
			for _, pathPid := range pathPids {
				if pathPid != "" {
					found := false
					for _, existingPid := range pids {
						if existingPid == pathPid {
							found = true
							break
						}
					}
					if !found {
						pids = append(pids, pathPid)
					}
				}
			}
		}
	}

	killedCount := 0

	for _, pidStr := range pids {
		if pidStr == "" {
			continue
		}

		pid := 0
		if _, err := fmt.Sscanf(pidStr, "%d", &pid); err != nil {
			continue
		}

		// Skip the master process PID if we know it
		if masterPID > 0 && pid == masterPID {
			continue
		}

		// Kill the process with SIGTERM first, then SIGKILL if needed
		if proc, err := os.FindProcess(pid); err == nil {
			processKilled := false

			// Try graceful shutdown first
			if killErr := proc.Signal(syscall.SIGTERM); killErr != nil {
				s.logger.Debug("Failed to send SIGTERM to process, trying SIGKILL",
					zap.Int("pid", pid),
					zap.Error(killErr))
				// Force kill if SIGTERM failed
				if killErr := proc.Signal(syscall.SIGKILL); killErr == nil {
					processKilled = true
				}
			} else {
				// Wait longer for PHP-FPM workers to shut down gracefully
				time.Sleep(2 * time.Second)

				// Check if process still exists, force kill if needed
				if killErr := proc.Signal(syscall.Signal(0)); killErr == nil {
					// Process still exists, force kill
					s.logger.Debug("Process still running after SIGTERM, sending SIGKILL",
						zap.Int("pid", pid))
					if killErr := proc.Signal(syscall.SIGKILL); killErr == nil {
						processKilled = true
					}
				} else {
					// Process exited gracefully
					processKilled = true
				}
			}

			if processKilled {
				killedCount++
				s.logger.Info("Killed orphaned PHP-FPM process",
					zap.Int("pid", pid))
			} else {
				s.logger.Warn("Failed to kill orphaned PHP-FPM process",
					zap.Int("pid", pid))
			}
		}
	}

	if killedCount > 0 {
		s.logger.Info("Cleaned up orphaned PHP-FPM processes",
			zap.Int("killed_count", killedCount),
			zap.String("binary", binary))
	} else {
		s.logger.Debug("No orphaned PHP-FPM processes found to clean up",
			zap.String("binary", binary))
	}
}

// cleanupSocketFiles removes socket files to prevent startup conflicts
func (s *Supervisor) cleanupSocketFiles() {
	// Clean up socket files in the sockets directory
	socketsDir := filepath.Join(filepath.Dir(s.globalConfigPath), "sockets")

	// Get all socket files
	matches, err := filepath.Glob(filepath.Join(socketsDir, "*.sock"))
	if err != nil {
		s.logger.Debug("Failed to list socket files", zap.Error(err))
		return
	}

	cleanedCount := 0
	for _, sockFile := range matches {
		if err := os.Remove(sockFile); err != nil {
			s.logger.Debug("Failed to remove socket file",
				zap.String("socket", sockFile),
				zap.Error(err))
		} else {
			cleanedCount++
			s.logger.Debug("Removed socket file", zap.String("socket", sockFile))
		}
	}

	if cleanedCount > 0 {
		s.logger.Info("Cleaned up socket files",
			zap.Int("count", cleanedCount),
			zap.String("directory", socketsDir))
	}
}

// healthMonitor monitors the health of all pools
func (s *Supervisor) healthMonitor(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.healthTicker.C:
			s.checkHealth(ctx)
		}
	}
}

// checkHealth checks the health of all pools
func (s *Supervisor) checkHealth(ctx context.Context) {
	s.mu.RLock()
	processes := make(map[string]*PoolProcess)
	for k, v := range s.processes {
		processes[k] = v
	}
	s.mu.RUnlock()

	for poolName, process := range processes {
		s.checkPoolHealth(ctx, poolName, process)
	}
}

// checkPoolHealth checks the health of a specific pool
func (s *Supervisor) checkPoolHealth(ctx context.Context, poolName string, process *PoolProcess) {
	process.mu.Lock()
	defer process.mu.Unlock()

	if process.Status != PoolStatusRunning {
		return
	}

	// Check if process is still alive
	if process.Cmd == nil || process.Cmd.Process == nil {
		process.HealthState = types.HealthStateUnhealthy
		return
	}

	// Send signal 0 to check if process exists
	err := process.Cmd.Process.Signal(syscall.Signal(0))
	if err != nil {
		s.logger.Error("Process appears to be dead",
			zap.String("pool", poolName),
			zap.Int("pid", process.PID),
			zap.Error(err))

		process.Status = PoolStatusFailed
		process.HealthState = types.HealthStateUnhealthy

		s.emitEvent(types.ProcessEventFailed, poolName, process.PID,
			fmt.Sprintf("Process health check failed: %v", err))

		// Attempt restart if configured
		go func() {
			time.Sleep(time.Second) // Brief delay before restart
			if err := s.restartPool(context.Background(), poolName); err != nil {
				s.logger.Error("Failed to restart failed pool",
					zap.String("pool", poolName),
					zap.Error(err))
			}
		}()

		return
	}

	process.HealthState = types.HealthStateHealthy
	process.LastHealth = time.Now()
}

// monitorLoop monitors process events and handles restarts
func (s *Supervisor) monitorLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			// Monitor for process exits
			s.mu.RLock()
			for poolName, process := range s.processes {
				go func(name string, p *PoolProcess) {
					p.mu.RLock()
					cmd := p.Cmd
					status := p.Status
					p.mu.RUnlock()

					if cmd != nil && status == PoolStatusRunning {
						if err := cmd.Wait(); err != nil {
							// Only log as error if supervisor is still running and this isn't a graceful shutdown
							// During shutdown or when receiving expected signals, process exits are normal
							s.mu.RLock()
							supervisorRunning := s.running
							s.mu.RUnlock()

							p.mu.RLock()
							currentStatus := p.Status
							p.mu.RUnlock()

							// Don't log expected shutdown/kill errors during shutdown
							isWaitError := strings.Contains(err.Error(), "wait: no child processes")
							isKillError := strings.Contains(err.Error(), "signal: killed")
							isTerminatedError := strings.Contains(err.Error(), "signal: terminated")

							if supervisorRunning && currentStatus != PoolStatusStopping && !isWaitError && !isKillError && !isTerminatedError {
								s.logger.Error("Process exited unexpectedly",
									zap.String("pool", name),
									zap.Error(err))

								p.mu.Lock()
								p.Status = PoolStatusFailed
								p.HealthState = types.HealthStateUnhealthy
								p.Restarts++
								p.mu.Unlock()

								s.emitEvent(types.ProcessEventFailed, name, p.PID,
									fmt.Sprintf("Process exited: %v", err))
							}
						}
					}
				}(poolName, process)
			}
			s.mu.RUnlock()

			time.Sleep(time.Second)
		}
	}
}

// emitEvent emits a process event
func (s *Supervisor) emitEvent(eventType types.ProcessEventType, pool string, pid int, message string) {
	event := types.ProcessEvent{
		Type:      eventType,
		Pool:      pool,
		PID:       pid,
		Message:   message,
		Timestamp: time.Now(),
	}

	select {
	case s.events <- event:
	default:
		// Channel full, log the event
		s.logger.Warn("Event channel full, dropping event",
			zap.String("type", string(eventType)),
			zap.String("pool", pool),
			zap.String("message", message))
	}
}

// ReloadPool reloads a specific pool configuration
func (s *Supervisor) ReloadPool(ctx context.Context, pool string) error {
	return s.reloadPool(ctx, pool)
}

// ScalePool scales a specific pool to the given number of workers
func (s *Supervisor) ScalePool(ctx context.Context, pool string, workers int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	process, exists := s.processes[pool]
	if !exists {
		return fmt.Errorf("pool not found: %s", pool)
	}

	if workers <= 0 {
		return fmt.Errorf("worker count must be positive: %d", workers)
	}

	// Store previous worker count for telemetry
	previousWorkers := process.Config.MaxWorkers

	// Update the pool configuration
	for i := range s.pools {
		if s.pools[i].Name == pool {
			s.pools[i].MaxWorkers = workers
			break
		}
	}

	// Update the process configuration
	process.Config.MaxWorkers = workers

	// Send USR2 signal to reload with new worker count
	if process.Cmd != nil && process.Cmd.Process != nil {
		if err := process.Cmd.Process.Signal(syscall.SIGUSR2); err != nil {
			s.logger.Error("Failed to send USR2 signal for scaling",
				zap.String("pool", pool),
				zap.Int("workers", workers),
				zap.Error(err))
			return fmt.Errorf("failed to signal process for scaling: %w", err)
		}
	}

	// Emit detailed scaling event with telemetry
	if s.eventEmitter != nil {
		action := "scale_up"
		if workers < previousWorkers {
			action = "scale_down"
		}

		scalingDetails := telemetry.ScalingEventDetails{
			Action:        action,
			PreviousCount: previousWorkers,
			NewCount:      workers,
			Reason:        "manual", // This could be enhanced to distinguish manual vs autoscaling
			Trigger:       "api",    // Could be "api", "config", etc.
		}

		if err := s.eventEmitter.EmitScalingEvent(ctx, pool, scalingDetails); err != nil {
			s.logger.Error("Failed to emit scaling event",
				zap.String("pool", pool),
				zap.Error(err))
			// Don't fail the scaling operation due to telemetry errors
		}
	}

	s.logger.Info("Pool scaled",
		zap.String("pool", pool),
		zap.Int("previous_workers", previousWorkers),
		zap.Int("new_workers", workers))

	s.emitEvent(types.ProcessEventReloaded, pool, process.PID, fmt.Sprintf("Pool scaled from %d to %d workers", previousWorkers, workers))

	return nil
}

// GetPoolScale returns the current number of workers for a pool
func (s *Supervisor) GetPoolScale(pool string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	process, exists := s.processes[pool]
	if !exists {
		return 0, fmt.Errorf("pool not found: %s", pool)
	}

	return process.Config.MaxWorkers, nil
}

// ValidatePoolConfig validates the configuration for a specific pool
func (s *Supervisor) ValidatePoolConfig(pool string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var poolConfig *config.PoolConfig
	for i := range s.pools {
		if s.pools[i].Name == pool {
			poolConfig = &s.pools[i]
			break
		}
	}

	if poolConfig == nil {
		return fmt.Errorf("pool not found: %s", pool)
	}

	// Validate the configuration file path
	if err := validateConfigPath(poolConfig.ConfigPath); err != nil {
		return fmt.Errorf("invalid config path for pool %s: %w", pool, err)
	}

	// Check if the PHP-FPM configuration is syntactically valid
	cmd := exec.Command("php-fpm", "-t", "-y", poolConfig.ConfigPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("invalid PHP-FPM configuration for pool %s: %w", pool, err)
	}

	s.logger.Info("Pool configuration validated successfully", zap.String("pool", pool))

	return nil
}

// GetPoolStatus returns the status of a specific pool
func (s *Supervisor) GetPoolStatus(pool string) (*api.PoolStatus, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	process, exists := s.processes[pool]
	if !exists {
		return nil, fmt.Errorf("pool '%s' not found", pool)
	}

	status := &api.PoolStatus{
		Name:            pool,
		Status:          string(process.Status),
		Health:          process.HealthState,
		ConfigPath:      process.Config.ConfigPath,
		FastCGIEndpoint: process.Config.FastCGIEndpoint,
		Workers: api.WorkerStatus{
			Active:  0, // Would need to query from PHP-FPM status
			Idle:    0,
			Total:   process.Config.MaxWorkers,
			Target:  process.Config.MaxWorkers,
			Maximum: process.Config.MaxWorkers,
		},
	}

	return status, nil
}

// GetAllPoolsStatus returns the status of all pools
func (s *Supervisor) GetAllPoolsStatus() ([]api.PoolStatus, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var statuses []api.PoolStatus
	for poolName, process := range s.processes {
		status := api.PoolStatus{
			Name:            poolName,
			Status:          string(process.Status),
			Health:          process.HealthState,
			ConfigPath:      process.Config.ConfigPath,
			FastCGIEndpoint: process.Config.FastCGIEndpoint,
			Workers: api.WorkerStatus{
				Active:  0, // Would need to query from PHP-FPM status
				Idle:    0,
				Total:   process.Config.MaxWorkers,
				Target:  process.Config.MaxWorkers,
				Maximum: process.Config.MaxWorkers,
			},
		}
		statuses = append(statuses, status)
	}

	return statuses, nil
}

// generatePHPFPMConfig generates and writes the PHP-FPM configuration files
func (s *Supervisor) generatePHPFPMConfig(poolName string) error {
	// Ensure socket directories exist before generating config
	baseDir := filepath.Dir(s.globalConfigPath)
	socketsDir := filepath.Join(baseDir, "sockets")
	if err := os.MkdirAll(socketsDir, 0755); err != nil {
		return fmt.Errorf("failed to create sockets directory: %w", err)
	}

	// Generate global PHP-FPM configuration
	globalConfig := s.generateGlobalConfig()

	// Write global config file
	if err := os.WriteFile(s.globalConfigPath, []byte(globalConfig), 0644); err != nil {
		return fmt.Errorf("failed to write global config: %w", err)
	}

	s.logger.Info("Generated PHP-FPM global configuration",
		zap.String("path", s.globalConfigPath))

	return nil
}

// generateGlobalConfig creates the global PHP-FPM configuration content
func (s *Supervisor) generateGlobalConfig() string {
	var config strings.Builder

	// Global PHP-FPM settings
	config.WriteString(";;;;;;;;;;;;;;;;;;;;;;\n")
	config.WriteString("; FPM Configuration ;\n")
	config.WriteString(";;;;;;;;;;;;;;;;;;;;;;\n\n")

	config.WriteString("; Generated by phpfpm-runtime-manager\n")
	config.WriteString("; Do not edit manually - changes will be overwritten\n\n")

	config.WriteString("[global]\n")

	// Get base directory from global config path
	baseDir := filepath.Dir(s.globalConfigPath)

	// PID file in structured pids subdirectory
	pidFile := filepath.Join(baseDir, "pids", "phpfpm-manager.pid")
	config.WriteString(fmt.Sprintf("pid = %s\n", pidFile))

	// Error log in structured logs subdirectory
	errorLog := filepath.Join(baseDir, "logs", "phpfpm-manager-error.log")
	config.WriteString(fmt.Sprintf("error_log = %s\n", errorLog))

	// Log level
	config.WriteString("log_level = notice\n")

	// Daemonize = no (we use --nodaemonize flag)
	config.WriteString("daemonize = no\n\n")

	// Add pool configurations - either inline or as includes
	if err := s.writePoolConfigs(); err != nil {
		s.logger.Error("Failed to write pool configs", zap.Error(err))
	}

	for _, pool := range s.pools {
		if pool.ConfigPath != "" {
			// Use include directive for separate pool config files
			config.WriteString(fmt.Sprintf("include = %s\n", pool.ConfigPath))
		} else {
			// Add pool configuration inline for pools without separate config files
			poolConfig := s.generatePoolConfig(pool)
			config.WriteString(poolConfig)
			config.WriteString("\n")
		}
	}

	return config.String()
}

// writePoolConfigs writes separate pool configuration files if ConfigPath is specified
func (s *Supervisor) writePoolConfigs() error {
	for _, pool := range s.pools {
		if pool.ConfigPath != "" {
			// Generate pool-specific configuration file content
			poolContent := s.generatePoolConfigFile(pool)

			// Write the pool config file
			if err := os.WriteFile(pool.ConfigPath, []byte(poolContent), 0644); err != nil {
				return fmt.Errorf("failed to write pool config %s: %w", pool.ConfigPath, err)
			}

			s.logger.Info("Generated pool configuration file",
				zap.String("pool", pool.Name),
				zap.String("path", pool.ConfigPath))
		}
	}
	return nil
}

// generatePoolConfigFile creates the content for a separate pool configuration file
func (s *Supervisor) generatePoolConfigFile(pool config.PoolConfig) string {
	var config strings.Builder

	config.WriteString(";;;;;;;;;;;;;;;;;;;;;;;;;;;;;;\n")
	config.WriteString(fmt.Sprintf("; Pool Configuration: %s ;\n", pool.Name))
	config.WriteString(";;;;;;;;;;;;;;;;;;;;;;;;;;;;;;\n\n")

	config.WriteString("; Generated by phpfpm-runtime-manager\n")
	config.WriteString("; Do not edit manually - changes will be overwritten\n\n")

	// Add the pool configuration section
	poolConfig := s.generatePoolConfig(pool)
	config.WriteString(poolConfig)

	return config.String()
}

// generatePoolConfig creates a pool-specific configuration section
func (s *Supervisor) generatePoolConfig(pool config.PoolConfig) string {
	var config strings.Builder

	config.WriteString(fmt.Sprintf("[%s]\n", pool.Name))

	// Pool manager - user/group inherited from running process (non-root by default)

	// Listen configuration
	// Auto-generate socket if no endpoint specified
	listenAddress := pool.FastCGIEndpoint
	if listenAddress == "" {
		// Auto-generate unix socket path
		baseDir := filepath.Dir(s.globalConfigPath)
		listenAddress = filepath.Join(baseDir, "sockets", fmt.Sprintf("%s.sock", pool.Name))
	} else if strings.HasPrefix(listenAddress, "unix:") {
		// Convert internal unix: format to PHP-FPM format
		listenAddress = strings.TrimPrefix(listenAddress, "unix:")
	}
	config.WriteString(fmt.Sprintf("listen = %s\n", listenAddress))

	// Status configuration with dedicated status socket
	if pool.FastCGIStatusPath != "" {
		// Generate dedicated status socket path
		baseDir := filepath.Dir(s.globalConfigPath)
		statusSocket := filepath.Join(baseDir, "sockets", fmt.Sprintf("%s-status.sock", pool.Name))

		config.WriteString(fmt.Sprintf("pm.status_listen = %s\n", statusSocket))
		config.WriteString(fmt.Sprintf("pm.status_path = %s\n", pool.FastCGIStatusPath))
		config.WriteString("ping.path = /ping\n")
	}

	// Process management
	config.WriteString("pm = dynamic\n")

	if pool.MaxWorkers > 0 {
		config.WriteString(fmt.Sprintf("pm.max_children = %d\n", pool.MaxWorkers))
		config.WriteString(fmt.Sprintf("pm.start_servers = %d\n", max(1, pool.MaxWorkers/4)))
		config.WriteString(fmt.Sprintf("pm.min_spare_servers = %d\n", max(1, pool.MaxWorkers/8)))
		config.WriteString(fmt.Sprintf("pm.max_spare_servers = %d\n", max(2, pool.MaxWorkers/2)))
	} else {
		// Default values for autoscaled pools
		config.WriteString("pm.max_children = 50\n")
		config.WriteString("pm.start_servers = 5\n")
		config.WriteString("pm.min_spare_servers = 2\n")
		config.WriteString("pm.max_spare_servers = 10\n")
	}

	config.WriteString("pm.max_requests = 1000\n")

	// Security
	config.WriteString("security.limit_extensions = .php\n")

	// Logging
	baseDir := filepath.Dir(s.globalConfigPath)
	poolLogFile := filepath.Join(baseDir, "logs", fmt.Sprintf("phpfpm-pool-%s.log", pool.Name))
	config.WriteString(fmt.Sprintf("access.log = %s\n", poolLogFile))
	config.WriteString("access.format = \"%%R - %%u %%t \\\"%%m %%r%%Q%%q\\\" %%s %%f %%{mili}d %%{kilo}M %%C%%%%\"\n")

	return config.String()
}

// startMasterProcess starts the single PHP-FPM master process
func (s *Supervisor) startMasterProcess(ctx context.Context) error {
	s.logger.Info("Starting PHP-FPM master process")

	// Initialize master process if nil
	if s.masterProcess == nil {
		s.masterProcess = &MasterProcess{
			Status: PoolStatusStarting,
		}
	}

	s.masterProcess.mu.Lock()
	defer s.masterProcess.mu.Unlock()

	if s.masterProcess.Status == PoolStatusRunning {
		return fmt.Errorf("PHP-FPM master is already running")
	}

	s.masterProcess.Status = PoolStatusStarting

	// Create command with generated global config
	s.logger.Debug("Starting PHP-FPM master process", zap.String("binary", s.phpfpmBinary))

	// Use the global config path - PHP-FPM will include pool configs from there
	cmd := exec.CommandContext(ctx, s.phpfpmBinary, "--fpm-config", s.globalConfigPath, "--nodaemonize")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Create new process group
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		s.masterProcess.Status = PoolStatusFailed
		s.emitEvent(types.ProcessEventFailed, "master", 0, fmt.Sprintf("Failed to start: %v", err))
		return fmt.Errorf("failed to start PHP-FPM master: %w", err)
	}

	s.masterProcess.Cmd = cmd
	s.masterProcess.PID = cmd.Process.Pid
	s.masterProcess.StartTime = time.Now()
	s.masterProcess.Status = PoolStatusRunning

	s.emitEvent(types.ProcessEventStarted, "master", s.masterProcess.PID, "PHP-FPM master started successfully")

	s.logger.Info("PHP-FPM master started successfully",
		zap.Int("pid", s.masterProcess.PID))

	return nil
}

// stopMasterProcess stops the single PHP-FPM master process
func (s *Supervisor) stopMasterProcess(ctx context.Context) error {
	if s.masterProcess == nil {
		return nil
	}

	s.masterProcess.mu.Lock()
	defer s.masterProcess.mu.Unlock()

	if s.masterProcess.Status == PoolStatusStopped {
		return nil
	}

	s.logger.Info("Stopping PHP-FPM master process")

	s.masterProcess.Status = PoolStatusStopping

	if s.masterProcess.Cmd == nil || s.masterProcess.Cmd.Process == nil {
		s.masterProcess.Status = PoolStatusStopped
		return nil
	}

	// Send SIGTERM to the entire process group for graceful shutdown
	// This ensures all child workers also receive the signal
	masterPID := s.masterProcess.Cmd.Process.Pid
	s.logger.Debug("Attempting to get process group for master", zap.Int("master_pid", masterPID))

	pgid, err := syscall.Getpgid(masterPID)
	if err != nil {
		s.logger.Warn("Failed to get process group ID", zap.Error(err), zap.Int("master_pid", masterPID))
		// Fallback to just the master process
		if err := s.masterProcess.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
			s.logger.Error("Failed to send SIGTERM to master process", zap.Error(err))
		} else {
			s.logger.Info("Sent SIGTERM to PHP-FPM master process (fallback)", zap.Int("master_pid", masterPID))
		}
		return
	}

	s.logger.Debug("Found process group", zap.Int("pgid", pgid), zap.Int("master_pid", masterPID))

	// Send signal to negative PID to target the entire process group
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
		s.logger.Error("Failed to send SIGTERM to process group", zap.Error(err), zap.Int("pgid", pgid))
		// Fallback to just the master process
		if err := s.masterProcess.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
			s.logger.Error("Failed to send SIGTERM to master process", zap.Error(err))
		} else {
			s.logger.Info("Sent SIGTERM to PHP-FPM master process (fallback)", zap.Int("master_pid", masterPID))
		}
	} else {
		s.logger.Info("Sent SIGTERM to PHP-FPM process group", zap.Int("pgid", pgid), zap.Int("master_pid", masterPID))
	}

	// Wait for graceful shutdown with timeout
	done := make(chan error, 1)
	go func() {
		done <- s.masterProcess.Cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil && !isExpectedSignalExit(err) {
			s.logger.Error("Master process exited with error", zap.Error(err))
		}
		// Clean up any orphaned workers even after graceful shutdown
		masterPID := s.masterProcess.Cmd.Process.Pid
		s.killOrphanedWorkers("master", masterPID)

	case <-time.After(30 * time.Second):
		// Force kill after timeout - give PHP-FPM workers more time to finish
		s.logger.Warn("Timeout waiting for graceful shutdown, force killing process group")
		masterPID := s.masterProcess.Cmd.Process.Pid

		// Try to kill the entire process group
		pgid, err := syscall.Getpgid(masterPID)
		if err == nil {
			if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
				s.logger.Error("Failed to kill process group", zap.Error(err))
			} else {
				s.logger.Info("Force killed PHP-FPM process group", zap.Int("pgid", pgid))
			}
		}

		// Also try to kill the master directly
		if err := s.masterProcess.Cmd.Process.Kill(); err != nil {
			s.logger.Error("Failed to kill master process", zap.Error(err))
		}

		// Clean up any orphaned workers that might still exist
		s.killOrphanedWorkers("master", masterPID)
	case <-ctx.Done():
		// Context canceled, force kill
		masterPID := s.masterProcess.Cmd.Process.Pid

		// Try to kill the entire process group
		pgid, err := syscall.Getpgid(masterPID)
		if err == nil {
			if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
				s.logger.Error("Failed to kill process group", zap.Error(err))
			} else {
				s.logger.Info("Force killed PHP-FPM process group on context cancel", zap.Int("pgid", pgid))
			}
		}

		// Also try to kill the master directly
		if err := s.masterProcess.Cmd.Process.Kill(); err != nil {
			s.logger.Error("Failed to kill master process", zap.Error(err))
		}

		// Clean up any orphaned workers
		s.killOrphanedWorkers("master", masterPID)
	}

	s.masterProcess.Status = PoolStatusStopped
	s.masterProcess.Cmd = nil
	s.masterProcess.PID = 0

	// Clean up socket files to prevent startup conflicts
	s.cleanupSocketFiles()

	s.emitEvent(types.ProcessEventStopped, "master", 0, "PHP-FPM master stopped")

	s.logger.Info("PHP-FPM master stopped")

	return nil
}

// isExpectedSignalExit checks if process exit was due to expected signals during shutdown
func isExpectedSignalExit(err error) bool {
	if err == nil {
		return true
	}

	errStr := err.Error()

	// All expected signal exits during graceful shutdown
	expectedSignals := []string{
		"signal: terminated", // SIGTERM (graceful)
		"signal: killed",     // SIGKILL (force kill)
		"signal: interrupt",  // SIGINT (Ctrl+C)
	}

	for _, expected := range expectedSignals {
		if errStr == expected {
			return true
		}
	}

	return false
}
