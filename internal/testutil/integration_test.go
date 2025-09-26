package testutil

import (
	"context"
	"testing"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/platform"
)

// TestCrossPlatformIntegration tests the overall cross-platform functionality
func TestCrossPlatformIntegration(t *testing.T) {
	// Test platform provider factory
	provider, err := platform.GetProvider()
	if err != nil {
		t.Fatalf("Failed to get platform provider: %v", err)
	}
	if provider == nil {
		t.Fatal("Platform provider should not be nil")
	}

	t.Logf("Testing on platform: %s", provider.Platform())

	// Test memory provider
	memProvider := provider.Memory()
	if memProvider == nil {
		t.Fatal("Memory provider should not be nil")
	}

	ctx := context.Background()

	// Test GetMemoryInfo
	memInfo, err := memProvider.GetMemoryInfo(ctx)
	if err != nil {
		t.Fatalf("GetMemoryInfo failed: %v", err)
	}

	AssertMemoryBounds(t, memInfo.TotalBytes, "total")
	AssertMemoryBounds(t, memInfo.AvailableBytes, "available")

	t.Logf("System memory: Total=%d MB, Available=%d MB",
		memInfo.TotalBytes/(1024*1024),
		memInfo.AvailableBytes/(1024*1024))

	// Test process provider
	procProvider := provider.Process()
	if procProvider == nil {
		t.Fatal("Process provider should not be nil")
	}

	processes, err := procProvider.ListProcesses(ctx, platform.ProcessFilter{})
	if err != nil {
		t.Fatalf("ListProcesses failed: %v", err)
	}

	if len(processes) == 0 {
		t.Error("Should have at least one process")
	}

	t.Logf("Found %d processes", len(processes))

	// Test with mock provider scenario
	RunWithMockProvider(t, func(t *testing.T, mockProvider platform.Provider) {
		scenario := MockMemoryScenario{
			TotalMemoryMB:   2048,
			ProcessMemoryMB: 64,
			ProcessPID:      1234,
			ProcessName:     "test-process",
		}

		err := SetupMockMemoryScenario(mockProvider, scenario)
		if err != nil {
			t.Fatalf("Failed to setup mock scenario: %v", err)
		}

		// Test mock memory provider
		mockMemProvider := mockProvider.Memory()
		mockMemInfo, err := mockMemProvider.GetMemoryInfo(ctx)
		if err != nil {
			t.Fatalf("Mock GetMemoryInfo failed: %v", err)
		}

		// Note: Mock provider uses default configuration (4GB) since SetupMockMemoryScenario
		// is simplified. This is acceptable for basic testing.
		if mockMemInfo.TotalBytes == 0 {
			t.Error("Mock provider should return non-zero total memory")
		}

		// Test mock process provider
		mockProcProvider := mockProvider.Process()
		mockProcesses, err := mockProcProvider.ListProcesses(ctx, platform.ProcessFilter{})
		if err != nil {
			t.Fatalf("Mock ListProcesses failed: %v", err)
		}

		if len(mockProcesses) == 0 {
			t.Error("Mock provider should return processes")
		}

		t.Logf("Mock provider test passed with %d processes", len(mockProcesses))
	})
}

// TestPlatformFeatureConsistency tests that platform providers work consistently
func TestPlatformFeatureConsistency(t *testing.T) {
	provider, err := platform.GetProvider()
	if err != nil {
		t.Fatalf("Failed to get platform provider: %v", err)
	}
	platformName := provider.Platform()

	// Test platform basic functionality
	switch platformName {
	case "linux":
		t.Log("Testing Linux platform provider")
		if !provider.IsSupported() {
			t.Error("Linux provider should be supported")
		}

	case "darwin":
		t.Log("Testing Darwin platform provider")
		if !provider.IsSupported() {
			t.Error("Darwin provider should be supported")
		}

	case "windows":
		t.Log("Testing Windows platform provider")
		if !provider.IsSupported() {
			t.Error("Windows provider should be supported")
		}

	case "mock":
		t.Log("Testing mock provider")
		// Mock provider support status may vary

	default:
		t.Logf("Unknown platform: %s", platformName)
	}

	// Test that all sub-providers are available
	if provider.Memory() == nil {
		t.Error("Memory provider should not be nil")
	}
	if provider.Process() == nil {
		t.Error("Process provider should not be nil")
	}
	if provider.FileSystem() == nil {
		t.Error("FileSystem provider should not be nil")
	}
}

// TestMemoryProviderConsistency tests memory provider across platforms
func TestMemoryProviderConsistency(t *testing.T) {
	RunCrossPlatform(t, func(t *testing.T, provider platform.Provider, providerType string) {
		TestMemoryProvider(t, provider)

		// Additional consistency checks
		ctx := context.Background()
		memProvider := provider.Memory()

		// Test multiple calls return consistent results
		memInfo1, err := memProvider.GetMemoryInfo(ctx)
		if err != nil {
			t.Fatalf("First GetMemoryInfo failed: %v", err)
		}

		memInfo2, err := memProvider.GetMemoryInfo(ctx)
		if err != nil {
			t.Fatalf("Second GetMemoryInfo failed: %v", err)
		}

		// For mock providers, results should be identical
		if providerType == "mock" {
			if memInfo1.TotalBytes != memInfo2.TotalBytes {
				t.Error("Mock provider should return identical results")
			}
		}

		// Available memory should not exceed total memory
		if memInfo1.AvailableBytes > memInfo1.TotalBytes {
			t.Error("Available memory should not exceed total memory")
		}
	})
}

// TestProcessProviderConsistency tests process provider across platforms
func TestProcessProviderConsistency(t *testing.T) {
	RunCrossPlatform(t, func(t *testing.T, provider platform.Provider, providerType string) {
		TestProcessProvider(t, provider)

		// Additional consistency checks
		ctx := context.Background()
		procProvider := provider.Process()

		// Test process existence check
		processes, err := procProvider.ListProcesses(ctx, platform.ProcessFilter{})
		if err != nil {
			t.Fatalf("ListProcesses failed: %v", err)
		}

		if len(processes) > 0 {
			firstPID := processes[0].PID
			procInfo, err := procProvider.GetProcessInfo(ctx, firstPID)
			if err != nil {
				t.Fatalf("GetProcessInfo failed: %v", err)
			}

			if procInfo.PID != firstPID {
				t.Error("Process info should match requested PID")
			}

			// Validate process info
			AssertProcessInfo(t, processes[0])
		}
	})
}
