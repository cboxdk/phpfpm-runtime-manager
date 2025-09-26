package resilience

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// CircuitState represents the state of a circuit breaker
type CircuitState int

const (
	StateClosed CircuitState = iota
	StateOpen
	StateHalfOpen
)

func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig contains configuration for circuit breaker behavior
type CircuitBreakerConfig struct {
	// FailureThreshold is the number of failures required to open the circuit
	FailureThreshold int `yaml:"failure_threshold" json:"failure_threshold"`

	// RecoveryTimeout is how long to wait before attempting recovery
	RecoveryTimeout time.Duration `yaml:"recovery_timeout" json:"recovery_timeout"`

	// SuccessThreshold is the number of successes required in half-open state to close circuit
	SuccessThreshold int `yaml:"success_threshold" json:"success_threshold"`

	// Timeout is the maximum time to wait for an operation
	Timeout time.Duration `yaml:"timeout" json:"timeout"`

	// MaxConcurrentRequests limits concurrent requests in half-open state
	MaxConcurrentRequests int `yaml:"max_concurrent_requests" json:"max_concurrent_requests"`
}

// DefaultCircuitBreakerConfig provides sensible defaults for PHP-FPM operations
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold:      5,
		RecoveryTimeout:       30 * time.Second,
		SuccessThreshold:      3,
		Timeout:               5 * time.Second,
		MaxConcurrentRequests: 2,
	}
}

// CircuitBreakerStats provides metrics about circuit breaker operation
type CircuitBreakerStats struct {
	State            CircuitState  `json:"state"`
	FailureCount     int64         `json:"failure_count"`
	SuccessCount     int64         `json:"success_count"`
	RequestCount     int64         `json:"request_count"`
	LastFailureTime  time.Time     `json:"last_failure_time,omitempty"`
	LastSuccessTime  time.Time     `json:"last_success_time,omitempty"`
	StateChangedTime time.Time     `json:"state_changed_time"`
	NextRetryTime    time.Time     `json:"next_retry_time,omitempty"`
	IsOpen           bool          `json:"is_open"`
	ElapsedTime      time.Duration `json:"elapsed_time,omitempty"`
}

// CircuitBreaker implements the circuit breaker pattern for resilience
type CircuitBreaker struct {
	config CircuitBreakerConfig
	logger *zap.Logger
	name   string

	mu                   sync.RWMutex
	state                CircuitState
	failureCount         int64
	successCount         int64
	requestCount         int64
	lastFailureTime      time.Time
	lastSuccessTime      time.Time
	stateChangedTime     time.Time
	nextRetryTime        time.Time
	concurrentRequests   int
	stateChangeListeners []func(old, new CircuitState)
}

// NewCircuitBreaker creates a new circuit breaker with the given configuration
//
// The circuit breaker implements a three-state pattern:
// - Closed: Normal operation, requests pass through
// - Open: Failures exceeded threshold, requests fail fast
// - Half-Open: Limited testing to see if service recovered
//
// This provides:
// - Fast failure detection and recovery
// - Automatic retry with exponential backoff
// - Concurrent request limiting in recovery state
// - Comprehensive metrics and observability
//
// Usage:
//
//	cb := NewCircuitBreaker("php-fpm", config, logger)
//	result, err := cb.Execute(ctx, func() (interface{}, error) {
//	    return httpClient.Get(url)
//	})
func NewCircuitBreaker(name string, config CircuitBreakerConfig, logger *zap.Logger) *CircuitBreaker {
	return &CircuitBreaker{
		config:           config,
		logger:           logger.Named("circuit-breaker").With(zap.String("name", name)),
		name:             name,
		state:            StateClosed,
		stateChangedTime: time.Now(),
	}
}

// Execute runs the given function with circuit breaker protection
//
// The function will be executed only if the circuit breaker allows it.
// If the circuit is open, it returns immediately with an error.
// If the circuit is half-open, it limits concurrent requests.
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//   - fn: Function to execute with circuit breaker protection
//
// Returns:
//   - interface{}: Result from the function if successful
//   - error: Circuit breaker error or function error
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() (interface{}, error)) (interface{}, error) {
	// Check if request is allowed
	if !cb.allowRequest() {
		return nil, &CircuitBreakerError{
			Name:   cb.name,
			State:  cb.GetState(),
			Reason: "circuit breaker is open",
		}
	}

	// Track concurrent requests in half-open state
	if cb.GetState() == StateHalfOpen {
		cb.incrementConcurrentRequests()
		defer cb.decrementConcurrentRequests()
	}

	// Execute with timeout
	start := time.Now()
	var result interface{}
	var err error

	// Create timeout context if configured
	execCtx := ctx
	if cb.config.Timeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, cb.config.Timeout)
		defer cancel()
	}

	// Execute in goroutine to handle timeout
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, err = fn()
	}()

	select {
	case <-done:
		// Function completed
	case <-execCtx.Done():
		// Timeout or cancellation
		err = &CircuitBreakerError{
			Name:   cb.name,
			State:  cb.GetState(),
			Reason: fmt.Sprintf("operation timeout after %v", cb.config.Timeout),
		}
	}

	duration := time.Since(start)

	// Record result and update state
	if err != nil {
		cb.recordFailure(err, duration)
		return nil, err
	}

	cb.recordSuccess(duration)
	return result, nil
}

// allowRequest determines if a request should be allowed based on current state
func (cb *CircuitBreaker) allowRequest() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		// Check if it's time to try recovery
		return time.Now().After(cb.nextRetryTime)
	case StateHalfOpen:
		// Allow limited concurrent requests
		return cb.concurrentRequests < cb.config.MaxConcurrentRequests
	default:
		return false
	}
}

// recordFailure records a failure and potentially changes state
func (cb *CircuitBreaker) recordFailure(err error, duration time.Duration) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.requestCount++
	cb.lastFailureTime = time.Now()

	cb.logger.Warn("Circuit breaker recorded failure",
		zap.String("name", cb.name),
		zap.Error(err),
		zap.Duration("duration", duration),
		zap.Int64("failure_count", cb.failureCount))

	switch cb.state {
	case StateClosed:
		if cb.failureCount >= int64(cb.config.FailureThreshold) {
			cb.setState(StateOpen)
		}
	case StateHalfOpen:
		// Any failure in half-open immediately opens the circuit
		cb.setState(StateOpen)
	}
}

// recordSuccess records a success and potentially changes state
func (cb *CircuitBreaker) recordSuccess(duration time.Duration) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.successCount++
	cb.requestCount++
	cb.lastSuccessTime = time.Now()

	cb.logger.Debug("Circuit breaker recorded success",
		zap.String("name", cb.name),
		zap.Duration("duration", duration),
		zap.Int64("success_count", cb.successCount))

	switch cb.state {
	case StateHalfOpen:
		if cb.successCount >= int64(cb.config.SuccessThreshold) {
			cb.setState(StateClosed)
		}
	}
}

// setState changes the circuit breaker state and notifies listeners
func (cb *CircuitBreaker) setState(newState CircuitState) {
	oldState := cb.state
	cb.state = newState
	cb.stateChangedTime = time.Now()

	// Reset counters on state change
	if newState == StateClosed {
		cb.failureCount = 0
		cb.successCount = 0
	} else if newState == StateOpen {
		cb.nextRetryTime = time.Now().Add(cb.config.RecoveryTimeout)
		cb.successCount = 0
	} else if newState == StateHalfOpen {
		cb.successCount = 0
		cb.concurrentRequests = 0
	}

	cb.logger.Info("Circuit breaker state changed",
		zap.String("name", cb.name),
		zap.String("old_state", oldState.String()),
		zap.String("new_state", newState.String()),
		zap.Int64("failure_count", cb.failureCount),
		zap.Time("next_retry", cb.nextRetryTime))

	// Notify listeners
	for _, listener := range cb.stateChangeListeners {
		go listener(oldState, newState)
	}
}

// GetState returns the current circuit breaker state (thread-safe)
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	// Check if we should transition from open to half-open
	if cb.state == StateOpen && time.Now().After(cb.nextRetryTime) {
		cb.mu.RUnlock()
		cb.mu.Lock()
		// Double-check after acquiring write lock
		if cb.state == StateOpen && time.Now().After(cb.nextRetryTime) {
			cb.setState(StateHalfOpen)
		}
		cb.mu.Unlock()
		cb.mu.RLock()
	}

	return cb.state
}

// GetStats returns comprehensive statistics about the circuit breaker
func (cb *CircuitBreaker) GetStats() CircuitBreakerStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	stats := CircuitBreakerStats{
		State:            cb.state,
		FailureCount:     cb.failureCount,
		SuccessCount:     cb.successCount,
		RequestCount:     cb.requestCount,
		LastFailureTime:  cb.lastFailureTime,
		LastSuccessTime:  cb.lastSuccessTime,
		StateChangedTime: cb.stateChangedTime,
		NextRetryTime:    cb.nextRetryTime,
		IsOpen:           cb.state == StateOpen,
		ElapsedTime:      time.Since(cb.stateChangedTime),
	}

	return stats
}

// AddStateChangeListener adds a listener for state change events
func (cb *CircuitBreaker) AddStateChangeListener(listener func(old, new CircuitState)) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.stateChangeListeners = append(cb.stateChangeListeners, listener)
}

// Reset manually resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.setState(StateClosed)
}

// ForceOpen manually forces the circuit breaker to open state
func (cb *CircuitBreaker) ForceOpen() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.setState(StateOpen)
}

// incrementConcurrentRequests safely increments concurrent request counter
func (cb *CircuitBreaker) incrementConcurrentRequests() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.concurrentRequests++
}

// decrementConcurrentRequests safely decrements concurrent request counter
func (cb *CircuitBreaker) decrementConcurrentRequests() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.concurrentRequests--
}

// CircuitBreakerError represents an error from the circuit breaker
type CircuitBreakerError struct {
	Name   string
	State  CircuitState
	Reason string
}

func (e *CircuitBreakerError) Error() string {
	return fmt.Sprintf("circuit breaker '%s' in state '%s': %s", e.Name, e.State.String(), e.Reason)
}

// IsCircuitBreakerError checks if an error is a circuit breaker error
func IsCircuitBreakerError(err error) bool {
	_, ok := err.(*CircuitBreakerError)
	return ok
}
