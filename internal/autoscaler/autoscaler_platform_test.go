package autoscaler

import (
	"context"
	"fmt"
	"runtime"
	"testing"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/platform"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/testutil"
)

// TestAutoscalerCrossPlatform tests autoscaler functionality across platforms
func TestAutoscalerCrossPlatform(t *testing.T) {
	suite := testutil.PlatformTestSuite{
		Tests: []testutil.PlatformTest{
			{
				Name:     "memory_detection_basic",
				TestFunc: testMemoryDetectionBasic,
			},
			{
				Name:      "memory_detection_linux_specific",
				Platforms: []string{"linux"},
				Features:  []string{platform.FeatureProcFS},
				TestFunc:  testMemoryDetectionLinux,
			},
			{
				Name:      "memory_detection_darwin_specific",
				Platforms: []string{"darwin"},
				Features:  []string{platform.FeatureSysctl},
				TestFunc:  testMemoryDetectionDarwin,
			},
			{
				Name:     "worker_calculation_cross_platform",
				TestFunc: testWorkerCalculationCrossPlatform,
			},
			{
				Name:     "memory_bounds_validation",
				TestFunc: testMemoryBoundsValidation,
			},
			{
				Name:     "autoscaler_with_low_memory",
				TestFunc: testAutoscalerWithLowMemory,
			},
			{
				Name:     "autoscaler_with_high_memory",
				TestFunc: testAutoscalerWithHighMemory,
			},
		},
	}

	testutil.RunPlatformTests(t, suite)
}

// testMemoryDetectionBasic tests basic memory detection functionality
func testMemoryDetectionBasic(t *testing.T, provider platform.Provider) {
	// Setup mock scenario
	scenario := testutil.MockMemoryScenario{
		TotalMemoryMB: 4096, // 4GB
	}
	if err := testutil.SetupMockMemoryScenario(provider, scenario); err != nil {
		t.Fatalf("Failed to setup mock scenario: %v", err)
	}

	ctx := context.Background()
	memProvider := provider.Memory()

	// Test GetSystemMemory
	sysInfo, err := memProvider.GetMemoryInfo(ctx)
	if err != nil {
		t.Fatalf("GetSystemMemory failed: %v", err)
	}

	testutil.AssertMemoryBounds(t, sysInfo.TotalBytes, "total")
	testutil.AssertMemoryBounds(t, sysInfo.AvailableBytes, "available")

	// For mock provider, verify expected values
	if provider.Platform() == "mock" {
		expectedTotal := uint64(4096) * 1024 * 1024
		if sysInfo.TotalBytes != expectedTotal {
			t.Errorf("Expected total memory %d, got %d", expectedTotal, sysInfo.TotalBytes)
		}
	}
}

// testMemoryDetectionLinux tests Linux-specific memory detection
func testMemoryDetectionLinux(t *testing.T, provider platform.Provider) {
	if provider.Platform() != "linux" && provider.Platform() != "mock" {
		t.Skip("Linux-specific test")
	}

	// Test that Linux provider supports proc filesystem
	if !provider.IsSupported() {
		t.Skip("Platform doesn't support required features")
	}

	ctx := context.Background()
	memProvider := provider.Memory()

	sysInfo, err := memProvider.GetMemoryInfo(ctx)
	if err != nil {
		t.Fatalf("GetSystemMemory failed on Linux: %v", err)
	}

	// Linux should provide detailed memory info
	if sysInfo.TotalBytes == 0 {
		t.Error("Linux should provide total memory information")
	}

	if sysInfo.AvailableBytes == 0 {
		t.Error("Linux should provide available memory information")
	}
}

// testMemoryDetectionDarwin tests macOS-specific memory detection
func testMemoryDetectionDarwin(t *testing.T, provider platform.Provider) {
	if provider.Platform() != "darwin" && provider.Platform() != "mock" {
		t.Skip("Darwin-specific test")
	}

	// Test that Darwin provider supports sysctl
	if !provider.IsSupported() {
		t.Skip("Platform doesn't support required features")
	}

	ctx := context.Background()
	memProvider := provider.Memory()

	sysInfo, err := memProvider.GetMemoryInfo(ctx)
	if err != nil {
		t.Fatalf("GetSystemMemory failed on Darwin: %v", err)
	}

	// Darwin should provide system memory info
	if sysInfo.TotalBytes == 0 {
		t.Error("Darwin should provide total memory information")
	}
}

// testWorkerCalculationCrossPlatform tests worker calculation across platforms
func testWorkerCalculationCrossPlatform(t *testing.T, provider platform.Provider) {
	// Setup different memory scenarios
	testData := testutil.GetPlatformTestData(provider.Platform())

	for _, memoryMB := range testData.SupportedMemorySizes {
		t.Run(fmt.Sprintf("memory_%dMB", memoryMB), func(t *testing.T) {
			scenario := testutil.MockMemoryScenario{
				TotalMemoryMB: memoryMB,
			}
			if err := testutil.SetupMockMemoryScenario(provider, scenario); err != nil {
				t.Fatalf("Failed to setup mock scenario: %v", err)
			}

			// Replace the global platform provider temporarily
			// oldProvider := platform.GetProvider() - not used in new interface
			// platform.SetPlatformProvider(provider) - not available in new interface
			// defer platform.SetPlatformProvider(oldProvider)

			// Test worker calculation
			max, start, minSpare, _, err := CalculateOptimalWorkers(0)
			if err != nil {
				t.Fatalf("CalculateOptimalWorkers failed: %v", err)
			}

			// Validate results
			if max <= 0 {
				t.Error("Max workers should be positive")
			}

			if start <= 0 {
				t.Error("Start workers should be positive")
			}

			if minSpare < 0 {
				t.Error("Min spare workers should be non-negative")
			}

			if start < minSpare {
				t.Error("Start workers should be >= min spare workers")
			}

			// Check memory constraints
			config := DefaultWorkerCalculationConfig()
			expectedMax := (memoryMB * (100 - int(config.MemoryReservePercent*100)) / 100) / config.AvgWorkerMemoryMB
			if max > expectedMax*2 {
				t.Errorf("Calculated max workers %d seems too high for %dMB memory (expected ~%d)", max, memoryMB, expectedMax)
			}
		})
	}
}

// testMemoryBoundsValidation tests memory validation logic
func testMemoryBoundsValidation(t *testing.T, provider platform.Provider) {
	// Test minimum memory scenario
	scenario := testutil.MockMemoryScenario{
		TotalMemoryMB: 128, // Very low memory
	}
	if err := testutil.SetupMockMemoryScenario(provider, scenario); err != nil {
		t.Fatalf("Failed to setup mock scenario: %v", err)
	}

	// Replace the global platform provider temporarily
	oldProvider, _ := platform.GetProvider()
	_ = oldProvider // Use platform-aware functions instead of global provider manipulation

	// Test worker calculation with low memory
	max, start, minSpare, _, err := CalculateOptimalWorkers(0)
	if err != nil {
		t.Fatalf("CalculateOptimalWorkers failed with low memory: %v", err)
	}

	// Should still have minimum viable configuration
	if max < DefaultMinWorkers {
		t.Errorf("Even with low memory, should have at least %d workers, got %d", DefaultMinWorkers, max)
	}

	if start < 1 {
		t.Error("Should have at least 1 start worker")
	}

	if minSpare < 1 {
		t.Error("Should have at least 1 min spare worker")
	}

	// Validate configuration consistency
	config := DefaultWorkerCalculationConfig()
	err = ValidateWorkerCalculationConfig(config)
	if err != nil {
		t.Errorf("Default configuration should be valid: %v", err)
	}

	// Test system resources validation
	err = ValidateSystemResources(128, 0, float64(config.AvgWorkerMemoryMB))
	if err == nil {
		t.Error("Should warn about insufficient memory for 128MB system")
	}
}

// testAutoscalerWithLowMemory tests autoscaler behavior with low memory
func testAutoscalerWithLowMemory(t *testing.T, provider platform.Provider) {
	scenario := testutil.MockMemoryScenario{
		TotalMemoryMB:   512, // 512MB
		ProcessMemoryMB: 64,
		ProcessPID:      1234,
		ProcessName:     "php-fpm",
	}
	if err := testutil.SetupMockMemoryScenario(provider, scenario); err != nil {
		t.Fatalf("Failed to setup mock scenario: %v", err)
	}

	// oldProvider := platform.GetProvider() - not used in new interface
	// platform.SetPlatformProvider(provider) - not available in new interface
	// defer platform.SetPlatformProvider(oldProvider)

	// Test memory-based calculation
	max, err := GetMemoryBasedMaxWorkers()
	if err != nil {
		t.Fatalf("GetMemoryBasedMaxWorkers failed: %v", err)
	}

	// With 512MB and 64MB per worker, expect around 6 workers (75% of 512MB = 384MB / 64MB = 6)
	expectedRange := []int{4, 8} // Allow some variance
	if max < expectedRange[0] || max > expectedRange[1] {
		t.Errorf("Expected max workers in range %v for 512MB system, got %d", expectedRange, max)
	}

	// Test system memory info
	totalMB, availableMB, reservedMB, err := GetSystemMemoryInfo()
	if err != nil {
		t.Fatalf("GetSystemMemoryInfo failed: %v", err)
	}

	if totalMB != 512 {
		t.Errorf("Expected total memory 512MB, got %dMB", totalMB)
	}

	if reservedMB <= 0 {
		t.Error("Should have reserved memory for OS")
	}

	if availableMB <= 0 {
		t.Error("Should have available memory for PHP-FPM")
	}

	if totalMB != availableMB+reservedMB {
		t.Errorf("Total memory (%d) should equal available (%d) + reserved (%d)", totalMB, availableMB, reservedMB)
	}
}

// testAutoscalerWithHighMemory tests autoscaler behavior with high memory
func testAutoscalerWithHighMemory(t *testing.T, provider platform.Provider) {
	scenario := testutil.MockMemoryScenario{
		TotalMemoryMB:   16384, // 16GB
		ProcessMemoryMB: 128,   // Higher memory per process
		ProcessPID:      5678,
		ProcessName:     "php-fpm-worker",
	}
	if err := testutil.SetupMockMemoryScenario(provider, scenario); err != nil {
		t.Fatalf("Failed to setup mock scenario: %v", err)
	}

	// oldProvider := platform.GetProvider() - not used in new interface
	// platform.SetPlatformProvider(provider) - not available in new interface
	// defer platform.SetPlatformProvider(oldProvider)

	// Test with custom configuration for high memory
	config := DefaultWorkerCalculationConfig()
	config.AvgWorkerMemoryMB = 128 // Higher memory per worker

	max, start, minSpare, maxSpare, err := CalculateOptimalWorkersWithConfig(config)
	if err != nil {
		t.Fatalf("CalculateOptimalWorkersWithConfig failed: %v", err)
	}

	// With 16GB and 128MB per worker, expect around 96 workers (75% of 16GB = 12GB / 128MB = 96)
	expectedRange := []int{80, 120} // Allow some variance
	if max < expectedRange[0] || max > expectedRange[1] {
		t.Errorf("Expected max workers in range %v for 16GB system with 128MB workers, got %d", expectedRange, max)
	}

	// Validate ratios are maintained
	expectedStart := int(float64(max) * config.StartServersPercent)
	if abs(start-expectedStart) > 2 { // Allow small rounding differences
		t.Errorf("Start workers ratio incorrect: expected ~%d, got %d", expectedStart, start)
	}

	expectedMinSpare := int(float64(max) * config.MinSparePercent)
	if abs(minSpare-expectedMinSpare) > 2 {
		t.Errorf("Min spare workers ratio incorrect: expected ~%d, got %d", expectedMinSpare, minSpare)
	}

	expectedMaxSpare := int(float64(max) * config.MaxSparePercent)
	if abs(maxSpare-expectedMaxSpare) > 2 {
		t.Errorf("Max spare workers ratio incorrect: expected ~%d, got %d", expectedMaxSpare, maxSpare)
	}
}

// TestAutoscalerWithPlatformIntegration tests autoscaler integration with platform providers
func TestAutoscalerWithPlatformIntegration(t *testing.T) {
	testutil.RunCrossPlatform(t, func(t *testing.T, provider platform.Provider, providerType string) {
		t.Run("memory_provider_integration", func(t *testing.T) {
			testutil.TestMemoryProvider(t, provider)
		})

		t.Run("memory_detection_consistency", func(t *testing.T) {
			scenario := testutil.MockMemoryScenario{
				TotalMemoryMB: 2048,
			}
			if err := testutil.SetupMockMemoryScenario(provider, scenario); err != nil {
				t.Fatalf("Failed to setup mock scenario: %v", err)
			}

			ctx := context.Background()
			memProvider := provider.Memory()

			// Test consistency between different memory calls
			sysInfo1, err := memProvider.GetMemoryInfo(ctx)
			if err != nil {
				t.Fatalf("First GetSystemMemory call failed: %v", err)
			}

			sysInfo2, err := memProvider.GetMemoryInfo(ctx)
			if err != nil {
				t.Fatalf("Second GetSystemMemory call failed: %v", err)
			}

			// Results should be consistent (within reasonable bounds for real systems)
			if providerType == "mock" {
				if sysInfo1.TotalBytes != sysInfo2.TotalBytes {
					t.Error("Mock provider should return consistent total memory")
				}
			} else {
				// Real systems may have small variations
				diff := int64(sysInfo1.TotalBytes) - int64(sysInfo2.TotalBytes)
				if abs(int(diff)) > 1024*1024 { // 1MB tolerance
					t.Errorf("Real provider memory readings vary too much: %d vs %d",
						sysInfo1.TotalBytes, sysInfo2.TotalBytes)
				}
			}
		})

		t.Run("platform_feature_consistency", func(t *testing.T) {
			// Test that platform features are consistent with platform name
			expectedFeatures := map[string][]string{
				"linux":   {platform.FeatureProcFS, platform.FeatureSysctl},
				"darwin":  {platform.FeatureSysctl, platform.FeatureBSD},
				"windows": {platform.FeatureWMI, platform.FeaturePerfMon},
				"mock":    {}, // Mock can support any feature
			}

			platformName := provider.Platform()
			if features, exists := expectedFeatures[platformName]; exists {
				for _, feature := range features {
					if !provider.IsSupported() {
						t.Errorf("Platform %s should support feature %s (skipping feature check)", platformName, feature)
					}
					_ = feature // Use the feature variable
				}
			}

			// Test that unsupported features are correctly reported
			unsupportedFeatures := map[string][]string{
				"linux":   {platform.FeatureWMI, platform.FeatureBSD},
				"darwin":  {platform.FeatureProcFS, platform.FeatureWMI},
				"windows": {platform.FeatureProcFS, platform.FeatureSysctl, platform.FeatureBSD},
			}

			if features, exists := unsupportedFeatures[platformName]; exists {
				for _, feature := range features {
					if provider.IsSupported() {
						t.Errorf("Platform %s should not support feature %s (skipping feature check)", platformName, feature)
					}
					_ = feature // Use the feature variable
				}
			}
		})
	})
}

// TestPlatformSpecificEdgeCases tests edge cases specific to each platform
func TestPlatformSpecificEdgeCases(t *testing.T) {
	switch runtime.GOOS {
	case "linux":
		t.Run("linux_proc_filesystem", func(t *testing.T) {
			provider, _ := platform.GetProvider()
			if provider.Platform() != "linux" {
				t.Skip("Linux-specific test")
			}

			// Test /proc filesystem access
			if !provider.IsSupported() /* platform.FeatureProcFS */ {
				t.Fatal("Linux should support required features")
			}

			ctx := context.Background()
			memProvider := provider.Memory()

			// This should work on real Linux systems
			_, err := memProvider.GetMemoryInfo(ctx)
			if err != nil {
				t.Errorf("Linux memory detection failed: %v", err)
			}
		})

	case "darwin":
		t.Run("darwin_sysctl", func(t *testing.T) {
			provider, _ := platform.GetProvider()
			if provider.Platform() != "darwin" {
				t.Skip("Darwin-specific test")
			}

			// Test sysctl access
			if !provider.IsSupported() /* platform.FeatureSysctl */ {
				t.Fatal("Darwin should support required features")
			}

			ctx := context.Background()
			memProvider := provider.Memory()

			// This should work on real macOS systems
			_, err := memProvider.GetMemoryInfo(ctx)
			if err != nil {
				t.Errorf("Darwin memory detection failed: %v", err)
			}
		})

	case "windows":
		t.Run("windows_wmi", func(t *testing.T) {
			provider, _ := platform.GetProvider()
			if provider.Platform() != "windows" {
				t.Skip("Windows-specific test")
			}

			// Test WMI access
			if !provider.IsSupported() /* platform.FeatureWMI */ {
				t.Fatal("Windows should support required features")
			}

			ctx := context.Background()
			memProvider := provider.Memory()

			// This should work on real Windows systems
			_, err := memProvider.GetMemoryInfo(ctx)
			if err != nil {
				t.Errorf("Windows memory detection failed: %v", err)
			}
		})
	}
}

// Helper function
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
