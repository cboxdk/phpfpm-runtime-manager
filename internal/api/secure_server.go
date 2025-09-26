package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/config"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/security"
	"go.uber.org/zap"
)

// SecureServerConfig contains configuration for the secure API server
type SecureServerConfig struct {
	// Server configuration
	Address        string        `json:"address"`
	Port           int           `json:"port"`
	ReadTimeout    time.Duration `json:"read_timeout"`
	WriteTimeout   time.Duration `json:"write_timeout"`
	IdleTimeout    time.Duration `json:"idle_timeout"`
	MaxHeaderBytes int           `json:"max_header_bytes"`

	// TLS configuration
	TLSEnabled    bool   `json:"tls_enabled"`
	CertFile      string `json:"cert_file"`
	KeyFile       string `json:"key_file"`
	MinTLSVersion string `json:"min_tls_version"`

	// Security configuration
	RateLimitConfig *RateLimitConfig      `json:"rate_limit_config"`
	AuditConfig     *security.AuditConfig `json:"audit_config"`

	// CORS configuration
	CORSEnabled bool     `json:"cors_enabled"`
	CORSOrigins []string `json:"cors_origins"`
	CORSMethods []string `json:"cors_methods"`
	CORSHeaders []string `json:"cors_headers"`

	// Additional security headers
	SecurityHeaders map[string]string `json:"security_headers"`
}

// SecureServer wraps the basic Server with security features
type SecureServer struct {
	*Server
	config      *SecureServerConfig
	rateLimiter *RateLimiter
	auditLogger *security.AuditLogger
	httpServer  *http.Server
}

// NewSecureServer creates a new secure API server with rate limiting and audit logging
func NewSecureServer(config *SecureServerConfig, logger *zap.Logger,
	manager ManagerInterface, supervisor SupervisorInterface,
	collector CollectorInterface, eventStorage EventStorageInterface,
	securityConfig *config.SecurityConfig) (*SecureServer, error) {

	// Create base server
	baseServer := &Server{
		logger:         logger,
		manager:        manager,
		supervisor:     supervisor,
		collector:      collector,
		eventStorage:   eventStorage,
		securityConfig: securityConfig,
		startTime:      time.Now(),
		version:        "1.0.0", // Should come from build info
	}

	// Create rate limiter
	rateLimiter := NewRateLimiter(config.RateLimitConfig, logger)

	// Create audit logger
	auditLogger, err := security.NewAuditLogger(config.AuditConfig, logger)
	if err != nil {
		return nil, err
	}

	secureServer := &SecureServer{
		Server:      baseServer,
		config:      config,
		rateLimiter: rateLimiter,
		auditLogger: auditLogger,
	}

	return secureServer, nil
}

// Start starts the secure server with all security middleware
func (ss *SecureServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Register routes
	ss.registerRoutes(mux)

	// Build middleware chain
	handler := ss.buildMiddlewareChain(mux)

	// Create HTTP server
	ss.httpServer = &http.Server{
		Addr:           ss.getListenAddress(),
		Handler:        handler,
		ReadTimeout:    ss.config.ReadTimeout,
		WriteTimeout:   ss.config.WriteTimeout,
		IdleTimeout:    ss.config.IdleTimeout,
		MaxHeaderBytes: ss.config.MaxHeaderBytes,
	}

	ss.logger.Info("Starting secure API server",
		zap.String("address", ss.httpServer.Addr),
		zap.Bool("tls_enabled", ss.config.TLSEnabled),
		zap.Bool("rate_limiting", ss.config.RateLimitConfig != nil),
		zap.Bool("audit_logging", ss.config.AuditConfig.Enabled))

	// Start server
	if ss.config.TLSEnabled {
		return ss.httpServer.ListenAndServeTLS(ss.config.CertFile, ss.config.KeyFile)
	}
	return ss.httpServer.ListenAndServe()
}

// Stop gracefully stops the secure server
func (ss *SecureServer) Stop(ctx context.Context) error {
	ss.logger.Info("Stopping secure API server")

	// Stop rate limiter
	if ss.rateLimiter != nil {
		ss.rateLimiter.Stop()
	}

	// Stop audit logger
	if ss.auditLogger != nil {
		ss.auditLogger.Stop()
	}

	// Stop HTTP server
	if ss.httpServer != nil {
		return ss.httpServer.Shutdown(ctx)
	}

	return nil
}

// buildMiddlewareChain builds the complete middleware chain with security features
func (ss *SecureServer) buildMiddlewareChain(handler http.Handler) http.Handler {
	// Middleware chain (applied in reverse order)
	middlewares := []func(http.Handler) http.Handler{
		ss.corsMiddleware,            // CORS headers
		ss.securityHeadersMiddleware, // Security headers
		ss.auditMiddleware,           // Audit logging
		ss.rateLimitMiddleware,       // Rate limiting
		ss.recoveryMiddleware,        // Panic recovery
		ss.requestContextMiddleware,  // Request context
		ss.MetricsMiddleware,         // Metrics collection
	}

	// Apply middlewares in reverse order
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}

	return handler
}

// registerRoutes registers all API routes
func (ss *SecureServer) registerRoutes(mux *http.ServeMux) {
	// Health endpoints
	mux.HandleFunc("/health", ss.ValidationMiddleware(HandlerConfig{
		RequiredMethod: "GET",
		LogOperation:   "health_check",
	}, ss.HealthHandler))

	mux.HandleFunc("/health/ready", ss.ValidationMiddleware(HandlerConfig{
		RequiredMethod: "GET",
		LogOperation:   "readiness_check",
	}, ss.ReadinessHandler))

	mux.HandleFunc("/health/live", ss.ValidationMiddleware(HandlerConfig{
		RequiredMethod: "GET",
		LogOperation:   "liveness_check",
	}, ss.LivenessHandler))

	// Pool management endpoints
	mux.HandleFunc("/api/v1/pools", ss.ValidationMiddleware(HandlerConfig{
		RequiredMethod: "GET",
		LogOperation:   "list_pools",
	}, ss.ListPoolsHandler))

	mux.HandleFunc("/api/v1/pools/status", ss.ValidationMiddleware(HandlerConfig{
		RequiredMethod: "GET",
		LogOperation:   "pool_status",
	}, ss.PoolStatusHandler))

	mux.HandleFunc("/api/v1/reload", ss.ValidationMiddleware(HandlerConfig{
		RequiredMethod:   "POST",
		ParseRequestBody: true,
		RequestType:      &ReloadRequest{},
		LogOperation:     "reload_config",
	}, ss.ReloadHandler))

	mux.HandleFunc("/api/v1/restart", ss.ValidationMiddleware(HandlerConfig{
		RequiredMethod: "POST",
		LogOperation:   "restart_service",
	}, ss.RestartHandler))

	// Scaling endpoints
	mux.HandleFunc("/api/v1/pools/scale", ss.ValidationMiddleware(HandlerConfig{
		RequiredMethod:   "POST",
		ParseRequestBody: true,
		RequestType:      &ScaleRequest{},
		LogOperation:     "scale_pool",
	}, ss.ScalePoolHandler))

	// Metrics endpoints
	mux.HandleFunc("/api/v1/metrics", ss.ValidationMiddleware(HandlerConfig{
		RequiredMethod: "GET",
		LogOperation:   "get_metrics",
	}, ss.MetricsHandler))

	// Configuration endpoints
	mux.HandleFunc("/api/v1/config", ss.ValidationMiddleware(HandlerConfig{
		RequiredMethod: "GET",
		LogOperation:   "get_config",
	}, ss.ConfigHandler))
}

// Middleware implementations

func (ss *SecureServer) requestContextMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Add request to context for audit logging
		ctx := context.WithValue(r.Context(), "http_request", r)

		// Generate request ID
		requestID := ss.generateRequestID()
		ctx = context.WithValue(ctx, "request_id", requestID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (ss *SecureServer) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				ss.logger.Error("Panic recovered",
					zap.Any("error", err),
					zap.String("path", r.URL.Path),
					zap.String("method", r.Method))

				// Log security event for unexpected panics
				ss.auditLogger.LogSecurityViolation(r.Context(),
					"server_panic", "Unexpected server panic occurred", 7,
					map[string]interface{}{
						"error":  err,
						"path":   r.URL.Path,
						"method": r.Method,
					})

				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error": {"code": "internal_error", "message": "Internal server error"}}`))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (ss *SecureServer) rateLimitMiddleware(next http.Handler) http.Handler {
	if ss.rateLimiter == nil {
		return next
	}
	return ss.rateLimiter.RateLimitMiddleware(next)
}

func (ss *SecureServer) auditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ss.auditLogger == nil {
			next.ServeHTTP(w, r)
			return
		}

		// Wrap response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Call next handler
		next.ServeHTTP(wrapped, r)

		// Log the request
		ss.auditLogger.LogHTTPRequest(r.Context(), r, wrapped.statusCode, ss.extractUserID(r))
	})
}

func (ss *SecureServer) securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Default security headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Custom security headers
		for key, value := range ss.config.SecurityHeaders {
			w.Header().Set(key, value)
		}

		next.ServeHTTP(w, r)
	})
}

func (ss *SecureServer) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ss.config.CORSEnabled {
			next.ServeHTTP(w, r)
			return
		}

		origin := r.Header.Get("Origin")
		if ss.isAllowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", ss.getAllowedMethods())
			w.Header().Set("Access-Control-Allow-Headers", ss.getAllowedHeaders())
			w.Header().Set("Access-Control-Max-Age", "3600")
		}

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Helper methods

func (ss *SecureServer) getListenAddress() string {
	if ss.config.Address != "" {
		return ss.config.Address
	}
	return ":8080"
}

func (ss *SecureServer) extractUserID(r *http.Request) string {
	// Extract user ID from various sources (Authorization header, JWT, etc.)
	// This is a placeholder implementation
	if auth := r.Header.Get("Authorization"); auth != "" {
		return "authenticated_user" // Simplified
	}
	return "anonymous"
}

func (ss *SecureServer) isAllowedOrigin(origin string) bool {
	if len(ss.config.CORSOrigins) == 0 {
		return false
	}
	for _, allowed := range ss.config.CORSOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}
	return false
}

func (ss *SecureServer) getAllowedMethods() string {
	if len(ss.config.CORSMethods) == 0 {
		return "GET, POST, PUT, DELETE, OPTIONS"
	}
	return strings.Join(ss.config.CORSMethods, ", ")
}

func (ss *SecureServer) getAllowedHeaders() string {
	if len(ss.config.CORSHeaders) == 0 {
		return "Content-Type, Authorization, X-Requested-With"
	}
	return strings.Join(ss.config.CORSHeaders, ", ")
}

// DefaultSecureServerConfig returns a production-ready secure server configuration
func DefaultSecureServerConfig() *SecureServerConfig {
	return &SecureServerConfig{
		Address:        "0.0.0.0",
		Port:           8080,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1MB

		TLSEnabled:    false, // Should be enabled in production
		MinTLSVersion: "1.2",

		RateLimitConfig: DefaultRateLimitConfig(),
		AuditConfig:     security.DefaultAuditConfig(),

		CORSEnabled: false,
		CORSOrigins: []string{},
		CORSMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		CORSHeaders: []string{"Content-Type", "Authorization", "X-Requested-With"},

		SecurityHeaders: map[string]string{
			"X-API-Version": "v1",
			"X-Service":     "phpfpm-manager",
		},
	}
}
