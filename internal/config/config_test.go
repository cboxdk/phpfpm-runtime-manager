package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	// Create a temporary config file for testing
	configContent := `
server:
  bind_address: "127.0.0.1:9090"
  metrics_path: "/metrics"
  tls:
    enabled: false
  auth:
    enabled: false
    type: "api_key"

storage:
  database_path: "./test.db"
  retention:
    raw: "1h"
    minute: "24h"
    hour: "168h"

phpfpm:
  global_config_path: "/opt/phpfpm-runtime-manager/php-fpm.conf"

pools:
  - name: "test-pool"
    config_path: "/tmp/test.conf"
    fastcgi_endpoint: "127.0.0.1:9001"
    fastcgi_status_path: "/status"
    max_workers: 5

monitoring:
  collect_interval: "1s"
  health_check_interval: "5s"

logging:
  level: "info"
  format: "json"
`

	// Create temporary config file
	tmpFile, err := os.CreateTemp("", "config_test_*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(configContent)); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	tmpFile.Close()

	// Create a temporary PHP-FPM config file for validation
	phpfpmConfig, err := os.CreateTemp("", "phpfpm_test_*.conf")
	if err != nil {
		t.Fatalf("Failed to create temp PHP-FPM config: %v", err)
	}
	defer os.Remove(phpfpmConfig.Name())
	phpfpmConfig.WriteString("[global]\npid = /tmp/test.pid\n")
	phpfpmConfig.Close()

	// Update the config content to use the actual temp file path
	configContent = `
server:
  bind_address: "127.0.0.1:9090"
  metrics_path: "/metrics"
  tls:
    enabled: false
  auth:
    enabled: false
    type: "api_key"

storage:
  database_path: "./test.db"
  retention:
    raw: "1h"
    minute: "24h"
    hour: "168h"

phpfpm:
  global_config_path: "/opt/phpfpm-runtime-manager/php-fpm.conf"

pools:
  - name: "test-pool"
    config_path: "` + phpfpmConfig.Name() + `"
    fastcgi_endpoint: "127.0.0.1:9001"
    fastcgi_status_path: "/status"
    max_workers: 5

monitoring:
  collect_interval: "1s"
  health_check_interval: "5s"

logging:
  level: "info"
  format: "json"
`

	// Rewrite the config file with the valid path
	tmpFile2, err := os.CreateTemp("", "config_test2_*.yaml")
	if err != nil {
		t.Fatalf("Failed to create second temp file: %v", err)
	}
	defer os.Remove(tmpFile2.Name())

	if _, err := tmpFile2.Write([]byte(configContent)); err != nil {
		t.Fatalf("Failed to write updated config: %v", err)
	}
	tmpFile2.Close()

	// Test loading the config
	cfg, err := Load(tmpFile2.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify configuration values
	if cfg.Server.BindAddress != "127.0.0.1:9090" {
		t.Errorf("Expected bind_address '127.0.0.1:9090', got '%s'", cfg.Server.BindAddress)
	}

	if cfg.Server.MetricsPath != "/metrics" {
		t.Errorf("Expected metrics_path '/metrics', got '%s'", cfg.Server.MetricsPath)
	}

	if len(cfg.Pools) != 1 {
		t.Errorf("Expected 1 pool, got %d", len(cfg.Pools))
	}

	if cfg.Pools[0].Name != "test-pool" {
		t.Errorf("Expected pool name 'test-pool', got '%s'", cfg.Pools[0].Name)
	}

	if cfg.Storage.Retention.Raw != time.Hour {
		t.Errorf("Expected raw retention 1h, got %v", cfg.Storage.Retention.Raw)
	}
}

func TestValidateInvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		hasErr bool
	}{
		{
			name: "no pools",
			config: Config{
				Pools: []PoolConfig{},
			},
			hasErr: true,
		},
		{
			name: "empty pool name",
			config: Config{
				Pools: []PoolConfig{
					{Name: "", ConfigPath: "/tmp/test.conf", FastCGIEndpoint: "127.0.0.1:9000", FastCGIStatusPath: "/status", MaxWorkers: 1},
				},
				Monitoring: MonitoringConfig{
					CollectInterval:     time.Second,
					HealthCheckInterval: time.Second,
				},
				Storage: StorageConfig{
					Retention: RetentionConfig{
						Raw:    time.Hour,
						Minute: time.Hour,
						Hour:   time.Hour,
					},
				},
			},
			hasErr: true,
		},
		{
			name: "duplicate pool name",
			config: Config{
				Pools: []PoolConfig{
					{Name: "same", ConfigPath: "/tmp/test1.conf", FastCGIEndpoint: "127.0.0.1:9000", FastCGIStatusPath: "/status", MaxWorkers: 1},
					{Name: "same", ConfigPath: "/tmp/test2.conf", FastCGIEndpoint: "127.0.0.1:9001", FastCGIStatusPath: "/status", MaxWorkers: 1},
				},
				Monitoring: MonitoringConfig{
					CollectInterval:     time.Second,
					HealthCheckInterval: time.Second,
				},
				Storage: StorageConfig{
					Retention: RetentionConfig{
						Raw:    time.Hour,
						Minute: time.Hour,
						Hour:   time.Hour,
					},
				},
			},
			hasErr: true,
		},
		{
			name: "invalid cpu threshold",
			config: Config{
				Pools: []PoolConfig{
					{
						Name:              "test",
						ConfigPath:        "/tmp/test.conf",
						FastCGIEndpoint:   "127.0.0.1:9000",
						FastCGIStatusPath: "/status",
						MaxWorkers:        1,
						Resources: ResourceLimits{
							CPUThreshold: 150.0, // Invalid: > 100
						},
					},
				},
				Monitoring: MonitoringConfig{
					CollectInterval:     time.Second,
					HealthCheckInterval: time.Second,
				},
				Storage: StorageConfig{
					Retention: RetentionConfig{
						Raw:    time.Hour,
						Minute: time.Hour,
						Hour:   time.Hour,
					},
				},
			},
			hasErr: true,
		},
		{
			name: "duplicate fastcgi endpoints",
			config: Config{
				Pools: []PoolConfig{
					{Name: "pool1", ConfigPath: "/tmp/test1.conf", FastCGIEndpoint: "127.0.0.1:9000", FastCGIStatusPath: "/status", MaxWorkers: 1},
					{Name: "pool2", ConfigPath: "/tmp/test2.conf", FastCGIEndpoint: "127.0.0.1:9000", FastCGIStatusPath: "/status", MaxWorkers: 1},
				},
				Monitoring: MonitoringConfig{
					CollectInterval:     time.Second,
					HealthCheckInterval: time.Second,
				},
				Storage: StorageConfig{
					Retention: RetentionConfig{
						Raw:    time.Hour,
						Minute: time.Hour,
						Hour:   time.Hour,
					},
				},
			},
			hasErr: true,
		},
		{
			name: "invalid cpu threshold",
			config: Config{
				Pools: []PoolConfig{
					{
						Name:              "test",
						ConfigPath:        "/tmp/test.conf",
						FastCGIEndpoint:   "127.0.0.1:9000",
						FastCGIStatusPath: "/status",
						MaxWorkers:        1,
						Resources: ResourceLimits{
							CPUThreshold: 150.0, // Invalid: > 100
						},
					},
				},
				Monitoring: MonitoringConfig{
					CollectInterval:     time.Second,
					HealthCheckInterval: time.Second,
				},
				Storage: StorageConfig{
					Retention: RetentionConfig{
						Raw:    time.Hour,
						Minute: time.Hour,
						Hour:   time.Hour,
					},
				},
			},
			hasErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate(&tt.config)
			if tt.hasErr && err == nil {
				t.Error("Expected validation error, but got none")
			}
			if !tt.hasErr && err != nil {
				t.Errorf("Expected no validation error, but got: %v", err)
			}
		})
	}
}

func TestAuthValidation(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		hasErr bool
	}{
		{
			name: "auth enabled without type",
			config: Config{
				Server: ServerConfig{
					Auth: AuthConfig{
						Enabled: true,
						Type:    "",
					},
				},
				Pools: []PoolConfig{
					{Name: "test", ConfigPath: "/tmp/test.conf", FastCGIEndpoint: "127.0.0.1:9000", FastCGIStatusPath: "/status", MaxWorkers: 1},
				},
				Monitoring: MonitoringConfig{
					CollectInterval:     time.Second,
					HealthCheckInterval: time.Second,
				},
				Storage: StorageConfig{
					Retention: RetentionConfig{
						Raw:    time.Hour,
						Minute: time.Hour,
						Hour:   time.Hour,
					},
				},
			},
			hasErr: true,
		},
		{
			name: "api_key auth without key",
			config: Config{
				Server: ServerConfig{
					Auth: AuthConfig{
						Enabled: true,
						Type:    "api_key",
						APIKey:  "",
					},
				},
				Pools: []PoolConfig{
					{Name: "test", ConfigPath: "/tmp/test.conf", FastCGIEndpoint: "127.0.0.1:9000", FastCGIStatusPath: "/status", MaxWorkers: 1},
				},
				Monitoring: MonitoringConfig{
					CollectInterval:     time.Second,
					HealthCheckInterval: time.Second,
				},
				Storage: StorageConfig{
					Retention: RetentionConfig{
						Raw:    time.Hour,
						Minute: time.Hour,
						Hour:   time.Hour,
					},
				},
			},
			hasErr: true,
		},
		{
			name: "basic auth with short password",
			config: Config{
				Server: ServerConfig{
					Auth: AuthConfig{
						Enabled: true,
						Type:    "basic",
						Basic: BasicAuthConfig{
							Username: "admin",
							Password: "short", // Too short
						},
					},
				},
				Pools: []PoolConfig{
					{Name: "test", ConfigPath: "/tmp/test.conf", FastCGIEndpoint: "127.0.0.1:9000", FastCGIStatusPath: "/status", MaxWorkers: 1},
				},
				Monitoring: MonitoringConfig{
					CollectInterval:     time.Second,
					HealthCheckInterval: time.Second,
				},
				Storage: StorageConfig{
					Retention: RetentionConfig{
						Raw:    time.Hour,
						Minute: time.Hour,
						Hour:   time.Hour,
					},
				},
			},
			hasErr: true,
		},
		{
			name: "mtls without tls enabled",
			config: Config{
				Server: ServerConfig{
					TLS: TLSConfig{
						Enabled: false,
					},
					Auth: AuthConfig{
						Enabled: true,
						Type:    "mtls",
					},
				},
				Pools: []PoolConfig{
					{Name: "test", ConfigPath: "/tmp/test.conf", FastCGIEndpoint: "127.0.0.1:9000", FastCGIStatusPath: "/status", MaxWorkers: 1},
				},
				Monitoring: MonitoringConfig{
					CollectInterval:     time.Second,
					HealthCheckInterval: time.Second,
				},
				Storage: StorageConfig{
					Retention: RetentionConfig{
						Raw:    time.Hour,
						Minute: time.Hour,
						Hour:   time.Hour,
					},
				},
			},
			hasErr: true,
		},
		{
			name: "valid api_key auth",
			config: Config{
				Server: ServerConfig{
					BindAddress: "0.0.0.0:9090",
					MetricsPath: "/metrics",
					HealthPath:  "/health",
					AdminPath:   "/admin",
					Auth: AuthConfig{
						Enabled: true,
						Type:    "api_key",
						APIKey:  "this-is-a-valid-32-character-api-key-example",
					},
					API: APIConfig{
						Enabled:     true,
						BasePath:    "/api/v1",
						MaxRequests: 100,
					},
				},
				Pools: []PoolConfig{
					{Name: "test", ConfigPath: "/tmp/test.conf", FastCGIEndpoint: "127.0.0.1:9000", FastCGIStatusPath: "/status", MaxWorkers: 1},
				},
				Monitoring: MonitoringConfig{
					CollectInterval:     time.Second,
					HealthCheckInterval: time.Second,
					CPUThreshold:        80.0,
					MemoryThreshold:     85.0,
					Batching: BatchingConfig{
						Enabled:      true,
						Size:         50,
						FlushTimeout: 5 * time.Second,
						MaxBatches:   10,
					},
					Buffering: BufferingConfig{
						ChannelSize:           1000,
						BackpressureThreshold: 750,
						EnableBackpressure:    true,
					},
					ObjectPooling: ObjectPoolingConfig{
						Enabled:           true,
						MetricSetCapacity: 20,
						MetricCapacity:    4,
						LabelMapCapacity:  4,
					},
				},
				Storage: StorageConfig{
					DatabasePath: ":memory:",
					Retention: RetentionConfig{
						Raw:    24 * time.Hour,
						Minute: 7 * 24 * time.Hour,
						Hour:   365 * 24 * time.Hour,
					},
					Aggregation: AggregationConfig{
						Enabled:   true,
						Interval:  time.Minute,
						BatchSize: 1000,
					},
					ConnectionPool: ConnectionPoolConfig{
						Enabled:         true,
						MaxOpenConns:    10,
						MaxIdleConns:    5,
						ConnMaxLifetime: 2 * time.Hour,
						ConnMaxIdleTime: 30 * time.Minute,
						HealthInterval:  30 * time.Second,
					},
				},
				Logging: LoggingConfig{
					Level:      "info",
					Format:     "json",
					OutputPath: "stdout",
					Structured: true,
				},
				Telemetry: TelemetryConfig{
					Enabled:        false, // disabled for simplicity
					ServiceName:    "test-service",
					ServiceVersion: "1.0.0",
					Environment:    "test",
				},
				Security: SecurityConfig{
					AllowInternalNetworks: false,
					AllowedSchemes:        []string{"http", "https"},
					MaxPoolNameLength:     64,
					MaxAPIKeyLength:       128,
					MaxEventSummaryLength: 1024,
				},
				PHPFPM: PHPFPMConfig{
					GlobalConfigPath: "/opt/phpfpm-runtime-manager/php-fpm.conf",
				},
			},
			hasErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate(&tt.config)
			if tt.hasErr && err == nil {
				t.Error("Expected validation error, but got none")
			}
			if !tt.hasErr && err != nil {
				t.Errorf("Expected no validation error, but got: %v", err)
			}
		})
	}
}

func TestApplyDefaults(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)

	// Test default values
	if cfg.Server.BindAddress != "0.0.0.0:9090" {
		t.Errorf("Expected default bind_address '0.0.0.0:9090', got '%s'", cfg.Server.BindAddress)
	}

	if cfg.Server.MetricsPath != "/metrics" {
		t.Errorf("Expected default metrics_path '/metrics', got '%s'", cfg.Server.MetricsPath)
	}

	if cfg.Server.HealthPath != "/health" {
		t.Errorf("Expected default health_path '/health', got '%s'", cfg.Server.HealthPath)
	}

	if cfg.Server.AdminPath != "/admin" {
		t.Errorf("Expected default admin_path '/admin', got '%s'", cfg.Server.AdminPath)
	}

	if cfg.Storage.DatabasePath != ":memory:" {
		t.Errorf("Expected default database_path ':memory:', got '%s'", cfg.Storage.DatabasePath)
	}

	if cfg.Storage.Retention.Raw != 24*time.Hour {
		t.Errorf("Expected default raw retention 24h, got %v", cfg.Storage.Retention.Raw)
	}

	if cfg.Monitoring.CollectInterval != time.Second {
		t.Errorf("Expected default collect interval 1s, got %v", cfg.Monitoring.CollectInterval)
	}

	if cfg.Logging.Level != "info" {
		t.Errorf("Expected default log level 'info', got '%s'", cfg.Logging.Level)
	}

	// Test PHP-FPM defaults
	if cfg.PHPFPM.GlobalConfigPath != "/opt/phpfpm-runtime-manager/php-fpm.conf" {
		t.Errorf("Expected default global_config_path '/opt/phpfpm-runtime-manager/php-fpm.conf', got '%s'", cfg.PHPFPM.GlobalConfigPath)
	}

	// Test automatic default pool creation
	if len(cfg.Pools) != 1 {
		t.Errorf("Expected 1 default pool, got %d", len(cfg.Pools))
	}

	if cfg.Pools[0].Name != "web" {
		t.Errorf("Expected default pool name 'web', got '%s'", cfg.Pools[0].Name)
	}

	if cfg.Pools[0].FastCGIEndpoint != "unix:/opt/phpfpm-runtime-manager/sockets/web.sock" {
		t.Errorf("Expected default FastCGI endpoint 'unix:/opt/phpfpm-runtime-manager/sockets/web.sock', got '%s'", cfg.Pools[0].FastCGIEndpoint)
	}

	if cfg.Pools[0].FastCGIStatusPath != "/status" {
		t.Errorf("Expected default FastCGI status path '/status', got '%s'", cfg.Pools[0].FastCGIStatusPath)
	}

	if cfg.Pools[0].MaxWorkers != 0 {
		t.Errorf("Expected MaxWorkers 0 (autoscaling), got %d", cfg.Pools[0].MaxWorkers)
	}
}

func TestAutoPortAssignment(t *testing.T) {
	cfg := &Config{
		Pools: []PoolConfig{
			{Name: "web"}, // No endpoint - should get 9000
			{Name: "api"}, // No endpoint - should get 9001
			{Name: "admin", FastCGIEndpoint: "127.0.0.1:9003"}, // Explicit endpoint
			{Name: "worker"}, // No endpoint - should get 9000 or 9001 depending on availability
		},
	}

	applyDefaults(cfg)

	// Validate that all pools got endpoints assigned
	if cfg.Pools[0].FastCGIEndpoint == "" {
		t.Error("Expected auto-assigned endpoint for web pool")
	}
	if cfg.Pools[1].FastCGIEndpoint == "" {
		t.Error("Expected auto-assigned endpoint for api pool")
	}
	if cfg.Pools[2].FastCGIEndpoint != "127.0.0.1:9003" {
		t.Errorf("Expected explicit endpoint 127.0.0.1:9003, got %s", cfg.Pools[2].FastCGIEndpoint)
	}
	if cfg.Pools[3].FastCGIEndpoint == "" {
		t.Error("Expected auto-assigned endpoint for worker pool")
	}

	// Validate that all endpoints are unique
	endpoints := make(map[string]bool)
	for _, pool := range cfg.Pools {
		if endpoints[pool.FastCGIEndpoint] {
			t.Errorf("Duplicate endpoint found: %s", pool.FastCGIEndpoint)
		}
		endpoints[pool.FastCGIEndpoint] = true
	}

	// Validate format (should all be 127.0.0.1:port)
	for _, pool := range cfg.Pools {
		if !strings.HasPrefix(pool.FastCGIEndpoint, "127.0.0.1:") {
			t.Errorf("Expected 127.0.0.1:port format, got %s", pool.FastCGIEndpoint)
		}
	}
}

func TestFastCGIEndpointValidation(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		hasErr   bool
	}{
		{"valid TCP endpoint", "127.0.0.1:9000", false},
		{"valid Unix socket", "unix:/var/run/php-fpm/pool.sock", false},
		{"valid hostname", "localhost:9000", false},
		{"valid IPv6", "[::1]:9000", false},
		{"empty endpoint", "", true},
		{"invalid TCP format", "127.0.0.1", true},
		{"empty unix socket path", "unix:", true},
		{"relative unix socket path", "unix:pool.sock", true},
		{"invalid port", "127.0.0.1:abc", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFastCGIEndpoint(tt.endpoint)
			if tt.hasErr && err == nil {
				t.Error("Expected validation error, but got none")
			}
			if !tt.hasErr && err != nil {
				t.Errorf("Expected no validation error, but got: %v", err)
			}
		})
	}
}
