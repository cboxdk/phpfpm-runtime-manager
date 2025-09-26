package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	oteltrace "go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Config represents telemetry configuration
type Config struct {
	Enabled        bool   `yaml:"enabled"`
	ServiceName    string `yaml:"service_name"`
	ServiceVersion string `yaml:"service_version"`
	Environment    string `yaml:"environment"`

	// Exporter configuration
	Exporter ExporterConfig `yaml:"exporter"`

	// Sampling configuration
	Sampling SamplingConfig `yaml:"sampling"`
}

// ExporterConfig configures telemetry exporters
type ExporterConfig struct {
	Type     string            `yaml:"type"` // "stdout", "otlp", "jaeger"
	Endpoint string            `yaml:"endpoint,omitempty"`
	Headers  map[string]string `yaml:"headers,omitempty"`
}

// SamplingConfig configures trace sampling
type SamplingConfig struct {
	Rate float64 `yaml:"rate"` // 0.0 to 1.0
}

// Service manages OpenTelemetry telemetry
type Service struct {
	config   Config
	logger   *zap.Logger
	provider *trace.TracerProvider
	tracer   oteltrace.Tracer
}

// NewService creates a new telemetry service
func NewService(config Config, logger *zap.Logger) (*Service, error) {
	if !config.Enabled {
		logger.Info("Telemetry disabled")
		return &Service{
			config: config,
			logger: logger,
		}, nil
	}

	// Create resource with service information
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(config.ServiceName),
			semconv.ServiceVersionKey.String(config.ServiceVersion),
			semconv.DeploymentEnvironmentKey.String(config.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create exporter based on configuration
	exporter, err := createExporter(config.Exporter)
	if err != nil {
		return nil, fmt.Errorf("failed to create exporter: %w", err)
	}

	// Create trace provider with sampling
	sampler := trace.TraceIDRatioBased(config.Sampling.Rate)
	provider := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(res),
		trace.WithSampler(sampler),
	)

	// Set global trace provider
	otel.SetTracerProvider(provider)

	// Create tracer
	tracer := provider.Tracer(config.ServiceName)

	logger.Info("Telemetry initialized",
		zap.String("service", config.ServiceName),
		zap.String("version", config.ServiceVersion),
		zap.String("environment", config.Environment),
		zap.String("exporter", config.Exporter.Type),
		zap.Float64("sampling_rate", config.Sampling.Rate))

	return &Service{
		config:   config,
		logger:   logger,
		provider: provider,
		tracer:   tracer,
	}, nil
}

// createExporter creates the appropriate exporter based on configuration
func createExporter(config ExporterConfig) (trace.SpanExporter, error) {
	switch config.Type {
	case "stdout":
		return stdouttrace.New(
			stdouttrace.WithPrettyPrint(),
		)
	case "otlp":
		if config.Endpoint == "" {
			return nil, fmt.Errorf("OTLP endpoint is required")
		}

		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpoint(config.Endpoint),
		}

		if len(config.Headers) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(config.Headers))
		}

		return otlptracehttp.New(context.Background(), opts...)
	default:
		return nil, fmt.Errorf("unsupported exporter type: %s", config.Type)
	}
}

// Start starts the telemetry service
func (s *Service) Start(ctx context.Context) error {
	if !s.config.Enabled {
		return nil
	}

	s.logger.Info("Telemetry service started")
	return nil
}

// Stop stops the telemetry service and flushes remaining spans
func (s *Service) Stop(ctx context.Context) error {
	if !s.config.Enabled || s.provider == nil {
		return nil
	}

	s.logger.Info("Stopping telemetry service")

	// Shutdown provider with timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second) // Keep specific timeout for telemetry
	defer cancel()

	if err := s.provider.Shutdown(shutdownCtx); err != nil {
		s.logger.Error("Failed to shutdown telemetry provider", zap.Error(err))
		return err
	}

	s.logger.Info("Telemetry service stopped")
	return nil
}

// Tracer returns the OpenTelemetry tracer
func (s *Service) Tracer() oteltrace.Tracer {
	if s.tracer == nil {
		return otel.Tracer("noop")
	}
	return s.tracer
}

// IsEnabled returns true if telemetry is enabled
func (s *Service) IsEnabled() bool {
	return s.config.Enabled
}
