// Package autoscaler error definitions provide structured error handling
// for the PHP-FPM autoscaling system with proper categorization and context.
//
// Error types are designed to support:
// - Detailed error context for debugging and monitoring
// - Temporary vs permanent error classification for retry logic
// - Critical error identification for alerting systems
// - Structured error wrapping for root cause analysis
package autoscaler

import (
	"errors"
	"fmt"
)

// Custom error types for better error handling and categorization.
// These errors provide structured context for different failure scenarios
// in the autoscaling system, enabling proper error handling and recovery.

var (
	// ErrInvalidPoolName indicates an invalid pool name was provided.
	// Pool names must meet validation requirements for length, characters, and format.
	ErrInvalidPoolName = errors.New("invalid pool name")

	// ErrAlreadyRunning indicates the autoscaler is already running.
	// This prevents multiple instances from interfering with each other.
	ErrAlreadyRunning = errors.New("autoscaler is already running")

	// ErrNotRunning indicates the autoscaler is not running
	ErrNotRunning = errors.New("autoscaler is not running")

	// ErrPoolNotFound indicates the specified pool was not found.
	// This occurs when operations reference pools that haven't been configured.
	ErrPoolNotFound = errors.New("pool not found")

	// ErrInvalidConfiguration indicates invalid configuration parameters.
	// This is a critical error that prevents safe autoscaling operation.
	ErrInvalidConfiguration = errors.New("invalid configuration")

	// ErrSystemMemoryUnavailable indicates system memory cannot be determined
	ErrSystemMemoryUnavailable = errors.New("system memory unavailable")

	// ErrInsufficientMemory indicates insufficient system memory for operation
	ErrInsufficientMemory = errors.New("insufficient system memory")

	// ErrInvalidWorkerCount indicates an invalid worker count was specified
	ErrInvalidWorkerCount = errors.New("invalid worker count")

	// ErrScalingDisabled indicates scaling is currently disabled
	ErrScalingDisabled = errors.New("scaling is disabled")
)

// ValidationError represents a configuration validation error with detailed context.
// This structured error type provides specific information about which configuration
// field failed validation and why, enabling precise error reporting and correction.
type ValidationError struct {
	Field   string      // The configuration field that failed validation
	Value   interface{} // The invalid value that was provided
	Message string      // Human-readable explanation of the validation failure
}

// Error implements the error interface, providing a formatted error message
// that includes the field name, invalid value, and detailed explanation.
func (e ValidationError) Error() string {
	return fmt.Sprintf("validation error for field '%s' with value '%v': %s", e.Field, e.Value, e.Message)
}

// ScalingError represents an error during scaling operations with full context.
// This error type captures the pool name, specific operation that failed,
// and the underlying cause for comprehensive error tracking and debugging.
type ScalingError struct {
	Pool      string // The PHP-FPM pool where the scaling operation failed
	Operation string // The specific scaling operation that failed (e.g., "scale_up", "config_update")
	Cause     error  // The underlying error that caused the scaling failure
}

func (e ScalingError) Error() string {
	return fmt.Sprintf("scaling error for pool '%s' during '%s': %v", e.Pool, e.Operation, e.Cause)
}

func (e ScalingError) Unwrap() error {
	return e.Cause
}

// BaselineError represents an error during baseline learning operations.
// This error type provides context about the learning phase and sample count
// to help diagnose issues with the machine learning baseline system.
type BaselineError struct {
	Phase   string // The baseline learning phase where the error occurred
	Samples int    // The number of samples collected when the error occurred
	Cause   error  // The underlying error that caused the baseline failure
}

func (e BaselineError) Error() string {
	return fmt.Sprintf("baseline error in phase '%s' with %d samples: %v", e.Phase, e.Samples, e.Cause)
}

func (e BaselineError) Unwrap() error {
	return e.Cause
}

// MetricsError represents an error during metrics collection or processing
type MetricsError struct {
	Source string
	Type   string
	Cause  error
}

func (e MetricsError) Error() string {
	return fmt.Sprintf("metrics error from '%s' for type '%s': %v", e.Source, e.Type, e.Cause)
}

func (e MetricsError) Unwrap() error {
	return e.Cause
}

// Helper functions for creating specific errors with proper context.
// These constructors ensure consistent error creation and provide
// convenient ways to wrap underlying errors with autoscaling-specific context.

// NewValidationError creates a new validation error with the specified field,
// value, and descriptive message. This is the preferred way to create
// validation errors throughout the autoscaling system.
func NewValidationError(field string, value interface{}, message string) *ValidationError {
	return &ValidationError{
		Field:   field,
		Value:   value,
		Message: message,
	}
}

// NewScalingError creates a new scaling error
func NewScalingError(pool, operation string, cause error) *ScalingError {
	return &ScalingError{
		Pool:      pool,
		Operation: operation,
		Cause:     cause,
	}
}

// NewBaselineError creates a new baseline error
func NewBaselineError(phase string, samples int, cause error) *BaselineError {
	return &BaselineError{
		Phase:   phase,
		Samples: samples,
		Cause:   cause,
	}
}

// NewMetricsError creates a new metrics error
func NewMetricsError(source, metricType string, cause error) *MetricsError {
	return &MetricsError{
		Source: source,
		Type:   metricType,
		Cause:  cause,
	}
}

// IsTemporaryError checks if an error is temporary and can be retried.
// Temporary errors are typically related to transient conditions like
// network issues, resource contention, or temporary service unavailability.
// This classification helps determine appropriate retry strategies.
func IsTemporaryError(err error) bool {
	var me *MetricsError
	if errors.As(err, &me) {
		return true // Metrics errors are usually temporary
	}

	var be *BaselineError
	if errors.As(err, &be) {
		return true // Baseline errors can often be retried
	}

	return false
}

// IsCriticalError checks if an error requires immediate attention and
// potentially disabling autoscaling to prevent system damage.
// Critical errors typically involve resource exhaustion, configuration
// corruption, or other conditions that could destabilize the system.
func IsCriticalError(err error) bool {
	return errors.Is(err, ErrInsufficientMemory) ||
		errors.Is(err, ErrSystemMemoryUnavailable) ||
		errors.Is(err, ErrInvalidConfiguration)
}
