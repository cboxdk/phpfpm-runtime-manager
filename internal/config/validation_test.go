package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidationFramework(t *testing.T) {
	// Test with completely empty config
	emptyConfig := &Config{}
	result := validateConfiguration(emptyConfig)

	if result.Valid {
		t.Error("Expected validation to fail for empty config")
	}

	if len(result.Errors) == 0 {
		t.Error("Expected validation errors for empty config")
	}

	// Test ValidationResult Error() method
	errorString := result.Error()
	if !strings.Contains(errorString, "validation failed") {
		t.Errorf("Expected error string to contain 'validation failed', got: %s", errorString)
	}
}

func TestServerConfigValidation(t *testing.T) {
	tests := []struct {
		name           string
		config         ServerConfig
		expectErrors   int
		expectWarnings int
		errorFields    []string
	}{
		{
			name: "valid server config",
			config: ServerConfig{
				BindAddress: "0.0.0.0:9090",
				MetricsPath: "/metrics",
				HealthPath:  "/health",
				AdminPath:   "/admin",
				TLS: TLSConfig{
					Enabled: false,
				},
				Auth: AuthConfig{
					Enabled: false,
				},
				API: APIConfig{
					Enabled:     true,
					BasePath:    "/api/v1",
					MaxRequests: 100,
				},
			},
			expectErrors: 0,
		},
		{
			name: "invalid bind address",
			config: ServerConfig{
				BindAddress: "invalid-address",
				MetricsPath: "/metrics",
				HealthPath:  "/health",
				AdminPath:   "/admin",
			},
			expectErrors: 1,
			errorFields:  []string{"server.bind_address"},
		},
		{
			name: "invalid paths",
			config: ServerConfig{
				BindAddress: "0.0.0.0:9090",
				MetricsPath: "metrics", // missing leading slash
				HealthPath:  "",        // empty
				AdminPath:   "admin",   // missing leading slash
			},
			expectErrors: 3,
			errorFields:  []string{"server.metrics_path", "server.health_path", "server.admin_path"},
		},
		{
			name: "TLS enabled without cert files",
			config: ServerConfig{
				BindAddress: "0.0.0.0:9090",
				MetricsPath: "/metrics",
				HealthPath:  "/health",
				AdminPath:   "/admin",
				TLS: TLSConfig{
					Enabled:  true,
					CertFile: "",
					KeyFile:  "",
				},
			},
			expectErrors: 2,
			errorFields:  []string{"server.tls.cert_file", "server.tls.key_file"},
		},
		{
			name: "invalid auth configuration - api_key missing",
			config: ServerConfig{
				BindAddress: "0.0.0.0:9090",
				MetricsPath: "/metrics",
				HealthPath:  "/health",
				AdminPath:   "/admin",
				Auth: AuthConfig{
					Enabled: true,
					Type:    "api_key",
					APIKey:  "",
					APIKeys: []APIKeyConfig{},
				},
			},
			expectErrors: 1,
			errorFields:  []string{"server.auth.api_key"},
		},
		{
			name: "invalid auth configuration - short password",
			config: ServerConfig{
				BindAddress: "0.0.0.0:9090",
				MetricsPath: "/metrics",
				HealthPath:  "/health",
				AdminPath:   "/admin",
				Auth: AuthConfig{
					Enabled: true,
					Type:    "basic",
					Basic: BasicAuthConfig{
						Username: "admin",
						Password: "123", // too short
					},
				},
			},
			expectErrors: 1,
			errorFields:  []string{"server.auth.basic.password"},
		},
		{
			name: "mTLS without TLS enabled",
			config: ServerConfig{
				BindAddress: "0.0.0.0:9090",
				MetricsPath: "/metrics",
				HealthPath:  "/health",
				AdminPath:   "/admin",
				TLS: TLSConfig{
					Enabled: false,
				},
				Auth: AuthConfig{
					Enabled: true,
					Type:    "mtls",
				},
			},
			expectErrors: 1,
			errorFields:  []string{"server.auth.type"},
		},
		{
			name: "API config with high max requests",
			config: ServerConfig{
				BindAddress: "0.0.0.0:9090",
				MetricsPath: "/metrics",
				HealthPath:  "/health",
				AdminPath:   "/admin",
				API: APIConfig{
					Enabled:     true,
					BasePath:    "/api/v1",
					MaxRequests: 50000, // very high
				},
			},
			expectWarnings: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &ValidationResult{Valid: true}
			validateServerConfig(&tt.config, result)

			if len(result.Errors) != tt.expectErrors {
				t.Errorf("Expected %d errors, got %d. Errors: %v",
					tt.expectErrors, len(result.Errors), result.Errors)
			}

			if len(result.Warnings) != tt.expectWarnings {
				t.Errorf("Expected %d warnings, got %d. Warnings: %v",
					tt.expectWarnings, len(result.Warnings), result.Warnings)
			}

			// Check specific error fields
			for _, expectedField := range tt.errorFields {
				found := false
				for _, err := range result.Errors {
					if err.Field == expectedField {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected error for field %s, but not found", expectedField)
				}
			}
		})
	}
}

func TestStorageConfigValidation(t *testing.T) {
	tests := []struct {
		name           string
		config         StorageConfig
		expectErrors   int
		expectWarnings int
	}{
		{
			name: "valid storage config",
			config: StorageConfig{
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
			expectErrors: 0,
		},
		{
			name: "empty database path",
			config: StorageConfig{
				DatabasePath: "",
			},
			expectErrors: 1,
		},
		{
			name: "invalid retention periods",
			config: StorageConfig{
				DatabasePath: ":memory:",
				Retention: RetentionConfig{
					Raw:    0,                // invalid
					Minute: -time.Hour,       // invalid
					Hour:   30 * time.Second, // invalid
				},
			},
			expectErrors: 3,
		},
		{
			name: "inverted retention hierarchy",
			config: StorageConfig{
				DatabasePath: ":memory:",
				Retention: RetentionConfig{
					Raw:    7 * 24 * time.Hour, // longer than minute
					Minute: 24 * time.Hour,     // longer than hour
					Hour:   12 * time.Hour,     // shorter than minute
				},
			},
			expectWarnings: 2, // two hierarchy violations
		},
		{
			name: "connection pool with idle > max",
			config: StorageConfig{
				DatabasePath: ":memory:",
				Retention: RetentionConfig{
					Raw:    time.Hour,
					Minute: 24 * time.Hour,
					Hour:   365 * 24 * time.Hour,
				},
				ConnectionPool: ConnectionPoolConfig{
					Enabled:      true,
					MaxOpenConns: 5,
					MaxIdleConns: 10, // greater than max open
				},
			},
			expectErrors: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &ValidationResult{Valid: true}
			validateStorageConfig(&tt.config, result)

			if len(result.Errors) != tt.expectErrors {
				t.Errorf("Expected %d errors, got %d. Errors: %v",
					tt.expectErrors, len(result.Errors), result.Errors)
			}

			if len(result.Warnings) != tt.expectWarnings {
				t.Errorf("Expected %d warnings, got %d. Warnings: %v",
					tt.expectWarnings, len(result.Warnings), result.Warnings)
			}
		})
	}
}

func TestPoolsConfigValidation(t *testing.T) {
	tests := []struct {
		name           string
		pools          []PoolConfig
		expectErrors   int
		expectWarnings int
	}{
		{
			name: "valid pools config",
			pools: []PoolConfig{
				{
					Name:              "web",
					FastCGIEndpoint:   "unix:/var/run/php-fpm/web.sock",
					FastCGIStatusPath: "/status",
					MaxWorkers:        10,
					HealthCheck: HealthConfig{
						Enabled:  true,
						Interval: 30 * time.Second,
						Timeout:  5 * time.Second,
						Retries:  3,
						Endpoint: "http://localhost:9000/ping",
					},
					Resources: ResourceLimits{
						CPUThreshold:    80.0,
						MemoryThreshold: 85.0,
						MaxMemoryMB:     512,
					},
				},
			},
			expectErrors: 0,
		},
		{
			name:         "no pools configured",
			pools:        []PoolConfig{},
			expectErrors: 1,
		},
		{
			name: "duplicate pool names",
			pools: []PoolConfig{
				{
					Name:              "web",
					FastCGIEndpoint:   "unix:/var/run/php-fpm/web1.sock",
					FastCGIStatusPath: "/status",
				},
				{
					Name:              "web", // duplicate
					FastCGIEndpoint:   "unix:/var/run/php-fpm/web2.sock",
					FastCGIStatusPath: "/status",
				},
			},
			expectErrors: 1,
		},
		{
			name: "duplicate FastCGI endpoints",
			pools: []PoolConfig{
				{
					Name:              "web1",
					FastCGIEndpoint:   "127.0.0.1:9000",
					FastCGIStatusPath: "/status",
				},
				{
					Name:              "web2",
					FastCGIEndpoint:   "127.0.0.1:9000", // duplicate
					FastCGIStatusPath: "/status",
				},
			},
			expectErrors: 1,
		},
		{
			name: "invalid FastCGI endpoint",
			pools: []PoolConfig{
				{
					Name:              "web",
					FastCGIEndpoint:   "invalid-endpoint",
					FastCGIStatusPath: "/status",
				},
			},
			expectErrors: 1,
		},
		{
			name: "invalid status path",
			pools: []PoolConfig{
				{
					Name:              "web",
					FastCGIEndpoint:   "unix:/var/run/php-fpm/web.sock",
					FastCGIStatusPath: "status", // missing leading slash
				},
			},
			expectErrors: 1,
		},
		{
			name: "very high max workers",
			pools: []PoolConfig{
				{
					Name:              "web",
					FastCGIEndpoint:   "unix:/var/run/php-fpm/web.sock",
					FastCGIStatusPath: "/status",
					MaxWorkers:        2000, // very high
				},
			},
			expectWarnings: 1,
		},
		{
			name: "invalid health check configuration",
			pools: []PoolConfig{
				{
					Name:              "web",
					FastCGIEndpoint:   "unix:/var/run/php-fpm/web.sock",
					FastCGIStatusPath: "/status",
					HealthCheck: HealthConfig{
						Enabled:  true,
						Interval: 5 * time.Second,
						Timeout:  10 * time.Second, // timeout > interval
						Retries:  0,                // too few retries
					},
				},
			},
			expectErrors: 2,
		},
		{
			name: "invalid resource limits",
			pools: []PoolConfig{
				{
					Name:              "web",
					FastCGIEndpoint:   "unix:/var/run/php-fpm/web.sock",
					FastCGIStatusPath: "/status",
					Resources: ResourceLimits{
						CPUThreshold:    150.0, // > 100
						MemoryThreshold: -10.0, // < 0
						MaxMemoryMB:     -100,  // negative
					},
				},
			},
			expectErrors: 3,
		},
		{
			name: "invalid scaling configuration",
			pools: []PoolConfig{
				{
					Name:              "web",
					FastCGIEndpoint:   "unix:/var/run/php-fpm/web.sock",
					FastCGIStatusPath: "/status",
					MaxWorkers:        20,
					Scaling: ScalingConfig{
						Enabled:           true,
						MinWorkers:        30,  // > MaxWorkers
						MaxWorkers:        25,  // > pool MaxWorkers
						TargetUtilization: 1.5, // > 1.0
					},
				},
			},
			expectErrors:   2, // min > max, target > 1.0
			expectWarnings: 1, // scaling max > pool max
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &ValidationResult{Valid: true}
			validatePoolsConfig(tt.pools, result)

			if len(result.Errors) != tt.expectErrors {
				t.Errorf("Expected %d errors, got %d. Errors: %v",
					tt.expectErrors, len(result.Errors), result.Errors)
			}

			if len(result.Warnings) != tt.expectWarnings {
				t.Errorf("Expected %d warnings, got %d. Warnings: %v",
					tt.expectWarnings, len(result.Warnings), result.Warnings)
			}
		})
	}
}

func TestMonitoringConfigValidation(t *testing.T) {
	tests := []struct {
		name           string
		config         MonitoringConfig
		expectErrors   int
		expectWarnings int
	}{
		{
			name: "valid monitoring config",
			config: MonitoringConfig{
				CollectInterval:     time.Second,
				HealthCheckInterval: 5 * time.Second,
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
			},
			expectErrors: 0,
		},
		{
			name: "invalid intervals and thresholds",
			config: MonitoringConfig{
				CollectInterval:     0,     // invalid
				HealthCheckInterval: -5,    // invalid
				CPUThreshold:        150.0, // > 100
				MemoryThreshold:     -10.0, // < 0
			},
			expectErrors: 4,
		},
		{
			name: "very frequent collection",
			config: MonitoringConfig{
				CollectInterval:     100 * time.Millisecond, // very frequent
				HealthCheckInterval: time.Second,
				CPUThreshold:        80.0,
				MemoryThreshold:     85.0,
			},
			expectWarnings: 1,
		},
		{
			name: "backpressure threshold > channel size",
			config: MonitoringConfig{
				CollectInterval:     time.Second,
				HealthCheckInterval: 5 * time.Second,
				CPUThreshold:        80.0,
				MemoryThreshold:     85.0,
				Buffering: BufferingConfig{
					ChannelSize:           1000,
					BackpressureThreshold: 1500, // > channel size
					EnableBackpressure:    true,
				},
			},
			expectErrors: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &ValidationResult{Valid: true}
			validateMonitoringConfig(&tt.config, result)

			if len(result.Errors) != tt.expectErrors {
				t.Errorf("Expected %d errors, got %d. Errors: %v",
					tt.expectErrors, len(result.Errors), result.Errors)
			}

			if len(result.Warnings) != tt.expectWarnings {
				t.Errorf("Expected %d warnings, got %d. Warnings: %v",
					tt.expectWarnings, len(result.Warnings), result.Warnings)
			}
		})
	}
}

func TestTelemetryConfigValidation(t *testing.T) {
	tests := []struct {
		name         string
		config       TelemetryConfig
		expectErrors int
	}{
		{
			name: "valid telemetry config",
			config: TelemetryConfig{
				Enabled:        true,
				ServiceName:    "test-service",
				ServiceVersion: "1.0.0",
				Environment:    "test",
				Exporter: TelemetryExporterConfig{
					Type:     "stdout",
					Endpoint: "",
				},
				Sampling: TelemetrySamplingConfig{
					Rate: 0.1,
				},
			},
			expectErrors: 0,
		},
		{
			name: "disabled telemetry",
			config: TelemetryConfig{
				Enabled: false,
				// Empty fields should not cause errors when disabled
			},
			expectErrors: 0,
		},
		{
			name: "missing required fields",
			config: TelemetryConfig{
				Enabled:        true,
				ServiceName:    "", // empty
				ServiceVersion: "", // empty
				Environment:    "", // empty
				Exporter: TelemetryExporterConfig{
					Type: "stdout",
				},
				Sampling: TelemetrySamplingConfig{
					Rate: 0.1,
				},
			},
			expectErrors: 3,
		},
		{
			name: "invalid exporter type",
			config: TelemetryConfig{
				Enabled:        true,
				ServiceName:    "test-service",
				ServiceVersion: "1.0.0",
				Environment:    "test",
				Exporter: TelemetryExporterConfig{
					Type: "invalid", // invalid type
				},
			},
			expectErrors: 1,
		},
		{
			name: "OTLP without endpoint",
			config: TelemetryConfig{
				Enabled:        true,
				ServiceName:    "test-service",
				ServiceVersion: "1.0.0",
				Environment:    "test",
				Exporter: TelemetryExporterConfig{
					Type:     "otlp",
					Endpoint: "", // missing for OTLP
				},
			},
			expectErrors: 1,
		},
		{
			name: "invalid sampling rate",
			config: TelemetryConfig{
				Enabled:        true,
				ServiceName:    "test-service",
				ServiceVersion: "1.0.0",
				Environment:    "test",
				Exporter: TelemetryExporterConfig{
					Type: "stdout",
				},
				Sampling: TelemetrySamplingConfig{
					Rate: 1.5, // > 1.0
				},
			},
			expectErrors: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &ValidationResult{Valid: true}
			validateTelemetryConfig(&tt.config, result)

			if len(result.Errors) != tt.expectErrors {
				t.Errorf("Expected %d errors, got %d. Errors: %v",
					tt.expectErrors, len(result.Errors), result.Errors)
			}
		})
	}
}

func TestSecurityConfigValidation(t *testing.T) {
	tests := []struct {
		name           string
		config         SecurityConfig
		expectErrors   int
		expectWarnings int
	}{
		{
			name: "valid security config",
			config: SecurityConfig{
				AllowInternalNetworks: false,
				AllowedNetworks:       []string{"192.168.1.0/24", "10.0.0.0/8"},
				AllowedSchemes:        []string{"http", "https"},
				MaxPoolNameLength:     64,
				MaxAPIKeyLength:       128,
				MaxEventSummaryLength: 1024,
			},
			expectErrors: 0,
		},
		{
			name: "invalid CIDR networks",
			config: SecurityConfig{
				AllowedNetworks:       []string{"192.168.1", "invalid-cidr"},
				MaxPoolNameLength:     64,
				MaxAPIKeyLength:       128,
				MaxEventSummaryLength: 1024,
			},
			expectErrors: 2,
		},
		{
			name: "invalid limits",
			config: SecurityConfig{
				MaxPoolNameLength:     0,      // too small
				MaxAPIKeyLength:       -10,    // negative
				MaxEventSummaryLength: 200000, // too large
			},
			expectErrors: 3,
		},
		{
			name: "uncommon schemes",
			config: SecurityConfig{
				AllowedSchemes:        []string{"ftp", "gopher"},
				MaxPoolNameLength:     64,
				MaxAPIKeyLength:       128,
				MaxEventSummaryLength: 1024,
			},
			expectWarnings: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &ValidationResult{Valid: true}
			validateSecurityConfig(&tt.config, result)

			if len(result.Errors) != tt.expectErrors {
				t.Errorf("Expected %d errors, got %d. Errors: %v",
					tt.expectErrors, len(result.Errors), result.Errors)
			}

			if len(result.Warnings) != tt.expectWarnings {
				t.Errorf("Expected %d warnings, got %d. Warnings: %v",
					tt.expectWarnings, len(result.Warnings), result.Warnings)
			}
		})
	}
}

func TestValidationHelperFunctions(t *testing.T) {
	t.Run("validateNetworkAddress", func(t *testing.T) {
		tests := []struct {
			address string
			valid   bool
		}{
			{"127.0.0.1:8080", true},
			{"0.0.0.0:9090", true},
			{"localhost:3000", true},
			{"invalid", false},
			{":8080", false},
			{"127.0.0.1:", false},
			{"127.0.0.1:70000", false}, // port too high
		}

		for _, tt := range tests {
			err := validateNetworkAddress(tt.address)
			if tt.valid && err != nil {
				t.Errorf("Expected %s to be valid, got error: %v", tt.address, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("Expected %s to be invalid, but got no error", tt.address)
			}
		}
	})

	t.Run("validateFastCGIEndpoint", func(t *testing.T) {
		tests := []struct {
			endpoint string
			valid    bool
		}{
			{"unix:/var/run/php-fpm.sock", true},
			{"127.0.0.1:9000", true},
			{"localhost:9001", true},
			{"unix:", false},                  // empty path
			{"unix:relative/path", false},     // relative path
			{"invalid-endpoint", false},       // no colon
			{"127.0.0.1:invalid-port", false}, // invalid port
		}

		for _, tt := range tests {
			err := validateFastCGIEndpoint(tt.endpoint)
			if tt.valid && err != nil {
				t.Errorf("Expected %s to be valid, got error: %v", tt.endpoint, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("Expected %s to be invalid, but got no error", tt.endpoint)
			}
		}
	})

	t.Run("validateDuration", func(t *testing.T) {
		tests := []struct {
			duration time.Duration
			min      time.Duration
			max      time.Duration
			valid    bool
		}{
			{5 * time.Second, time.Second, 10 * time.Second, true},
			{500 * time.Millisecond, time.Second, 10 * time.Second, false}, // too small
			{15 * time.Second, time.Second, 10 * time.Second, false},       // too large
			{time.Hour, time.Minute, 0, true},                              // no max limit
		}

		for _, tt := range tests {
			err := validateDuration(tt.duration, tt.min, tt.max, "test_field")
			if tt.valid && err != nil {
				t.Errorf("Expected duration %s to be valid, got error: %v", tt.duration, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("Expected duration %s to be invalid, but got no error", tt.duration)
			}
		}
	})

	t.Run("validatePercentage", func(t *testing.T) {
		tests := []struct {
			value float64
			valid bool
		}{
			{50.0, true},
			{0.0, true},
			{100.0, true},
			{-10.0, false},
			{150.0, false},
		}

		for _, tt := range tests {
			err := validatePercentage(tt.value, "test_field")
			if tt.valid && err != nil {
				t.Errorf("Expected percentage %f to be valid, got error: %v", tt.value, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("Expected percentage %f to be invalid, but got no error", tt.value)
			}
		}
	})
}

func TestFullConfigValidation(t *testing.T) {
	// Test with a complete valid configuration
	validConfig := &Config{
		Server: ServerConfig{
			BindAddress: "0.0.0.0:9090",
			MetricsPath: "/metrics",
			HealthPath:  "/health",
			AdminPath:   "/admin",
			TLS: TLSConfig{
				Enabled: false,
			},
			Auth: AuthConfig{
				Enabled: false,
			},
			API: APIConfig{
				Enabled:     true,
				BasePath:    "/api/v1",
				MaxRequests: 100,
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
		Pools: []PoolConfig{
			{
				Name:              "web",
				FastCGIEndpoint:   "unix:/var/run/php-fpm/web.sock",
				FastCGIStatusPath: "/status",
				MaxWorkers:        10,
				HealthCheck: HealthConfig{
					Enabled:  true,
					Interval: 30 * time.Second,
					Timeout:  5 * time.Second,
					Retries:  3,
				},
				Resources: ResourceLimits{
					CPUThreshold:    80.0,
					MemoryThreshold: 85.0,
					MaxMemoryMB:     512,
				},
			},
		},
		Monitoring: MonitoringConfig{
			CollectInterval:     time.Second,
			HealthCheckInterval: 5 * time.Second,
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
		Logging: LoggingConfig{
			Level:      "info",
			Format:     "json",
			OutputPath: "stdout",
			Structured: true,
		},
		Telemetry: TelemetryConfig{
			Enabled:        true,
			ServiceName:    "phpfpm-runtime-manager",
			ServiceVersion: "1.0.0",
			Environment:    "test",
			Exporter: TelemetryExporterConfig{
				Type: "stdout",
			},
			Sampling: TelemetrySamplingConfig{
				Rate: 0.1,
			},
		},
		Security: SecurityConfig{
			AllowInternalNetworks: false,
			AllowedNetworks:       []string{"192.168.1.0/24"},
			AllowedSchemes:        []string{"http", "https"},
			MaxPoolNameLength:     64,
			MaxAPIKeyLength:       128,
			MaxEventSummaryLength: 1024,
		},
		PHPFPM: PHPFPMConfig{
			GlobalConfigPath: "/opt/phpfpm-runtime-manager/php-fpm.conf",
		},
	}

	result := validateConfiguration(validConfig)

	if !result.Valid {
		t.Errorf("Expected valid configuration to pass validation. Errors: %v", result.Errors)
	}

	if len(result.Errors) > 0 {
		t.Errorf("Expected no errors for valid config, got: %v", result.Errors)
	}
}

func TestEdgeCasesAndDefensiveCoding(t *testing.T) {
	t.Run("nil pointer safety", func(t *testing.T) {
		// These should not panic
		result := &ValidationResult{Valid: true}

		validateServerConfig(nil, result)
		validateStorageConfig(nil, result)
		validatePoolsConfig(nil, result)
		validateMonitoringConfig(nil, result)
		validateLoggingConfig(nil, result)
		validateTelemetryConfig(nil, result)
		validateSecurityConfig(nil, result)
		validatePHPFPMConfig(nil, result)

		// Should have errors due to nil configs
		if len(result.Errors) == 0 {
			t.Error("Expected errors when validating nil configs")
		}
	})

	t.Run("empty slices and maps", func(t *testing.T) {
		config := &Config{
			Pools: []PoolConfig{}, // empty pools
			Security: SecurityConfig{
				AllowedNetworks: []string{}, // empty networks
				AllowedSchemes:  []string{}, // empty schemes
			},
		}

		result := validateConfiguration(config)

		// Should fail because no pools are configured
		if result.Valid {
			t.Error("Expected validation to fail with empty pools")
		}
	})

	t.Run("very large values", func(t *testing.T) {
		config := &Config{
			Server: ServerConfig{
				BindAddress: "0.0.0.0:9090",
				MetricsPath: "/metrics",
				HealthPath:  "/health",
				AdminPath:   "/admin",
				API: APIConfig{
					MaxRequests: 1000000, // very large
				},
			},
			Pools: []PoolConfig{
				{
					Name:              "web",
					FastCGIEndpoint:   "unix:/var/run/php-fpm/web.sock",
					FastCGIStatusPath: "/status",
					MaxWorkers:        100000, // very large
				},
			},
			Storage: StorageConfig{
				DatabasePath: ":memory:",
				Aggregation: AggregationConfig{
					BatchSize: 10000000, // very large
				},
			},
			Monitoring: MonitoringConfig{
				CollectInterval:     time.Nanosecond, // very small
				HealthCheckInterval: time.Second,
			},
		}

		result := validateConfiguration(config)

		// Should have warnings about performance
		if len(result.Warnings) == 0 {
			t.Error("Expected warnings for extreme values")
		}
	})
}

// TestFilesystemValidation tests file and directory validation
func TestFilesystemValidation(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "phpfpm-config-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test file
	testFile := filepath.Join(tmpDir, "test.conf")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	t.Run("validateFilePath", func(t *testing.T) {
		tests := []struct {
			path      string
			mustExist bool
			valid     bool
		}{
			{testFile, true, true},             // existing file
			{"/nonexistent/file", true, false}, // non-existing file (must exist)
			{"/tmp/newfile", false, true},      // non-existing file (may not exist)
			{"relative/path", false, false},    // relative path
			{"", false, false},                 // empty path
		}

		for _, tt := range tests {
			err := validateFilePath(tt.path, tt.mustExist)
			if tt.valid && err != nil {
				t.Errorf("Expected path %s to be valid, got error: %v", tt.path, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("Expected path %s to be invalid, but got no error", tt.path)
			}
		}
	})

	t.Run("validateDirectoryPath", func(t *testing.T) {
		tests := []struct {
			path   string
			create bool
			valid  bool
		}{
			{tmpDir, false, true},                         // existing directory
			{"/nonexistent/dir", false, false},            // non-existing directory (don't create)
			{filepath.Join(tmpDir, "newdir"), true, true}, // non-existing directory (create)
			{testFile, false, false},                      // file, not directory
			{"relative/path", false, false},               // relative path
		}

		for _, tt := range tests {
			err := validateDirectoryPath(tt.path, tt.create)
			if tt.valid && err != nil {
				t.Errorf("Expected directory %s to be valid, got error: %v", tt.path, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("Expected directory %s to be invalid, but got no error", tt.path)
			}
		}
	})
}

// TestNetworkValidation tests network-specific validation
func TestNetworkValidation(t *testing.T) {
	t.Run("isValidHostname", func(t *testing.T) {
		tests := []struct {
			hostname string
			valid    bool
		}{
			{"localhost", true},
			{"example.com", true},
			{"sub.example.com", true},
			{"test-host", true},
			{"123host", true},
			{"-invalid", false},  // starts with dash
			{"invalid-", false},  // ends with dash
			{"host..com", false}, // double dot
			{"verylonghostnamethatisinvalidbecauseitexceedsthemaximumlengthof63characters.com", false}, // too long
			{"", false},                 // empty
			{"host with spaces", false}, // spaces
		}

		for _, tt := range tests {
			valid := isValidHostname(tt.hostname)
			if valid != tt.valid {
				t.Errorf("isValidHostname(%s) = %v, want %v", tt.hostname, valid, tt.valid)
			}
		}
	})

	t.Run("port availability check", func(t *testing.T) {
		// This test is environment-dependent, so we'll just check that the function doesn't panic
		available := isPortAvailable(0) // port 0 should be available (OS assigns random port)
		_ = available                   // Just ensure it runs without panic

		// Test obviously unavailable port (if we can bind to it, great; if not, that's also fine)
		// Just ensuring no panic
		_ = isPortAvailable(65536) // Invalid port number
	})
}
