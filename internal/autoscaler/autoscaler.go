// Package autoscaler provides intelligent PHP-FPM worker autoscaling with adaptive baseline learning.
//
// This package implements a sophisticated autoscaling system that learns from actual workload patterns
// to make intelligent scaling decisions. It includes memory-aware scaling, queue pressure detection,
// confidence-based scaling adjustments, and hysteresis protection to prevent oscillations.
//
// Key features:
//   - Adaptive baseline learning from 50+ samples
//   - Confidence-based scaling (conservative with low confidence, aggressive with high confidence)
//   - Memory-aware scaling using actual system memory detection
//   - Queue pressure detection with processing velocity calculations
//   - Hysteresis protection to prevent scaling oscillations
//   - Platform-specific system memory detection (Linux /proc/meminfo, macOS fallback)
//   - Individual PHP-FPM worker state tracking (Idle, Running, Reading, Finishing, Ending)
//
// Example usage:
//
//	// Create intelligent scaler for a pool
//	scaler, err := NewIntelligentScaler("my-pool")
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Calculate optimal worker configuration
//	max, start, minSpare, maxSpare, err := CalculateOptimalWorkers(0)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Update with worker metrics for learning
//	scaler.UpdateWorkerMetrics(&WorkerMetrics{
//		PID: 1234,
//		State: WorkerStateActive,
//		MemoryUsageMB: 64.5,
//	})
//
//	// Calculate target workers based on current conditions
//	targetWorkers, err := scaler.CalculateTargetWorkers(context.Background())
//	if err != nil {
//		log.Fatal(err)
//	}
package autoscaler

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// systemMemoryCache caches the system memory value to avoid repeated syscalls
type systemMemoryCache struct {
	value     int
	timestamp time.Time
	ttl       time.Duration
}

var memCache = systemMemoryCache{
	ttl: MemoryCacheTTL, // Cache for 5 minutes
}

// getSystemMemory returns the system's total memory in MB using platform-specific methods.
//
// This function implements platform-specific memory detection with caching for performance:
//   - Linux: Reads from /proc/meminfo for accurate system memory
//   - macOS: Uses fallback method (future: sysctl implementation)
//   - Other platforms: Uses Go runtime stats with conservative multiplier
//
// The result is cached for 5 minutes to avoid repeated system calls.
// Memory values are validated to be within reasonable bounds (512MB to 1TB).
//
// Returns the total system memory in MB, or an error if detection fails.
func getSystemMemory() (int, error) {
	// Check cache first
	if time.Since(memCache.timestamp) < memCache.ttl && memCache.value > 0 {
		return memCache.value, nil
	}

	var totalMemoryMB int
	var err error

	// Try platform-specific methods
	switch runtime.GOOS {
	case "linux":
		totalMemoryMB, err = getLinuxSystemMemory()
	case "darwin":
		totalMemoryMB, err = getDarwinSystemMemory()
	default:
		// Fallback method using Go runtime
		totalMemoryMB, err = getFallbackSystemMemory()
	}

	if err != nil {
		return 0, fmt.Errorf("failed to get system memory: %w", err)
	}

	// Validate reasonable bounds
	if totalMemoryMB < SystemMemoryMinMB {
		totalMemoryMB = FallbackSystemMemoryGB * 1024 // Default fallback
	} else if totalMemoryMB > SystemMemoryMaxMB {
		totalMemoryMB = SystemMemoryMaxMB // Cap at maximum
	}

	// Update cache
	memCache.value = totalMemoryMB
	memCache.timestamp = time.Now()

	return totalMemoryMB, nil
}

// getLinuxSystemMemory reads memory from /proc/meminfo on Linux
func getLinuxSystemMemory() (int, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				memKB, err := strconv.Atoi(fields[1])
				if err != nil {
					return 0, fmt.Errorf("failed to parse MemTotal: %w", err)
				}
				return memKB / 1024, nil // Convert KB to MB
			}
		}
	}

	return 0, fmt.Errorf("MemTotal not found in /proc/meminfo")
}

// getDarwinSystemMemory uses sysctl on macOS
func getDarwinSystemMemory() (int, error) {
	// For macOS, we need to use system calls or fall back to runtime
	// This is a simplified implementation - in production, use CGO with sysctl
	return getFallbackSystemMemory()
}

// getFallbackSystemMemory provides a fallback method using Go runtime
func getFallbackSystemMemory() (int, error) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Use the heap limit as a reasonable estimate
	// This won't be accurate for system memory but provides a working baseline
	systemMemoryMB := int(m.Sys / 1024 / 1024)

	// Apply conservative multiplier assuming Go process uses ~10-20% of system memory
	systemMemoryMB = systemMemoryMB * 8

	// Ensure minimum reasonable value
	if systemMemoryMB < SystemMemoryMinMB {
		systemMemoryMB = FallbackSystemMemoryGB * 1024 // Default fallback
	}

	return systemMemoryMB, nil
}

// WorkerCalculationConfig holds configuration for worker calculations
type WorkerCalculationConfig struct {
	// Memory configuration
	AvgWorkerMemoryMB    int     // Average memory per worker (default: 64MB)
	MemoryReservePercent float64 // Percent of memory to reserve for OS (default: 25%)

	// Worker ratios (as percentages of max workers)
	StartServersPercent float64 // Start servers ratio (default: 25%)
	MinSparePercent     float64 // Min spare servers ratio (default: 12.5%)
	MaxSparePercent     float64 // Max spare servers ratio (default: 50%)

	// Bounds
	MinWorkers int // Minimum workers (default: 2)
	MaxWorkers int // Maximum workers (0 = no limit)
}

// DefaultWorkerCalculationConfig returns default configuration
func DefaultWorkerCalculationConfig() WorkerCalculationConfig {
	return WorkerCalculationConfig{
		AvgWorkerMemoryMB:    DefaultAvgWorkerMemoryMB,
		MemoryReservePercent: DefaultMemoryReservePercent,
		StartServersPercent:  DefaultStartServersPercent,
		MinSparePercent:      DefaultMinSparePercent,
		MaxSparePercent:      DefaultMaxSparePercent,
		MinWorkers:           DefaultMinWorkers,
		MaxWorkers:           0, // No hard limit by default
	}
}

// CalculateOptimalWorkers calculates optimal PHP-FPM worker configuration based on system resources.
//
// This function determines the optimal number of PHP-FPM workers and their configuration ratios
// based on available system memory and conservative estimates. It implements intelligent defaults
// inspired by advanced autoscaling logic with memory constraints.
//
// Parameters:
//   - maxWorkers: Maximum workers limit (0 = no limit, calculated from memory)
//
// Returns:
//   - max: Maximum workers based on memory constraints
//   - start: Initial number of workers to start (25% of max)
//   - minSpare: Minimum spare workers to maintain (12.5% of max)
//   - maxSpare: Maximum spare workers allowed (50% of max)
//   - err: Error if calculation fails
//
// The calculation reserves 25% of system memory for the OS and uses a conservative
// estimate of 64MB per PHP-FPM worker. All ratios are configurable via
// CalculateOptimalWorkersWithConfig().
//
// Example:
//
//	max, start, minSpare, maxSpare, err := CalculateOptimalWorkers(0)
//	// On 4GB system: max=48, start=12, minSpare=6, maxSpare=24
func CalculateOptimalWorkers(maxWorkers int) (max, start, minSpare, maxSpare int, err error) {
	config := DefaultWorkerCalculationConfig()
	if maxWorkers > 0 {
		config.MaxWorkers = maxWorkers
	}

	return CalculateOptimalWorkersWithConfig(config)
}

// CalculateOptimalWorkersWithConfig calculates optimal workers with custom configuration
func CalculateOptimalWorkersWithConfig(config WorkerCalculationConfig) (max, start, minSpare, maxSpare int, err error) {
	// Validate configuration first
	if err := ValidateWorkerCalculationConfig(config); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("invalid configuration: %w", err)
	}

	systemMemoryMB, err := getSystemMemory()
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("failed to get system memory: %w", err)
	}

	// Validate system resources
	if err := ValidateSystemResources(systemMemoryMB, config.MaxWorkers, float64(config.AvgWorkerMemoryMB)); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("insufficient system resources: %w", err)
	}

	// Calculate available memory for PHP-FPM workers
	reservedMemoryMB := int(float64(systemMemoryMB) * config.MemoryReservePercent)
	availableMemoryMB := systemMemoryMB - reservedMemoryMB

	// Ensure we have at least minimum available memory
	if availableMemoryMB < SystemMemoryMinMB {
		availableMemoryMB = SystemMemoryMinMB
	}

	// Calculate maximum workers based on memory constraints
	memoryBasedMax := availableMemoryMB / config.AvgWorkerMemoryMB

	// Apply user-defined maximum if specified
	calculatedMax := memoryBasedMax
	if config.MaxWorkers > 0 && calculatedMax > config.MaxWorkers {
		calculatedMax = config.MaxWorkers
	}

	// Apply minimum workers constraint
	if calculatedMax < config.MinWorkers {
		calculatedMax = config.MinWorkers
	}

	// Calculate derived values using configured ratios
	start = int(float64(calculatedMax) * config.StartServersPercent)
	if start < 1 {
		start = 1
	}
	if start > calculatedMax {
		start = calculatedMax
	}

	minSpare = int(float64(calculatedMax) * config.MinSparePercent)
	if minSpare < 1 {
		minSpare = 1
	}
	if minSpare >= calculatedMax {
		minSpare = calculatedMax - 1
	}

	maxSpare = int(float64(calculatedMax) * config.MaxSparePercent)
	if maxSpare <= minSpare {
		maxSpare = minSpare + 1
	}
	if maxSpare > calculatedMax {
		maxSpare = calculatedMax
	}

	// Ensure logical consistency: start >= minSpare
	if start < minSpare {
		start = minSpare
	}

	return calculatedMax, start, minSpare, maxSpare, nil
}

// GetMemoryBasedMaxWorkers returns the maximum workers based on available system memory
func GetMemoryBasedMaxWorkers() (int, error) {
	config := DefaultWorkerCalculationConfig()
	maxWorkers, _, _, _, err := CalculateOptimalWorkersWithConfig(config)
	return maxWorkers, err
}

// GetSystemMemoryInfo returns system memory information for diagnostics
func GetSystemMemoryInfo() (totalMB, availableMB, reservedMB int, err error) {
	totalMB, err = getSystemMemory()
	if err != nil {
		return 0, 0, 0, err
	}

	config := DefaultWorkerCalculationConfig()
	reservedMB = int(float64(totalMB) * config.MemoryReservePercent)
	availableMB = totalMB - reservedMB

	return totalMB, availableMB, reservedMB, nil
}
