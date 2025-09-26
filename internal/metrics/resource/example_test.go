package resource_test

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/metrics/resource"
)

// ExampleNewResourceMonitor demonstrates basic resource monitoring setup
func ExampleNewResourceMonitor() {
	// Create a resource monitor with default configuration
	monitor, err := resource.NewResourceMonitor()
	if err != nil {
		log.Fatalf("Failed to create resource monitor: %v", err)
	}
	defer monitor.Close()

	// Get current memory statistics
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	memStats, err := monitor.GetMemoryStats(ctx)
	if err != nil {
		log.Fatalf("Failed to get memory stats: %v", err)
	}

	fmt.Printf("Memory Usage: %d MB\n", memStats.UsageBytes/(1024*1024))
	fmt.Printf("Memory Limit: %d MB\n", memStats.LimitBytes/(1024*1024))
	fmt.Printf("Memory Available: %d MB\n", memStats.AvailableBytes/(1024*1024))

	// Output (example):
	// Memory Usage: 2048 MB
	// Memory Limit: 8192 MB
	// Memory Available: 6144 MB
}

// ExampleResourceMonitor_GetCPUStats demonstrates CPU monitoring
func ExampleResourceMonitor_GetCPUStats() {
	monitor, err := resource.NewResourceMonitor()
	if err != nil {
		log.Fatalf("Failed to create resource monitor: %v", err)
	}
	defer monitor.Close()

	ctx := context.Background()

	// Get CPU statistics
	cpuStats, err := monitor.GetCPUStats(ctx)
	if err != nil {
		log.Fatalf("Failed to get CPU stats: %v", err)
	}

	fmt.Printf("CPU Usage: %.2f%%\n", cpuStats.UsagePercent)
	fmt.Printf("Available Cores: %.0f\n", cpuStats.AvailableCores)
	fmt.Printf("CPU Limit: %.0f cores\n", cpuStats.LimitCores)
	fmt.Printf("Limit Source: %s\n", cpuStats.LimitSource)

	// Output (example):
	// CPU Usage: 25.50%
	// Available Cores: 4
	// CPU Limit: 4 cores
	// Limit Source: system
}

// ExampleResourceMonitor_IsContainerized demonstrates container detection
func ExampleResourceMonitor_IsContainerized() {
	monitor, err := resource.NewResourceMonitor()
	if err != nil {
		log.Fatalf("Failed to create resource monitor: %v", err)
	}
	defer monitor.Close()

	if monitor.IsContainerized() {
		// Get detailed container information
		info := monitor.GetEnvironmentInfo()
		if info.Container != nil {
			fmt.Printf("Running in container\n")
			fmt.Printf("Runtime: %s\n", info.Container.Runtime)
			fmt.Printf("Container ID: %s\n", info.Container.ID)

			if info.Container.Orchestrator == "kubernetes" {
				fmt.Printf("Kubernetes Pod: %s\n", info.Container.PodName)
				fmt.Printf("Kubernetes Namespace: %s\n", info.Container.Namespace)
			}
		}
	} else {
		fmt.Println("Running on bare metal or VM")
	}

	// Output (when containerized):
	// Running in container
	// Runtime: docker
	// Container ID: a1b2c3d4e5f6
}

// ExampleNewResourceMonitorWithConfig demonstrates custom configuration
func ExampleNewResourceMonitorWithConfig() {
	// Create custom configuration
	config := &resource.Config{
		UpdateInterval:        500 * time.Millisecond, // Faster updates
		EnableCaching:         true,                   // Enable caching
		EnableDetailedIO:      true,                   // Detailed I/O stats
		EnableDetailedNetwork: true,                   // Detailed network stats
	}

	monitor, err := resource.NewResourceMonitorWithConfig(config)
	if err != nil {
		log.Fatalf("Failed to create resource monitor: %v", err)
	}
	defer monitor.Close()

	ctx := context.Background()

	// Get detailed I/O statistics
	ioStats, err := monitor.GetIOStats(ctx)
	if err != nil {
		log.Fatalf("Failed to get I/O stats: %v", err)
	}

	fmt.Printf("I/O Read: %d bytes\n", ioStats.ReadBytes)
	fmt.Printf("I/O Write: %d bytes\n", ioStats.WriteBytes)
	fmt.Printf("I/O Operations: %d read, %d write\n", ioStats.ReadOps, ioStats.WriteOps)

	// Per-device statistics (if enabled)
	if len(ioStats.Devices) > 0 {
		for device, stats := range ioStats.Devices {
			fmt.Printf("Device %s: %d read, %d write bytes\n",
				device, stats.ReadBytes, stats.WriteBytes)
		}
	}

	// Output (example):
	// I/O Read: 1048576 bytes
	// I/O Write: 2097152 bytes
	// I/O Operations: 256 read, 512 write
	// Device sda: 524288 read, 1048576 write bytes
}

// ExampleResourceMonitor_GetLimits demonstrates resource limits
func ExampleResourceMonitor_GetLimits() {
	monitor, err := resource.NewResourceMonitor()
	if err != nil {
		log.Fatalf("Failed to create resource monitor: %v", err)
	}
	defer monitor.Close()

	ctx := context.Background()

	// Get resource limits
	limits, err := monitor.GetLimits(ctx)
	if err != nil {
		log.Fatalf("Failed to get resource limits: %v", err)
	}

	// Memory limits
	if limits.Memory != nil {
		fmt.Printf("Memory Limit: %d MB (%s)\n",
			limits.Memory.LimitBytes/(1024*1024),
			limits.Memory.Source)

		if limits.Memory.SwapLimitBytes > 0 {
			fmt.Printf("Swap Limit: %d MB\n",
				limits.Memory.SwapLimitBytes/(1024*1024))
		}
	}

	// CPU limits
	if limits.CPU != nil {
		fmt.Printf("CPU Limit: %.0f cores (%s)\n",
			limits.CPU.LimitCores,
			limits.CPU.Source)

		if limits.CPU.Shares > 0 {
			fmt.Printf("CPU Shares: %d\n", limits.CPU.Shares)
		}

		if limits.CPU.QuotaUs > 0 && limits.CPU.QuotaPeriodUs > 0 {
			quota := float64(limits.CPU.QuotaUs) / float64(limits.CPU.QuotaPeriodUs)
			fmt.Printf("CPU Quota: %.2f cores\n", quota)
		}
	}

	// Output (example in container):
	// Memory Limit: 2048 MB (container)
	// CPU Limit: 2 cores (cgroup)
	// CPU Shares: 1024
	// CPU Quota: 2.00 cores
}

// ExampleResourceMonitor_GetEnvironmentInfo demonstrates environment detection
func ExampleResourceMonitor_GetEnvironmentInfo() {
	monitor, err := resource.NewResourceMonitor()
	if err != nil {
		log.Fatalf("Failed to create resource monitor: %v", err)
	}
	defer monitor.Close()

	info := monitor.GetEnvironmentInfo()

	fmt.Printf("Platform: %s\n", info.Platform)
	fmt.Printf("Architecture: %s\n", info.Architecture)

	// Host information
	if info.Host != nil {
		fmt.Printf("Hostname: %s\n", info.Host.Hostname)
		fmt.Printf("OS: %s %s\n", info.Host.OS, info.Host.OSVersion)
		fmt.Printf("Total Memory: %d MB\n", info.Host.TotalMemory/(1024*1024))
		fmt.Printf("Total Cores: %d\n", info.Host.TotalCores)
	}

	// Container information
	if info.Container != nil {
		fmt.Printf("Container Runtime: %s\n", info.Container.Runtime)
		fmt.Printf("Container Image: %s\n", info.Container.Image)
	}

	// Cgroup information (Linux)
	if info.Cgroup != nil {
		fmt.Printf("Cgroup Version: %s\n", info.Cgroup.Version.String())
		fmt.Printf("Cgroup Path: %s\n", info.Cgroup.Path)
		fmt.Printf("Controllers: %v\n", info.Cgroup.Controllers)
	}

	// Virtualization information
	if info.Virtualization != nil {
		fmt.Printf("Virtualization: %s\n", info.Virtualization.Type)
		if info.Virtualization.Hypervisor != "" {
			fmt.Printf("Hypervisor: %s\n", info.Virtualization.Hypervisor)
		}

		if info.Virtualization.Guest != nil {
			fmt.Printf("VM Memory Limit: %d MB\n",
				info.Virtualization.Guest.MemoryLimit/(1024*1024))
			fmt.Printf("VM CPU Limit: %d cores\n",
				info.Virtualization.Guest.CPULimit)
		}
	}

	// Output (example on macOS):
	// Platform: darwin
	// Architecture: arm64
	// Hostname: MacBook-Pro.local
	// OS: darwin 15.6.1
	// Total Memory: 16384 MB
	// Total Cores: 8
}

// ExampleResourceMonitor_continuousMonitoring demonstrates continuous resource monitoring
func ExampleResourceMonitor_continuousMonitoring() {
	monitor, err := resource.NewResourceMonitor()
	if err != nil {
		log.Fatalf("Failed to create resource monitor: %v", err)
	}
	defer monitor.Close()

	// Monitor resources for 10 seconds
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	fmt.Println("Starting continuous monitoring...")

	for {
		select {
		case <-ctx.Done():
			fmt.Println("Monitoring completed")
			return
		case <-ticker.C:
			// Get current statistics
			memStats, err := monitor.GetMemoryStats(ctx)
			if err != nil {
				log.Printf("Failed to get memory stats: %v", err)
				continue
			}

			cpuStats, err := monitor.GetCPUStats(ctx)
			if err != nil {
				log.Printf("Failed to get CPU stats: %v", err)
				continue
			}

			// Calculate memory usage percentage
			memUsagePct := float64(memStats.UsageBytes) / float64(memStats.LimitBytes) * 100

			fmt.Printf("[%s] Memory: %.1f%% (%d MB), CPU: %.1f%%\n",
				time.Now().Format("15:04:05"),
				memUsagePct,
				memStats.UsageBytes/(1024*1024),
				cpuStats.UsagePercent)
		}
	}

	// Output (example):
	// Starting continuous monitoring...
	// [15:30:01] Memory: 45.5% (3641 MB), CPU: 12.3%
	// [15:30:02] Memory: 45.6% (3645 MB), CPU: 15.7%
	// [15:30:03] Memory: 45.4% (3635 MB), CPU: 8.9%
	// ...
	// Monitoring completed
}

// ExampleResourceMonitor_linuxCgroupSpecific demonstrates Linux-specific cgroup features
func ExampleResourceMonitor_linuxCgroupSpecific() {
	// Skip on non-Linux platforms
	if runtime.GOOS != "linux" {
		fmt.Println("Linux-specific example, skipping on", runtime.GOOS)
		return
	}

	monitor, err := resource.NewResourceMonitor()
	if err != nil {
		log.Fatalf("Failed to create resource monitor: %v", err)
	}
	defer monitor.Close()

	ctx := context.Background()

	// Get memory statistics with cgroup v2 pressure information
	memStats, err := monitor.GetMemoryStats(ctx)
	if err != nil {
		log.Fatalf("Failed to get memory stats: %v", err)
	}

	// Display memory pressure information (cgroup v2 only)
	if memStats.Pressure != nil {
		if memStats.Pressure.Some != nil {
			fmt.Printf("Memory Pressure (some): avg10=%.2f%%, avg60=%.2f%%, avg300=%.2f%%\n",
				memStats.Pressure.Some.Avg10,
				memStats.Pressure.Some.Avg60,
				memStats.Pressure.Some.Avg300)
		}

		if memStats.Pressure.Full != nil {
			fmt.Printf("Memory Pressure (full): avg10=%.2f%%, avg60=%.2f%%, avg300=%.2f%%\n",
				memStats.Pressure.Full.Avg10,
				memStats.Pressure.Full.Avg60,
				memStats.Pressure.Full.Avg300)
		}
	}

	// Get CPU statistics with throttling information
	cpuStats, err := monitor.GetCPUStats(ctx)
	if err != nil {
		log.Fatalf("Failed to get CPU stats: %v", err)
	}

	// Display CPU throttling information
	if cpuStats.Throttling != nil {
		throttleRatio := float64(cpuStats.Throttling.ThrottledPeriods) /
			float64(cpuStats.Throttling.TotalPeriods) * 100

		fmt.Printf("CPU Throttling: %.2f%% (%d/%d periods), Time: %v\n",
			throttleRatio,
			cpuStats.Throttling.ThrottledPeriods,
			cpuStats.Throttling.TotalPeriods,
			cpuStats.Throttling.ThrottledTime)
	}

	// Output (example in constrained container):
	// Memory Pressure (some): avg10=2.45%, avg60=1.87%, avg300=0.92%
	// CPU Throttling: 15.30% (153/1000 periods), Time: 2.5s
}
