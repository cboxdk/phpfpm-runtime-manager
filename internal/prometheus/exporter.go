package prometheus

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/api"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/config"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// authMiddleware provides authentication for protected endpoints
func (e *Exporter) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip authentication if not enabled
		if !e.config.Auth.Enabled {
			next.ServeHTTP(w, r)
			return
		}

		authenticated := false
		var authError string

		switch e.config.Auth.Type {
		case "api_key":
			authenticated, authError = e.validateAPIKey(r)
		case "basic":
			authenticated, authError = e.validateBasicAuth(r)
		case "mtls":
			authenticated, authError = e.validateMTLS(r)
		default:
			authError = "unsupported authentication type"
		}

		if !authenticated {
			e.logger.Warn("Authentication failed",
				zap.String("remote_addr", r.RemoteAddr),
				zap.String("user_agent", r.UserAgent()),
				zap.String("error", authError))

			w.Header().Set("WWW-Authenticate", e.getAuthHeader())
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// validateAPIKey validates API key authentication
func (e *Exporter) validateAPIKey(r *http.Request) (bool, string) {
	// Check for API key in Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
			return e.checkAPIKey(parts[1])
		}
	}

	// Check for API key in X-API-Key header
	apiKey := r.Header.Get("X-API-Key")
	if apiKey != "" {
		return e.checkAPIKey(apiKey)
	}

	// Note: API keys in query parameters are insecure and have been removed
	// Use Authorization header or X-API-Key header instead

	return false, "API key not provided"
}

// checkAPIKey validates if the provided API key is valid
func (e *Exporter) checkAPIKey(providedKey string) (bool, string) {
	// Check single API key configuration
	if e.config.Auth.APIKey != "" {
		if subtle.ConstantTimeCompare([]byte(providedKey), []byte(e.config.Auth.APIKey)) == 1 {
			return true, ""
		}
	}

	// Check multiple API keys configuration
	for _, apiKey := range e.config.Auth.APIKeys {
		if subtle.ConstantTimeCompare([]byte(providedKey), []byte(apiKey.Key)) == 1 {
			return true, ""
		}
	}

	return false, "invalid API key"
}

// validateBasicAuth validates basic authentication
func (e *Exporter) validateBasicAuth(r *http.Request) (bool, string) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return false, "Authorization header not provided"
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "basic" {
		return false, "invalid Authorization header format"
	}

	payload, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return false, "invalid base64 encoding in Authorization header"
	}

	credentials := strings.SplitN(string(payload), ":", 2)
	if len(credentials) != 2 {
		return false, "invalid credentials format"
	}

	username, password := credentials[0], credentials[1]

	// Use constant time comparison to prevent timing attacks
	usernameMatch := subtle.ConstantTimeCompare([]byte(username), []byte(e.config.Auth.Basic.Username)) == 1
	passwordMatch := subtle.ConstantTimeCompare([]byte(password), []byte(e.config.Auth.Basic.Password)) == 1

	if usernameMatch && passwordMatch {
		return true, ""
	}

	return false, "invalid username or password"
}

// validateMTLS validates mutual TLS authentication
func (e *Exporter) validateMTLS(r *http.Request) (bool, string) {
	if r.TLS == nil {
		return false, "TLS connection required"
	}

	if len(r.TLS.PeerCertificates) == 0 {
		return false, "client certificate required"
	}

	// Client certificate is present and verified by the TLS layer
	// Additional validation can be added here if needed
	return true, ""
}

// getAuthHeader returns the appropriate WWW-Authenticate header
func (e *Exporter) getAuthHeader() string {
	switch e.config.Auth.Type {
	case "api_key":
		return `Bearer realm="PHP-FPM Runtime Manager"`
	case "basic":
		return `Basic realm="PHP-FPM Runtime Manager"`
	case "mtls":
		return `Certificate realm="PHP-FPM Runtime Manager"`
	default:
		return `Bearer realm="PHP-FPM Runtime Manager"`
	}
}

// getTLSConfig returns TLS configuration for mTLS authentication
func (e *Exporter) getTLSConfig() *tls.Config {
	return &tls.Config{
		ClientAuth:               tls.RequireAndVerifyClientCert,
		PreferServerCipherSuites: true,
		MinVersion:               tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
		},
	}
}

// rateLimitMiddleware provides rate limiting for endpoints
func (e *Exporter) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check rate limit
		if !e.rateLimiter.Allow() {
			e.logger.Warn("Rate limit exceeded",
				zap.String("remote_addr", r.RemoteAddr),
				zap.String("user_agent", r.UserAgent()))

			w.Header().Set("Retry-After", "1")
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// apiRateLimitMiddleware provides rate limiting for API endpoints
func (e *Exporter) apiRateLimitMiddleware(limiter *rate.Limiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow() {
			e.logger.Warn("API rate limit exceeded",
				zap.String("remote_addr", r.RemoteAddr),
				zap.String("user_agent", r.UserAgent()),
				zap.String("path", r.URL.Path))

			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"Too Many Requests","message":"API rate limit exceeded","timestamp":"` + time.Now().Format(time.RFC3339) + `"}`))
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Exporter exposes metrics in Prometheus format
type Exporter struct {
	config config.ServerConfig
	logger *zap.Logger

	// HTTP server
	server *http.Server

	// API server
	apiServer *api.Server

	// Prometheus metrics registry
	registry *prometheus.Registry

	// Metric collectors
	metrics map[string]prometheus.Collector
	mu      sync.RWMutex

	// Rate limiting
	rateLimiter *rate.Limiter

	// Component interfaces for API
	manager    api.ManagerInterface
	supervisor api.SupervisorInterface
	collector  api.CollectorInterface

	// Predefined metrics
	phpfpmAcceptedConnections *prometheus.CounterVec
	phpfpmListenQueue         *prometheus.GaugeVec
	phpfpmListenQueueLength   *prometheus.GaugeVec
	phpfpmMaxListenQueue      *prometheus.GaugeVec
	phpfpmIdleProcesses       *prometheus.GaugeVec
	phpfpmActiveProcesses     *prometheus.GaugeVec
	phpfpmTotalProcesses      *prometheus.GaugeVec
	phpfpmMaxActiveProcesses  *prometheus.GaugeVec
	phpfpmMaxChildrenReached  *prometheus.CounterVec
	phpfpmSlowRequests        *prometheus.CounterVec
	phpfpmStartSince          *prometheus.GaugeVec
	phpfpmPoolUtilization     *prometheus.GaugeVec

	// Process metrics
	phpfpmProcessCPUPercent      *prometheus.GaugeVec
	phpfpmProcessCPUTotal        *prometheus.CounterVec
	phpfpmProcessMemoryRSS       *prometheus.GaugeVec
	phpfpmProcessMemoryVSize     *prometheus.GaugeVec
	phpfpmProcessMemoryPeakRSS   *prometheus.GaugeVec
	phpfpmProcessMemoryPeakVSize *prometheus.GaugeVec
	phpfpmProcessMemoryMax       *prometheus.GaugeVec
	phpfpmProcessMemoryMin       *prometheus.GaugeVec
	phpfpmProcessMemoryAvg       *prometheus.GaugeVec
	phpfpmProcessMemorySamples   *prometheus.CounterVec

	// System metrics
	systemLoadAverage     *prometheus.GaugeVec
	systemCPUUser         *prometheus.GaugeVec
	systemCPUSystem       *prometheus.GaugeVec
	systemCPUIdle         *prometheus.GaugeVec
	systemCPUIOWait       *prometheus.GaugeVec
	systemCPUUsage        *prometheus.GaugeVec
	systemMemoryTotal     *prometheus.GaugeVec
	systemMemoryAvailable *prometheus.GaugeVec
	systemMemoryFree      *prometheus.GaugeVec

	// Opcache metrics
	phpfpmOpcacheHitRate       *prometheus.GaugeVec
	phpfpmOpcacheMemoryUsage   *prometheus.GaugeVec
	phpfpmOpcacheEnabled       *prometheus.GaugeVec
	phpfpmOpcacheCachedScripts *prometheus.GaugeVec
	phpfpmOpcacheCachedKeys    *prometheus.GaugeVec
	phpfpmOpcacheHitsTotal     *prometheus.CounterVec
	phpfpmOpcacheMissesTotal   *prometheus.CounterVec
	phpfpmOpcacheWasted        *prometheus.GaugeVec
	phpfpmOpcacheUsedMemory    *prometheus.GaugeVec
	phpfpmOpcacheFreeMemory    *prometheus.GaugeVec
	phpfpmOpcacheWastedMemory  *prometheus.GaugeVec
	phpfpmOpcacheHashRestarts  *prometheus.CounterVec

	// Availability and error tracking
	phpfpmUp             *prometheus.GaugeVec
	phpfpmScrapeFailures *prometheus.CounterVec

	// Individual process metrics
	phpfpmProcessState             *prometheus.GaugeVec
	phpfpmProcessRequestsTotal     *prometheus.CounterVec
	phpfpmProcessRequestDuration   *prometheus.GaugeVec
	phpfpmProcessLastRequestCPU    *prometheus.GaugeVec
	phpfpmProcessLastRequestMemory *prometheus.GaugeVec
	phpfpmProcessStartSince        *prometheus.GaugeVec

	// Collection metrics
	phpfpmCollectionDuration *prometheus.GaugeVec
	phpfpmMetricsCollected   *prometheus.GaugeVec

	// State
	running bool
}

// NewExporter creates a new Prometheus exporter
func NewExporter(config config.ServerConfig, logger *zap.Logger) (*Exporter, error) {
	registry := prometheus.NewRegistry()

	// Create rate limiter: 100 requests per second with burst of 200
	rateLimiter := rate.NewLimiter(100, 200)

	e := &Exporter{
		config:      config,
		logger:      logger,
		registry:    registry,
		metrics:     make(map[string]prometheus.Collector),
		rateLimiter: rateLimiter,
	}

	// Initialize predefined metrics
	if err := e.initMetrics(); err != nil {
		return nil, fmt.Errorf("failed to initialize metrics: %w", err)
	}

	return e, nil
}

// SetAPIComponents sets the component interfaces needed for the API server
func (e *Exporter) SetAPIComponents(manager api.ManagerInterface, supervisor api.SupervisorInterface, collector api.CollectorInterface, eventStorage api.EventStorageInterface, securityConfig *config.SecurityConfig, version string) {
	e.manager = manager
	e.supervisor = supervisor
	e.collector = collector

	// Create API server if API is enabled
	if e.config.API.Enabled {
		e.apiServer = api.NewServer(e.logger, manager, supervisor, collector, eventStorage, securityConfig, version)
	}
}

// Start begins serving metrics
func (e *Exporter) Start(ctx context.Context) error {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return fmt.Errorf("exporter is already running")
	}
	e.running = true
	e.mu.Unlock()

	e.logger.Info("Starting Prometheus exporter",
		zap.String("bind_address", e.config.BindAddress),
		zap.String("metrics_path", e.config.MetricsPath))

	// Create HTTP server
	mux := http.NewServeMux()

	// Apply rate limiting and authentication middleware to metrics endpoint
	metricsHandler := promhttp.HandlerFor(e.registry, promhttp.HandlerOpts{
		ErrorLog:      zap.NewStdLog(e.logger),
		ErrorHandling: promhttp.ContinueOnError,
	})
	// Chain middlewares: rate limiting first, then authentication
	protectedHandler := e.rateLimitMiddleware(e.authMiddleware(metricsHandler))
	mux.Handle(e.config.MetricsPath, protectedHandler)

	// Public endpoints (no authentication required)
	mux.HandleFunc("/", e.rootHandler)
	mux.HandleFunc("/health", e.healthHandler)

	// API endpoints (with authentication if enabled)
	if e.config.API.Enabled && e.apiServer != nil {
		e.logger.Info("Enabling REST API endpoints", zap.String("base_path", e.config.API.BasePath))

		// Create API-specific rate limiter
		apiRateLimiter := rate.NewLimiter(rate.Limit(e.config.API.MaxRequests), e.config.API.MaxRequests*2)

		// API routes with authentication and rate limiting
		apiMux := http.NewServeMux()
		e.apiServer.SetupRoutes(apiMux)

		// Apply middlewares to API routes
		apiHandler := e.apiRateLimitMiddleware(apiRateLimiter, e.authMiddleware(apiMux))
		mux.Handle(e.config.API.BasePath+"/", http.StripPrefix(e.config.API.BasePath, apiHandler))
	}

	e.server = &http.Server{
		Addr:         e.config.BindAddress,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Configure TLS for mTLS authentication
	if e.config.Auth.Enabled && e.config.Auth.Type == "mtls" {
		e.server.TLSConfig = e.getTLSConfig()
	}

	// Start server in goroutine
	go func() {
		if e.config.TLS.Enabled {
			e.logger.Info("Starting HTTPS server",
				zap.String("cert_file", e.config.TLS.CertFile),
				zap.String("key_file", e.config.TLS.KeyFile))

			err := e.server.ListenAndServeTLS(e.config.TLS.CertFile, e.config.TLS.KeyFile)
			if err != nil && err != http.ErrServerClosed {
				e.logger.Error("HTTPS server failed", zap.Error(err))
			}
		} else {
			e.logger.Info("Starting HTTP server")
			err := e.server.ListenAndServe()
			if err != nil && err != http.ErrServerClosed {
				e.logger.Error("HTTP server failed", zap.Error(err))
			}
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := e.server.Shutdown(shutdownCtx); err != nil {
		e.logger.Error("Server shutdown failed", zap.Error(err))
		return err
	}

	e.logger.Info("Prometheus exporter stopped")
	return nil
}

// Stop halts the metrics server
func (e *Exporter) Stop(ctx context.Context) error {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return nil
	}
	e.running = false
	e.mu.Unlock()

	if e.server != nil {
		return e.server.Shutdown(ctx)
	}

	return nil
}

// UpdateMetrics refreshes the exposed metrics
func (e *Exporter) UpdateMetrics(metricSet types.MetricSet) error {
	e.mu.RLock()
	running := e.running
	e.mu.RUnlock()

	if !running {
		return fmt.Errorf("exporter is not running")
	}

	// Process each metric
	for _, metric := range metricSet.Metrics {
		if err := e.updateMetric(metric); err != nil {
			e.logger.Error("Failed to update metric",
				zap.String("metric", metric.Name),
				zap.Error(err))
		}
	}

	return nil
}

// updateMetric updates a specific metric
func (e *Exporter) updateMetric(metric types.Metric) error {
	switch metric.Name {
	// PHP-FPM pool metrics
	case "phpfpm_accepted_connections_total":
		e.phpfpmAcceptedConnections.With(metric.Labels).Add(metric.Value)
	case "phpfpm_listen_queue":
		e.phpfpmListenQueue.With(metric.Labels).Set(metric.Value)
	case "phpfpm_listen_queue_length":
		e.phpfpmListenQueueLength.With(metric.Labels).Set(metric.Value)
	case "phpfpm_max_listen_queue":
		e.phpfpmMaxListenQueue.With(metric.Labels).Set(metric.Value)
	case "phpfpm_idle_processes":
		e.phpfpmIdleProcesses.With(metric.Labels).Set(metric.Value)
	case "phpfpm_active_processes":
		e.phpfpmActiveProcesses.With(metric.Labels).Set(metric.Value)
	case "phpfpm_total_processes":
		e.phpfpmTotalProcesses.With(metric.Labels).Set(metric.Value)
	case "phpfpm_max_active_processes":
		e.phpfpmMaxActiveProcesses.With(metric.Labels).Set(metric.Value)
	case "phpfpm_max_children_reached_total":
		e.phpfpmMaxChildrenReached.With(metric.Labels).Add(metric.Value)
	case "phpfpm_slow_requests_total":
		e.phpfpmSlowRequests.With(metric.Labels).Add(metric.Value)
	case "phpfpm_start_since_seconds":
		e.phpfpmStartSince.With(metric.Labels).Set(metric.Value)
	case "phpfpm_pool_utilization_ratio":
		e.phpfpmPoolUtilization.With(metric.Labels).Set(metric.Value)

	// Process CPU metrics
	case "phpfpm_process_cpu_percent":
		e.phpfpmProcessCPUPercent.With(metric.Labels).Set(metric.Value)
	case "phpfpm_process_cpu_total_seconds":
		e.phpfpmProcessCPUTotal.With(metric.Labels).Add(metric.Value)

	// Process memory metrics
	case "phpfpm_process_memory_rss_bytes":
		e.phpfpmProcessMemoryRSS.With(metric.Labels).Set(metric.Value)
	case "phpfpm_process_memory_vsize_bytes":
		e.phpfpmProcessMemoryVSize.With(metric.Labels).Set(metric.Value)
	case "phpfpm_process_memory_peak_rss_bytes":
		e.phpfpmProcessMemoryPeakRSS.With(metric.Labels).Set(metric.Value)
	case "phpfpm_process_memory_peak_vsize_bytes":
		e.phpfpmProcessMemoryPeakVSize.With(metric.Labels).Set(metric.Value)
	case "phpfpm_process_memory_max_bytes":
		e.phpfpmProcessMemoryMax.With(metric.Labels).Set(metric.Value)
	case "phpfpm_process_memory_min_bytes":
		e.phpfpmProcessMemoryMin.With(metric.Labels).Set(metric.Value)
	case "phpfpm_process_memory_avg_bytes":
		e.phpfpmProcessMemoryAvg.With(metric.Labels).Set(metric.Value)
	case "phpfpm_process_memory_samples_total":
		e.phpfpmProcessMemorySamples.With(metric.Labels).Add(metric.Value)

	// System metrics
	case "system_load_average_1m":
		e.systemLoadAverage.With(prometheus.Labels{}).Set(metric.Value)
	case "system_cpu_user_percent":
		e.systemCPUUser.With(prometheus.Labels{}).Set(metric.Value)
	case "system_cpu_system_percent":
		e.systemCPUSystem.With(prometheus.Labels{}).Set(metric.Value)
	case "system_cpu_idle_percent":
		e.systemCPUIdle.With(prometheus.Labels{}).Set(metric.Value)
	case "system_cpu_iowait_percent":
		e.systemCPUIOWait.With(prometheus.Labels{}).Set(metric.Value)
	case "system_cpu_usage_percent":
		e.systemCPUUsage.With(prometheus.Labels{}).Set(metric.Value)
	case "system_memory_total_bytes":
		e.systemMemoryTotal.With(prometheus.Labels{}).Set(metric.Value)
	case "system_memory_available_bytes":
		e.systemMemoryAvailable.With(prometheus.Labels{}).Set(metric.Value)
	case "system_memory_free_bytes":
		e.systemMemoryFree.With(prometheus.Labels{}).Set(metric.Value)

	// Opcache metrics
	case "phpfpm_opcache_hit_rate":
		e.phpfpmOpcacheHitRate.With(metric.Labels).Set(metric.Value)
	case "phpfpm_opcache_memory_usage_bytes":
		e.phpfpmOpcacheMemoryUsage.With(metric.Labels).Set(metric.Value)
	case "phpfpm_opcache_enabled":
		e.phpfpmOpcacheEnabled.With(metric.Labels).Set(metric.Value)
	case "phpfpm_opcache_cached_scripts":
		e.phpfpmOpcacheCachedScripts.With(metric.Labels).Set(metric.Value)
	case "phpfpm_opcache_cached_keys":
		e.phpfpmOpcacheCachedKeys.With(metric.Labels).Set(metric.Value)
	case "phpfpm_opcache_hits_total":
		e.phpfpmOpcacheHitsTotal.With(metric.Labels).Add(metric.Value)
	case "phpfpm_opcache_misses_total":
		e.phpfpmOpcacheMissesTotal.With(metric.Labels).Add(metric.Value)
	case "phpfpm_opcache_wasted_percentage":
		e.phpfpmOpcacheWasted.With(metric.Labels).Set(metric.Value)

	// Availability and error tracking
	case "phpfpm_up":
		e.phpfpmUp.With(metric.Labels).Set(metric.Value)
	case "phpfpm_scrape_failures_total":
		e.phpfpmScrapeFailures.With(metric.Labels).Add(metric.Value)

	// Individual process metrics
	case "phpfpm_process_state":
		e.phpfpmProcessState.With(metric.Labels).Set(metric.Value)
	case "phpfpm_process_requests_total":
		e.phpfpmProcessRequestsTotal.With(metric.Labels).Add(metric.Value)
	case "phpfpm_process_request_duration_seconds":
		e.phpfpmProcessRequestDuration.With(metric.Labels).Set(metric.Value)
	case "phpfpm_process_last_request_cpu_seconds":
		e.phpfpmProcessLastRequestCPU.With(metric.Labels).Set(metric.Value)
	case "phpfpm_process_last_request_memory_bytes":
		e.phpfpmProcessLastRequestMemory.With(metric.Labels).Set(metric.Value)
	case "phpfpm_process_start_since_seconds":
		e.phpfpmProcessStartSince.With(metric.Labels).Set(metric.Value)

	// Collection metrics
	case "phpfpm_collection_duration_seconds":
		e.phpfpmCollectionDuration.With(prometheus.Labels{}).Set(metric.Value)
	case "phpfpm_metrics_collected_total":
		e.phpfpmMetricsCollected.With(prometheus.Labels{}).Set(metric.Value)

	// OPcache metrics
	case "phpfpm_opcache_used_memory_bytes":
		e.phpfpmOpcacheUsedMemory.With(metric.Labels).Set(metric.Value)
	case "phpfpm_opcache_free_memory_bytes":
		e.phpfpmOpcacheFreeMemory.With(metric.Labels).Set(metric.Value)
	case "phpfpm_opcache_wasted_memory_bytes":
		e.phpfpmOpcacheWastedMemory.With(metric.Labels).Set(metric.Value)
	case "phpfpm_opcache_hash_restarts_total":
		e.phpfpmOpcacheHashRestarts.With(metric.Labels).Add(metric.Value)

	default:
		// Unknown metric - log a warning
		e.logger.Debug("Unknown metric ignored",
			zap.String("metric", metric.Name),
			zap.Float64("value", metric.Value))
	}

	return nil
}

// initMetrics initializes all Prometheus metrics
func (e *Exporter) initMetrics() error {
	// PHP-FPM pool metrics
	e.phpfpmAcceptedConnections = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "phpfpm_accepted_connections_total",
			Help: "Total number of accepted connections",
		},
		[]string{"pool", "socket"},
	)

	e.phpfpmListenQueue = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_listen_queue",
			Help: "Current number of connections in the listen queue",
		},
		[]string{"pool", "socket"},
	)

	e.phpfpmListenQueueLength = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_listen_queue_length",
			Help: "Current listen queue length",
		},
		[]string{"pool", "socket"},
	)

	e.phpfpmMaxListenQueue = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_max_listen_queue",
			Help: "Maximum number of connections in the listen queue",
		},
		[]string{"pool", "socket"},
	)

	e.phpfpmIdleProcesses = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_idle_processes",
			Help: "Number of idle processes",
		},
		[]string{"pool", "socket"},
	)

	e.phpfpmActiveProcesses = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_active_processes",
			Help: "Number of active processes",
		},
		[]string{"pool", "socket"},
	)

	e.phpfpmTotalProcesses = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_total_processes",
			Help: "Total number of processes",
		},
		[]string{"pool", "socket"},
	)

	e.phpfpmMaxActiveProcesses = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_max_active_processes",
			Help: "Maximum number of active processes",
		},
		[]string{"pool", "socket"},
	)

	e.phpfpmMaxChildrenReached = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "phpfpm_max_children_reached_total",
			Help: "Total number of times max children limit was reached",
		},
		[]string{"pool", "socket"},
	)

	e.phpfpmSlowRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "phpfpm_slow_requests_total",
			Help: "Total number of slow requests",
		},
		[]string{"pool", "socket"},
	)

	e.phpfpmStartSince = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_start_since_seconds",
			Help: "Number of seconds since pool start",
		},
		[]string{"pool", "socket"},
	)

	e.phpfpmPoolUtilization = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_pool_utilization_ratio",
			Help: "Pool utilization ratio (active processes / max workers)",
		},
		[]string{"pool", "socket"},
	)

	// Process CPU metrics
	e.phpfpmProcessCPUPercent = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_process_cpu_percent",
			Help: "Process CPU usage percentage",
		},
		[]string{"pid"},
	)

	e.phpfpmProcessCPUTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "phpfpm_process_cpu_total_seconds",
			Help: "Total process CPU time in seconds",
		},
		[]string{"pid"},
	)

	// Process memory metrics
	e.phpfpmProcessMemoryRSS = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_process_memory_rss_bytes",
			Help: "Process resident set size in bytes",
		},
		[]string{"pid"},
	)

	e.phpfpmProcessMemoryVSize = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_process_memory_vsize_bytes",
			Help: "Process virtual memory size in bytes",
		},
		[]string{"pid"},
	)

	e.phpfpmProcessMemoryPeakRSS = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_process_memory_peak_rss_bytes",
			Help: "Process peak resident set size in bytes",
		},
		[]string{"pid"},
	)

	e.phpfpmProcessMemoryPeakVSize = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_process_memory_peak_vsize_bytes",
			Help: "Process peak virtual memory size in bytes",
		},
		[]string{"pid"},
	)

	e.phpfpmProcessMemoryMax = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_process_memory_max_bytes",
			Help: "Maximum memory usage observed for process",
		},
		[]string{"pid"},
	)

	e.phpfpmProcessMemoryMin = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_process_memory_min_bytes",
			Help: "Minimum memory usage observed for process",
		},
		[]string{"pid"},
	)

	e.phpfpmProcessMemoryAvg = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_process_memory_avg_bytes",
			Help: "Average memory usage for process",
		},
		[]string{"pid"},
	)

	e.phpfpmProcessMemorySamples = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "phpfpm_process_memory_samples_total",
			Help: "Total number of memory samples taken for process",
		},
		[]string{"pid"},
	)

	// System metrics
	e.systemLoadAverage = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "system_load_average_1m",
			Help: "System load average (1 minute)",
		},
		[]string{},
	)

	e.systemCPUUser = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "system_cpu_user_percent",
			Help: "System CPU user time percentage",
		},
		[]string{},
	)

	e.systemCPUSystem = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "system_cpu_system_percent",
			Help: "System CPU system time percentage",
		},
		[]string{},
	)

	e.systemCPUIdle = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "system_cpu_idle_percent",
			Help: "System CPU idle time percentage",
		},
		[]string{},
	)

	e.systemCPUIOWait = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "system_cpu_iowait_percent",
			Help: "System CPU I/O wait time percentage",
		},
		[]string{},
	)

	e.systemCPUUsage = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "system_cpu_usage_percent",
			Help: "System CPU usage percentage",
		},
		[]string{},
	)

	e.systemMemoryTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "system_memory_total_bytes",
			Help: "Total system memory in bytes",
		},
		[]string{},
	)

	e.systemMemoryAvailable = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "system_memory_available_bytes",
			Help: "Available system memory in bytes",
		},
		[]string{},
	)

	e.systemMemoryFree = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "system_memory_free_bytes",
			Help: "Free system memory in bytes",
		},
		[]string{},
	)

	// Opcache metrics
	e.phpfpmOpcacheHitRate = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_opcache_hit_rate",
			Help: "PHP opcache hit rate",
		},
		[]string{"pool"},
	)

	e.phpfpmOpcacheMemoryUsage = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_opcache_memory_usage_bytes",
			Help: "PHP opcache memory usage in bytes",
		},
		[]string{"pool", "type"},
	)

	e.phpfpmOpcacheEnabled = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_opcache_enabled",
			Help: "PHP opcache enabled status",
		},
		[]string{"pool"},
	)

	e.phpfpmOpcacheCachedScripts = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_opcache_cached_scripts",
			Help: "Number of scripts cached in opcache",
		},
		[]string{"pool"},
	)

	e.phpfpmOpcacheCachedKeys = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_opcache_cached_keys",
			Help: "Number of keys cached in opcache",
		},
		[]string{"pool"},
	)

	e.phpfpmOpcacheHitsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "phpfpm_opcache_hits_total",
			Help: "Total opcache hits",
		},
		[]string{"pool"},
	)

	e.phpfpmOpcacheMissesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "phpfpm_opcache_misses_total",
			Help: "Total opcache misses",
		},
		[]string{"pool"},
	)

	e.phpfpmOpcacheWasted = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_opcache_wasted_percentage",
			Help: "Opcache wasted memory percentage",
		},
		[]string{"pool"},
	)

	e.phpfpmOpcacheUsedMemory = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_opcache_used_memory_bytes",
			Help: "Opcache used memory in bytes",
		},
		[]string{"pool"},
	)

	e.phpfpmOpcacheFreeMemory = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_opcache_free_memory_bytes",
			Help: "Opcache free memory in bytes",
		},
		[]string{"pool"},
	)

	e.phpfpmOpcacheWastedMemory = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_opcache_wasted_memory_bytes",
			Help: "Opcache wasted memory in bytes",
		},
		[]string{"pool"},
	)

	e.phpfpmOpcacheHashRestarts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "phpfpm_opcache_hash_restarts_total",
			Help: "Total opcache hash restarts",
		},
		[]string{"pool"},
	)

	// Availability and error tracking
	e.phpfpmUp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_up",
			Help: "Whether PHP-FPM pool can be reached (1 = up, 0 = down)",
		},
		[]string{"pool", "socket"},
	)

	e.phpfpmScrapeFailures = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "phpfpm_scrape_failures_total",
			Help: "Total number of scrape failures",
		},
		[]string{"pool", "socket"},
	)

	// Individual process metrics
	e.phpfpmProcessState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_process_state",
			Help: "Process state (1=idle, 2=running, 3=finishing)",
		},
		[]string{"pool", "pid", "socket"},
	)

	e.phpfpmProcessRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "phpfpm_process_requests_total",
			Help: "Total requests handled by process",
		},
		[]string{"pool", "pid", "socket"},
	)

	e.phpfpmProcessRequestDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_process_request_duration_seconds",
			Help: "Current request duration in seconds",
		},
		[]string{"pool", "pid", "socket"},
	)

	e.phpfpmProcessLastRequestCPU = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_process_last_request_cpu_seconds",
			Help: "CPU time consumed by last request",
		},
		[]string{"pool", "pid", "socket"},
	)

	e.phpfpmProcessLastRequestMemory = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_process_last_request_memory_bytes",
			Help: "Memory consumed by last request",
		},
		[]string{"pool", "pid", "socket"},
	)

	e.phpfpmProcessStartSince = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_process_start_since_seconds",
			Help: "Number of seconds since process start",
		},
		[]string{"pool", "pid", "socket"},
	)

	// Collection metrics
	e.phpfpmCollectionDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_collection_duration_seconds",
			Help: "Time taken to collect metrics in seconds",
		},
		[]string{},
	)

	e.phpfpmMetricsCollected = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "phpfpm_metrics_collected_total",
			Help: "Total number of metrics collected",
		},
		[]string{},
	)

	// Register all metrics
	collectors := []prometheus.Collector{
		e.phpfpmAcceptedConnections,
		e.phpfpmListenQueue,
		e.phpfpmListenQueueLength,
		e.phpfpmMaxListenQueue,
		e.phpfpmIdleProcesses,
		e.phpfpmActiveProcesses,
		e.phpfpmTotalProcesses,
		e.phpfpmMaxActiveProcesses,
		e.phpfpmMaxChildrenReached,
		e.phpfpmSlowRequests,
		e.phpfpmStartSince,
		e.phpfpmPoolUtilization,
		e.phpfpmProcessCPUPercent,
		e.phpfpmProcessCPUTotal,
		e.phpfpmProcessMemoryRSS,
		e.phpfpmProcessMemoryVSize,
		e.phpfpmProcessMemoryPeakRSS,
		e.phpfpmProcessMemoryPeakVSize,
		e.phpfpmProcessMemoryMax,
		e.phpfpmProcessMemoryMin,
		e.phpfpmProcessMemoryAvg,
		e.phpfpmProcessMemorySamples,
		e.systemLoadAverage,
		e.systemCPUUser,
		e.systemCPUSystem,
		e.systemCPUIdle,
		e.systemCPUIOWait,
		e.systemCPUUsage,
		e.systemMemoryTotal,
		e.systemMemoryAvailable,
		e.systemMemoryFree,
		e.phpfpmOpcacheHitRate,
		e.phpfpmOpcacheMemoryUsage,
		e.phpfpmOpcacheEnabled,
		e.phpfpmOpcacheCachedScripts,
		e.phpfpmOpcacheCachedKeys,
		e.phpfpmOpcacheHitsTotal,
		e.phpfpmOpcacheMissesTotal,
		e.phpfpmOpcacheWasted,
		e.phpfpmOpcacheUsedMemory,
		e.phpfpmOpcacheFreeMemory,
		e.phpfpmOpcacheWastedMemory,
		e.phpfpmOpcacheHashRestarts,
		e.phpfpmUp,
		e.phpfpmScrapeFailures,
		e.phpfpmProcessState,
		e.phpfpmProcessRequestsTotal,
		e.phpfpmProcessRequestDuration,
		e.phpfpmProcessLastRequestCPU,
		e.phpfpmProcessLastRequestMemory,
		e.phpfpmProcessStartSince,
		e.phpfpmCollectionDuration,
		e.phpfpmMetricsCollected,
	}

	for _, collector := range collectors {
		if err := e.registry.Register(collector); err != nil {
			return fmt.Errorf("failed to register collector: %w", err)
		}
	}

	e.logger.Info("Initialized Prometheus metrics", zap.Int("collectors", len(collectors)))
	return nil
}

// rootHandler handles the root path
func (e *Exporter) rootHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<html>
<head><title>PHP-FPM Runtime Manager</title></head>
<body>
<h1>PHP-FPM Runtime Manager</h1>
<p><a href="%s">Metrics</a></p>
<p><a href="/health">Health</a></p>
</body>
</html>`, e.config.MetricsPath)
}

// healthHandler handles health checks
func (e *Exporter) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"ok","timestamp":"%s"}`, time.Now().UTC().Format(time.RFC3339))
}
