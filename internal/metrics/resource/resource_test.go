package resource

import (
	"context"
	"runtime"
	"testing"
	"time"
)

func TestNewResourceMonitor(t *testing.T) {
	monitor, err := NewResourceMonitor()
	if err != nil {
		t.Fatalf("Failed to create resource monitor: %v", err)
	}
	defer monitor.Close()

	if monitor == nil {
		t.Fatal("Resource monitor should not be nil")
	}
}

func TestResourceMonitorMemoryStats(t *testing.T) {
	monitor, err := NewResourceMonitor()
	if err != nil {
		t.Fatalf("Failed to create resource monitor: %v", err)
	}
	defer monitor.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stats, err := monitor.GetMemoryStats(ctx)
	if err != nil {
		t.Fatalf("Failed to get memory stats: %v", err)
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

	if stats.AvailableBytes < 0 {
		t.Errorf("Available memory should be non-negative, got %d", stats.AvailableBytes)
	}

	if stats.Timestamp.IsZero() {
		t.Error("Memory stats timestamp should not be zero")
	}

	// Validate limit source
	validSources := []LimitSource{
		LimitSourceSystem, LimitSourceContainer, LimitSourceCgroup, LimitSourceVM, LimitSourceK8s,
	}
	found := false
	for _, source := range validSources {
		if stats.LimitSource == source {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Invalid limit source: %s", stats.LimitSource)
	}
}

func TestResourceMonitorCPUStats(t *testing.T) {
	monitor, err := NewResourceMonitor()
	if err != nil {
		t.Fatalf("Failed to create resource monitor: %v", err)
	}
	defer monitor.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stats, err := monitor.GetCPUStats(ctx)
	if err != nil {
		t.Fatalf("Failed to get CPU stats: %v", err)
	}

	if stats == nil {
		t.Fatal("CPU stats should not be nil")
	}

	if stats.UsagePercent < 0 || stats.UsagePercent > 1000 {
		t.Errorf("CPU usage percentage should be reasonable, got %.2f", stats.UsagePercent)
	}

	if stats.AvailableCores <= 0 {
		t.Errorf("Available cores should be positive, got %.2f", stats.AvailableCores)
	}

	if stats.LimitCores <= 0 {
		t.Errorf("CPU limit should be positive, got %.2f", stats.LimitCores)
	}

	if stats.Timestamp.IsZero() {
		t.Error("CPU stats timestamp should not be zero")
	}

	// Should match system CPU count for non-containerized environments
	systemCPUs := runtime.NumCPU()
	if !monitor.IsContainerized() && int(stats.LimitCores) != systemCPUs {
		t.Logf("Warning: CPU limit %.0f doesn't match system CPUs %d (may be normal in virtualized environment)", stats.LimitCores, systemCPUs)
	}
}

func TestResourceMonitorIOStats(t *testing.T) {
	monitor, err := NewResourceMonitor()
	if err != nil {
		t.Fatalf("Failed to create resource monitor: %v", err)
	}
	defer monitor.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stats, err := monitor.GetIOStats(ctx)
	if err != nil {
		t.Fatalf("Failed to get I/O stats: %v", err)
	}

	if stats == nil {
		t.Fatal("I/O stats should not be nil")
	}

	// I/O stats can be zero on systems without activity
	if stats.ReadBytes < 0 {
		t.Errorf("Read bytes should be non-negative, got %d", stats.ReadBytes)
	}

	if stats.WriteBytes < 0 {
		t.Errorf("Write bytes should be non-negative, got %d", stats.WriteBytes)
	}

	if stats.Timestamp.IsZero() {
		t.Error("I/O stats timestamp should not be zero")
	}
}

func TestResourceMonitorNetworkStats(t *testing.T) {
	monitor, err := NewResourceMonitor()
	if err != nil {
		t.Fatalf("Failed to create resource monitor: %v", err)
	}
	defer monitor.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stats, err := monitor.GetNetworkStats(ctx)
	if err != nil {
		t.Fatalf("Failed to get network stats: %v", err)
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

	if stats.Timestamp.IsZero() {
		t.Error("Network stats timestamp should not be zero")
	}
}

func TestResourceMonitorLimits(t *testing.T) {
	monitor, err := NewResourceMonitor()
	if err != nil {
		t.Fatalf("Failed to create resource monitor: %v", err)
	}
	defer monitor.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	limits, err := monitor.GetLimits(ctx)
	if err != nil {
		t.Fatalf("Failed to get resource limits: %v", err)
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
	}

	if limits.CPU == nil {
		t.Error("CPU limits should not be nil")
	} else {
		if limits.CPU.LimitCores <= 0 {
			t.Errorf("CPU limit should be positive, got %.2f", limits.CPU.LimitCores)
		}
	}
}

func TestResourceMonitorEnvironmentInfo(t *testing.T) {
	monitor, err := NewResourceMonitor()
	if err != nil {
		t.Fatalf("Failed to create resource monitor: %v", err)
	}
	defer monitor.Close()

	info := monitor.GetEnvironmentInfo()
	if info == nil {
		t.Fatal("Environment info should not be nil")
	}

	if info.Platform == "" {
		t.Error("Platform should not be empty")
	}

	if info.Architecture == "" {
		t.Error("Architecture should not be empty")
	}

	if info.Host == nil {
		t.Error("Host info should not be nil")
	} else {
		if info.Host.Hostname == "" {
			t.Error("Hostname should not be empty")
		}
		if info.Host.OS == "" {
			t.Error("OS should not be empty")
		}
		if info.Host.TotalMemory <= 0 {
			t.Errorf("Total memory should be positive, got %d", info.Host.TotalMemory)
		}
		if info.Host.TotalCores <= 0 {
			t.Errorf("Total cores should be positive, got %d", info.Host.TotalCores)
		}
	}
}

func TestResourceMonitorPlatformSpecific(t *testing.T) {
	monitor, err := NewResourceMonitor()
	if err != nil {
		t.Fatalf("Failed to create resource monitor: %v", err)
	}
	defer monitor.Close()

	info := monitor.GetEnvironmentInfo()

	switch runtime.GOOS {
	case "linux":
		// On Linux, should have cgroup information
		if info.Cgroup == nil {
			t.Log("Warning: No cgroup info detected on Linux (may be normal)")
		} else {
			if info.Cgroup.Version == CgroupVersionUnknown {
				t.Error("Cgroup version should be detected on Linux")
			}
			if info.Cgroup.Path == "" {
				t.Error("Cgroup path should not be empty")
			}
		}

	case "darwin":
		// On macOS, check for Apple Virtualization
		if info.Virtualization != nil {
			if info.Virtualization.Type == VirtualizationTypeVZ {
				t.Log("Apple Virtualization (VZ) detected")
			}
		}
	}
}

func TestContainerDetection(t *testing.T) {
	monitor, err := NewResourceMonitor()
	if err != nil {
		t.Fatalf("Failed to create resource monitor: %v", err)
	}
	defer monitor.Close()

	isContainerized := monitor.IsContainerized()
	info := monitor.GetEnvironmentInfo()

	if isContainerized {
		if info.Container == nil {
			t.Error("Container info should not be nil when containerized")
		} else {
			if info.Container.Runtime == ContainerRuntimeUnknown {
				t.Error("Container runtime should be detected")
			}
		}
	} else {
		t.Log("Running on bare metal or VM")
	}
}

func TestResourceMonitorConfig(t *testing.T) {
	config := DefaultConfig()
	if config == nil {
		t.Fatal("Default config should not be nil")
	}

	if config.UpdateInterval <= 0 {
		t.Error("Update interval should be positive")
	}

	if len(config.ContainerPaths) == 0 {
		t.Error("Container paths should not be empty")
	}

	if len(config.CgroupPaths) == 0 {
		t.Error("Cgroup paths should not be empty")
	}

	monitor, err := NewResourceMonitorWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create resource monitor with config: %v", err)
	}
	defer monitor.Close()

	if monitor == nil {
		t.Fatal("Resource monitor should not be nil")
	}
}

func TestLimitSourceString(t *testing.T) {
	tests := []struct {
		source   LimitSource
		expected string
	}{
		{LimitSourceSystem, "system"},
		{LimitSourceContainer, "container"},
		{LimitSourceCgroup, "cgroup"},
		{LimitSourceVM, "vm"},
		{LimitSourceK8s, "kubernetes"},
		{LimitSourceUnknown, "unknown"},
	}

	for _, test := range tests {
		if test.source.String() != test.expected {
			t.Errorf("Expected %s, got %s", test.expected, test.source.String())
		}
	}
}

func TestContainerRuntimeString(t *testing.T) {
	tests := []struct {
		runtime  ContainerRuntime
		expected string
	}{
		{ContainerRuntimeDocker, "docker"},
		{ContainerRuntimePodman, "podman"},
		{ContainerRuntimeContainerd, "containerd"},
		{ContainerRuntimeCRIO, "cri-o"},
		{ContainerRuntimeSystemd, "systemd"},
		{ContainerRuntimeLXC, "lxc"},
		{ContainerRuntimeUnknown, "unknown"},
	}

	for _, test := range tests {
		if test.runtime.String() != test.expected {
			t.Errorf("Expected %s, got %s", test.expected, test.runtime.String())
		}
	}
}

func TestCgroupVersionString(t *testing.T) {
	tests := []struct {
		version  CgroupVersion
		expected string
	}{
		{CgroupVersion1, "v1"},
		{CgroupVersion2, "v2"},
		{CgroupVersionUnknown, "unknown"},
	}

	for _, test := range tests {
		if test.version.String() != test.expected {
			t.Errorf("Expected %s, got %s", test.expected, test.version.String())
		}
	}
}

// Benchmark tests
func BenchmarkMemoryStats(b *testing.B) {
	monitor, err := NewResourceMonitor()
	if err != nil {
		b.Fatalf("Failed to create resource monitor: %v", err)
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

func BenchmarkCPUStats(b *testing.B) {
	monitor, err := NewResourceMonitor()
	if err != nil {
		b.Fatalf("Failed to create resource monitor: %v", err)
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
