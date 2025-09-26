package telemetry

import (
	"context"
	"testing"

	"go.uber.org/zap/zaptest"
)

func TestNewService(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name      string
		config    Config
		wantError bool
	}{
		{
			name: "telemetry disabled",
			config: Config{
				Enabled: false,
			},
			wantError: false,
		},
		{
			name: "telemetry enabled with stdout exporter",
			config: Config{
				Enabled:        true,
				ServiceName:    "test-service",
				ServiceVersion: "1.0.0",
				Environment:    "test",
				Exporter: ExporterConfig{
					Type: "stdout",
				},
				Sampling: SamplingConfig{
					Rate: 0.5,
				},
			},
			wantError: false,
		},
		{
			name: "otlp exporter without endpoint",
			config: Config{
				Enabled:     true,
				ServiceName: "test-service",
				Exporter: ExporterConfig{
					Type: "otlp",
				},
			},
			wantError: true,
		},
		{
			name: "unsupported exporter type",
			config: Config{
				Enabled:     true,
				ServiceName: "test-service",
				Exporter: ExporterConfig{
					Type: "unsupported",
				},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := NewService(tt.config, logger)

			if tt.wantError {
				if err == nil {
					t.Error("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if service == nil {
					t.Error("expected service but got nil")
				}
			}
		})
	}
}

func TestServiceStartStop(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name   string
		config Config
	}{
		{
			name: "disabled service",
			config: Config{
				Enabled: false,
			},
		},
		{
			name: "enabled service",
			config: Config{
				Enabled:        true,
				ServiceName:    "test-service",
				ServiceVersion: "1.0.0",
				Environment:    "test",
				Exporter: ExporterConfig{
					Type: "stdout",
				},
				Sampling: SamplingConfig{
					Rate: 1.0,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := NewService(tt.config, logger)
			if err != nil {
				t.Fatalf("failed to create service: %v", err)
			}

			ctx := context.Background()

			// Test Start
			if err := service.Start(ctx); err != nil {
				t.Errorf("Start() error: %v", err)
			}

			// Test Stop
			if err := service.Stop(ctx); err != nil {
				t.Errorf("Stop() error: %v", err)
			}
		})
	}
}

func TestServiceIsEnabled(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name     string
		config   Config
		expected bool
	}{
		{
			name: "disabled service",
			config: Config{
				Enabled: false,
			},
			expected: false,
		},
		{
			name: "enabled service",
			config: Config{
				Enabled:        true,
				ServiceName:    "test",
				ServiceVersion: "1.0.0",
				Exporter: ExporterConfig{
					Type: "stdout",
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := NewService(tt.config, logger)
			if err != nil {
				t.Fatalf("failed to create service: %v", err)
			}

			if got := service.IsEnabled(); got != tt.expected {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestServiceTracer(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name   string
		config Config
	}{
		{
			name: "disabled service returns noop tracer",
			config: Config{
				Enabled: false,
			},
		},
		{
			name: "enabled service returns valid tracer",
			config: Config{
				Enabled:        true,
				ServiceName:    "test-service",
				ServiceVersion: "1.0.0",
				Exporter: ExporterConfig{
					Type: "stdout",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := NewService(tt.config, logger)
			if err != nil {
				t.Fatalf("failed to create service: %v", err)
			}

			tracer := service.Tracer()
			if tracer == nil {
				t.Error("Tracer() returned nil")
			}
		})
	}
}
