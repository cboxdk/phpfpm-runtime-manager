//go:build linux

package resource

import (
	"os"
	"testing"
)

func TestCgroupManager(t *testing.T) {
	// Check if we're in a cgroup environment
	if _, err := os.Stat("/sys/fs/cgroup"); os.IsNotExist(err) {
		t.Skip("Skipping cgroup tests: /sys/fs/cgroup not found")
	}

	manager, err := NewCgroupManager()
	if err != nil {
		t.Fatalf("Failed to create cgroup manager: %v", err)
	}

	if manager == nil {
		t.Fatal("Cgroup manager should not be nil")
	}
}

func TestCgroupVersion(t *testing.T) {
	if _, err := os.Stat("/sys/fs/cgroup"); os.IsNotExist(err) {
		t.Skip("Skipping cgroup tests: /sys/fs/cgroup not found")
	}

	manager, err := NewCgroupManager()
	if err != nil {
		t.Fatalf("Failed to create cgroup manager: %v", err)
	}

	info := manager.GetInfo()
	if info.Version != CgroupVersion1 && info.Version != CgroupVersion2 {
		t.Errorf("Expected cgroup v1 or v2, got %v", info.Version)
	}

	t.Logf("Detected cgroup %s", info.Version.String())
}

func TestCgroupMemoryStats(t *testing.T) {
	if _, err := os.Stat("/sys/fs/cgroup"); os.IsNotExist(err) {
		t.Skip("Skipping cgroup tests: /sys/fs/cgroup not found")
	}

	manager, err := NewCgroupManager()
	if err != nil {
		t.Fatalf("Failed to create cgroup manager: %v", err)
	}

	stats, err := manager.GetMemoryStats()
	if err != nil {
		t.Logf("Memory stats not available (may be normal): %v", err)
		return
	}

	if stats == nil {
		t.Fatal("Memory stats should not be nil")
	}

	if stats.UsageBytes < 0 {
		t.Errorf("Memory usage should be non-negative, got %d", stats.UsageBytes)
	}

	if stats.LimitBytes < 0 {
		t.Errorf("Memory limit should be non-negative, got %d", stats.LimitBytes)
	}

	t.Logf("Cgroup memory - Usage: %d bytes, Limit: %d bytes", stats.UsageBytes, stats.LimitBytes)
}

func TestCgroupCPUStats(t *testing.T) {
	if _, err := os.Stat("/sys/fs/cgroup"); os.IsNotExist(err) {
		t.Skip("Skipping cgroup tests: /sys/fs/cgroup not found")
	}

	manager, err := NewCgroupManager()
	if err != nil {
		t.Fatalf("Failed to create cgroup manager: %v", err)
	}

	stats, err := manager.GetCPUStats()
	if err != nil {
		t.Logf("CPU stats not available (may be normal): %v", err)
		return
	}

	if stats == nil {
		t.Fatal("CPU stats should not be nil")
	}

	if stats.LimitCores < 0 {
		t.Errorf("CPU limit should be non-negative, got %.2f", stats.LimitCores)
	}

	if stats.Shares < 0 {
		t.Errorf("CPU shares should be non-negative, got %d", stats.Shares)
	}

	t.Logf("Cgroup CPU - Limit: %.2f cores, Shares: %d", stats.LimitCores, stats.Shares)
}

func TestCgroupIOStats(t *testing.T) {
	if _, err := os.Stat("/sys/fs/cgroup"); os.IsNotExist(err) {
		t.Skip("Skipping cgroup tests: /sys/fs/cgroup not found")
	}

	manager, err := NewCgroupManager()
	if err != nil {
		t.Fatalf("Failed to create cgroup manager: %v", err)
	}

	stats, err := manager.GetIOStats()
	if err != nil {
		t.Logf("I/O stats not available (may be normal): %v", err)
		return
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

	t.Logf("Cgroup I/O - Read: %d bytes, Write: %d bytes", stats.ReadBytes, stats.WriteBytes)
}

func TestCgroupV2Features(t *testing.T) {
	if _, err := os.Stat("/sys/fs/cgroup"); os.IsNotExist(err) {
		t.Skip("Skipping cgroup tests: /sys/fs/cgroup not found")
	}

	manager, err := NewCgroupManager()
	if err != nil {
		t.Fatalf("Failed to create cgroup manager: %v", err)
	}

	info := manager.GetInfo()
	if info.Version != CgroupVersion2 {
		t.Skip("Skipping cgroup v2 specific tests")
	}

	// Test memory pressure information
	memStats, err := manager.GetMemoryStats()
	if err == nil && memStats != nil && memStats.Pressure != nil {
		if memStats.Pressure.Some != nil {
			t.Logf("Memory pressure (some): avg10=%.2f, avg60=%.2f, avg300=%.2f",
				memStats.Pressure.Some.Avg10,
				memStats.Pressure.Some.Avg60,
				memStats.Pressure.Some.Avg300)
		}
	}

	// Test CPU throttling information
	cpuStats, err := manager.GetCPUStats()
	if err == nil && cpuStats != nil && cpuStats.Throttling != nil {
		t.Logf("CPU throttling - periods: %d, throttled: %d, time: %v",
			cpuStats.Throttling.TotalPeriods,
			cpuStats.Throttling.ThrottledPeriods,
			cpuStats.Throttling.ThrottledTime)
	}
}

func TestCgroupPath(t *testing.T) {
	if _, err := os.Stat("/proc/self/cgroup"); os.IsNotExist(err) {
		t.Skip("Skipping cgroup tests: /proc/self/cgroup not found")
	}

	manager, err := NewCgroupManager()
	if err != nil {
		t.Fatalf("Failed to create cgroup manager: %v", err)
	}

	info := manager.GetInfo()
	if info.Path == "" {
		t.Error("Cgroup path should not be empty")
	}

	t.Logf("Cgroup path: %s", info.Path)
}

func TestCgroupControllers(t *testing.T) {
	if _, err := os.Stat("/sys/fs/cgroup"); os.IsNotExist(err) {
		t.Skip("Skipping cgroup tests: /sys/fs/cgroup not found")
	}

	manager, err := NewCgroupManager()
	if err != nil {
		t.Fatalf("Failed to create cgroup manager: %v", err)
	}

	info := manager.GetInfo()
	if len(info.Controllers) == 0 {
		t.Log("No cgroup controllers found (may be normal)")
		return
	}

	t.Logf("Available controllers: %v", info.Controllers)

	// Common controllers we expect
	expectedControllers := []string{"memory", "cpu", "io"}
	for _, expected := range expectedControllers {
		found := false
		for _, controller := range info.Controllers {
			if controller == expected {
				found = true
				break
			}
		}
		if !found {
			t.Logf("Controller %s not found (may be normal)", expected)
		}
	}
}

func TestContainerDetectionLinux(t *testing.T) {
	detector := NewContainerDetector()
	if detector == nil {
		t.Fatal("Container detector should not be nil")
	}

	info := detector.DetectContainer()
	if info == nil {
		t.Fatal("Container info should not be nil")
	}

	t.Logf("Container runtime: %s", info.Runtime)

	if info.Runtime != ContainerRuntimeUnknown {
		t.Logf("Container detected - Runtime: %s, ID: %s, Name: %s",
			info.Runtime, info.ID, info.Name)

		if info.Orchestrator != "" {
			t.Logf("Orchestrator: %s", info.Orchestrator)
		}

		if info.PodName != "" {
			t.Logf("Pod: %s, Namespace: %s", info.PodName, info.Namespace)
		}
	} else {
		t.Log("No container runtime detected (running on bare metal or VM)")
	}

	// Test caching
	info2 := detector.DetectContainer()
	if info.Runtime != info2.Runtime {
		t.Error("Container detection should be cached and consistent")
	}
}

func BenchmarkCgroupMemoryStats(b *testing.B) {
	if _, err := os.Stat("/sys/fs/cgroup"); os.IsNotExist(err) {
		b.Skip("Skipping cgroup benchmark: /sys/fs/cgroup not found")
	}

	manager, err := NewCgroupManager()
	if err != nil {
		b.Fatalf("Failed to create cgroup manager: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := manager.GetMemoryStats()
		if err != nil {
			b.Fatalf("Failed to get memory stats: %v", err)
		}
	}
}

func BenchmarkContainerDetection(b *testing.B) {
	detector := NewContainerDetector()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		detector.DetectContainer()
	}
}
