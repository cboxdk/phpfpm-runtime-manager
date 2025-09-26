package autoscaler

import (
	"fmt"
	"strings"
	"time"
)

// ValidatePoolName validates a pool name according to naming conventions and security requirements
func ValidatePoolName(name string) error {
	if name == "" {
		return NewValidationError("pool_name", name, "pool name cannot be empty")
	}

	if len(name) < MinPoolNameLength {
		return NewValidationError("pool_name", name, fmt.Sprintf("pool name must be at least %d characters", MinPoolNameLength))
	}

	if len(name) > MaxPoolNameLength {
		return NewValidationError("pool_name", name, fmt.Sprintf("pool name cannot exceed %d characters", MaxPoolNameLength))
	}

	// Security: Check for reserved system names
	if err := validateNotReservedName(name); err != nil {
		return err
	}

	// Security: Check for dangerous patterns
	if err := validateSafePoolName(name); err != nil {
		return err
	}

	// Check for valid characters (alphanumeric, hyphens, underscores)
	for _, char := range name {
		if !((char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '-' || char == '_') {
			return NewValidationError("pool_name", name, "pool name can only contain letters, numbers, hyphens, and underscores")
		}
	}

	// Pool name cannot start with a number or special character
	first := rune(name[0])
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z')) {
		return NewValidationError("pool_name", name, "pool name must start with a letter")
	}

	// Security: Pool name cannot end with special characters
	last := rune(name[len(name)-1])
	if last == '-' || last == '_' {
		return NewValidationError("pool_name", name, "pool name cannot end with special characters")
	}

	return nil
}

// validateNotReservedName checks if the pool name conflicts with reserved system names
func validateNotReservedName(name string) error {
	reservedNames := []string{
		// System reserved
		"default", "www", "admin", "root", "system", "daemon",
		// PHP-FPM reserved
		"php-fpm", "fpm", "status", "ping", "config",
		// Common service names
		"nginx", "apache", "mysql", "redis", "memcached",
		// Security sensitive
		"test", "debug", "staging", "production", "dev",
	}

	lowerName := strings.ToLower(name)
	for _, reserved := range reservedNames {
		if lowerName == reserved {
			return NewValidationError("pool_name", name,
				fmt.Sprintf("'%s' is a reserved name and cannot be used as pool name", name))
		}
	}

	return nil
}

// validateSafePoolName checks for potentially dangerous patterns in pool names
func validateSafePoolName(name string) error {
	lowerName := strings.ToLower(name)

	// Check for path traversal patterns
	dangerousPatterns := []string{
		"..", "./", "//", "\\", "/../", "/etc", "/var", "/usr", "/bin",
	}

	for _, pattern := range dangerousPatterns {
		if strings.Contains(lowerName, pattern) {
			return NewValidationError("pool_name", name,
				"pool name contains potentially dangerous patterns")
		}
	}

	// Check for SQL injection patterns
	sqlPatterns := []string{
		"select", "insert", "update", "delete", "drop", "union", "exec",
	}

	for _, pattern := range sqlPatterns {
		if strings.Contains(lowerName, pattern) {
			return NewValidationError("pool_name", name,
				"pool name contains SQL keywords which are not allowed")
		}
	}

	// Check for script injection patterns
	scriptPatterns := []string{
		"<script", "javascript:", "onload", "onerror", "eval(",
	}

	for _, pattern := range scriptPatterns {
		if strings.Contains(lowerName, pattern) {
			return NewValidationError("pool_name", name,
				"pool name contains script patterns which are not allowed")
		}
	}

	return nil
}

// ValidateWorkerCalculationConfig validates worker calculation configuration
func ValidateWorkerCalculationConfig(config WorkerCalculationConfig) error {
	if config.AvgWorkerMemoryMB < MinWorkerMemoryMB {
		return NewValidationError("avg_worker_memory_mb", config.AvgWorkerMemoryMB,
			fmt.Sprintf("average worker memory must be at least %d MB", MinWorkerMemoryMB))
	}

	if config.AvgWorkerMemoryMB > MaxWorkerMemoryMB {
		return NewValidationError("avg_worker_memory_mb", config.AvgWorkerMemoryMB,
			fmt.Sprintf("average worker memory cannot exceed %d MB", MaxWorkerMemoryMB))
	}

	if config.MemoryReservePercent < 0 || config.MemoryReservePercent > 1 {
		return NewValidationError("memory_reserve_percent", config.MemoryReservePercent,
			"memory reserve percent must be between 0 and 1")
	}

	if config.StartServersPercent < 0 || config.StartServersPercent > 1 {
		return NewValidationError("start_servers_percent", config.StartServersPercent,
			"start servers percent must be between 0 and 1")
	}

	if config.MinSparePercent < 0 || config.MinSparePercent > 1 {
		return NewValidationError("min_spare_percent", config.MinSparePercent,
			"min spare percent must be between 0 and 1")
	}

	if config.MaxSparePercent < 0 || config.MaxSparePercent > 1 {
		return NewValidationError("max_spare_percent", config.MaxSparePercent,
			"max spare percent must be between 0 and 1")
	}

	if config.MaxSparePercent <= config.MinSparePercent {
		return NewValidationError("max_spare_percent", config.MaxSparePercent,
			"max spare percent must be greater than min spare percent")
	}

	if config.MinWorkers < MinViableWorkers {
		return NewValidationError("min_workers", config.MinWorkers,
			fmt.Sprintf("minimum workers must be at least %d", MinViableWorkers))
	}

	if config.MaxWorkers < 0 {
		return NewValidationError("max_workers", config.MaxWorkers,
			"max workers cannot be negative (use 0 for no limit)")
	}

	if config.MaxWorkers > 0 && config.MaxWorkers < config.MinWorkers {
		return NewValidationError("max_workers", config.MaxWorkers,
			"max workers must be greater than or equal to min workers")
	}

	return nil
}

// ValidateHysteresisConfig validates hysteresis configuration
func ValidateHysteresisConfig(config HysteresisConfig) error {
	if config.ScaleUpCooldown < MinScalingInterval {
		return NewValidationError("scale_up_cooldown", config.ScaleUpCooldown,
			fmt.Sprintf("scale up cooldown must be at least %v", MinScalingInterval))
	}

	if config.ScaleUpCooldown > MaxScalingInterval {
		return NewValidationError("scale_up_cooldown", config.ScaleUpCooldown,
			fmt.Sprintf("scale up cooldown cannot exceed %v", MaxScalingInterval))
	}

	if config.ScaleDownCooldown < MinScalingInterval {
		return NewValidationError("scale_down_cooldown", config.ScaleDownCooldown,
			fmt.Sprintf("scale down cooldown must be at least %v", MinScalingInterval))
	}

	if config.ScaleDownCooldown > MaxScalingInterval {
		return NewValidationError("scale_down_cooldown", config.ScaleDownCooldown,
			fmt.Sprintf("scale down cooldown cannot exceed %v", MaxScalingInterval))
	}

	if config.MinConfidenceThreshold < 0 || config.MinConfidenceThreshold > 1 {
		return NewValidationError("min_confidence_threshold", config.MinConfidenceThreshold,
			"minimum confidence threshold must be between 0 and 1")
	}

	if config.ConsistentSamplesRequired < MinConsistentSamplesRequired {
		return NewValidationError("consistent_samples_required", config.ConsistentSamplesRequired,
			fmt.Sprintf("consistent samples required must be at least %d", MinConsistentSamplesRequired))
	}

	return nil
}

// ValidateScalingBaseline validates baseline configuration
func ValidateScalingBaseline(baseline *ScalingBaseline) error {
	if baseline == nil {
		return NewValidationError("baseline", nil, "baseline cannot be nil")
	}

	if baseline.TargetSamples < MinConsistentSamplesRequired {
		return NewValidationError("target_samples", baseline.TargetSamples,
			fmt.Sprintf("target samples must be at least %d", MinConsistentSamplesRequired))
	}

	if baseline.MaxSamples < baseline.TargetSamples {
		return NewValidationError("max_samples", baseline.MaxSamples,
			"max samples must be greater than or equal to target samples")
	}

	if !baseline.IsLearning {
		// Additional validation for completed learning phase
		if baseline.OptimalMemoryPerWorker < MinWorkerMemoryMB {
			return NewValidationError("optimal_memory_per_worker", baseline.OptimalMemoryPerWorker,
				fmt.Sprintf("optimal memory per worker must be at least %d MB", MinWorkerMemoryMB))
		}

		if baseline.OptimalMemoryPerWorker > MaxWorkerMemoryMB {
			return NewValidationError("optimal_memory_per_worker", baseline.OptimalMemoryPerWorker,
				fmt.Sprintf("optimal memory per worker cannot exceed %d MB", MaxWorkerMemoryMB))
		}

		if baseline.ConfidenceScore < 0 || baseline.ConfidenceScore > 1 {
			return NewValidationError("confidence_score", baseline.ConfidenceScore,
				"confidence score must be between 0 and 1")
		}
	}

	return nil
}

// ValidateQueueMetrics validates queue metrics
func ValidateQueueMetrics(metrics *QueueMetrics) error {
	if metrics == nil {
		return nil // nil metrics is acceptable
	}

	if metrics.Depth < 0 {
		return NewValidationError("queue_depth", metrics.Depth,
			"queue depth cannot be negative")
	}

	if metrics.ProcessingVelocity < 0 {
		return NewValidationError("processing_velocity", metrics.ProcessingVelocity,
			"processing velocity cannot be negative")
	}

	if metrics.OldestJobAge < 0 {
		return NewValidationError("oldest_job_age", metrics.OldestJobAge,
			"oldest job age cannot be negative")
	}

	if metrics.AverageWaitTime < 0 {
		return NewValidationError("average_wait_time", metrics.AverageWaitTime,
			"average wait time cannot be negative")
	}

	if metrics.EstimatedProcessTime < 0 {
		return NewValidationError("estimated_process_time", metrics.EstimatedProcessTime,
			"estimated process time cannot be negative")
	}

	return nil
}

// ValidateWorkerCount validates a worker count value
func ValidateWorkerCount(count int, maxAllowed int) error {
	if count < 0 {
		return NewValidationError("worker_count", count, "worker count cannot be negative")
	}

	if maxAllowed > 0 && count > maxAllowed {
		return NewValidationError("worker_count", count,
			fmt.Sprintf("worker count cannot exceed maximum allowed (%d)", maxAllowed))
	}

	return nil
}

// ValidateScalingInterval validates a scaling interval
func ValidateScalingInterval(interval time.Duration) error {
	if interval < MinScalingInterval {
		return NewValidationError("scaling_interval", interval,
			fmt.Sprintf("scaling interval must be at least %v", MinScalingInterval))
	}

	if interval > MaxScalingInterval {
		return NewValidationError("scaling_interval", interval,
			fmt.Sprintf("scaling interval cannot exceed %v", MaxScalingInterval))
	}

	return nil
}

// SanitizePoolName sanitizes a pool name by removing invalid characters
func SanitizePoolName(name string) string {
	// Replace invalid characters with underscores
	sanitized := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '-' || r == '_' {
			return r
		}
		return '_'
	}, name)

	// Ensure it starts with a letter
	if len(sanitized) > 0 {
		first := rune(sanitized[0])
		if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z')) {
			sanitized = "pool_" + sanitized
		}
	}

	// Trim to max length
	if len(sanitized) > MaxPoolNameLength {
		sanitized = sanitized[:MaxPoolNameLength]
	}

	// Ensure minimum length
	if len(sanitized) < MinPoolNameLength {
		sanitized = "pool_default"
	}

	return sanitized
}

// ValidateSystemResources validates that system has sufficient resources
func ValidateSystemResources(memoryMB, maxWorkers int, avgMemoryPerWorker float64) error {
	if memoryMB < SystemMemoryMinMB {
		return NewValidationError("system_memory", memoryMB,
			fmt.Sprintf("system memory must be at least %d MB", SystemMemoryMinMB))
	}

	if maxWorkers > 0 {
		requiredMemory := float64(maxWorkers) * avgMemoryPerWorker * (1 + DefaultMemoryReservePercent)
		if requiredMemory > float64(memoryMB) {
			return NewValidationError("system_resources", map[string]interface{}{
				"available_memory_mb":   memoryMB,
				"required_memory_mb":    requiredMemory,
				"max_workers":           maxWorkers,
				"avg_memory_per_worker": avgMemoryPerWorker,
			}, "insufficient system memory for requested worker configuration")
		}
	}

	return nil
}
