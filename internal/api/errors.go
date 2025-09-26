package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ErrorBuilder provides a fluent interface for building structured errors
type ErrorBuilder struct {
	code       string
	message    string
	statusCode int
	details    string
	helpURL    string
	context    map[string]interface{}
	timestamp  time.Time
}

// NewError creates a new error builder
func NewError(code, message string) *ErrorBuilder {
	return &ErrorBuilder{
		code:      code,
		message:   message,
		timestamp: time.Now(),
		context:   make(map[string]interface{}),
	}
}

// WithStatus sets the HTTP status code
func (e *ErrorBuilder) WithStatus(statusCode int) *ErrorBuilder {
	e.statusCode = statusCode
	return e
}

// WithDetails adds detailed error information
func (e *ErrorBuilder) WithDetails(details string) *ErrorBuilder {
	e.details = details
	return e
}

// WithHelpURL adds a help URL for error resolution
func (e *ErrorBuilder) WithHelpURL(url string) *ErrorBuilder {
	e.helpURL = url
	return e
}

// WithContext adds contextual information
func (e *ErrorBuilder) WithContext(key string, value interface{}) *ErrorBuilder {
	e.context[key] = value
	return e
}

// Build creates the final BusinessError
func (e *ErrorBuilder) Build() *BusinessError {
	if e.statusCode == 0 {
		e.statusCode = http.StatusInternalServerError
	}

	return &BusinessError{
		Code:       e.code,
		Message:    e.message,
		Details:    e.details,
		StatusCode: e.statusCode,
		HelpURL:    e.helpURL,
		Context:    e.context,
		Timestamp:  e.timestamp,
	}
}

// Enhanced BusinessError with additional context
type BusinessError struct {
	Code       string                 `json:"code"`
	Message    string                 `json:"message"`
	Details    string                 `json:"details,omitempty"`
	StatusCode int                    `json:"-"`
	HelpURL    string                 `json:"help_url,omitempty"`
	Context    map[string]interface{} `json:"context,omitempty"`
	Timestamp  time.Time              `json:"timestamp"`
}

func (e BusinessError) Error() string {
	return e.Message
}

// Common error builders for typical scenarios
var (
	// Authentication errors
	ErrAuthenticationFailed = func(details string) *BusinessError {
		return NewError("auth_failed", "Authentication failed").
			WithStatus(http.StatusUnauthorized).
			WithDetails(details).
			WithHelpURL("/docs/api#authentication").
			Build()
	}

	ErrAuthorizationFailed = func(resource string) *BusinessError {
		return NewError("auth_insufficient", "Insufficient permissions").
			WithStatus(http.StatusForbidden).
			WithContext("resource", resource).
			WithHelpURL("/docs/api#authorization").
			Build()
	}

	// Pool management errors
	ErrPoolNotFound = func(poolName string) *BusinessError {
		return NewError("pool_not_found", "Pool not found").
			WithStatus(http.StatusNotFound).
			WithContext("pool_name", poolName).
			WithDetails(fmt.Sprintf("Pool '%s' does not exist or is not configured", poolName)).
			WithHelpURL("/docs/pools#management").
			Build()
	}

	ErrPoolAlreadyRunning = func(poolName string) *BusinessError {
		return NewError("pool_already_running", "Pool is already running").
			WithStatus(http.StatusConflict).
			WithContext("pool_name", poolName).
			WithDetails("Use restart endpoint to force restart").
			WithHelpURL("/docs/pools#lifecycle").
			Build()
	}

	ErrPoolStartFailed = func(poolName string, reason error) *BusinessError {
		return NewError("pool_start_failed", "Failed to start pool").
			WithStatus(http.StatusInternalServerError).
			WithContext("pool_name", poolName).
			WithDetails(reason.Error()).
			WithHelpURL("/docs/troubleshooting#pool-startup").
			Build()
	}

	// Configuration errors
	ErrConfigInvalid = func(validationErrors []string) *BusinessError {
		err := NewError("config_invalid", "Configuration validation failed").
			WithStatus(http.StatusBadRequest).
			WithHelpURL("/docs/configuration#validation")

		for i, validationError := range validationErrors {
			err.WithContext(fmt.Sprintf("error_%d", i), validationError)
		}

		return err.Build()
	}

	ErrConfigReloadFailed = func(reason error) *BusinessError {
		return NewError("config_reload_failed", "Configuration reload failed").
			WithStatus(http.StatusInternalServerError).
			WithDetails(reason.Error()).
			WithHelpURL("/docs/troubleshooting#configuration-reload").
			Build()
	}

	// Request validation errors
	ErrInvalidJSON = func(parseError error) *BusinessError {
		return NewError("invalid_json", "Invalid JSON in request body").
			WithStatus(http.StatusBadRequest).
			WithDetails(parseError.Error()).
			WithHelpURL("/docs/api#request-format").
			Build()
	}

	ErrMissingParameter = func(paramName string) *BusinessError {
		return NewError("missing_parameter", "Required parameter missing").
			WithStatus(http.StatusBadRequest).
			WithContext("parameter", paramName).
			WithDetails(fmt.Sprintf("Parameter '%s' is required", paramName)).
			WithHelpURL("/docs/api#parameters").
			Build()
	}

	ErrInvalidParameter = func(paramName, reason string) *BusinessError {
		return NewError("invalid_parameter", "Invalid parameter value").
			WithStatus(http.StatusBadRequest).
			WithContext("parameter", paramName).
			WithDetails(reason).
			WithHelpURL("/docs/api#parameters").
			Build()
	}

	// Rate limiting errors
	ErrRateLimited = func(resetTime time.Time) *BusinessError {
		return NewError("rate_limited", "Rate limit exceeded").
			WithStatus(http.StatusTooManyRequests).
			WithContext("reset_time", resetTime.Unix()).
			WithDetails(fmt.Sprintf("Rate limit will reset at %s", resetTime.Format(time.RFC3339))).
			WithHelpURL("/docs/api#rate-limiting").
			Build()
	}

	// System errors
	ErrServiceUnavailable = func(service string, reason error) *BusinessError {
		return NewError("service_unavailable", "Service temporarily unavailable").
			WithStatus(http.StatusServiceUnavailable).
			WithContext("service", service).
			WithDetails(reason.Error()).
			WithHelpURL("/docs/troubleshooting#service-health").
			Build()
	}

	ErrInternalError = func(operation string, err error) *BusinessError {
		return NewError("internal_error", "Internal server error").
			WithStatus(http.StatusInternalServerError).
			WithContext("operation", operation).
			WithDetails("An unexpected error occurred. Please try again or contact support.").
			WithHelpURL("/docs/troubleshooting#general").
			Build()
	}
)

// ValidationError represents field-level validation errors
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Value   string `json:"value,omitempty"`
}

// ValidationErrors represents multiple validation errors
type ValidationErrors struct {
	Errors []ValidationError `json:"errors"`
}

func (v ValidationErrors) Error() string {
	return fmt.Sprintf("Validation failed with %d errors", len(v.Errors))
}

// NewValidationErrors creates a new validation errors collection
func NewValidationErrors() *ValidationErrors {
	return &ValidationErrors{
		Errors: make([]ValidationError, 0),
	}
}

// AddError adds a validation error
func (v *ValidationErrors) AddError(field, message, value string) {
	v.Errors = append(v.Errors, ValidationError{
		Field:   field,
		Message: message,
		Value:   value,
	})
}

// HasErrors returns true if there are validation errors
func (v *ValidationErrors) HasErrors() bool {
	return len(v.Errors) > 0
}

// ToBusinessError converts validation errors to a business error
func (v *ValidationErrors) ToBusinessError() *BusinessError {
	return NewError("validation_failed", "Request validation failed").
		WithStatus(http.StatusBadRequest).
		WithContext("validation_errors", v.Errors).
		WithHelpURL("/docs/api#validation").
		Build()
}

// Recovery middleware for panic handling
func RecoveryMiddleware(logger interface{}) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					// Log the panic
					if l, ok := logger.(interface{ Error(string, ...interface{}) }); ok {
						l.Error("Panic in HTTP handler: %v", err)
					}

					// Return error response
					response := StandardResponse{
						Success:   false,
						Message:   "Internal server error",
						RequestID: r.Header.Get("X-Request-ID"),
						Timestamp: time.Now(),
						Error: &ErrorInfo{
							Code:    "panic_recovered",
							Details: "An unexpected error occurred",
							HelpURL: "https://docs.phpfpm-manager.com/docs/troubleshooting#panic-recovery",
						},
					}

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)

					// Don't handle encoding errors here as we're already in error state
					_ = json.NewEncoder(w).Encode(response)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// HealthCheckError represents health check specific errors
type HealthCheckError struct {
	Component string                 `json:"component"`
	Status    string                 `json:"status"`
	Error     string                 `json:"error,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
	LastCheck time.Time              `json:"last_check"`
}

// NewHealthCheckError creates a new health check error
func NewHealthCheckError(component, status, errorMsg string) *HealthCheckError {
	return &HealthCheckError{
		Component: component,
		Status:    status,
		Error:     errorMsg,
		Details:   make(map[string]interface{}),
		LastCheck: time.Now(),
	}
}

// WithDetail adds a detail to the health check error
func (h *HealthCheckError) WithDetail(key string, value interface{}) *HealthCheckError {
	h.Details[key] = value
	return h
}

// UserFriendlyErrorMessages provides user-friendly error messages
var UserFriendlyErrorMessages = map[string]string{
	"pool_not_found":       "The requested pool doesn't exist. Check your pool configuration and try again.",
	"pool_already_running": "This pool is already running. Use the restart endpoint if you need to restart it.",
	"config_invalid":       "Your configuration has validation errors. Please check the configuration format and required fields.",
	"auth_failed":          "Authentication failed. Please check your API key or credentials.",
	"rate_limited":         "You've exceeded the API rate limit. Please wait before making more requests.",
	"service_unavailable":  "The service is temporarily unavailable. Please try again in a few moments.",
}

// GetUserFriendlyMessage returns a user-friendly message for an error code
func GetUserFriendlyMessage(errorCode string) string {
	if msg, exists := UserFriendlyErrorMessages[errorCode]; exists {
		return msg
	}
	return "An error occurred. Please check the documentation or contact support for assistance."
}
