//go:build darwin

package resource

import (
	"context"
	"runtime"
	"testing"
	"time"
)

func TestDarwinResourceMonitor(t *testing.T) {
	monitor, err := newPlatformMonitor()
	if err != nil {
		t.Fatalf("Failed to create Darwin resource monitor: %v", err)
	}
	defer monitor.Close()

	if monitor == nil {
		t.Fatal("Darwin resource monitor should not be nil")
	}
}

func TestDarwinMemoryStats(t *testing.T) {
	monitor, err := newPlatformMonitor()
	if err != nil {
		t.Fatalf("Failed to create Darwin resource monitor: %v", err)
	}
	defer monitor.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stats, err := monitor.GetMemoryStats(ctx)
	if err != nil {
		t.Fatalf("Failed to get Darwin memory stats: %v", err)
	}

	if stats == nil {
		t.Fatal("Memory stats should not be nil")
	}

	if stats.UsageBytes <= 0 {
		t.Errorf("Memory usage should be positive, got %d", stats.UsageBytes)
	}

	if stats.LimitBytes <= 0 {
		t.Errorf("Memory limit should be positive, got %d", stats.LimitBytes)
	}

	// On macOS, limit source should be system or VM
	if stats.LimitSource != LimitSourceSystem && stats.LimitSource != LimitSourceVM {
		t.Errorf("Expected system or VM limit source on macOS, got %s", stats.LimitSource)
	}

	t.Logf("Darwin memory - Usage: %d MB, Limit: %d MB, Source: %s",
		stats.UsageBytes/(1024*1024),
		stats.LimitBytes/(1024*1024),
		stats.LimitSource)
}

func TestDarwinCPUStats(t *testing.T) {
	monitor, err := newPlatformMonitor()
	if err != nil {
		t.Fatalf("Failed to create Darwin resource monitor: %v", err)
	}
	defer monitor.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stats, err := monitor.GetCPUStats(ctx)
	if err != nil {
		t.Fatalf("Failed to get Darwin CPU stats: %v", err)
	}

	if stats == nil {
		t.Fatal("CPU stats should not be nil")
	}

	if stats.UsagePercent < 0 || stats.UsagePercent > 1000 {
		t.Errorf("CPU usage percentage should be reasonable, got %.2f", stats.UsagePercent)
	}

	expectedCores := float64(runtime.NumCPU())
	if stats.AvailableCores != expectedCores {
		t.Errorf("Available cores should match runtime.NumCPU(), expected %.0f, got %.2f",
			expectedCores, stats.AvailableCores)
	}

	if stats.LimitCores <= 0 {
		t.Errorf("CPU limit should be positive, got %.2f", stats.LimitCores)
	}

	// On non-virtualized macOS, should match system CPU count
	if stats.LimitSource == LimitSourceSystem && int(stats.LimitCores) != runtime.NumCPU() {
		t.Errorf("CPU limit should match system CPUs, expected %d, got %.0f",
			runtime.NumCPU(), stats.LimitCores)
	}

	t.Logf("Darwin CPU - Usage: %.2f%%, Available: %.0f cores, Limit: %.0f cores, Source: %s",
		stats.UsagePercent, stats.AvailableCores, stats.LimitCores, stats.LimitSource)
}

func TestDarwinIOStats(t *testing.T) {
	monitor, err := newPlatformMonitor()
	if err != nil {
		t.Fatalf("Failed to create Darwin resource monitor: %v", err)
	}
	defer monitor.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stats, err := monitor.GetIOStats(ctx)
	if err != nil {
		t.Fatalf("Failed to get Darwin I/O stats: %v", err)
	}

	if stats == nil {
		t.Fatal("I/O stats should not be nil")
	}

	if stats.ReadBytes < 0 {
		t.Errorf("Read bytes should be non-negative, got %d", stats.ReadBytes)
	}

	if stats.WriteBytes < 0 {
		t.Errorf("Write bytes should be non-negative, got %d", stats.WriteBytes)
	}

	t.Logf("Darwin I/O - Read: %d bytes, Write: %d bytes", stats.ReadBytes, stats.WriteBytes)
}

func TestDarwinNetworkStats(t *testing.T) {
	monitor, err := newPlatformMonitor()
	if err != nil {
		t.Fatalf("Failed to create Darwin resource monitor: %v", err)
	}
	defer monitor.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stats, err := monitor.GetNetworkStats(ctx)
	if err != nil {
		t.Fatalf("Failed to get Darwin network stats: %v", err)
	}

	if stats == nil {
		t.Fatal("Network stats should not be nil")
	}

	if stats.RxBytes < 0 {
		t.Errorf("RX bytes should be non-negative, got %d", stats.RxBytes)
	}

	if stats.TxBytes < 0 {
		t.Errorf("TX bytes should be non-negative, got %d", stats.TxBytes)
	}

	t.Logf("Darwin network - RX: %d bytes, TX: %d bytes", stats.RxBytes, stats.TxBytes)
}

func TestDarwinVirtualizationDetection(t *testing.T) {
	monitor, err := newPlatformMonitor()
	if err != nil {
		t.Fatalf("Failed to create Darwin resource monitor: %v", err)
	}
	defer monitor.Close()

	info := monitor.GetEnvironmentInfo()
	if info == nil {
		t.Fatal("Environment info should not be nil")
	}

	t.Logf("Platform: %s, Architecture: %s", info.Platform, info.Architecture)

	if info.Virtualization != nil {
		t.Logf("Virtualization detected - Type: %s, Hypervisor: %s",
			info.Virtualization.Type, info.Virtualization.Hypervisor)

		if info.Virtualization.Type == VirtualizationTypeVZ {
			t.Log("Running under Apple Virtualization Framework (VZ)")

			if info.Virtualization.Guest != nil {
				t.Logf("Guest limits - Memory: %d bytes, CPU: %d cores",
					info.Virtualization.Guest.MemoryLimit,
					info.Virtualization.Guest.CPULimit)
			}
		}
	} else {
		t.Log("No virtualization detected (running on bare metal)")
	}
}

func TestDarwinHostInfo(t *testing.T) {
	monitor, err := newPlatformMonitor()
	if err != nil {
		t.Fatalf("Failed to create Darwin resource monitor: %v", err)
	}
	defer monitor.Close()

	info := monitor.GetEnvironmentInfo()
	if info.Host == nil {
		t.Fatal("Host info should not be nil")
	}

	if info.Host.Hostname == "" {
		t.Error("Hostname should not be empty")
	}

	if info.Host.OS == "" {
		t.Error("OS should not be empty")
	}

	if info.Host.OSVersion == "" {
		t.Error("OS version should not be empty")
	}

	if info.Host.KernelVersion == "" {
		t.Error("Kernel version should not be empty")
	}

	if info.Host.TotalMemory <= 0 {
		t.Errorf("Total memory should be positive, got %d", info.Host.TotalMemory)
	}

	if info.Host.TotalCores <= 0 {
		t.Errorf("Total cores should be positive, got %d", info.Host.TotalCores)
	}

	expectedCores := runtime.NumCPU()
	if info.Host.TotalCores != expectedCores {
		t.Errorf("Total cores should match runtime.NumCPU(), expected %d, got %d",
			expectedCores, info.Host.TotalCores)
	}

	t.Logf("Darwin host - OS: %s %s, Kernel: %s, Memory: %d MB, Cores: %d",
		info.Host.OS, info.Host.OSVersion, info.Host.KernelVersion,
		info.Host.TotalMemory/(1024*1024), info.Host.TotalCores)
}

func TestDarwinContainerDetection(t *testing.T) {
	monitor, err := newPlatformMonitor()
	if err != nil {
		t.Fatalf("Failed to create Darwin resource monitor: %v", err)
	}
	defer monitor.Close()

	// macOS generally doesn't run in containers, but check anyway
	isContainerized := monitor.IsContainerized()
	if isContainerized {
		t.Log("Container detected on macOS (unusual but possible)")

		info := monitor.GetEnvironmentInfo()
		if info.Container != nil {
			t.Logf("Container info - Runtime: %s, ID: %s",
				info.Container.Runtime, info.Container.ID)
		}
	} else {
		t.Log("No container detected (expected on macOS)")
	}
}

func TestDarwinResourceLimits(t *testing.T) {
	monitor, err := newPlatformMonitor()
	if err != nil {
		t.Fatalf("Failed to create Darwin resource monitor: %v", err)
	}
	defer monitor.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	limits, err := monitor.GetLimits(ctx)
	if err != nil {
		t.Fatalf("Failed to get Darwin resource limits: %v", err)
	}

	if limits == nil {
		t.Fatal("Resource limits should not be nil")
	}

	if limits.Memory == nil {
		t.Error("Memory limits should not be nil")
	} else {
		if limits.Memory.LimitBytes <= 0 {
			t.Errorf("Memory limit should be positive, got %d", limits.Memory.LimitBytes)
		}

		// On macOS, source should be system or VM
		if limits.Memory.Source != LimitSourceSystem && limits.Memory.Source != LimitSourceVM {
			t.Errorf("Expected system or VM memory limit source, got %s", limits.Memory.Source)
		}
	}

	if limits.CPU == nil {
		t.Error("CPU limits should not be nil")
	} else {
		if limits.CPU.LimitCores <= 0 {
			t.Errorf("CPU limit should be positive, got %.2f", limits.CPU.LimitCores)
		}

		// On macOS, source should be system or VM
		if limits.CPU.Source != LimitSourceSystem && limits.CPU.Source != LimitSourceVM {
			t.Errorf("Expected system or VM CPU limit source, got %s", limits.CPU.Source)
		}
	}

	t.Logf("Darwin limits - Memory: %d MB (%s), CPU: %.0f cores (%s)",
		limits.Memory.LimitBytes/(1024*1024), limits.Memory.Source,
		limits.CPU.LimitCores, limits.CPU.Source)
}

// Benchmark tests for Darwin
func BenchmarkDarwinMemoryStats(b *testing.B) {
	monitor, err := newPlatformMonitor()
	if err != nil {
		b.Fatalf("Failed to create Darwin resource monitor: %v", err)
	}
	defer monitor.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := monitor.GetMemoryStats(ctx)
		if err != nil {
			b.Fatalf("Failed to get memory stats: %v", err)
		}
	}
}

func BenchmarkDarwinCPUStats(b *testing.B) {
	monitor, err := newPlatformMonitor()
	if err != nil {
		b.Fatalf("Failed to create Darwin resource monitor: %v", err)
	}
	defer monitor.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := monitor.GetCPUStats(ctx)
		if err != nil {
			b.Fatalf("Failed to get CPU stats: %v", err)
		}
	}
}
