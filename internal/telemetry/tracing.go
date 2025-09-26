package telemetry

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

const (
	// Trace operation names
	TraceMetricsCollection = "phpfpm.metrics.collection"
	TraceWorkerStart       = "phpfpm.worker.start"
	TraceWorkerStop        = "phpfpm.worker.stop"
	TraceFastCGIRequest    = "phpfpm.fastcgi.request"
	TraceOpcacheCollection = "phpfpm.opcache.collection"
	TraceScalingDecision   = "phpfpm.scaling.decision"
	TracePoolManagement    = "phpfpm.pool.management"
	TraceConfigReload      = "phpfpm.config.reload"

	// Attribute keys
	AttrPoolName        = "phpfpm.pool.name"
	AttrWorkerPID       = "phpfpm.worker.pid"
	AttrWorkerState     = "phpfpm.worker.state"
	AttrMetricsCount    = "phpfpm.metrics.count"
	AttrFastCGIEndpoint = "phpfpm.fastcgi.endpoint"
	AttrScalingAction   = "phpfpm.scaling.action"
	AttrCurrentWorkers  = "phpfpm.scaling.current_workers"
	AttrTargetWorkers   = "phpfpm.scaling.target_workers"
	AttrErrorType       = "phpfpm.error.type"
	AttrConfigPath      = "phpfpm.config.path"
)

// TraceHelper provides helper methods for creating traces
type TraceHelper struct {
	tracer oteltrace.Tracer
}

// NewTraceHelper creates a new trace helper
func NewTraceHelper(serviceName string) *TraceHelper {
	return &TraceHelper{
		tracer: otel.Tracer(serviceName),
	}
}

// StartSpan starts a new tracing span with common attributes
func (th *TraceHelper) StartSpan(ctx context.Context, operationName string, attrs ...attribute.KeyValue) (context.Context, oteltrace.Span) {
	return th.tracer.Start(ctx, operationName, oteltrace.WithAttributes(attrs...))
}

// RecordError records an error on the span
func (th *TraceHelper) RecordError(span oteltrace.Span, err error, description string) {
	if err != nil {
		span.SetStatus(codes.Error, description)
		span.RecordError(err, oteltrace.WithAttributes(
			attribute.String(AttrErrorType, description),
		))
	}
}

// SetSpanSuccess marks span as successful
func (th *TraceHelper) SetSpanSuccess(span oteltrace.Span) {
	span.SetStatus(codes.Ok, "Success")
}

// TraceMetricsCollectionFunc traces metrics collection operations
func (th *TraceHelper) TraceMetricsCollectionFunc(ctx context.Context, poolName string, fn func(context.Context) error) error {
	ctx, span := th.StartSpan(ctx, TraceMetricsCollection,
		attribute.String(AttrPoolName, poolName),
	)
	defer span.End()

	start := time.Now()
	err := fn(ctx)
	duration := time.Since(start)

	span.SetAttributes(
		attribute.Int64("duration_ms", duration.Milliseconds()),
	)

	if err != nil {
		th.RecordError(span, err, "metrics collection failed")
		return err
	}

	th.SetSpanSuccess(span)
	return nil
}

// TraceFastCGIRequestFunc traces FastCGI requests
func (th *TraceHelper) TraceFastCGIRequestFunc(ctx context.Context, endpoint string, operation string, fn func(context.Context) error) error {
	ctx, span := th.StartSpan(ctx, TraceFastCGIRequest,
		attribute.String(AttrFastCGIEndpoint, endpoint),
		attribute.String("operation", operation),
	)
	defer span.End()

	start := time.Now()
	err := fn(ctx)
	duration := time.Since(start)

	span.SetAttributes(
		attribute.Int64("duration_ms", duration.Milliseconds()),
	)

	if err != nil {
		th.RecordError(span, err, "fastcgi request failed")
		return err
	}

	th.SetSpanSuccess(span)
	return nil
}

// TraceWorkerOperationFunc traces worker lifecycle operations
func (th *TraceHelper) TraceWorkerOperationFunc(ctx context.Context, poolName string, pid int, operation string, fn func(context.Context) error) error {
	operationName := TraceWorkerStart
	if operation == "stop" {
		operationName = TraceWorkerStop
	}

	ctx, span := th.StartSpan(ctx, operationName,
		attribute.String(AttrPoolName, poolName),
		attribute.Int(AttrWorkerPID, pid),
		attribute.String("operation", operation),
	)
	defer span.End()

	start := time.Now()
	err := fn(ctx)
	duration := time.Since(start)

	span.SetAttributes(
		attribute.Int64("duration_ms", duration.Milliseconds()),
	)

	if err != nil {
		th.RecordError(span, err, "worker operation failed")
		return err
	}

	th.SetSpanSuccess(span)
	return nil
}

// TraceScalingDecisionFunc traces autoscaling decisions
func (th *TraceHelper) TraceScalingDecisionFunc(ctx context.Context, poolName string, currentWorkers, targetWorkers int, action string, fn func(context.Context) error) error {
	ctx, span := th.StartSpan(ctx, TraceScalingDecision,
		attribute.String(AttrPoolName, poolName),
		attribute.Int(AttrCurrentWorkers, currentWorkers),
		attribute.Int(AttrTargetWorkers, targetWorkers),
		attribute.String(AttrScalingAction, action),
	)
	defer span.End()

	start := time.Now()
	err := fn(ctx)
	duration := time.Since(start)

	span.SetAttributes(
		attribute.Int64("duration_ms", duration.Milliseconds()),
	)

	if err != nil {
		th.RecordError(span, err, "scaling decision failed")
		return err
	}

	th.SetSpanSuccess(span)
	return nil
}

// TraceOpcacheCollectionFunc traces OPcache metrics collection
func (th *TraceHelper) TraceOpcacheCollectionFunc(ctx context.Context, poolName string, fn func(context.Context) error) error {
	ctx, span := th.StartSpan(ctx, TraceOpcacheCollection,
		attribute.String(AttrPoolName, poolName),
	)
	defer span.End()

	start := time.Now()
	err := fn(ctx)
	duration := time.Since(start)

	span.SetAttributes(
		attribute.Int64("duration_ms", duration.Milliseconds()),
	)

	if err != nil {
		th.RecordError(span, err, "opcache collection failed")
		return err
	}

	th.SetSpanSuccess(span)
	return nil
}

// GetTraceHelper returns a trace helper instance from telemetry service
func (s *Service) GetTraceHelper() *TraceHelper {
	if !s.config.Enabled {
		return &TraceHelper{tracer: otel.Tracer("noop")}
	}
	return &TraceHelper{tracer: s.tracer}
}
