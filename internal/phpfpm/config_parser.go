package phpfpm

import (
	"bufio"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

var (
	fpmConfigCache     = make(map[string]*FPMConfig)
	fpmConfigCacheLock sync.Mutex
)

// FPMConfig represents parsed PHP-FPM configuration
type FPMConfig struct {
	Global map[string]string
	Pools  map[string]map[string]string
}

// ParseFPMConfig parses PHP-FPM configuration using php-fpm -tt
func ParseFPMConfig(FPMBinaryPath string, FPMConfigPath string) (*FPMConfig, error) {
	key := FPMBinaryPath + "::" + FPMConfigPath

	fpmConfigCacheLock.Lock()
	cached, ok := fpmConfigCache[key]
	fpmConfigCacheLock.Unlock()

	if ok {
		return cached, nil
	}

	cmd := exec.Command(FPMBinaryPath, "-tt", "--fpm-config", FPMConfigPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to run php-fpm -tt: %w\nOutput: %s", err, output)
	}

	fpmconfig := &FPMConfig{
		Global: make(map[string]string),
		Pools:  make(map[string]map[string]string),
	}
	currentSection := "global"

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if idx := strings.Index(line, "] NOTICE:"); idx != -1 {
			line = strings.TrimSpace(line[idx+len("] NOTICE:"):])
		}

		line = strings.ReplaceAll(line, "\\t", "")
		line = strings.ReplaceAll(line, "\t", "")

		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section := strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			if section == "global" {
				currentSection = "global"
				continue
			}
			currentSection = section
			if _, ok := fpmconfig.Pools[currentSection]; !ok {
				fpmconfig.Pools[currentSection] = make(map[string]string)
			}
			continue
		}

		if strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			// Remove quotes from both key and value
			key = strings.Trim(key, `"`)
			val = strings.Trim(val, `"`)
			if val == "undefined" {
				val = ""
			}

			if currentSection != "global" {
				if _, ok := fpmconfig.Pools[currentSection]; !ok {
					fpmconfig.Pools[currentSection] = make(map[string]string)
				}
				fpmconfig.Pools[currentSection][key] = val
			} else {
				fpmconfig.Global[key] = val
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan php-fpm config output: %w", err)
	}

	fpmConfigCacheLock.Lock()
	fpmConfigCache[key] = fpmconfig
	fpmConfigCacheLock.Unlock()

	return fpmconfig, nil
}

// GetPoolConfigMetrics extracts metrics-relevant configuration from a pool
func GetPoolConfigMetrics(pool map[string]string) PoolConfigMetrics {
	metrics := PoolConfigMetrics{}

	// Parse PM settings
	metrics.PM = pool["pm"]
	metrics.MaxChildren = parseIntWithDefault(pool["pm.max_children"], 5)
	metrics.StartServers = parseIntWithDefault(pool["pm.start_servers"], 2)
	metrics.MinSpareServers = parseIntWithDefault(pool["pm.min_spare_servers"], 1)
	metrics.MaxSpareServers = parseIntWithDefault(pool["pm.max_spare_servers"], 3)
	metrics.MaxRequests = parseIntWithDefault(pool["pm.max_requests"], 0)
	metrics.ProcessIdleTimeout = parseIntWithDefault(pool["pm.process_idle_timeout"], 10)

	// Parse resource limits
	metrics.RlimitFiles = parseIntWithDefault(pool["rlimit_files"], 1024)
	metrics.RlimitCore = parseIntWithDefault(pool["rlimit_core"], 0)

	// Parse timeouts
	metrics.RequestSlowlogTimeout = parseIntWithDefault(pool["request_slowlog_timeout"], 0)
	metrics.RequestTerminateTimeout = parseIntWithDefault(pool["request_terminate_timeout"], 0)

	return metrics
}

// PoolConfigMetrics represents pool configuration metrics
type PoolConfigMetrics struct {
	PM                      string
	MaxChildren             int
	StartServers            int
	MinSpareServers         int
	MaxSpareServers         int
	MaxRequests             int
	ProcessIdleTimeout      int
	RlimitFiles             int
	RlimitCore              int
	RequestSlowlogTimeout   int
	RequestTerminateTimeout int
}

func parseIntWithDefault(value string, defaultValue int) int {
	if value == "" {
		return defaultValue
	}
	// Remove 's' suffix if present (for timeouts)
	value = strings.TrimSuffix(value, "s")
	if v, err := strconv.Atoi(value); err == nil {
		return v
	}
	return defaultValue
}

// ClearCache clears the configuration cache (useful for testing)
func ClearCache() {
	fpmConfigCacheLock.Lock()
	fpmConfigCache = make(map[string]*FPMConfig)
	fpmConfigCacheLock.Unlock()
}
