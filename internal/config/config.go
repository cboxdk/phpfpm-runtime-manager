package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	Server     ServerConfig     `yaml:"server"`
	PHPFPM     PHPFPMConfig     `yaml:"phpfpm"`
	Storage    StorageConfig    `yaml:"storage"`
	Pools      []PoolConfig     `yaml:"pools"`
	Monitoring MonitoringConfig `yaml:"monitoring"`
	Logging    LoggingConfig    `yaml:"logging"`
	Telemetry  TelemetryConfig  `yaml:"telemetry"`
	Security   SecurityConfig   `yaml:"security"`
}

// PHPFPMConfig contains global PHP-FPM configuration settings
type PHPFPMConfig struct {
	GlobalConfigPath string `yaml:"global_config_path"`
	IncludePath      string `yaml:"include_path,omitempty"`
}

// ServerConfig contains HTTP server settings
type ServerConfig struct {
	BindAddress string     `yaml:"bind_address"`
	MetricsPath string     `yaml:"metrics_path"`
	HealthPath  string     `yaml:"health_path"`
	AdminPath   string     `yaml:"admin_path"` // Admin API endpoint for management
	TLS         TLSConfig  `yaml:"tls"`
	Auth        AuthConfig `yaml:"auth"`
	API         APIConfig  `yaml:"api"`
}

// TLSConfig contains TLS/SSL settings
type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// AuthConfig contains authentication settings
type AuthConfig struct {
	Enabled bool            `yaml:"enabled"`
	Type    string          `yaml:"type"` // "api_key", "basic", "mtls"
	APIKey  string          `yaml:"api_key"`
	Basic   BasicAuthConfig `yaml:"basic"`
	APIKeys []APIKeyConfig  `yaml:"api_keys"`
}

// BasicAuthConfig contains basic authentication settings
type BasicAuthConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// APIKeyConfig contains API key authentication settings
type APIKeyConfig struct {
	Name   string   `yaml:"name"`
	Key    string   `yaml:"key"`
	Scopes []string `yaml:"scopes"`
}

// APIConfig contains REST API settings
type APIConfig struct {
	Enabled     bool   `yaml:"enabled"`
	BasePath    string `yaml:"base_path"`
	MaxRequests int    `yaml:"max_requests"`
}

// StorageConfig contains timeseries storage settings
type StorageConfig struct {
	DatabasePath   string               `yaml:"database_path"`
	Retention      RetentionConfig      `yaml:"retention"`
	Aggregation    AggregationConfig    `yaml:"aggregation"`
	ConnectionPool ConnectionPoolConfig `yaml:"connection_pool"`
}

// RetentionConfig defines data retention policies
type RetentionConfig struct {
	Raw    time.Duration `yaml:"raw"`
	Minute time.Duration `yaml:"minute"`
	Hour   time.Duration `yaml:"hour"`
}

// AggregationConfig defines aggregation settings
type AggregationConfig struct {
	Enabled   bool          `yaml:"enabled"`
	Interval  time.Duration `yaml:"interval"`
	BatchSize int           `yaml:"batch_size"`
}

// ConnectionPoolConfig contains database connection pool settings
type ConnectionPoolConfig struct {
	MaxOpenConns    int           `yaml:"max_open_conns"`
	MaxIdleConns    int           `yaml:"max_idle_conns"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `yaml:"conn_max_idle_time"`
	HealthInterval  time.Duration `yaml:"health_interval"`
	Enabled         bool          `yaml:"enabled"`
}

// PoolConfig represents a PHP-FPM pool configuration
type PoolConfig struct {
	Name              string         `yaml:"name"`
	ConfigPath        string         `yaml:"config_path,omitempty"` // Optional: separate pool config file
	FastCGIEndpoint   string         `yaml:"fastcgi_endpoint"`      // TCP: host:port or Unix: unix:/path/to/socket
	FastCGIStatusPath string         `yaml:"fastcgi_status_path"`   // Status path (e.g., "/status")
	MaxWorkers        int            `yaml:"max_workers"`
	HealthCheck       HealthConfig   `yaml:"health_check"`
	Resources         ResourceLimits `yaml:"resources"`
	Scaling           ScalingConfig  `yaml:"scaling,omitempty"`
}

// HealthConfig defines health check settings for a pool
type HealthConfig struct {
	Enabled  bool          `yaml:"enabled"`
	Interval time.Duration `yaml:"interval"`
	Timeout  time.Duration `yaml:"timeout"`
	Retries  int           `yaml:"retries"`
	Endpoint string        `yaml:"endpoint"`
}

// ResourceLimits defines resource constraints for a pool
type ResourceLimits struct {
	CPUThreshold    float64 `yaml:"cpu_threshold"`
	MemoryThreshold float64 `yaml:"memory_threshold"`
	MaxMemoryMB     int     `yaml:"max_memory_mb"`
}

// ScalingConfig defines automatic scaling settings for a pool
type ScalingConfig struct {
	Enabled           bool    `yaml:"enabled"`
	MinWorkers        int     `yaml:"min_workers"`
	MaxWorkers        int     `yaml:"max_workers"`
	TargetUtilization float64 `yaml:"target_utilization"` // 0.0 to 1.0
}

// MonitoringConfig contains metrics collection settings
type MonitoringConfig struct {
	CollectInterval     time.Duration       `yaml:"collect_interval"`
	HealthCheckInterval time.Duration       `yaml:"health_check_interval"`
	CPUThreshold        float64             `yaml:"cpu_threshold"`
	MemoryThreshold     float64             `yaml:"memory_threshold"`
	EnableOpcache       bool                `yaml:"enable_opcache"`
	EnableSystemMetrics bool                `yaml:"enable_system_metrics"`
	Batching            BatchingConfig      `yaml:"batching"`
	Buffering           BufferingConfig     `yaml:"buffering"`
	ObjectPooling       ObjectPoolingConfig `yaml:"object_pooling"`
}

// BatchingConfig contains metrics batching settings
type BatchingConfig struct {
	Enabled      bool          `yaml:"enabled"`
	Size         int           `yaml:"size"`
	FlushTimeout time.Duration `yaml:"flush_timeout"`
	MaxBatches   int           `yaml:"max_batches"`
}

// BufferingConfig contains channel buffering settings
type BufferingConfig struct {
	ChannelSize           int  `yaml:"channel_size"`
	BackpressureThreshold int  `yaml:"backpressure_threshold"`
	EnableBackpressure    bool `yaml:"enable_backpressure"`
}

// ObjectPoolingConfig contains object pooling settings
type ObjectPoolingConfig struct {
	Enabled           bool `yaml:"enabled"`
	MetricSetCapacity int  `yaml:"metric_set_capacity"`
	MetricCapacity    int  `yaml:"metric_capacity"`
	LabelMapCapacity  int  `yaml:"label_map_capacity"`
}

// LoggingConfig contains logging settings
type LoggingConfig struct {
	Level      string `yaml:"level"`
	Format     string `yaml:"format"`
	OutputPath string `yaml:"output_path"`
	Structured bool   `yaml:"structured"`
}

// TelemetryConfig contains telemetry settings
type TelemetryConfig struct {
	Enabled        bool                    `yaml:"enabled"`
	ServiceName    string                  `yaml:"service_name"`
	ServiceVersion string                  `yaml:"service_version"`
	Environment    string                  `yaml:"environment"`
	Exporter       TelemetryExporterConfig `yaml:"exporter"`
	Sampling       TelemetrySamplingConfig `yaml:"sampling"`
}

// TelemetryExporterConfig configures telemetry exporters
type TelemetryExporterConfig struct {
	Type     string            `yaml:"type"` // "stdout", "otlp", "jaeger"
	Endpoint string            `yaml:"endpoint,omitempty"`
	Headers  map[string]string `yaml:"headers,omitempty"`
}

// TelemetrySamplingConfig configures trace sampling
type TelemetrySamplingConfig struct {
	Rate float64 `yaml:"rate"` // 0.0 to 1.0
}

// SecurityConfig contains security-related configuration
type SecurityConfig struct {
	// Network access controls
	AllowInternalNetworks bool     `yaml:"allow_internal_networks"` // Allow access to internal networks (for containers)
	AllowedNetworks       []string `yaml:"allowed_networks"`        // Additional allowed network CIDRs

	// URL validation settings
	AllowedSchemes []string `yaml:"allowed_schemes"` // Allowed URL schemes (default: http, https)

	// Input validation settings
	MaxPoolNameLength     int `yaml:"max_pool_name_length"`
	MaxAPIKeyLength       int `yaml:"max_api_key_length"`
	MaxEventSummaryLength int `yaml:"max_event_summary_length"`
}

// LoadDefault creates a zero-configuration setup with all defaults
func LoadDefault() (*Config, error) {
	var config Config

	// Apply defaults (will create default "web" pool automatically)
	applyDefaults(&config)

	// Enable telemetry by default for zero-config mode
	config.Telemetry.Enabled = true

	// Create necessary directories before validation
	if err := ensureConfigDirectories(&config); err != nil {
		return nil, fmt.Errorf("directory creation failed: %w", err)
	}

	// Validate configuration
	if err := validate(&config); err != nil {
		return nil, fmt.Errorf("invalid default configuration: %w", err)
	}

	return &config, nil
}

// Load reads and parses the configuration file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Apply defaults
	applyDefaults(&config)

	// Create necessary directories before validation
	if err := ensureConfigDirectories(&config); err != nil {
		return nil, fmt.Errorf("directory creation failed: %w", err)
	}

	// Validate configuration
	if err := validate(&config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// applyDefaults sets default values for optional configuration fields
func applyDefaults(cfg *Config) {
	if cfg.Server.BindAddress == "" {
		cfg.Server.BindAddress = "0.0.0.0:9090"
	}
	if cfg.Server.MetricsPath == "" {
		cfg.Server.MetricsPath = "/metrics"
	}
	if cfg.Server.HealthPath == "" {
		cfg.Server.HealthPath = "/health"
	}
	if cfg.Server.AdminPath == "" {
		cfg.Server.AdminPath = "/admin"
	}

	// API defaults
	if cfg.Server.API.BasePath == "" {
		cfg.Server.API.BasePath = "/api/v1"
	}
	if cfg.Server.API.MaxRequests == 0 {
		cfg.Server.API.MaxRequests = 100
	}

	// Default to in-memory database
	if cfg.Storage.DatabasePath == "" {
		cfg.Storage.DatabasePath = ":memory:"
	}

	// PHP-FPM global config defaults
	if cfg.PHPFPM.GlobalConfigPath == "" {
		// Use dedicated directory for PHP-FPM Runtime Manager
		cfg.PHPFPM.GlobalConfigPath = "/opt/phpfpm-runtime-manager/php-fpm.conf"
	}
	if cfg.Storage.Retention.Raw == 0 {
		cfg.Storage.Retention.Raw = 24 * time.Hour
	}
	if cfg.Storage.Retention.Minute == 0 {
		cfg.Storage.Retention.Minute = 7 * 24 * time.Hour
	}
	if cfg.Storage.Retention.Hour == 0 {
		cfg.Storage.Retention.Hour = 365 * 24 * time.Hour
	}

	if cfg.Storage.Aggregation.Interval == 0 {
		cfg.Storage.Aggregation.Interval = time.Minute
	}
	if cfg.Storage.Aggregation.BatchSize == 0 {
		cfg.Storage.Aggregation.BatchSize = 1000
	}

	// Connection pool defaults
	if cfg.Storage.ConnectionPool.MaxOpenConns == 0 {
		cfg.Storage.ConnectionPool.MaxOpenConns = 25
	}
	if cfg.Storage.ConnectionPool.MaxIdleConns == 0 {
		cfg.Storage.ConnectionPool.MaxIdleConns = 10
	}
	if cfg.Storage.ConnectionPool.ConnMaxLifetime == 0 {
		cfg.Storage.ConnectionPool.ConnMaxLifetime = 2 * time.Hour
	}
	if cfg.Storage.ConnectionPool.ConnMaxIdleTime == 0 {
		cfg.Storage.ConnectionPool.ConnMaxIdleTime = 30 * time.Minute
	}
	if cfg.Storage.ConnectionPool.HealthInterval == 0 {
		cfg.Storage.ConnectionPool.HealthInterval = 30 * time.Second
	}
	cfg.Storage.ConnectionPool.Enabled = true // Default enabled

	if cfg.Monitoring.CollectInterval == 0 {
		cfg.Monitoring.CollectInterval = time.Second
	}
	if cfg.Monitoring.HealthCheckInterval == 0 {
		cfg.Monitoring.HealthCheckInterval = 5 * time.Second
	}
	if cfg.Monitoring.CPUThreshold == 0 {
		cfg.Monitoring.CPUThreshold = 80.0
	}
	if cfg.Monitoring.MemoryThreshold == 0 {
		cfg.Monitoring.MemoryThreshold = 85.0
	}

	// Batching defaults
	if cfg.Monitoring.Batching.Size == 0 {
		cfg.Monitoring.Batching.Size = 50
	}
	if cfg.Monitoring.Batching.FlushTimeout == 0 {
		cfg.Monitoring.Batching.FlushTimeout = 5 * time.Second
	}
	if cfg.Monitoring.Batching.MaxBatches == 0 {
		cfg.Monitoring.Batching.MaxBatches = 10
	}
	cfg.Monitoring.Batching.Enabled = true // Default enabled

	// Buffering defaults
	if cfg.Monitoring.Buffering.ChannelSize == 0 {
		if cfg.Monitoring.CollectInterval < time.Second {
			cfg.Monitoring.Buffering.ChannelSize = 5000 // High frequency
		} else {
			cfg.Monitoring.Buffering.ChannelSize = 2000 // Normal frequency
		}
	}
	if cfg.Monitoring.Buffering.BackpressureThreshold == 0 {
		cfg.Monitoring.Buffering.BackpressureThreshold = cfg.Monitoring.Buffering.ChannelSize * 3 / 4
	}
	cfg.Monitoring.Buffering.EnableBackpressure = true // Default enabled

	// Object pooling defaults
	if cfg.Monitoring.ObjectPooling.MetricSetCapacity == 0 {
		cfg.Monitoring.ObjectPooling.MetricSetCapacity = 20
	}
	if cfg.Monitoring.ObjectPooling.MetricCapacity == 0 {
		cfg.Monitoring.ObjectPooling.MetricCapacity = 4
	}
	if cfg.Monitoring.ObjectPooling.LabelMapCapacity == 0 {
		cfg.Monitoring.ObjectPooling.LabelMapCapacity = 4
	}
	cfg.Monitoring.ObjectPooling.Enabled = true // Default enabled

	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "json"
	}

	// Telemetry defaults
	if cfg.Telemetry.ServiceName == "" {
		cfg.Telemetry.ServiceName = "phpfpm-runtime-manager"
	}
	if cfg.Telemetry.ServiceVersion == "" {
		cfg.Telemetry.ServiceVersion = "1.0.0"
	}
	if cfg.Telemetry.Environment == "" {
		cfg.Telemetry.Environment = "development"
	}
	if cfg.Telemetry.Exporter.Type == "" {
		cfg.Telemetry.Exporter.Type = "stdout"
	}
	if cfg.Telemetry.Sampling.Rate == 0 {
		cfg.Telemetry.Sampling.Rate = 0.1 // 10% sampling by default
	}

	// Security defaults
	if len(cfg.Security.AllowedSchemes) == 0 {
		cfg.Security.AllowedSchemes = []string{"http", "https"}
	}
	if cfg.Security.MaxPoolNameLength == 0 {
		cfg.Security.MaxPoolNameLength = 64
	}
	if cfg.Security.MaxAPIKeyLength == 0 {
		cfg.Security.MaxAPIKeyLength = 128
	}
	if cfg.Security.MaxEventSummaryLength == 0 {
		cfg.Security.MaxEventSummaryLength = 1024
	}
	// Default to false for production security, can be overridden for containers
	// cfg.Security.AllowInternalNetworks defaults to false

	// Create default "web" pool if no pools are configured
	if len(cfg.Pools) == 0 {
		cfg.Pools = []PoolConfig{
			{
				Name:              "web",
				FastCGIEndpoint:   "unix:/opt/phpfpm-runtime-manager/sockets/web.sock",
				FastCGIStatusPath: "/status",
				MaxWorkers:        0, // 0 = autoscaling will calculate dynamically
			},
		}
	}

	// Auto-assign FastCGI endpoints if not specified
	usedSockets := make(map[string]bool)
	usedPorts := make(map[int]bool)

	// Get base directory for socket paths
	baseDir := filepath.Dir(cfg.PHPFPM.GlobalConfigPath)
	if baseDir == "" || baseDir == "." {
		baseDir = "/opt/phpfpm-runtime-manager"
	}

	// First pass: collect already assigned endpoints
	for _, pool := range cfg.Pools {
		if pool.FastCGIEndpoint != "" {
			if strings.HasPrefix(pool.FastCGIEndpoint, "unix:") {
				usedSockets[pool.FastCGIEndpoint] = true
			} else {
				// TCP endpoint - collect used ports
				if host, portStr, err := net.SplitHostPort(pool.FastCGIEndpoint); err == nil && host == "127.0.0.1" {
					if port, err := strconv.Atoi(portStr); err == nil {
						usedPorts[port] = true
					}
				}
			}
		}
	}

	// Second pass: assign missing endpoints (default to Unix sockets)
	for i := range cfg.Pools {
		pool := &cfg.Pools[i]

		// Auto-assign FastCGI endpoint if not specified
		if pool.FastCGIEndpoint == "" {
			// Default to Unix socket path based on pool name
			socketPath := fmt.Sprintf("unix:%s/sockets/%s.sock", baseDir, pool.Name)

			// Ensure unique socket path
			counter := 1
			for usedSockets[socketPath] {
				socketPath = fmt.Sprintf("unix:%s/sockets/%s-%d.sock", baseDir, pool.Name, counter)
				counter++
			}

			pool.FastCGIEndpoint = socketPath
			usedSockets[socketPath] = true
		}

		// Apply defaults for each pool
		// Health check defaults
		if pool.HealthCheck.Interval == 0 {
			pool.HealthCheck.Interval = 30 * time.Second
		}
		if pool.HealthCheck.Timeout == 0 {
			pool.HealthCheck.Timeout = 5 * time.Second
		}
		if pool.HealthCheck.Retries == 0 {
			pool.HealthCheck.Retries = 3
		}

		// Resource limits defaults
		if pool.Resources.CPUThreshold == 0 {
			pool.Resources.CPUThreshold = 80.0
		}
		if pool.Resources.MemoryThreshold == 0 {
			pool.Resources.MemoryThreshold = 85.0
		}

		// FastCGI status path default
		if pool.FastCGIStatusPath == "" {
			pool.FastCGIStatusPath = "/status"
		}

		// MaxWorkers is optional - if 0, autoscaling will calculate dynamically based on system resources

		// Scaling defaults (if enabled)
		if pool.Scaling.Enabled {
			if pool.Scaling.MinWorkers == 0 {
				pool.Scaling.MinWorkers = maxInt(1, pool.MaxWorkers/4) // 25% of max workers
			}
			if pool.Scaling.MaxWorkers == 0 && pool.MaxWorkers > 0 {
				pool.Scaling.MaxWorkers = pool.MaxWorkers
			}
			if pool.Scaling.TargetUtilization == 0 {
				pool.Scaling.TargetUtilization = 0.7 // 70% utilization target
			}
		}

	}
}

// ValidationError represents a structured validation error
type ValidationError struct {
	Field      string      // Configuration field path (e.g., "pools[0].name")
	Value      interface{} // Invalid value
	Message    string      // Human-readable error message
	Suggestion string      // Suggested fix
}

// ValidationResult contains the results of configuration validation
type ValidationResult struct {
	Valid    bool              // Overall validation status
	Errors   []ValidationError // List of validation errors
	Warnings []ValidationError // List of validation warnings
}

// Error implements the error interface for ValidationResult
func (vr *ValidationResult) Error() string {
	if len(vr.Errors) == 0 {
		return "no validation errors"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("configuration validation failed with %d error(s):\n", len(vr.Errors)))

	for i, err := range vr.Errors {
		sb.WriteString(fmt.Sprintf("  %d. %s: %s", i+1, err.Field, err.Message))
		if err.Suggestion != "" {
			sb.WriteString(fmt.Sprintf(" (suggestion: %s)", err.Suggestion))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// validate checks the configuration for required fields and consistency
func validate(cfg *Config) error {
	result := validateConfiguration(cfg)
	if !result.Valid {
		return result
	}
	return nil
}

// validateConfiguration performs comprehensive validation and returns detailed results
func validateConfiguration(cfg *Config) *ValidationResult {
	result := &ValidationResult{Valid: true}

	// Validate server configuration
	validateServerConfig(&cfg.Server, result)

	// Validate storage configuration
	validateStorageConfig(&cfg.Storage, result)

	// Validate pools configuration
	validatePoolsConfig(cfg.Pools, result)

	// Validate monitoring configuration
	validateMonitoringConfig(&cfg.Monitoring, result)

	// Validate logging configuration
	validateLoggingConfig(&cfg.Logging, result)

	// Validate telemetry configuration
	validateTelemetryConfig(&cfg.Telemetry, result)

	// Validate security configuration
	validateSecurityConfig(&cfg.Security, result)

	// Validate PHP-FPM configuration
	validatePHPFPMConfig(&cfg.PHPFPM, result)

	// Set overall validation status
	result.Valid = len(result.Errors) == 0

	return result
}

// maxInt returns the maximum of two integers
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// validateServerConfig validates server configuration
func validateServerConfig(cfg *ServerConfig, result *ValidationResult) {
	if cfg == nil {
		result.Errors = append(result.Errors, ValidationError{
			Field:      "server",
			Value:      nil,
			Message:    "server configuration cannot be nil",
			Suggestion: "provide server configuration",
		})
		return
	}

	// Validate bind address
	if cfg.BindAddress == "" {
		result.Errors = append(result.Errors, ValidationError{
			Field:      "server.bind_address",
			Value:      cfg.BindAddress,
			Message:    "bind address cannot be empty",
			Suggestion: "use '0.0.0.0:9090' for all interfaces or '127.0.0.1:9090' for localhost only",
		})
	} else {
		if err := validateNetworkAddress(cfg.BindAddress); err != nil {
			result.Errors = append(result.Errors, ValidationError{
				Field:      "server.bind_address",
				Value:      cfg.BindAddress,
				Message:    fmt.Sprintf("invalid bind address: %v", err),
				Suggestion: "use format 'host:port' e.g., '0.0.0.0:9090'",
			})
		}
	}

	// Validate paths
	if cfg.MetricsPath == "" || !strings.HasPrefix(cfg.MetricsPath, "/") {
		result.Errors = append(result.Errors, ValidationError{
			Field:      "server.metrics_path",
			Value:      cfg.MetricsPath,
			Message:    "metrics path must start with '/'",
			Suggestion: "use '/metrics'",
		})
	}

	if cfg.HealthPath == "" || !strings.HasPrefix(cfg.HealthPath, "/") {
		result.Errors = append(result.Errors, ValidationError{
			Field:      "server.health_path",
			Value:      cfg.HealthPath,
			Message:    "health path must start with '/'",
			Suggestion: "use '/health'",
		})
	}

	if cfg.AdminPath == "" || !strings.HasPrefix(cfg.AdminPath, "/") {
		result.Errors = append(result.Errors, ValidationError{
			Field:      "server.admin_path",
			Value:      cfg.AdminPath,
			Message:    "admin path must start with '/'",
			Suggestion: "use '/admin'",
		})
	}

	// Validate TLS configuration
	validateTLSConfig(&cfg.TLS, result)

	// Validate authentication configuration
	validateAuthConfig(&cfg.Auth, cfg.TLS.Enabled, result)

	// Validate API configuration
	validateAPIConfig(&cfg.API, result)
}

// validateTLSConfig validates TLS configuration
func validateTLSConfig(cfg *TLSConfig, result *ValidationResult) {
	if cfg.Enabled {
		if cfg.CertFile == "" {
			result.Errors = append(result.Errors, ValidationError{
				Field:      "server.tls.cert_file",
				Value:      cfg.CertFile,
				Message:    "TLS certificate file path is required when TLS is enabled",
				Suggestion: "provide path to PEM-encoded certificate file",
			})
		} else if err := validateFilePath(cfg.CertFile, false); err != nil {
			result.Warnings = append(result.Warnings, ValidationError{
				Field:      "server.tls.cert_file",
				Value:      cfg.CertFile,
				Message:    fmt.Sprintf("certificate file issue: %v", err),
				Suggestion: "ensure file exists and is readable",
			})
		}

		if cfg.KeyFile == "" {
			result.Errors = append(result.Errors, ValidationError{
				Field:      "server.tls.key_file",
				Value:      cfg.KeyFile,
				Message:    "TLS private key file path is required when TLS is enabled",
				Suggestion: "provide path to PEM-encoded private key file",
			})
		} else if err := validateFilePath(cfg.KeyFile, false); err != nil {
			result.Warnings = append(result.Warnings, ValidationError{
				Field:      "server.tls.key_file",
				Value:      cfg.KeyFile,
				Message:    fmt.Sprintf("private key file issue: %v", err),
				Suggestion: "ensure file exists and is readable",
			})
		}
	}
}

// validateAuthConfig validates authentication configuration
func validateAuthConfig(cfg *AuthConfig, tlsEnabled bool, result *ValidationResult) {
	if !cfg.Enabled {
		return
	}

	if cfg.Type == "" {
		result.Errors = append(result.Errors, ValidationError{
			Field:      "server.auth.type",
			Value:      cfg.Type,
			Message:    "authentication type is required when auth is enabled",
			Suggestion: "use 'api_key', 'basic', or 'mtls'",
		})
		return
	}

	switch cfg.Type {
	case "api_key":
		if cfg.APIKey == "" && len(cfg.APIKeys) == 0 {
			result.Errors = append(result.Errors, ValidationError{
				Field:      "server.auth.api_key",
				Value:      nil,
				Message:    "api_key or api_keys must be configured when auth type is 'api_key'",
				Suggestion: "provide either a single api_key or list of api_keys",
			})
		}

		if cfg.APIKey != "" && len(cfg.APIKey) < 32 {
			result.Errors = append(result.Errors, ValidationError{
				Field:      "server.auth.api_key",
				Value:      "[REDACTED]",
				Message:    "API key must be at least 32 characters",
				Suggestion: "generate a secure random string of at least 32 characters",
			})
		}

		// Validate API keys array
		for i, apiKey := range cfg.APIKeys {
			if apiKey.Name == "" {
				result.Errors = append(result.Errors, ValidationError{
					Field:      fmt.Sprintf("server.auth.api_keys[%d].name", i),
					Value:      apiKey.Name,
					Message:    "API key name is required",
					Suggestion: "provide a descriptive name like 'prometheus' or 'monitoring'",
				})
			}

			if apiKey.Key == "" {
				result.Errors = append(result.Errors, ValidationError{
					Field:      fmt.Sprintf("server.auth.api_keys[%d].key", i),
					Value:      apiKey.Key,
					Message:    "API key value is required",
					Suggestion: "generate a secure random string",
				})
			} else if len(apiKey.Key) < 32 {
				result.Errors = append(result.Errors, ValidationError{
					Field:      fmt.Sprintf("server.auth.api_keys[%d].key", i),
					Value:      "[REDACTED]",
					Message:    "API key must be at least 32 characters",
					Suggestion: "generate a secure random string of at least 32 characters",
				})
			}
		}

	case "basic":
		if cfg.Basic.Username == "" {
			result.Errors = append(result.Errors, ValidationError{
				Field:      "server.auth.basic.username",
				Value:      cfg.Basic.Username,
				Message:    "username is required for basic auth",
				Suggestion: "provide a valid username",
			})
		}

		if cfg.Basic.Password == "" {
			result.Errors = append(result.Errors, ValidationError{
				Field:      "server.auth.basic.password",
				Value:      cfg.Basic.Password,
				Message:    "password is required for basic auth",
				Suggestion: "provide a secure password",
			})
		} else if len(cfg.Basic.Password) < 8 {
			result.Errors = append(result.Errors, ValidationError{
				Field:      "server.auth.basic.password",
				Value:      "[REDACTED]",
				Message:    "password must be at least 8 characters",
				Suggestion: "use a strong password with at least 8 characters",
			})
		}

	case "mtls":
		if !tlsEnabled {
			result.Errors = append(result.Errors, ValidationError{
				Field:      "server.auth.type",
				Value:      cfg.Type,
				Message:    "TLS must be enabled when using mTLS authentication",
				Suggestion: "set server.tls.enabled to true",
			})
		}

	default:
		result.Errors = append(result.Errors, ValidationError{
			Field:      "server.auth.type",
			Value:      cfg.Type,
			Message:    fmt.Sprintf("invalid authentication type '%s'", cfg.Type),
			Suggestion: "use 'api_key', 'basic', or 'mtls'",
		})
	}
}

// validateAPIConfig validates API configuration
func validateAPIConfig(cfg *APIConfig, result *ValidationResult) {
	if cfg.Enabled {
		if cfg.BasePath == "" || !strings.HasPrefix(cfg.BasePath, "/") {
			result.Errors = append(result.Errors, ValidationError{
				Field:      "server.api.base_path",
				Value:      cfg.BasePath,
				Message:    "API base path must start with '/'",
				Suggestion: "use '/api/v1'",
			})
		}

		if cfg.MaxRequests <= 0 {
			result.Errors = append(result.Errors, ValidationError{
				Field:      "server.api.max_requests",
				Value:      cfg.MaxRequests,
				Message:    "max requests must be positive",
				Suggestion: "use a value like 100 for rate limiting",
			})
		} else if cfg.MaxRequests > 10000 {
			result.Warnings = append(result.Warnings, ValidationError{
				Field:      "server.api.max_requests",
				Value:      cfg.MaxRequests,
				Message:    "very high max requests limit may impact performance",
				Suggestion: "consider lowering to a reasonable limit like 1000",
			})
		}
	}
}

// validateFastCGIEndpoint validates the format of a FastCGI endpoint
func validateFastCGIEndpoint(endpoint string) error {
	if endpoint == "" {
		return fmt.Errorf("endpoint cannot be empty")
	}

	// Check for Unix socket prefix
	if strings.HasPrefix(endpoint, "unix:") {
		socketPath := strings.TrimPrefix(endpoint, "unix:")
		if socketPath == "" {
			return fmt.Errorf("unix socket path cannot be empty")
		}
		if !strings.HasPrefix(socketPath, "/") {
			return fmt.Errorf("unix socket path must be absolute")
		}
		return nil
	}

	// Validate TCP endpoint (host:port)
	if !strings.Contains(endpoint, ":") {
		return fmt.Errorf("TCP endpoint must be in format host:port")
	}

	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		return fmt.Errorf("invalid host:port format: %w", err)
	}

	if host == "" {
		return fmt.Errorf("host cannot be empty")
	}

	if port == "" {
		return fmt.Errorf("port cannot be empty")
	}

	// Validate that we can parse the address
	if _, err := net.ResolveTCPAddr("tcp", endpoint); err != nil {
		return fmt.Errorf("invalid TCP address: %w", err)
	}

	return nil
}

// validateNetworkAddress validates a network address in host:port format
func validateNetworkAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid address format: %w", err)
	}

	if host == "" {
		return fmt.Errorf("host cannot be empty")
	}

	if port == "" {
		return fmt.Errorf("port cannot be empty")
	}

	// Validate port number
	portNum, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("invalid port number: %w", err)
	}

	if portNum < 1 || portNum > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}

	// Validate host
	if ip := net.ParseIP(host); ip == nil {
		// Not an IP, try to resolve as hostname
		if _, err := net.LookupHost(host); err != nil {
			// If we can't resolve it, it might still be valid at runtime
			// Just check for basic format validity
			if !isValidHostname(host) {
				return fmt.Errorf("invalid hostname format")
			}
		}
	}

	return nil
}

// validateFilePath validates a file path exists and is accessible
func validateFilePath(path string, mustExist bool) error {
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}

	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute")
	}

	if mustExist {
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("file does not exist")
			}
			return fmt.Errorf("cannot access file: %w", err)
		}
	}

	return nil
}

// validateDirectoryPath validates a directory path and optionally creates it
func validateDirectoryPath(path string, createIfNotExist bool) error {
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}

	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute")
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			if createIfNotExist {
				return os.MkdirAll(path, 0755)
			}
			return fmt.Errorf("directory does not exist")
		}
		return fmt.Errorf("cannot access directory: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("path is not a directory")
	}

	return nil
}

// isValidHostname validates hostname format
func isValidHostname(hostname string) bool {
	hostnameRegex := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)
	parts := strings.Split(hostname, ".")

	for _, part := range parts {
		if !hostnameRegex.MatchString(part) {
			return false
		}
	}

	return true
}

// validateDuration validates a duration is within acceptable bounds
func validateDuration(d time.Duration, min, max time.Duration, fieldName string) *ValidationError {
	if d < min {
		return &ValidationError{
			Field:      fieldName,
			Value:      d.String(),
			Message:    fmt.Sprintf("duration %s is below minimum %s", d, min),
			Suggestion: fmt.Sprintf("use a value >= %s", min),
		}
	}

	if max > 0 && d > max {
		return &ValidationError{
			Field:      fieldName,
			Value:      d.String(),
			Message:    fmt.Sprintf("duration %s is above maximum %s", d, max),
			Suggestion: fmt.Sprintf("use a value <= %s", max),
		}
	}

	return nil
}

// validatePercentage validates a percentage value (0-100)
func validatePercentage(value float64, fieldName string) *ValidationError {
	if value < 0 || value > 100 {
		return &ValidationError{
			Field:      fieldName,
			Value:      value,
			Message:    "percentage must be between 0 and 100",
			Suggestion: "use a value between 0.0 and 100.0",
		}
	}
	return nil
}

// validatePositiveInt validates a positive integer
func validatePositiveInt(value int, fieldName string) *ValidationError {
	if value <= 0 {
		return &ValidationError{
			Field:      fieldName,
			Value:      value,
			Message:    "value must be positive",
			Suggestion: "use a value > 0",
		}
	}
	return nil
}

// validateStringNotEmpty validates a string is not empty
func validateStringNotEmpty(value, fieldName string) *ValidationError {
	if strings.TrimSpace(value) == "" {
		return &ValidationError{
			Field:      fieldName,
			Value:      value,
			Message:    "value cannot be empty",
			Suggestion: "provide a non-empty value",
		}
	}
	return nil
}

// validateURL validates a URL format
func validateURL(urlStr, fieldName string) *ValidationError {
	if urlStr == "" {
		return &ValidationError{
			Field:      fieldName,
			Value:      urlStr,
			Message:    "URL cannot be empty",
			Suggestion: "provide a valid URL",
		}
	}

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return &ValidationError{
			Field:      fieldName,
			Value:      urlStr,
			Message:    fmt.Sprintf("invalid URL format: %v", err),
			Suggestion: "use format like 'http://localhost:4317'",
		}
	}

	if parsedURL.Scheme == "" {
		return &ValidationError{
			Field:      fieldName,
			Value:      urlStr,
			Message:    "URL must include scheme (http/https)",
			Suggestion: "add 'http://' or 'https://' prefix",
		}
	}

	return nil
}

// isPortAvailable checks if a TCP port is available for binding
func isPortAvailable(port int) bool {
	address := fmt.Sprintf("127.0.0.1:%d", port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return false
	}
	listener.Close()
	return true
}

// validateStorageConfig validates storage configuration
func validateStorageConfig(cfg *StorageConfig, result *ValidationResult) {
	if cfg == nil {
		result.Errors = append(result.Errors, ValidationError{
			Field:      "storage",
			Value:      nil,
			Message:    "storage configuration cannot be nil",
			Suggestion: "provide storage configuration",
		})
		return
	}

	// Validate database path
	if cfg.DatabasePath == "" {
		result.Errors = append(result.Errors, ValidationError{
			Field:      "storage.database_path",
			Value:      cfg.DatabasePath,
			Message:    "database path cannot be empty",
			Suggestion: "use ':memory:' for in-memory database or provide a file path",
		})
	} else if cfg.DatabasePath != ":memory:" {
		// For file-based databases, validate the directory exists
		dir := filepath.Dir(cfg.DatabasePath)
		if dir != "." && dir != "/" {
			if err := validateDirectoryPath(dir, false); err != nil {
				result.Warnings = append(result.Warnings, ValidationError{
					Field:      "storage.database_path",
					Value:      cfg.DatabasePath,
					Message:    fmt.Sprintf("database directory issue: %v", err),
					Suggestion: "ensure parent directory exists and is writable",
				})
			}
		}
	}

	// Validate retention policies
	if err := validateDuration(cfg.Retention.Raw, time.Minute, 365*24*time.Hour, "storage.retention.raw"); err != nil {
		result.Errors = append(result.Errors, *err)
	}

	if err := validateDuration(cfg.Retention.Minute, time.Hour, 365*24*time.Hour, "storage.retention.minute"); err != nil {
		result.Errors = append(result.Errors, *err)
	}

	if err := validateDuration(cfg.Retention.Hour, 24*time.Hour, 10*365*24*time.Hour, "storage.retention.hour"); err != nil {
		result.Errors = append(result.Errors, *err)
	}

	// Check retention hierarchy (raw < minute < hour)
	if cfg.Retention.Raw > cfg.Retention.Minute {
		result.Warnings = append(result.Warnings, ValidationError{
			Field:      "storage.retention",
			Value:      fmt.Sprintf("raw: %s, minute: %s", cfg.Retention.Raw, cfg.Retention.Minute),
			Message:    "raw retention should be shorter than minute retention",
			Suggestion: "adjust retention periods to follow hierarchy: raw < minute < hour",
		})
	}

	if cfg.Retention.Minute > cfg.Retention.Hour {
		result.Warnings = append(result.Warnings, ValidationError{
			Field:      "storage.retention",
			Value:      fmt.Sprintf("minute: %s, hour: %s", cfg.Retention.Minute, cfg.Retention.Hour),
			Message:    "minute retention should be shorter than hour retention",
			Suggestion: "adjust retention periods to follow hierarchy: raw < minute < hour",
		})
	}

	// Validate aggregation configuration
	validateAggregationConfig(&cfg.Aggregation, result)

	// Validate connection pool configuration
	validateConnectionPoolConfig(&cfg.ConnectionPool, result)
}

// validateAggregationConfig validates aggregation configuration
func validateAggregationConfig(cfg *AggregationConfig, result *ValidationResult) {
	if cfg.Enabled {
		if err := validateDuration(cfg.Interval, 10*time.Second, time.Hour, "storage.aggregation.interval"); err != nil {
			result.Errors = append(result.Errors, *err)
		}

		if err := validatePositiveInt(cfg.BatchSize, "storage.aggregation.batch_size"); err != nil {
			result.Errors = append(result.Errors, *err)
		} else if cfg.BatchSize > 100000 {
			result.Warnings = append(result.Warnings, ValidationError{
				Field:      "storage.aggregation.batch_size",
				Value:      cfg.BatchSize,
				Message:    "very large batch size may impact performance",
				Suggestion: "consider using a smaller batch size like 1000-10000",
			})
		}
	}
}

// validateConnectionPoolConfig validates database connection pool configuration
func validateConnectionPoolConfig(cfg *ConnectionPoolConfig, result *ValidationResult) {
	if cfg.Enabled {
		if err := validatePositiveInt(cfg.MaxOpenConns, "storage.connection_pool.max_open_conns"); err != nil {
			result.Errors = append(result.Errors, *err)
		}

		if err := validatePositiveInt(cfg.MaxIdleConns, "storage.connection_pool.max_idle_conns"); err != nil {
			result.Errors = append(result.Errors, *err)
		}

		if cfg.MaxIdleConns > cfg.MaxOpenConns {
			result.Errors = append(result.Errors, ValidationError{
				Field:      "storage.connection_pool.max_idle_conns",
				Value:      cfg.MaxIdleConns,
				Message:    "max idle connections cannot exceed max open connections",
				Suggestion: fmt.Sprintf("set to <= %d", cfg.MaxOpenConns),
			})
		}

		if err := validateDuration(cfg.ConnMaxLifetime, time.Minute, 24*time.Hour, "storage.connection_pool.conn_max_lifetime"); err != nil {
			result.Errors = append(result.Errors, *err)
		}

		if err := validateDuration(cfg.ConnMaxIdleTime, 30*time.Second, time.Hour, "storage.connection_pool.conn_max_idle_time"); err != nil {
			result.Errors = append(result.Errors, *err)
		}

		if err := validateDuration(cfg.HealthInterval, 10*time.Second, 10*time.Minute, "storage.connection_pool.health_interval"); err != nil {
			result.Errors = append(result.Errors, *err)
		}
	}
}

// validatePoolsConfig validates pools configuration
func validatePoolsConfig(pools []PoolConfig, result *ValidationResult) {
	if pools == nil {
		result.Errors = append(result.Errors, ValidationError{
			Field:      "pools",
			Value:      nil,
			Message:    "pools configuration cannot be nil",
			Suggestion: "provide pools configuration",
		})
		return
	}

	if len(pools) == 0 {
		result.Errors = append(result.Errors, ValidationError{
			Field:      "pools",
			Value:      nil,
			Message:    "at least one pool must be configured",
			Suggestion: "add a pool configuration with name, fastcgi_endpoint, and other settings",
		})
		return
	}

	poolNames := make(map[string]bool)
	fastcgiEndpoints := make(map[string]bool)

	for i, pool := range pools {
		fieldPrefix := fmt.Sprintf("pools[%d]", i)

		// Validate pool name
		if err := validateStringNotEmpty(pool.Name, fieldPrefix+".name"); err != nil {
			result.Errors = append(result.Errors, *err)
			continue // Skip further validation for this pool
		}

		// Check for duplicate pool names
		if poolNames[pool.Name] {
			result.Errors = append(result.Errors, ValidationError{
				Field:      fieldPrefix + ".name",
				Value:      pool.Name,
				Message:    fmt.Sprintf("duplicate pool name '%s'", pool.Name),
				Suggestion: "use unique names for each pool",
			})
		}
		poolNames[pool.Name] = true

		// Validate FastCGI endpoint
		if pool.FastCGIEndpoint == "" {
			result.Errors = append(result.Errors, ValidationError{
				Field:      fieldPrefix + ".fastcgi_endpoint",
				Value:      pool.FastCGIEndpoint,
				Message:    "FastCGI endpoint cannot be empty",
				Suggestion: "use 'unix:/path/to/socket' or 'host:port' format",
			})
		} else {
			if err := validateFastCGIEndpoint(pool.FastCGIEndpoint); err != nil {
				result.Errors = append(result.Errors, ValidationError{
					Field:      fieldPrefix + ".fastcgi_endpoint",
					Value:      pool.FastCGIEndpoint,
					Message:    fmt.Sprintf("invalid FastCGI endpoint: %v", err),
					Suggestion: "use 'unix:/path/to/socket' or 'host:port' format",
				})
			}

			// Check for duplicate endpoints
			if fastcgiEndpoints[pool.FastCGIEndpoint] {
				result.Errors = append(result.Errors, ValidationError{
					Field:      fieldPrefix + ".fastcgi_endpoint",
					Value:      pool.FastCGIEndpoint,
					Message:    fmt.Sprintf("duplicate FastCGI endpoint '%s'", pool.FastCGIEndpoint),
					Suggestion: "use unique endpoints for each pool",
				})
			}
			fastcgiEndpoints[pool.FastCGIEndpoint] = true
		}

		// Validate FastCGI status path
		if err := validateStringNotEmpty(pool.FastCGIStatusPath, fieldPrefix+".fastcgi_status_path"); err != nil {
			result.Errors = append(result.Errors, *err)
		} else if !strings.HasPrefix(pool.FastCGIStatusPath, "/") {
			result.Errors = append(result.Errors, ValidationError{
				Field:      fieldPrefix + ".fastcgi_status_path",
				Value:      pool.FastCGIStatusPath,
				Message:    "status path must start with '/'",
				Suggestion: "use '/status' or '/fpm-status'",
			})
		}

		// Validate max workers (0 is allowed for auto-scaling)
		if pool.MaxWorkers < 0 {
			result.Errors = append(result.Errors, ValidationError{
				Field:      fieldPrefix + ".max_workers",
				Value:      pool.MaxWorkers,
				Message:    "max workers cannot be negative",
				Suggestion: "use 0 for auto-scaling or a positive number",
			})
		} else if pool.MaxWorkers > 1000 {
			result.Warnings = append(result.Warnings, ValidationError{
				Field:      fieldPrefix + ".max_workers",
				Value:      pool.MaxWorkers,
				Message:    "very high max workers count may exhaust system resources",
				Suggestion: "consider a lower value based on available memory",
			})
		}

		// Validate health check configuration
		validateHealthConfig(&pool.HealthCheck, fieldPrefix+".health_check", result)

		// Validate resource limits
		validateResourceLimits(&pool.Resources, fieldPrefix+".resources", result)

		// Validate scaling configuration if enabled
		if pool.Scaling.Enabled {
			validateScalingConfig(&pool.Scaling, pool.MaxWorkers, fieldPrefix+".scaling", result)
		}
	}
}

// validateHealthConfig validates health check configuration
func validateHealthConfig(cfg *HealthConfig, fieldPrefix string, result *ValidationResult) {
	if cfg.Enabled {
		if err := validateDuration(cfg.Interval, 5*time.Second, 10*time.Minute, fieldPrefix+".interval"); err != nil {
			result.Errors = append(result.Errors, *err)
		}

		if err := validateDuration(cfg.Timeout, time.Second, 30*time.Second, fieldPrefix+".timeout"); err != nil {
			result.Errors = append(result.Errors, *err)
		}

		// Timeout should be less than interval
		if cfg.Timeout >= cfg.Interval {
			result.Errors = append(result.Errors, ValidationError{
				Field:      fieldPrefix + ".timeout",
				Value:      cfg.Timeout.String(),
				Message:    "timeout must be less than interval",
				Suggestion: fmt.Sprintf("use a timeout < %s", cfg.Interval),
			})
		}

		if cfg.Retries < 1 || cfg.Retries > 10 {
			result.Errors = append(result.Errors, ValidationError{
				Field:      fieldPrefix + ".retries",
				Value:      cfg.Retries,
				Message:    "retries must be between 1 and 10",
				Suggestion: "use a value like 3 for reasonable retry behavior",
			})
		}

		if cfg.Endpoint != "" {
			if err := validateURL(cfg.Endpoint, fieldPrefix+".endpoint"); err != nil {
				result.Errors = append(result.Errors, *err)
			}
		}
	}
}

// validateResourceLimits validates resource limits configuration
func validateResourceLimits(cfg *ResourceLimits, fieldPrefix string, result *ValidationResult) {
	if err := validatePercentage(cfg.CPUThreshold, fieldPrefix+".cpu_threshold"); err != nil {
		result.Errors = append(result.Errors, *err)
	}

	if err := validatePercentage(cfg.MemoryThreshold, fieldPrefix+".memory_threshold"); err != nil {
		result.Errors = append(result.Errors, *err)
	}

	if cfg.MaxMemoryMB < 0 {
		result.Errors = append(result.Errors, ValidationError{
			Field:      fieldPrefix + ".max_memory_mb",
			Value:      cfg.MaxMemoryMB,
			Message:    "max memory cannot be negative",
			Suggestion: "use 0 for no limit or a positive value in MB",
		})
	} else if cfg.MaxMemoryMB > 0 && cfg.MaxMemoryMB < 32 {
		result.Warnings = append(result.Warnings, ValidationError{
			Field:      fieldPrefix + ".max_memory_mb",
			Value:      cfg.MaxMemoryMB,
			Message:    "very low memory limit may cause process crashes",
			Suggestion: "consider at least 64MB for typical PHP processes",
		})
	}
}

// validateScalingConfig validates scaling configuration
func validateScalingConfig(cfg *ScalingConfig, maxWorkers int, fieldPrefix string, result *ValidationResult) {
	if err := validatePositiveInt(cfg.MinWorkers, fieldPrefix+".min_workers"); err != nil {
		result.Errors = append(result.Errors, *err)
	}

	if err := validatePositiveInt(cfg.MaxWorkers, fieldPrefix+".max_workers"); err != nil {
		result.Errors = append(result.Errors, *err)
	}

	if cfg.MinWorkers > cfg.MaxWorkers {
		result.Errors = append(result.Errors, ValidationError{
			Field:      fieldPrefix + ".min_workers",
			Value:      cfg.MinWorkers,
			Message:    "min workers cannot exceed max workers",
			Suggestion: fmt.Sprintf("use a value <= %d", cfg.MaxWorkers),
		})
	}

	// Check consistency with pool max workers
	if maxWorkers > 0 && cfg.MaxWorkers > maxWorkers {
		result.Warnings = append(result.Warnings, ValidationError{
			Field:      fieldPrefix + ".max_workers",
			Value:      cfg.MaxWorkers,
			Message:    "scaling max workers exceeds pool max workers",
			Suggestion: fmt.Sprintf("consider adjusting to <= %d", maxWorkers),
		})
	}

	if cfg.TargetUtilization <= 0 || cfg.TargetUtilization > 1.0 {
		result.Errors = append(result.Errors, ValidationError{
			Field:      fieldPrefix + ".target_utilization",
			Value:      cfg.TargetUtilization,
			Message:    "target utilization must be between 0 and 1",
			Suggestion: "use a value like 0.7 for 70% utilization",
		})
	}
}

// validateMonitoringConfig validates monitoring configuration
func validateMonitoringConfig(cfg *MonitoringConfig, result *ValidationResult) {
	if cfg == nil {
		result.Errors = append(result.Errors, ValidationError{
			Field:      "monitoring",
			Value:      nil,
			Message:    "monitoring configuration cannot be nil",
			Suggestion: "provide monitoring configuration",
		})
		return
	}

	if err := validateDuration(cfg.CollectInterval, 100*time.Millisecond, time.Minute, "monitoring.collect_interval"); err != nil {
		result.Errors = append(result.Errors, *err)
	}

	if err := validateDuration(cfg.HealthCheckInterval, time.Second, 10*time.Minute, "monitoring.health_check_interval"); err != nil {
		result.Errors = append(result.Errors, *err)
	}

	if err := validatePercentage(cfg.CPUThreshold, "monitoring.cpu_threshold"); err != nil {
		result.Errors = append(result.Errors, *err)
	}

	if err := validatePercentage(cfg.MemoryThreshold, "monitoring.memory_threshold"); err != nil {
		result.Errors = append(result.Errors, *err)
	}

	// Validate batching configuration
	validateBatchingConfig(&cfg.Batching, result)

	// Validate buffering configuration
	validateBufferingConfig(&cfg.Buffering, result)

	// Validate object pooling configuration
	validateObjectPoolingConfig(&cfg.ObjectPooling, result)

	// Performance optimization warnings
	if cfg.CollectInterval < 500*time.Millisecond {
		result.Warnings = append(result.Warnings, ValidationError{
			Field:      "monitoring.collect_interval",
			Value:      cfg.CollectInterval.String(),
			Message:    "very frequent collection may impact performance",
			Suggestion: "consider using >= 500ms for production workloads",
		})
	}
}

// validateBatchingConfig validates batching configuration
func validateBatchingConfig(cfg *BatchingConfig, result *ValidationResult) {
	if cfg.Enabled {
		if err := validatePositiveInt(cfg.Size, "monitoring.batching.size"); err != nil {
			result.Errors = append(result.Errors, *err)
		}

		if err := validateDuration(cfg.FlushTimeout, time.Second, time.Minute, "monitoring.batching.flush_timeout"); err != nil {
			result.Errors = append(result.Errors, *err)
		}

		if err := validatePositiveInt(cfg.MaxBatches, "monitoring.batching.max_batches"); err != nil {
			result.Errors = append(result.Errors, *err)
		}
	}
}

// validateBufferingConfig validates buffering configuration
func validateBufferingConfig(cfg *BufferingConfig, result *ValidationResult) {
	if err := validatePositiveInt(cfg.ChannelSize, "monitoring.buffering.channel_size"); err != nil {
		result.Errors = append(result.Errors, *err)
	}

	if cfg.EnableBackpressure {
		if err := validatePositiveInt(cfg.BackpressureThreshold, "monitoring.buffering.backpressure_threshold"); err != nil {
			result.Errors = append(result.Errors, *err)
		}

		if cfg.BackpressureThreshold > cfg.ChannelSize {
			result.Errors = append(result.Errors, ValidationError{
				Field:      "monitoring.buffering.backpressure_threshold",
				Value:      cfg.BackpressureThreshold,
				Message:    "backpressure threshold cannot exceed channel size",
				Suggestion: fmt.Sprintf("use a value <= %d", cfg.ChannelSize),
			})
		}
	}
}

// validateObjectPoolingConfig validates object pooling configuration
func validateObjectPoolingConfig(cfg *ObjectPoolingConfig, result *ValidationResult) {
	if cfg.Enabled {
		if err := validatePositiveInt(cfg.MetricSetCapacity, "monitoring.object_pooling.metric_set_capacity"); err != nil {
			result.Errors = append(result.Errors, *err)
		}

		if err := validatePositiveInt(cfg.MetricCapacity, "monitoring.object_pooling.metric_capacity"); err != nil {
			result.Errors = append(result.Errors, *err)
		}

		if err := validatePositiveInt(cfg.LabelMapCapacity, "monitoring.object_pooling.label_map_capacity"); err != nil {
			result.Errors = append(result.Errors, *err)
		}
	}
}

// validateLoggingConfig validates logging configuration
func validateLoggingConfig(cfg *LoggingConfig, result *ValidationResult) {
	if cfg == nil {
		result.Errors = append(result.Errors, ValidationError{
			Field:      "logging",
			Value:      nil,
			Message:    "logging configuration cannot be nil",
			Suggestion: "provide logging configuration",
		})
		return
	}

	validLevels := map[string]bool{
		"debug": true, "info": true, "warn": true, "error": true,
	}

	if !validLevels[strings.ToLower(cfg.Level)] {
		result.Errors = append(result.Errors, ValidationError{
			Field:      "logging.level",
			Value:      cfg.Level,
			Message:    "invalid log level",
			Suggestion: "use 'debug', 'info', 'warn', or 'error'",
		})
	}

	validFormats := map[string]bool{
		"json": true, "console": true,
	}

	if !validFormats[strings.ToLower(cfg.Format)] {
		result.Errors = append(result.Errors, ValidationError{
			Field:      "logging.format",
			Value:      cfg.Format,
			Message:    "invalid log format",
			Suggestion: "use 'json' or 'console'",
		})
	}

	// Validate output path
	if cfg.OutputPath != "" && cfg.OutputPath != "stdout" && cfg.OutputPath != "stderr" {
		// It's a file path
		dir := filepath.Dir(cfg.OutputPath)
		if dir != "." && dir != "/" {
			if err := validateDirectoryPath(dir, false); err != nil {
				result.Warnings = append(result.Warnings, ValidationError{
					Field:      "logging.output_path",
					Value:      cfg.OutputPath,
					Message:    fmt.Sprintf("log directory issue: %v", err),
					Suggestion: "ensure parent directory exists and is writable",
				})
			}
		}
	}
}

// validateTelemetryConfig validates telemetry configuration
func validateTelemetryConfig(cfg *TelemetryConfig, result *ValidationResult) {
	if cfg == nil {
		result.Errors = append(result.Errors, ValidationError{
			Field:      "telemetry",
			Value:      nil,
			Message:    "telemetry configuration cannot be nil",
			Suggestion: "provide telemetry configuration",
		})
		return
	}

	if !cfg.Enabled {
		return
	}

	if err := validateStringNotEmpty(cfg.ServiceName, "telemetry.service_name"); err != nil {
		result.Errors = append(result.Errors, *err)
	}

	if err := validateStringNotEmpty(cfg.ServiceVersion, "telemetry.service_version"); err != nil {
		result.Errors = append(result.Errors, *err)
	}

	if err := validateStringNotEmpty(cfg.Environment, "telemetry.environment"); err != nil {
		result.Errors = append(result.Errors, *err)
	}

	// Validate exporter configuration
	validateTelemetryExporterConfig(&cfg.Exporter, result)

	// Validate sampling configuration
	validateTelemetrySamplingConfig(&cfg.Sampling, result)
}

// validateTelemetryExporterConfig validates telemetry exporter configuration
func validateTelemetryExporterConfig(cfg *TelemetryExporterConfig, result *ValidationResult) {
	validTypes := map[string]bool{
		"stdout": true, "otlp": true, "jaeger": true,
	}

	if !validTypes[cfg.Type] {
		result.Errors = append(result.Errors, ValidationError{
			Field:      "telemetry.exporter.type",
			Value:      cfg.Type,
			Message:    "invalid exporter type",
			Suggestion: "use 'stdout', 'otlp', or 'jaeger'",
		})
	}

	// For OTLP and Jaeger, endpoint is required
	if (cfg.Type == "otlp" || cfg.Type == "jaeger") && cfg.Endpoint == "" {
		result.Errors = append(result.Errors, ValidationError{
			Field:      "telemetry.exporter.endpoint",
			Value:      cfg.Endpoint,
			Message:    fmt.Sprintf("endpoint is required for %s exporter", cfg.Type),
			Suggestion: "provide the collector endpoint URL",
		})
	}

	// Validate endpoint URL if provided
	if cfg.Endpoint != "" {
		if err := validateURL(cfg.Endpoint, "telemetry.exporter.endpoint"); err != nil {
			result.Errors = append(result.Errors, *err)
		}
	}
}

// validateTelemetrySamplingConfig validates telemetry sampling configuration
func validateTelemetrySamplingConfig(cfg *TelemetrySamplingConfig, result *ValidationResult) {
	if cfg.Rate < 0 || cfg.Rate > 1.0 {
		result.Errors = append(result.Errors, ValidationError{
			Field:      "telemetry.sampling.rate",
			Value:      cfg.Rate,
			Message:    "sampling rate must be between 0 and 1",
			Suggestion: "use 0.1 for 10% sampling or 1.0 for all traces",
		})
	}
}

// validateSecurityConfig validates security configuration
func validateSecurityConfig(cfg *SecurityConfig, result *ValidationResult) {
	if cfg == nil {
		result.Errors = append(result.Errors, ValidationError{
			Field:      "security",
			Value:      nil,
			Message:    "security configuration cannot be nil",
			Suggestion: "provide security configuration",
		})
		return
	}

	// Validate allowed networks (CIDR format)
	for i, network := range cfg.AllowedNetworks {
		if _, _, err := net.ParseCIDR(network); err != nil {
			result.Errors = append(result.Errors, ValidationError{
				Field:      fmt.Sprintf("security.allowed_networks[%d]", i),
				Value:      network,
				Message:    fmt.Sprintf("invalid CIDR format: %v", err),
				Suggestion: "use format like '192.168.1.0/24' or '10.0.0.0/8'",
			})
		}
	}

	// Validate allowed schemes
	validSchemes := map[string]bool{
		"http": true, "https": true, "ftp": true, "ftps": true,
	}

	for i, scheme := range cfg.AllowedSchemes {
		if !validSchemes[strings.ToLower(scheme)] {
			result.Warnings = append(result.Warnings, ValidationError{
				Field:      fmt.Sprintf("security.allowed_schemes[%d]", i),
				Value:      scheme,
				Message:    "uncommon URL scheme",
				Suggestion: "typical schemes are 'http' and 'https'",
			})
		}
	}

	// Validate limits
	if cfg.MaxPoolNameLength <= 0 || cfg.MaxPoolNameLength > 1000 {
		result.Errors = append(result.Errors, ValidationError{
			Field:      "security.max_pool_name_length",
			Value:      cfg.MaxPoolNameLength,
			Message:    "max pool name length must be between 1 and 1000",
			Suggestion: "use a reasonable value like 64",
		})
	}

	if cfg.MaxAPIKeyLength <= 0 || cfg.MaxAPIKeyLength > 10000 {
		result.Errors = append(result.Errors, ValidationError{
			Field:      "security.max_api_key_length",
			Value:      cfg.MaxAPIKeyLength,
			Message:    "max API key length must be between 1 and 10000",
			Suggestion: "use a reasonable value like 128",
		})
	}

	if cfg.MaxEventSummaryLength <= 0 || cfg.MaxEventSummaryLength > 100000 {
		result.Errors = append(result.Errors, ValidationError{
			Field:      "security.max_event_summary_length",
			Value:      cfg.MaxEventSummaryLength,
			Message:    "max event summary length must be between 1 and 100000",
			Suggestion: "use a reasonable value like 1024",
		})
	}
}

// validatePHPFPMConfig validates PHP-FPM configuration
func validatePHPFPMConfig(cfg *PHPFPMConfig, result *ValidationResult) {
	if cfg == nil {
		result.Errors = append(result.Errors, ValidationError{
			Field:      "phpfpm",
			Value:      nil,
			Message:    "PHP-FPM configuration cannot be nil",
			Suggestion: "provide PHP-FPM configuration",
		})
		return
	}

	if err := validateStringNotEmpty(cfg.GlobalConfigPath, "phpfpm.global_config_path"); err != nil {
		result.Errors = append(result.Errors, *err)
	} else {
		// Check if the parent directory exists for global config
		dir := filepath.Dir(cfg.GlobalConfigPath)
		if dir != "." && dir != "/" {
			if err := validateDirectoryPath(dir, false); err != nil {
				result.Warnings = append(result.Warnings, ValidationError{
					Field:      "phpfpm.global_config_path",
					Value:      cfg.GlobalConfigPath,
					Message:    fmt.Sprintf("config directory issue: %v", err),
					Suggestion: "ensure parent directory exists and is writable",
				})
			}
		}
	}

	// Validate include path if specified
	if cfg.IncludePath != "" {
		if err := validateDirectoryPath(cfg.IncludePath, false); err != nil {
			result.Warnings = append(result.Warnings, ValidationError{
				Field:      "phpfpm.include_path",
				Value:      cfg.IncludePath,
				Message:    fmt.Sprintf("include directory issue: %v", err),
				Suggestion: "ensure directory exists and is readable",
			})
		}
	}
}

// GetValidationResult returns detailed validation results for external use
func GetValidationResult(cfg *Config) *ValidationResult {
	return validateConfiguration(cfg)
}

// ensureConfigDirectories creates all necessary directories for config-defined paths
func ensureConfigDirectories(cfg *Config) error {
	var paths []string

	// Collect all paths that need parent directories created
	if cfg.PHPFPM.GlobalConfigPath != "" {
		paths = append(paths, cfg.PHPFPM.GlobalConfigPath)
	}

	if cfg.PHPFPM.IncludePath != "" {
		// For include path, we need to ensure the directory itself exists
		if err := os.MkdirAll(cfg.PHPFPM.IncludePath, 0755); err != nil {
			return fmt.Errorf("creating include directory %s: %w", cfg.PHPFPM.IncludePath, err)
		}
	}

	// Storage database path
	if cfg.Storage.DatabasePath != "" && cfg.Storage.DatabasePath != ":memory:" {
		paths = append(paths, cfg.Storage.DatabasePath)
	}

	// Pool config paths and socket paths
	for _, pool := range cfg.Pools {
		if pool.ConfigPath != "" {
			paths = append(paths, pool.ConfigPath)
		}

		// Unix socket paths
		if strings.HasPrefix(pool.FastCGIEndpoint, "unix:") {
			socketPath := strings.TrimPrefix(pool.FastCGIEndpoint, "unix:")
			paths = append(paths, socketPath)
		}
	}

	// TLS certificate paths
	if cfg.Server.TLS.Enabled {
		if cfg.Server.TLS.CertFile != "" {
			paths = append(paths, cfg.Server.TLS.CertFile)
		}
		if cfg.Server.TLS.KeyFile != "" {
			paths = append(paths, cfg.Server.TLS.KeyFile)
		}
	}

	// Logging output path (if it's a file path, not stdout/stderr)
	if cfg.Logging.OutputPath != "" &&
		cfg.Logging.OutputPath != "stdout" &&
		cfg.Logging.OutputPath != "stderr" {
		paths = append(paths, cfg.Logging.OutputPath)
	}

	// Create parent directories for all collected paths
	for _, path := range paths {
		if path == "" {
			continue
		}

		dir := filepath.Dir(path)
		if dir != "." && dir != "/" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("creating directory %s for path %s: %w", dir, path, err)
			}
		}
	}

	return nil
}
