package config

import "time"

// Application constants for configuration and resource management
const (
	// Channel Buffer Sizes
	DefaultEventChannelBuffer   = 100  // Manager event channel buffer size
	DefaultMetricsChannelBuffer = 1000 // Metrics channel buffer size

	// Timeouts and Delays
	DefaultShutdownTimeout     = 5 * time.Second        // Telemetry provider shutdown timeout
	DefaultStartupDelay        = 100 * time.Millisecond // Component startup delay
	DefaultHealthCheckInterval = 30 * time.Second       // Health check interval

	// Event and Storage Limits
	DefaultEventQueryLimit = 100  // Default limit for event queries
	DefaultAnnotationLimit = 1000 // Default limit for annotation queries
	MaxEventQueryLimit     = 1000 // Maximum allowed limit for event queries

	// Configuration Defaults
	DefaultConfigPath   = "configs/example.yaml"   // Default configuration file path
	DefaultServiceName  = "phpfpm-runtime-manager" // Default telemetry service name
	DefaultSamplingRate = 0.1                      // Default telemetry sampling rate (10%)

	// Validation Constants
	MinWorkerCount     = 1    // Minimum number of workers per pool
	MaxWorkerCount     = 1000 // Maximum number of workers per pool
	DefaultWorkerCount = 5    // Default number of workers per pool

	// API Constants
	APIVersion        = "v1"             // Current API version
	DefaultAPITimeout = 30 * time.Second // Default API request timeout

	// Security Constants
	MaxRequestBodySize = 1 << 20 // 1MB maximum request body size

	// Rate Limiting
	DefaultRateLimit = 100 // Requests per minute per IP
	BurstLimit       = 10  // Burst requests allowed
)

// Environment-specific constants
const (
	EnvDevelopment = "development"
	EnvStaging     = "staging"
	EnvProduction  = "production"
)

// Telemetry exporter types
const (
	ExporterTypeStdout = "stdout"
	ExporterTypeOTLP   = "otlp"
	ExporterTypeJaeger = "jaeger"
)

// Health states
const (
	HealthStateHealthy   = "healthy"
	HealthStateUnhealthy = "unhealthy"
	HealthStateUnknown   = "unknown"
	HealthStateStopping  = "stopping"
)
