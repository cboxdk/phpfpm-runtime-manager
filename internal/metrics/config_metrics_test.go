package metrics

import (
	"testing"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/config"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/phpfpm"
	"go.uber.org/zap"
)

func TestCollector_GetPHPFPMConfig(t *testing.T) {
	logger := zap.NewNop()

	// Create test configuration
	monitoringConfig := config.MonitoringConfig{}
	pools := []config.PoolConfig{
		{
			Name:            "www",
			FastCGIEndpoint: "unix:/var/run/php-fpm.sock",
		},
	}

	// Create collector with mock PHP-FPM binary path
	collector, err := NewCollectorWithPHPFPM(monitoringConfig, pools, "", "", logger)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Test with no PHP-FPM binary configured
	poolConfig := collector.getPHPFPMConfig("www")
	if poolConfig != nil {
		t.Errorf("Expected nil config when no PHP-FPM binary is configured, got %v", poolConfig)
	}

	// Create collector with mock PHP-FPM binary path
	collector2, err := NewCollectorWithPHPFPM(monitoringConfig, pools, "/usr/bin/php-fpm", "/etc/php-fpm.conf", logger)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Mock the parsed config directly
	collector2.phpfpmConfig = &phpfpm.FPMConfig{
		Global: map[string]string{
			"pid": "/var/run/php-fpm.pid",
		},
		Pools: map[string]map[string]string{
			"www": {
				"listen":               "/var/run/php-fpm.sock",
				"pm":                   "dynamic",
				"pm.max_children":      "50",
				"pm.start_servers":     "5",
				"pm.min_spare_servers": "5",
				"pm.max_spare_servers": "10",
				"pm.max_requests":      "500",
			},
		},
	}

	// Test retrieving pool config metrics
	poolConfig = collector2.getPHPFPMConfig("www")
	if poolConfig == nil {
		t.Fatal("Expected pool config metrics, got nil")
	}

	// Verify the config values
	if poolConfig.PM != "dynamic" {
		t.Errorf("Expected PM to be 'dynamic', got '%s'", poolConfig.PM)
	}
	if poolConfig.MaxChildren != 50 {
		t.Errorf("Expected MaxChildren to be 50, got %d", poolConfig.MaxChildren)
	}
	if poolConfig.StartServers != 5 {
		t.Errorf("Expected StartServers to be 5, got %d", poolConfig.StartServers)
	}
	if poolConfig.MinSpareServers != 5 {
		t.Errorf("Expected MinSpareServers to be 5, got %d", poolConfig.MinSpareServers)
	}
	if poolConfig.MaxSpareServers != 10 {
		t.Errorf("Expected MaxSpareServers to be 10, got %d", poolConfig.MaxSpareServers)
	}
	if poolConfig.MaxRequests != 500 {
		t.Errorf("Expected MaxRequests to be 500, got %d", poolConfig.MaxRequests)
	}

	// Test with non-existent pool
	poolConfig = collector2.getPHPFPMConfig("nonexistent")
	if poolConfig != nil {
		t.Errorf("Expected nil for non-existent pool, got %v", poolConfig)
	}
}

func TestCollector_GetPHPFPMConfig_MatchByListen(t *testing.T) {
	logger := zap.NewNop()

	// Create test configuration
	monitoringConfig := config.MonitoringConfig{}
	pools := []config.PoolConfig{
		{
			Name:            "mypool",
			FastCGIEndpoint: "unix:/var/run/mypool.sock",
		},
	}

	// Create collector with mock PHP-FPM binary path
	collector, err := NewCollectorWithPHPFPM(monitoringConfig, pools, "/usr/bin/php-fpm", "/etc/php-fpm.conf", logger)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Mock the parsed config with a different pool name but same listen socket
	collector.phpfpmConfig = &phpfpm.FPMConfig{
		Pools: map[string]map[string]string{
			"www": {
				"listen":          "/var/run/mypool.sock",
				"pm":              "static",
				"pm.max_children": "20",
			},
		},
	}

	// Test retrieving pool config by matching listen directive
	poolConfig := collector.getPHPFPMConfig("mypool")
	if poolConfig == nil {
		t.Fatal("Expected pool config metrics matched by listen directive, got nil")
	}

	// Verify the config values
	if poolConfig.PM != "static" {
		t.Errorf("Expected PM to be 'static', got '%s'", poolConfig.PM)
	}
	if poolConfig.MaxChildren != 20 {
		t.Errorf("Expected MaxChildren to be 20, got %d", poolConfig.MaxChildren)
	}
}
