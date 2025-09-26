package autoscaler

import (
	"context"
	"fmt"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/platform"
)

// PlatformAwareMemoryProvider wraps the platform abstraction for autoscaler use
type PlatformAwareMemoryProvider struct {
	provider platform.Provider
	cache    systemMemoryCache
}

// NewPlatformAwareMemoryProvider creates a new platform-aware memory provider
func NewPlatformAwareMemoryProvider(provider platform.Provider) *PlatformAwareMemoryProvider {
	return &PlatformAwareMemoryProvider{
		provider: provider,
		cache: systemMemoryCache{
			ttl: MemoryCacheTTL,
		},
	}
}

// GetMemoryInfoMB returns system memory in MB using platform abstraction
func (p *PlatformAwareMemoryProvider) GetMemoryInfoMB() (int, error) {
	// Check cache first
	if time.Since(p.cache.timestamp) < p.cache.ttl && p.cache.value > 0 {
		return p.cache.value, nil
	}

	ctx := context.Background()
	sysInfo, err := p.provider.Memory().GetMemoryInfo(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get system memory: %w", err)
	}

	totalMemoryMB := int(sysInfo.TotalBytes / (1024 * 1024))

	// Validate reasonable bounds
	if totalMemoryMB < SystemMemoryMinMB {
		totalMemoryMB = FallbackSystemMemoryGB * 1024 // Default fallback
	} else if totalMemoryMB > SystemMemoryMaxMB {
		totalMemoryMB = SystemMemoryMaxMB // Cap at maximum
	}

	// Update cache
	p.cache.value = totalMemoryMB
	p.cache.timestamp = time.Now()

	return totalMemoryMB, nil
}

// GetAvailableMemoryMB returns available memory in MB
func (p *PlatformAwareMemoryProvider) GetAvailableMemoryMB() (int, error) {
	ctx := context.Background()
	availableBytes, err := p.provider.Memory().GetAvailableMemory(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get available memory: %w", err)
	}

	return int(availableBytes / (1024 * 1024)), nil
}

// GetProvider returns the underlying platform provider
func (p *PlatformAwareMemoryProvider) GetProvider() platform.Provider {
	return p.provider
}

// Global platform-aware memory provider instance
var globalMemoryProvider *PlatformAwareMemoryProvider

// GetGlobalMemoryProvider returns the global memory provider instance
func GetGlobalMemoryProvider() *PlatformAwareMemoryProvider {
	if globalMemoryProvider == nil {
		provider, err := platform.GetProvider()
		if err != nil {
			return nil // Return nil if platform provider is not available
		}
		globalMemoryProvider = NewPlatformAwareMemoryProvider(provider)
	}
	return globalMemoryProvider
}

// SetGlobalMemoryProvider sets the global memory provider (for testing)
func SetGlobalMemoryProvider(provider *PlatformAwareMemoryProvider) {
	globalMemoryProvider = provider
}

// ResetGlobalMemoryProvider resets the global memory provider to auto-detection
func ResetGlobalMemoryProvider() {
	globalMemoryProvider = nil
}

// Platform-aware version of getSystemMemory that uses the abstraction layer
func getSystemMemoryPlatformAware() (int, error) {
	return GetGlobalMemoryProvider().GetMemoryInfoMB()
}

// Platform-aware system memory info
func GetMemoryInfoPlatformAware() (totalMB, availableMB, reservedMB int, err error) {
	provider := GetGlobalMemoryProvider()

	totalMB, err = provider.GetMemoryInfoMB()
	if err != nil {
		return 0, 0, 0, err
	}

	availableFromSystem, err := provider.GetAvailableMemoryMB()
	if err != nil {
		// Fallback to calculation
		config := DefaultWorkerCalculationConfig()
		reservedMB = int(float64(totalMB) * config.MemoryReservePercent)
		availableMB = totalMB - reservedMB
	} else {
		availableMB = availableFromSystem
		reservedMB = totalMB - availableMB
	}

	return totalMB, availableMB, reservedMB, nil
}

// Platform-aware memory-based max workers calculation
func GetMemoryBasedMaxWorkersPlatformAware() (int, error) {
	provider := GetGlobalMemoryProvider()
	totalMemoryMB, err := provider.GetMemoryInfoMB()
	if err != nil {
		return 0, err
	}

	config := DefaultWorkerCalculationConfig()
	reservedMemoryMB := int(float64(totalMemoryMB) * config.MemoryReservePercent)
	availableMemoryMB := totalMemoryMB - reservedMemoryMB

	// Ensure we have at least minimum available memory
	if availableMemoryMB < SystemMemoryMinMB {
		availableMemoryMB = SystemMemoryMinMB
	}

	// Calculate maximum workers based on memory constraints
	maxWorkers := availableMemoryMB / config.AvgWorkerMemoryMB

	// Apply minimum workers constraint
	if maxWorkers < config.MinWorkers {
		maxWorkers = config.MinWorkers
	}

	return maxWorkers, nil
}

// UpdateAutoscalerToUsePlatformAbstraction updates the existing autoscaler functions
// to use the platform abstraction layer instead of direct system calls

// Platform-aware version of CalculateOptimalWorkers
func CalculateOptimalWorkersPlatformAware(maxWorkers int) (max, start, minSpare, maxSpare int, err error) {
	config := DefaultWorkerCalculationConfig()
	if maxWorkers > 0 {
		config.MaxWorkers = maxWorkers
	}

	return CalculateOptimalWorkersWithConfigPlatformAware(config)
}

// Platform-aware version of CalculateOptimalWorkersWithConfig
func CalculateOptimalWorkersWithConfigPlatformAware(config WorkerCalculationConfig) (max, start, minSpare, maxSpare int, err error) {
	// Validate configuration first
	if err := ValidateWorkerCalculationConfig(config); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("invalid configuration: %w", err)
	}

	systemMemoryMB, err := getSystemMemoryPlatformAware()
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

// Helper function to determine if we should use platform-aware functions
func ShouldUsePlatformAware() bool {
	// Use platform-aware functions by default, allow override via environment
	// This provides backward compatibility while enabling the new platform abstraction
	return true
}

// Backward-compatible wrapper functions that can switch between old and new implementations

// BackwardCompatibleGetMemoryInfo provides backward compatibility
func BackwardCompatibleGetMemoryInfo() (int, error) {
	if ShouldUsePlatformAware() {
		return getSystemMemoryPlatformAware()
	}
	return getSystemMemory() // Original implementation
}

// BackwardCompatibleCalculateOptimalWorkers provides backward compatibility
func BackwardCompatibleCalculateOptimalWorkers(maxWorkers int) (max, start, minSpare, maxSpare int, err error) {
	if ShouldUsePlatformAware() {
		return CalculateOptimalWorkersPlatformAware(maxWorkers)
	}
	return CalculateOptimalWorkers(maxWorkers) // Original implementation
}

// BackwardCompatibleGetMemoryInfoDetailed provides backward compatibility with detailed memory info
func BackwardCompatibleGetMemoryInfoDetailed() (totalMB, availableMB, reservedMB int, err error) {
	if ShouldUsePlatformAware() {
		return GetMemoryInfoPlatformAware()
	}
	return 0, 0, 0, fmt.Errorf("non-platform-aware memory info not available")
}

// BackwardCompatibleGetMemoryBasedMaxWorkers provides backward compatibility
func BackwardCompatibleGetMemoryBasedMaxWorkers() (int, error) {
	if ShouldUsePlatformAware() {
		return GetMemoryBasedMaxWorkersPlatformAware()
	}
	return GetMemoryBasedMaxWorkers() // Original implementation
}
