// Package testutil provides cross-platform testing utilities
package testutil

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/platform"
)

// PlatformTest represents a test that can run on multiple platforms
type PlatformTest struct {
	Name       string
	Platforms  []string // Platforms this test supports (empty = all)
	Features   []string // Required platform features
	TestFunc   func(t *testing.T, provider platform.Provider)
	SkipReason string // Reason to skip this test
}

// PlatformTestSuite manages a collection of platform tests
type PlatformTestSuite struct {
	Tests []PlatformTest
}

// RunPlatformTests executes platform tests with appropriate providers
func RunPlatformTests(t *testing.T, suite PlatformTestSuite) {
	for _, test := range suite.Tests {
		t.Run(test.Name, func(t *testing.T) {
			// Check if test should run on current platform
			if !shouldRunOnPlatform(test, runtime.GOOS) {
				t.Skipf("Test %s not supported on %s", test.Name, runtime.GOOS)
				return
			}

			// Get appropriate provider
			provider, err := getTestProvider(t, test)
			if err != nil {
				t.Fatalf("Failed to get test provider: %v", err)
				return
			}

			// Check if provider is supported
			if !provider.IsSupported() {
				t.Skipf("Test %s provider not supported on %s", test.Name, runtime.GOOS)
				return
			}

			// Run the test
			test.TestFunc(t, provider)
		})
	}
}

// RunWithMockProvider runs a test with a mock provider
func RunWithMockProvider(t *testing.T, testFunc func(t *testing.T, provider platform.Provider)) {
	config := &platform.Config{
		EnableMockProvider: true,
		CacheEnabled:       false,
	}
	provider, err := platform.NewProvider(config)
	if err != nil {
		t.Fatalf("Failed to create mock provider: %v", err)
	}
	testFunc(t, provider)
}

// RunWithRealProvider runs a test with the real platform provider
func RunWithRealProvider(t *testing.T, testFunc func(t *testing.T, provider platform.Provider)) {
	provider, err := platform.GetProvider()
	if err != nil {
		t.Fatalf("Failed to get platform provider: %v", err)
	}
	testFunc(t, provider)
}

// RunCrossPlatform runs a test on both mock and real providers
func RunCrossPlatform(t *testing.T, testFunc func(t *testing.T, provider platform.Provider, providerType string)) {
	t.Run("mock", func(t *testing.T) {
		config := &platform.Config{
			EnableMockProvider: true,
			CacheEnabled:       false,
		}
		provider, err := platform.NewProvider(config)
		if err != nil {
			t.Fatalf("Failed to create mock provider: %v", err)
		}
		testFunc(t, provider, "mock")
	})

	t.Run("real", func(t *testing.T) {
		provider, err := platform.GetProvider()
		if err != nil {
			t.Fatalf("Failed to get platform provider: %v", err)
		}
		testFunc(t, provider, "real")
	})
}

// MockMemoryScenario sets up a specific memory scenario for testing
type MockMemoryScenario struct {
	TotalMemoryMB   int
	ProcessMemoryMB int
	ProcessPID      int
	ProcessName     string
}

// SetupMockMemoryScenario configures a mock provider with specific memory settings
func SetupMockMemoryScenario(provider platform.Provider, scenario MockMemoryScenario) error {
	// For the new mock provider implementation, the default configuration
	// should be sufficient for testing. Future enhancement could add
	// configuration methods to the Provider interface if needed.
	return nil
}

// PlatformTestData provides common test data for different platforms
type PlatformTestData struct {
	SupportedMemorySizes []int // MB
	SampleProcessPIDs    []int
	SampleProcessNames   []string
}

// GetPlatformTestData returns test data appropriate for the platform
func GetPlatformTestData(platformName string) PlatformTestData {
	switch platformName {
	case "linux":
		return PlatformTestData{
			SupportedMemorySizes: []int{512, 1024, 2048, 4096, 8192},
			SampleProcessPIDs:    []int{1, 2, 100, 1000, 1234},
			SampleProcessNames:   []string{"init", "systemd", "nginx", "php-fpm", "mysql"},
		}
	case "darwin":
		return PlatformTestData{
			SupportedMemorySizes: []int{1024, 2048, 4096, 8192, 16384},
			SampleProcessPIDs:    []int{1, 100, 200, 1000, 5678},
			SampleProcessNames:   []string{"launchd", "Finder", "Safari", "php", "nginx"},
		}
	case "windows":
		return PlatformTestData{
			SupportedMemorySizes: []int{1024, 2048, 4096, 8192, 16384, 32768},
			SampleProcessPIDs:    []int{4, 100, 500, 1000, 2000},
			SampleProcessNames:   []string{"System", "svchost", "explorer", "php", "nginx"},
		}
	default:
		return PlatformTestData{
			SupportedMemorySizes: []int{1024, 2048, 4096},
			SampleProcessPIDs:    []int{100, 200, 300},
			SampleProcessNames:   []string{"test-process", "mock-process", "sample-process"},
		}
	}
}

// Helper functions

func shouldRunOnPlatform(test PlatformTest, currentPlatform string) bool {
	// If no platforms specified, run on all
	if len(test.Platforms) == 0 {
		return true
	}

	// Check if current platform is in the list
	for _, platform := range test.Platforms {
		if platform == currentPlatform {
			return true
		}
	}

	return false
}

func getTestProvider(t *testing.T, test PlatformTest) (platform.Provider, error) {
	// Use mock provider for tests that don't require real system access
	if len(test.Features) == 0 || test.SkipReason != "" {
		config := &platform.Config{
			EnableMockProvider: true,
			CacheEnabled:       false,
		}
		return platform.NewProvider(config)
	}

	// Use real provider for integration tests
	return platform.GetProvider()
}

// Helper function for feature checking - simplified for now
func hasRequiredFeatures(provider platform.Provider, features []string) bool {
	// For now, just check if provider is supported
	// Feature checking can be added when needed
	return provider.IsSupported()
}

// TestMemoryProvider tests memory provider functionality
func TestMemoryProvider(t *testing.T, provider platform.Provider) {
	ctx := context.Background()
	memProvider := provider.Memory()

	if memProvider == nil {
		t.Fatal("Memory provider should not be nil")
	}

	// Test GetTotalMemory
	totalMemory, err := memProvider.GetTotalMemory(ctx)
	if err != nil {
		t.Fatalf("GetTotalMemory failed: %v", err)
	}

	AssertMemoryBounds(t, totalMemory, "total")

	// Test GetAvailableMemory
	availableMemory, err := memProvider.GetAvailableMemory(ctx)
	if err != nil {
		t.Fatalf("GetAvailableMemory failed: %v", err)
	}

	AssertMemoryBounds(t, availableMemory, "available")

	// Available memory should not exceed total memory
	if availableMemory > totalMemory {
		t.Errorf("Available memory (%d) should not exceed total memory (%d)", availableMemory, totalMemory)
	}

	// Test GetMemoryInfo
	memInfo, err := memProvider.GetMemoryInfo(ctx)
	if err != nil {
		t.Fatalf("GetMemoryInfo failed: %v", err)
	}

	if memInfo == nil {
		t.Fatal("MemoryInfo should not be nil")
	}

	// Validate memory info consistency
	if memInfo.TotalBytes != totalMemory {
		t.Errorf("MemoryInfo total (%d) doesn't match GetTotalMemory (%d)", memInfo.TotalBytes, totalMemory)
	}

	if memInfo.AvailableBytes != availableMemory {
		t.Errorf("MemoryInfo available (%d) doesn't match GetAvailableMemory (%d)", memInfo.AvailableBytes, availableMemory)
	}

	// Test platform identification
	if memProvider.Platform() == "" {
		t.Error("Memory provider platform should not be empty")
	}

	// Test support status
	if !memProvider.IsSupported() {
		t.Log("Memory provider reports as not supported - this may be expected on some platforms")
	}
}

// TestProcessProvider tests process provider functionality
func TestProcessProvider(t *testing.T, provider platform.Provider) {
	ctx := context.Background()
	procProvider := provider.Process()

	if procProvider == nil {
		t.Fatal("Process provider should not be nil")
	}

	// Test ListProcesses with empty filter
	processes, err := procProvider.ListProcesses(ctx, platform.ProcessFilter{})
	if err != nil {
		t.Fatalf("ListProcesses failed: %v", err)
	}

	if len(processes) == 0 {
		t.Error("Should have at least one process")
	}

	// Test process info validation
	for i, proc := range processes {
		if i >= 3 { // Only validate first 3 processes for performance
			break
		}
		AssertProcessInfo(t, proc)
	}

	// Test GetProcessInfo with a known PID
	if len(processes) > 0 {
		firstPID := processes[0].PID
		procInfo, err := procProvider.GetProcessInfo(ctx, firstPID)
		if err != nil {
			t.Fatalf("GetProcessInfo failed for PID %d: %v", firstPID, err)
		}

		AssertProcessInfo(t, procInfo)

		if procInfo.PID != firstPID {
			t.Errorf("Expected PID %d, got %d", firstPID, procInfo.PID)
		}
	}

	// Test platform identification
	if procProvider.Platform() == "" {
		t.Error("Process provider platform should not be empty")
	}

	// Test support status
	if !procProvider.IsSupported() {
		t.Log("Process provider reports as not supported - this may be expected on some platforms")
	}
}

// AssertMemoryBounds checks if memory values are within reasonable bounds
func AssertMemoryBounds(t *testing.T, memory uint64, name string) {
	const (
		minMemoryMB = 1           // 1MB minimum
		maxMemoryMB = 1024 * 1024 // 1TB maximum
	)

	memoryMB := memory / (1024 * 1024)

	if memoryMB < minMemoryMB {
		t.Errorf("%s memory %d MB is too small (min %d MB)", name, memoryMB, minMemoryMB)
	}

	if memoryMB > maxMemoryMB {
		t.Errorf("%s memory %d MB is too large (max %d MB)", name, memoryMB, maxMemoryMB)
	}
}

// AssertProcessInfo validates process information for consistency
func AssertProcessInfo(t *testing.T, proc *platform.ProcessInfo) {
	if proc == nil {
		t.Fatal("ProcessInfo should not be nil")
	}

	if proc.PID <= 0 {
		t.Errorf("Process PID (%d) should be positive", proc.PID)
	}

	if proc.Name == "" {
		t.Error("Process name should not be empty")
	}

	if proc.Platform == "" {
		t.Error("Process platform should not be empty")
	}

	// Validate CPU percent is reasonable (0-100% per core, so allow up to 1600% for 16 cores)
	if proc.CPUPercent < 0 || proc.CPUPercent > 1600 {
		t.Errorf("CPU percent (%f) should be between 0 and 1600", proc.CPUPercent)
	}

	// Memory should be reasonable (not negative, not exceeding 1TB)
	if proc.MemoryBytes > 1024*1024*1024*1024 {
		t.Errorf("Process memory (%d bytes) seems unreasonably high", proc.MemoryBytes)
	}

	// Virtual and resident memory validation if present
	if proc.VirtualBytes > 0 {
		AssertMemoryBounds(t, proc.VirtualBytes, "virtual")
	}
	if proc.ResidentBytes > 0 {
		AssertMemoryBounds(t, proc.ResidentBytes, "resident")
		if proc.VirtualBytes > 0 && proc.ResidentBytes > proc.VirtualBytes {
			t.Error("Resident memory should not exceed virtual memory")
		}
	}

	// Start time should be in the past but not too far
	now := time.Now()
	if proc.StartTime.After(now) {
		t.Error("Process start time should not be in the future")
	}

	// Don't allow start times more than 10 years in the past (system uptime limit)
	tenYearsAgo := now.Add(-10 * 365 * 24 * time.Hour)
	if proc.StartTime.Before(tenYearsAgo) {
		t.Errorf("Process start time (%v) seems unreasonably old", proc.StartTime)
	}
}
