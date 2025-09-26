package supervisor

import (
	"strings"
	"testing"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/config"
	"go.uber.org/zap"
)

func TestGenerateGlobalConfig_UsesCorrectPaths(t *testing.T) {
	// Setup supervisor with custom global config path
	pools := []config.PoolConfig{
		{
			Name:            "test-pool",
			FastCGIEndpoint: "127.0.0.1:9000",
			MaxWorkers:      10,
		},
	}

	supervisor := &Supervisor{
		pools:            pools,
		logger:           zap.NewNop(),
		globalConfigPath: "/opt/phpfpm-runtime-manager/php-fpm.conf",
	}

	// Generate global config
	configContent := supervisor.generateGlobalConfig()

	// Verify that all paths use the runtime manager directory
	expectedPaths := []string{
		"pid = /opt/phpfpm-runtime-manager/pids/phpfpm-manager.pid",
		"error_log = /opt/phpfpm-runtime-manager/logs/phpfpm-manager-error.log",
		"access.log = /opt/phpfpm-runtime-manager/logs/phpfpm-pool-test-pool.log",
	}

	for _, expectedPath := range expectedPaths {
		if !strings.Contains(configContent, expectedPath) {
			t.Errorf("Expected config to contain '%s', but it was not found.\nGenerated config:\n%s", expectedPath, configContent)
		}
	}

	// Verify that temp directory paths are NOT used
	forbiddenPatterns := []string{
		"/tmp/",
		"/var/folders/",
		"TempDir",
	}

	for _, pattern := range forbiddenPatterns {
		if strings.Contains(configContent, pattern) {
			t.Errorf("Config should not contain temp directory pattern '%s', but it was found.\nGenerated config:\n%s", pattern, configContent)
		}
	}
}

func TestGeneratePoolConfig_UsesCorrectLogPath(t *testing.T) {
	// Setup supervisor
	supervisor := &Supervisor{
		logger:           zap.NewNop(),
		globalConfigPath: "/opt/phpfpm-runtime-manager/php-fpm.conf",
	}

	pool := config.PoolConfig{
		Name:            "web",
		FastCGIEndpoint: "127.0.0.1:9000",
		MaxWorkers:      20,
	}

	// Generate pool config
	poolConfig := supervisor.generatePoolConfig(pool)

	// Verify the log path uses runtime manager directory
	expectedLogPath := "access.log = /opt/phpfpm-runtime-manager/logs/phpfpm-pool-web.log"
	if !strings.Contains(poolConfig, expectedLogPath) {
		t.Errorf("Expected pool config to contain '%s', but it was not found.\nGenerated pool config:\n%s", expectedLogPath, poolConfig)
	}

	// Verify no temp directory paths
	if strings.Contains(poolConfig, "/tmp/") || strings.Contains(poolConfig, "/var/folders/") {
		t.Errorf("Pool config should not contain temp directory paths.\nGenerated pool config:\n%s", poolConfig)
	}

	// Verify no user/group settings (running as non-root by default)
	if strings.Contains(poolConfig, "user = ") || strings.Contains(poolConfig, "group = ") {
		t.Errorf("Pool config should not contain user/group settings for non-root operation.\nGenerated pool config:\n%s", poolConfig)
	}
}
