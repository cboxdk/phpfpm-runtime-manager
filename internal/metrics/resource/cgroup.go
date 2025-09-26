package resource

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// CgroupManager handles cgroup v1 and v2 interactions
type CgroupManager struct {
	version       CgroupVersion
	mountPoint    string
	cgroupPath    string
	controllers   []string
	hierarchyInfo map[string]string
}

// CgroupMemoryStats represents memory statistics from cgroups
type CgroupMemoryStats struct {
	UsageBytes     int64
	LimitBytes     int64
	RSSBytes       int64
	CacheBytes     int64
	SwapUsageBytes int64
	SwapLimitBytes int64
	Pressure       *MemoryPressure
}

// CgroupCPUStats represents CPU statistics from cgroups
type CgroupCPUStats struct {
	LimitCores    float64
	Shares        int64
	QuotaPeriodUs int64
	QuotaUs       int64
	Throttling    *CPUThrottling
}

// CgroupIOStats represents I/O statistics from cgroups
type CgroupIOStats struct {
	ReadBytes  int64
	WriteBytes int64
	ReadOps    int64
	WriteOps   int64
}

// NewCgroupManager creates a new cgroup manager
func NewCgroupManager() (*CgroupManager, error) {
	cm := &CgroupManager{
		hierarchyInfo: make(map[string]string),
	}

	// Detect cgroup version and mount point
	if err := cm.detectCgroupVersion(); err != nil {
		return nil, err
	}

	// Get current process cgroup path
	if err := cm.getCurrentCgroupPath(); err != nil {
		return nil, err
	}

	// Get available controllers
	if err := cm.getAvailableControllers(); err != nil {
		return nil, err
	}

	return cm, nil
}

// detectCgroupVersion detects whether system is using cgroup v1 or v2
func (cm *CgroupManager) detectCgroupVersion() error {
	// Check for cgroup v2 first
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err == nil {
		// Check if this is a unified hierarchy (cgroup v2)
		if data, err := os.ReadFile("/sys/fs/cgroup/cgroup.controllers"); err == nil && len(data) > 0 {
			cm.version = CgroupVersion2
			cm.mountPoint = "/sys/fs/cgroup"
			return nil
		}
	}

	// Check for cgroup v1
	if _, err := os.Stat("/proc/cgroups"); err == nil {
		// Parse /proc/cgroups to find mount points
		if err := cm.parseCgroupV1MountPoints(); err == nil {
			cm.version = CgroupVersion1
			return nil
		}
	}

	return ErrCgroupNotFound
}

// parseCgroupV1MountPoints parses cgroup v1 mount points
func (cm *CgroupManager) parseCgroupV1MountPoints() error {
	file, err := os.Open("/proc/mounts")
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) >= 3 && parts[2] == "cgroup" {
			cm.mountPoint = parts[1]
			return nil
		}
	}

	// Default to /sys/fs/cgroup for cgroup v1
	if _, err := os.Stat("/sys/fs/cgroup"); err == nil {
		cm.mountPoint = "/sys/fs/cgroup"
		return nil
	}

	return ErrCgroupNotFound
}

// getCurrentCgroupPath gets the current process's cgroup path
func (cm *CgroupManager) getCurrentCgroupPath() error {
	file, err := os.Open("/proc/self/cgroup")
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}

		hierarchyID := parts[0]
		controllers := parts[1]
		path := parts[2]

		if cm.version == CgroupVersion2 {
			// For cgroup v2, hierarchy ID is 0
			if hierarchyID == "0" {
				cm.cgroupPath = path
				return nil
			}
		} else {
			// For cgroup v1, store hierarchy info
			cm.hierarchyInfo[controllers] = path
			if controllers == "" || strings.Contains(controllers, "memory") {
				cm.cgroupPath = path
			}
		}
	}

	if cm.cgroupPath == "" {
		cm.cgroupPath = "/"
	}

	return nil
}

// getAvailableControllers gets available cgroup controllers
func (cm *CgroupManager) getAvailableControllers() error {
	if cm.version == CgroupVersion2 {
		return cm.getCgroupV2Controllers()
	}
	return cm.getCgroupV1Controllers()
}

// getCgroupV2Controllers gets controllers for cgroup v2
func (cm *CgroupManager) getCgroupV2Controllers() error {
	controllerPath := filepath.Join(cm.mountPoint, "cgroup.controllers")
	data, err := os.ReadFile(controllerPath)
	if err != nil {
		return err
	}

	cm.controllers = strings.Fields(string(data))
	return nil
}

// getCgroupV1Controllers gets controllers for cgroup v1
func (cm *CgroupManager) getCgroupV1Controllers() error {
	file, err := os.Open("/proc/cgroups")
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Skip header
	scanner.Scan()

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) >= 4 {
			controller := parts[0]
			enabled := parts[3]
			if enabled == "1" {
				cm.controllers = append(cm.controllers, controller)
			}
		}
	}

	return scanner.Err()
}

// GetMemoryStats returns memory statistics from cgroups
func (cm *CgroupManager) GetMemoryStats() (*CgroupMemoryStats, error) {
	if cm.version == CgroupVersion2 {
		return cm.getMemoryStatsV2()
	}
	return cm.getMemoryStatsV1()
}

// getMemoryStatsV2 gets memory stats for cgroup v2
func (cm *CgroupManager) getMemoryStatsV2() (*CgroupMemoryStats, error) {
	stats := &CgroupMemoryStats{}

	// Get memory.current (usage)
	if usage, err := cm.readInt64FromFile("memory.current"); err == nil {
		stats.UsageBytes = usage
	}

	// Get memory.max (limit)
	if limit, err := cm.readInt64FromFile("memory.max"); err == nil && limit != 9223372036854775807 {
		stats.LimitBytes = limit
	}

	// Get memory.swap.current (swap usage)
	if swapUsage, err := cm.readInt64FromFile("memory.swap.current"); err == nil {
		stats.SwapUsageBytes = swapUsage
	}

	// Get memory.swap.max (swap limit)
	if swapLimit, err := cm.readInt64FromFile("memory.swap.max"); err == nil && swapLimit != 9223372036854775807 {
		stats.SwapLimitBytes = swapLimit
	}

	// Parse memory.stat for detailed statistics
	if err := cm.parseMemoryStatV2(stats); err != nil {
		// Log error but continue
	}

	// Get memory pressure information
	if pressure, err := cm.getMemoryPressureV2(); err == nil {
		stats.Pressure = pressure
	}

	return stats, nil
}

// getMemoryStatsV1 gets memory stats for cgroup v1
func (cm *CgroupManager) getMemoryStatsV1() (*CgroupMemoryStats, error) {
	stats := &CgroupMemoryStats{}

	memoryController := "memory"
	basePath := cm.getControllerPath(memoryController)

	// Get memory.usage_in_bytes
	if usage, err := cm.readInt64FromPath(filepath.Join(basePath, "memory.usage_in_bytes")); err == nil {
		stats.UsageBytes = usage
	}

	// Get memory.limit_in_bytes
	if limit, err := cm.readInt64FromPath(filepath.Join(basePath, "memory.limit_in_bytes")); err == nil && limit != 9223372036854775807 {
		stats.LimitBytes = limit
	}

	// Get memory.memsw.usage_in_bytes (memory + swap)
	if memswUsage, err := cm.readInt64FromPath(filepath.Join(basePath, "memory.memsw.usage_in_bytes")); err == nil {
		if memswUsage > stats.UsageBytes {
			stats.SwapUsageBytes = memswUsage - stats.UsageBytes
		}
	}

	// Get memory.memsw.limit_in_bytes (memory + swap limit)
	if memswLimit, err := cm.readInt64FromPath(filepath.Join(basePath, "memory.memsw.limit_in_bytes")); err == nil && memswLimit != 9223372036854775807 {
		if memswLimit > stats.LimitBytes {
			stats.SwapLimitBytes = memswLimit - stats.LimitBytes
		}
	}

	// Parse memory.stat for detailed statistics
	if err := cm.parseMemoryStatV1(stats, basePath); err != nil {
		// Log error but continue
	}

	return stats, nil
}

// GetCPUStats returns CPU statistics from cgroups
func (cm *CgroupManager) GetCPUStats() (*CgroupCPUStats, error) {
	if cm.version == CgroupVersion2 {
		return cm.getCPUStatsV2()
	}
	return cm.getCPUStatsV1()
}

// getCPUStatsV2 gets CPU stats for cgroup v2
func (cm *CgroupManager) getCPUStatsV2() (*CgroupCPUStats, error) {
	stats := &CgroupCPUStats{}

	// Get cpu.max (quota and period)
	if cpuMaxData, err := cm.readStringFromFile("cpu.max"); err == nil {
		cm.parseCPUMaxV2(stats, cpuMaxData)
	}

	// Get cpu.weight (equivalent to shares)
	if weight, err := cm.readInt64FromFile("cpu.weight"); err == nil {
		// Convert weight to shares (weight * 1024 / 100)
		stats.Shares = weight * 1024 / 100
	}

	// Get CPU statistics from cpu.stat
	if err := cm.parseCPUStatV2(stats); err != nil {
		// Log error but continue
	}

	return stats, nil
}

// getCPUStatsV1 gets CPU stats for cgroup v1
func (cm *CgroupManager) getCPUStatsV1() (*CgroupCPUStats, error) {
	stats := &CgroupCPUStats{}

	cpuController := "cpu"
	basePath := cm.getControllerPath(cpuController)

	// Get cpu.cfs_quota_us
	if quota, err := cm.readInt64FromPath(filepath.Join(basePath, "cpu.cfs_quota_us")); err == nil && quota > 0 {
		stats.QuotaUs = quota
	}

	// Get cpu.cfs_period_us
	if period, err := cm.readInt64FromPath(filepath.Join(basePath, "cpu.cfs_period_us")); err == nil && period > 0 {
		stats.QuotaPeriodUs = period
	}

	// Calculate limit cores from quota and period
	if stats.QuotaUs > 0 && stats.QuotaPeriodUs > 0 {
		stats.LimitCores = float64(stats.QuotaUs) / float64(stats.QuotaPeriodUs)
	}

	// Get cpu.shares
	if shares, err := cm.readInt64FromPath(filepath.Join(basePath, "cpu.shares")); err == nil {
		stats.Shares = shares
	}

	// Get CPU throttling statistics
	if err := cm.parseCPUStatV1(stats, basePath); err != nil {
		// Log error but continue
	}

	return stats, nil
}

// GetIOStats returns I/O statistics from cgroups
func (cm *CgroupManager) GetIOStats() (*CgroupIOStats, error) {
	if cm.version == CgroupVersion2 {
		return cm.getIOStatsV2()
	}
	return cm.getIOStatsV1()
}

// getIOStatsV2 gets I/O stats for cgroup v2
func (cm *CgroupManager) getIOStatsV2() (*CgroupIOStats, error) {
	stats := &CgroupIOStats{}

	// Parse io.stat
	ioStatPath := filepath.Join(cm.mountPoint, cm.cgroupPath, "io.stat")
	if err := cm.parseIOStatV2(stats, ioStatPath); err != nil {
		return nil, err
	}

	return stats, nil
}

// getIOStatsV1 gets I/O stats for cgroup v1
func (cm *CgroupManager) getIOStatsV1() (*CgroupIOStats, error) {
	stats := &CgroupIOStats{}

	blkioController := "blkio"
	basePath := cm.getControllerPath(blkioController)

	// Parse blkio.throttle.io_service_bytes
	if err := cm.parseIOStatV1(stats, basePath); err != nil {
		return nil, err
	}

	return stats, nil
}

// GetIOLimits returns I/O limits from cgroups
func (cm *CgroupManager) GetIOLimits() (*IOLimits, error) {
	limits := &IOLimits{}

	if cm.version == CgroupVersion2 {
		// Parse io.max for v2
		ioMaxPath := filepath.Join(cm.mountPoint, cm.cgroupPath, "io.max")
		if data, err := os.ReadFile(ioMaxPath); err == nil {
			cm.parseIOMaxV2(limits, string(data))
		}
	} else {
		// Parse blkio limits for v1
		blkioController := "blkio"
		basePath := cm.getControllerPath(blkioController)
		cm.parseIOLimitsV1(limits, basePath)
	}

	return limits, nil
}

// GetInfo returns cgroup information
func (cm *CgroupManager) GetInfo() *CgroupInfo {
	return &CgroupInfo{
		Version:     cm.version,
		Path:        cm.cgroupPath,
		Controllers: cm.controllers,
	}
}

// Helper methods

func (cm *CgroupManager) getControllerPath(controller string) string {
	if cm.version == CgroupVersion2 {
		return filepath.Join(cm.mountPoint, cm.cgroupPath)
	}

	// For cgroup v1, find the specific controller mount point
	if path, exists := cm.hierarchyInfo[controller]; exists {
		return filepath.Join(cm.mountPoint, controller, path)
	}

	// Fallback
	return filepath.Join(cm.mountPoint, controller, cm.cgroupPath)
}

func (cm *CgroupManager) readInt64FromFile(filename string) (int64, error) {
	path := filepath.Join(cm.mountPoint, cm.cgroupPath, filename)
	return cm.readInt64FromPath(path)
}

func (cm *CgroupManager) readInt64FromPath(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	value := strings.TrimSpace(string(data))
	if value == "max" {
		return 9223372036854775807, nil // Max int64
	}

	return strconv.ParseInt(value, 10, 64)
}

func (cm *CgroupManager) readStringFromFile(filename string) (string, error) {
	path := filepath.Join(cm.mountPoint, cm.cgroupPath, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (cm *CgroupManager) parseMemoryStatV2(stats *CgroupMemoryStats) error {
	memStatPath := filepath.Join(cm.mountPoint, cm.cgroupPath, "memory.stat")
	file, err := os.Open(memStatPath)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			continue
		}

		switch key {
		case "anon":
			stats.RSSBytes = value
		case "file":
			stats.CacheBytes = value
		}
	}

	return scanner.Err()
}

func (cm *CgroupManager) parseMemoryStatV1(stats *CgroupMemoryStats, basePath string) error {
	memStatPath := filepath.Join(basePath, "memory.stat")
	file, err := os.Open(memStatPath)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			continue
		}

		switch key {
		case "rss":
			stats.RSSBytes = value
		case "cache":
			stats.CacheBytes = value
		}
	}

	return scanner.Err()
}

func (cm *CgroupManager) getMemoryPressureV2() (*MemoryPressure, error) {
	pressurePath := filepath.Join(cm.mountPoint, cm.cgroupPath, "memory.pressure")
	file, err := os.Open(pressurePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	pressure := &MemoryPressure{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "some ") {
			pressure.Some = cm.parsePressureLine(line)
		} else if strings.HasPrefix(line, "full ") {
			pressure.Full = cm.parsePressureLine(line)
		}
	}

	return pressure, scanner.Err()
}

func (cm *CgroupManager) parsePressureLine(line string) *PressureStats {
	stats := &PressureStats{}
	parts := strings.Fields(line)
	for _, part := range parts[1:] { // Skip the first part (some/full)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}

		key := kv[0]
		value := kv[1]

		switch key {
		case "avg10":
			if val, err := strconv.ParseFloat(value, 64); err == nil {
				stats.Avg10 = val
			}
		case "avg60":
			if val, err := strconv.ParseFloat(value, 64); err == nil {
				stats.Avg60 = val
			}
		case "avg300":
			if val, err := strconv.ParseFloat(value, 64); err == nil {
				stats.Avg300 = val
			}
		case "total":
			if val, err := strconv.ParseInt(value, 10, 64); err == nil {
				stats.Total = val
			}
		}
	}

	return stats
}

func (cm *CgroupManager) parseCPUMaxV2(stats *CgroupCPUStats, cpuMaxData string) {
	parts := strings.Fields(cpuMaxData)
	if len(parts) >= 2 {
		if quota, err := strconv.ParseInt(parts[0], 10, 64); err == nil && quota > 0 {
			stats.QuotaUs = quota
		}
		if period, err := strconv.ParseInt(parts[1], 10, 64); err == nil && period > 0 {
			stats.QuotaPeriodUs = period
		}

		if stats.QuotaUs > 0 && stats.QuotaPeriodUs > 0 {
			stats.LimitCores = float64(stats.QuotaUs) / float64(stats.QuotaPeriodUs)
		}
	}
}

func (cm *CgroupManager) parseCPUStatV2(stats *CgroupCPUStats) error {
	cpuStatPath := filepath.Join(cm.mountPoint, cm.cgroupPath, "cpu.stat")
	file, err := os.Open(cpuStatPath)
	if err != nil {
		return err
	}
	defer file.Close()

	throttling := &CPUThrottling{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			continue
		}

		switch key {
		case "nr_throttled":
			throttling.ThrottledPeriods = value
		case "throttled_usec":
			throttling.ThrottledTime = time.Duration(value) * time.Microsecond
		case "nr_periods":
			throttling.TotalPeriods = value
		}
	}

	if throttling.ThrottledPeriods > 0 || throttling.TotalPeriods > 0 {
		stats.Throttling = throttling
	}

	return scanner.Err()
}

func (cm *CgroupManager) parseCPUStatV1(stats *CgroupCPUStats, basePath string) error {
	throttleStatPath := filepath.Join(basePath, "cpu.stat")
	file, err := os.Open(throttleStatPath)
	if err != nil {
		return err
	}
	defer file.Close()

	throttling := &CPUThrottling{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			continue
		}

		switch key {
		case "nr_throttled":
			throttling.ThrottledPeriods = value
		case "throttled_time":
			throttling.ThrottledTime = time.Duration(value) * time.Nanosecond
		case "nr_periods":
			throttling.TotalPeriods = value
		}
	}

	if throttling.ThrottledPeriods > 0 || throttling.TotalPeriods > 0 {
		stats.Throttling = throttling
	}

	return scanner.Err()
}

func (cm *CgroupManager) parseIOStatV2(stats *CgroupIOStats, ioStatPath string) error {
	file, err := os.Open(ioStatPath)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		// deviceID := parts[0]
		for _, stat := range parts[1:] {
			kv := strings.SplitN(stat, "=", 2)
			if len(kv) != 2 {
				continue
			}

			key := kv[0]
			value, err := strconv.ParseInt(kv[1], 10, 64)
			if err != nil {
				continue
			}

			switch key {
			case "rbytes":
				stats.ReadBytes += value
			case "wbytes":
				stats.WriteBytes += value
			case "rios":
				stats.ReadOps += value
			case "wios":
				stats.WriteOps += value
			}
		}
	}

	return scanner.Err()
}

func (cm *CgroupManager) parseIOStatV1(stats *CgroupIOStats, basePath string) error {
	// Try different blkio stat files
	statFiles := []string{
		"blkio.throttle.io_service_bytes",
		"blkio.io_service_bytes",
		"blkio.io_service_bytes_recursive",
	}

	for _, statFile := range statFiles {
		statPath := filepath.Join(basePath, statFile)
		if err := cm.parseBlkioServiceBytes(stats, statPath); err == nil {
			break
		}
	}

	return nil
}

func (cm *CgroupManager) parseBlkioServiceBytes(stats *CgroupIOStats, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) != 3 {
			continue
		}

		// deviceID := parts[0]
		operation := parts[1]
		value, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			continue
		}

		switch operation {
		case "Read":
			stats.ReadBytes += value
		case "Write":
			stats.WriteBytes += value
		}
	}

	return scanner.Err()
}

func (cm *CgroupManager) parseIOMaxV2(limits *IOLimits, ioMaxData string) {
	lines := strings.Split(ioMaxData, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		// deviceID := parts[0]
		for _, limit := range parts[1:] {
			kv := strings.SplitN(limit, "=", 2)
			if len(kv) != 2 {
				continue
			}

			key := kv[0]
			value := kv[1]

			if value == "max" {
				continue
			}

			if val, err := strconv.ParseInt(value, 10, 64); err == nil {
				switch key {
				case "rbps":
					limits.ReadBytesPerSec = val
				case "wbps":
					limits.WriteBytesPerSec = val
				case "riops":
					limits.ReadOpsPerSec = val
				case "wiops":
					limits.WriteOpsPerSec = val
				}
			}
		}
	}
}

func (cm *CgroupManager) parseIOLimitsV1(limits *IOLimits, basePath string) {
	// Parse various blkio limit files
	limitFiles := map[string]*int64{
		"blkio.throttle.read_bps_device":   &limits.ReadBytesPerSec,
		"blkio.throttle.write_bps_device":  &limits.WriteBytesPerSec,
		"blkio.throttle.read_iops_device":  &limits.ReadOpsPerSec,
		"blkio.throttle.write_iops_device": &limits.WriteOpsPerSec,
	}

	for filename, target := range limitFiles {
		limitPath := filepath.Join(basePath, filename)
		if data, err := os.ReadFile(limitPath); err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}

				parts := strings.Fields(line)
				if len(parts) >= 2 {
					if val, err := strconv.ParseInt(parts[1], 10, 64); err == nil && val > 0 {
						*target = val
						break
					}
				}
			}
		}
	}
}
