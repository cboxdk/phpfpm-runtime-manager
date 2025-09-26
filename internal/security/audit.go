package security

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// AuditLogger handles security audit logging with structured events
type AuditLogger struct {
	logger     *zap.Logger
	config     *AuditConfig
	mu         sync.RWMutex
	eventChan  chan *AuditEvent
	stopChan   chan struct{}
	wg         sync.WaitGroup
	fileWriter *os.File
}

// AuditConfig defines audit logging configuration
type AuditConfig struct {
	// Output configuration
	Enabled       bool   `json:"enabled"`
	LogFile       string `json:"log_file"`
	RotateSize    int64  `json:"rotate_size"`  // bytes
	RotateAge     int    `json:"rotate_age"`   // days
	RotateCount   int    `json:"rotate_count"` // number of files to keep
	EnableSyslog  bool   `json:"enable_syslog"`
	EnableConsole bool   `json:"enable_console"`

	// Event filtering
	LogLevel          string   `json:"log_level"`           // debug, info, warn, error
	EventTypes        []string `json:"event_types"`         // filter by event types
	ExcludeUserAgents []string `json:"exclude_user_agents"` // health check exclusions
	ExcludeIPs        []string `json:"exclude_ips"`         // internal service exclusions

	// Performance settings
	BufferSize    int           `json:"buffer_size"`    // channel buffer size
	FlushInterval time.Duration `json:"flush_interval"` // force flush interval
	AsyncLogging  bool          `json:"async_logging"`  // enable async processing

	// Compliance settings
	IncludePII       bool   `json:"include_pii"`       // include personally identifiable information
	RedactSensitive  bool   `json:"redact_sensitive"`  // redact sensitive data
	ComplianceFormat string `json:"compliance_format"` // json, cef, leef
	OrganizationID   string `json:"organization_id"`   // organization identifier
}

// AuditEvent represents a security audit event
type AuditEvent struct {
	// Core event information
	Timestamp  time.Time `json:"timestamp"`
	EventID    string    `json:"event_id"`
	EventType  string    `json:"event_type"`
	EventClass string    `json:"event_class"`
	Severity   string    `json:"severity"`
	Message    string    `json:"message"`

	// Source information
	SourceIP   string `json:"source_ip"`
	SourcePort int    `json:"source_port,omitempty"`
	UserAgent  string `json:"user_agent,omitempty"`
	UserID     string `json:"user_id,omitempty"`
	SessionID  string `json:"session_id,omitempty"`

	// Request information
	RequestID  string `json:"request_id,omitempty"`
	Method     string `json:"method,omitempty"`
	URL        string `json:"url,omitempty"`
	Endpoint   string `json:"endpoint,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`

	// Action information
	Action   string `json:"action"`
	Resource string `json:"resource,omitempty"`
	Target   string `json:"target,omitempty"`
	Outcome  string `json:"outcome"` // success, failure, error

	// Security context
	RiskScore     int    `json:"risk_score,omitempty"`
	ThreatLevel   string `json:"threat_level,omitempty"`
	RuleTriggered string `json:"rule_triggered,omitempty"`

	// Additional context
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	Tags     []string               `json:"tags,omitempty"`

	// Compliance fields
	OrgID           string `json:"org_id,omitempty"`
	ComplianceClass string `json:"compliance_class,omitempty"`
}

// Event types for security audit logging
const (
	EventTypeAuthentication = "authentication"
	EventTypeAuthorization  = "authorization"
	EventTypeAccess         = "access"
	EventTypeDataAccess     = "data_access"
	EventTypeConfiguration  = "configuration"
	EventTypeSystem         = "system"
	EventTypeSecurity       = "security"
	EventTypeCompliance     = "compliance"
	EventTypeError          = "error"
)

// Event classes for categorization
const (
	EventClassLogin             = "login"
	EventClassLogout            = "logout"
	EventClassAPIAccess         = "api_access"
	EventClassConfigRead        = "config_read"
	EventClassConfigWrite       = "config_write"
	EventClassPoolAction        = "pool_action"
	EventClassMetrics           = "metrics"
	EventClassHealthCheck       = "health_check"
	EventClassRateLimit         = "rate_limit"
	EventClassSecurityViolation = "security_violation"
)

// Severity levels
const (
	SeverityLow      = "low"
	SeverityMedium   = "medium"
	SeverityHigh     = "high"
	SeverityCritical = "critical"
)

// Outcome values
const (
	OutcomeSuccess = "success"
	OutcomeFailure = "failure"
	OutcomeError   = "error"
	OutcomeDenied  = "denied"
)

// NewAuditLogger creates a new audit logger with the given configuration
func NewAuditLogger(config *AuditConfig, logger *zap.Logger) (*AuditLogger, error) {
	if !config.Enabled {
		return &AuditLogger{
			logger: logger,
			config: config,
		}, nil
	}

	auditLogger := &AuditLogger{
		logger:   logger,
		config:   config,
		stopChan: make(chan struct{}),
	}

	// Setup async processing if enabled
	if config.AsyncLogging {
		auditLogger.eventChan = make(chan *AuditEvent, config.BufferSize)
		auditLogger.wg.Add(1)
		go auditLogger.eventProcessor()
	}

	// Setup file logging if enabled
	if config.LogFile != "" {
		if err := auditLogger.setupFileLogging(); err != nil {
			return nil, fmt.Errorf("failed to setup file logging: %w", err)
		}
	}

	return auditLogger, nil
}

// LogEvent logs a security audit event
func (al *AuditLogger) LogEvent(ctx context.Context, event *AuditEvent) {
	if !al.config.Enabled {
		return
	}

	// Set defaults
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	if event.EventID == "" {
		event.EventID = generateEventID()
	}
	if event.OrgID == "" {
		event.OrgID = al.config.OrganizationID
	}

	// Apply filtering
	if !al.shouldLogEvent(event) {
		return
	}

	// Redact sensitive data if enabled
	if al.config.RedactSensitive {
		al.redactSensitiveData(event)
	}

	// Process event
	if al.config.AsyncLogging && al.eventChan != nil {
		select {
		case al.eventChan <- event:
		case <-ctx.Done():
			al.logger.Warn("Failed to queue audit event: context cancelled")
		default:
			al.logger.Warn("Audit event channel full, dropping event",
				zap.String("event_type", event.EventType),
				zap.String("event_id", event.EventID))
		}
	} else {
		al.writeEvent(event)
	}
}

// LogHTTPRequest logs an HTTP request as an audit event
func (al *AuditLogger) LogHTTPRequest(ctx context.Context, r *http.Request, statusCode int, userID string) {
	event := &AuditEvent{
		EventType:  EventTypeAccess,
		EventClass: EventClassAPIAccess,
		Severity:   al.getSeverityForStatusCode(statusCode),
		Message:    fmt.Sprintf("HTTP %s %s", r.Method, r.URL.Path),
		SourceIP:   al.getClientIP(r),
		UserAgent:  r.UserAgent(),
		UserID:     userID,
		RequestID:  al.getRequestID(ctx),
		Method:     r.Method,
		URL:        r.URL.String(),
		Endpoint:   r.URL.Path,
		HTTPStatus: statusCode,
		Action:     "http_request",
		Outcome:    al.getOutcomeForStatusCode(statusCode),
		Metadata: map[string]interface{}{
			"content_length": r.ContentLength,
			"protocol":       r.Proto,
			"host":           r.Host,
		},
	}

	// Add query parameters (redacted if sensitive)
	if r.URL.RawQuery != "" {
		if al.config.RedactSensitive {
			event.Metadata["query_params"] = "[REDACTED]"
		} else {
			event.Metadata["query_params"] = r.URL.RawQuery
		}
	}

	al.LogEvent(ctx, event)
}

// LogAuthentication logs an authentication event
func (al *AuditLogger) LogAuthentication(ctx context.Context, userID, method string, success bool, details map[string]interface{}) {
	severity := SeverityLow
	outcome := OutcomeSuccess
	message := fmt.Sprintf("Authentication %s for user %s", method, userID)

	if !success {
		severity = SeverityMedium
		outcome = OutcomeFailure
		message = fmt.Sprintf("Authentication %s failed for user %s", method, userID)
	}

	event := &AuditEvent{
		EventType:  EventTypeAuthentication,
		EventClass: EventClassLogin,
		Severity:   severity,
		Message:    message,
		UserID:     userID,
		Action:     "authenticate",
		Outcome:    outcome,
		Metadata:   details,
	}

	if r := al.getRequestFromContext(ctx); r != nil {
		event.SourceIP = al.getClientIP(r)
		event.UserAgent = r.UserAgent()
		event.RequestID = al.getRequestID(ctx)
	}

	al.LogEvent(ctx, event)
}

// LogAuthorization logs an authorization event
func (al *AuditLogger) LogAuthorization(ctx context.Context, userID, resource, action string, allowed bool, reason string) {
	severity := SeverityLow
	outcome := OutcomeSuccess
	message := fmt.Sprintf("Authorization %s for %s on %s", action, userID, resource)

	if !allowed {
		severity = SeverityMedium
		outcome = OutcomeDenied
		message = fmt.Sprintf("Authorization %s denied for %s on %s: %s", action, userID, resource, reason)
	}

	event := &AuditEvent{
		EventType:  EventTypeAuthorization,
		EventClass: EventClassAPIAccess,
		Severity:   severity,
		Message:    message,
		UserID:     userID,
		Action:     action,
		Resource:   resource,
		Outcome:    outcome,
		Metadata: map[string]interface{}{
			"reason": reason,
		},
	}

	if r := al.getRequestFromContext(ctx); r != nil {
		event.SourceIP = al.getClientIP(r)
		event.RequestID = al.getRequestID(ctx)
	}

	al.LogEvent(ctx, event)
}

// LogConfigurationChange logs a configuration change event
func (al *AuditLogger) LogConfigurationChange(ctx context.Context, userID, configType, action string, changes map[string]interface{}) {
	event := &AuditEvent{
		EventType:  EventTypeConfiguration,
		EventClass: EventClassConfigWrite,
		Severity:   SeverityMedium,
		Message:    fmt.Sprintf("Configuration %s: %s by %s", action, configType, userID),
		UserID:     userID,
		Action:     action,
		Resource:   configType,
		Outcome:    OutcomeSuccess,
		Metadata:   changes,
		Tags:       []string{"configuration", "change"},
	}

	if r := al.getRequestFromContext(ctx); r != nil {
		event.SourceIP = al.getClientIP(r)
		event.RequestID = al.getRequestID(ctx)
	}

	al.LogEvent(ctx, event)
}

// LogSecurityViolation logs a security violation event
func (al *AuditLogger) LogSecurityViolation(ctx context.Context, violationType, description string, riskScore int, metadata map[string]interface{}) {
	severity := SeverityHigh
	if riskScore >= 8 {
		severity = SeverityCritical
	} else if riskScore >= 5 {
		severity = SeverityHigh
	} else {
		severity = SeverityMedium
	}

	event := &AuditEvent{
		EventType:     EventTypeSecurity,
		EventClass:    EventClassSecurityViolation,
		Severity:      severity,
		Message:       fmt.Sprintf("Security violation: %s - %s", violationType, description),
		Action:        "security_violation",
		Outcome:       OutcomeError,
		RiskScore:     riskScore,
		ThreatLevel:   severity,
		RuleTriggered: violationType,
		Metadata:      metadata,
		Tags:          []string{"security", "violation", violationType},
	}

	if r := al.getRequestFromContext(ctx); r != nil {
		event.SourceIP = al.getClientIP(r)
		event.UserAgent = r.UserAgent()
		event.RequestID = al.getRequestID(ctx)
	}

	al.LogEvent(ctx, event)
}

// LogRateLimit logs a rate limiting event
func (al *AuditLogger) LogRateLimit(ctx context.Context, clientIP, endpoint, limitType string, metadata map[string]interface{}) {
	event := &AuditEvent{
		EventType:     EventTypeSecurity,
		EventClass:    EventClassRateLimit,
		Severity:      SeverityMedium,
		Message:       fmt.Sprintf("Rate limit exceeded: %s for %s on %s", limitType, clientIP, endpoint),
		SourceIP:      clientIP,
		Action:        "rate_limit_exceeded",
		Resource:      endpoint,
		Outcome:       OutcomeDenied,
		RuleTriggered: limitType,
		Metadata:      metadata,
		Tags:          []string{"rate_limit", limitType},
	}

	if r := al.getRequestFromContext(ctx); r != nil {
		event.UserAgent = r.UserAgent()
		event.RequestID = al.getRequestID(ctx)
	}

	al.LogEvent(ctx, event)
}

// shouldLogEvent determines if an event should be logged based on configuration
func (al *AuditLogger) shouldLogEvent(event *AuditEvent) bool {
	// Check event type filtering
	if len(al.config.EventTypes) > 0 {
		allowed := false
		for _, eventType := range al.config.EventTypes {
			if event.EventType == eventType {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}

	// Check IP exclusions
	for _, excludeIP := range al.config.ExcludeIPs {
		if event.SourceIP == excludeIP {
			return false
		}
	}

	// Check user agent exclusions
	for _, excludeUA := range al.config.ExcludeUserAgents {
		if strings.Contains(event.UserAgent, excludeUA) {
			return false
		}
	}

	return true
}

// redactSensitiveData removes or masks sensitive information from events
func (al *AuditLogger) redactSensitiveData(event *AuditEvent) {
	// Redact URL parameters that might contain sensitive data
	if event.URL != "" {
		event.URL = al.redactURL(event.URL)
	}

	// Redact sensitive metadata
	if event.Metadata != nil {
		for key, value := range event.Metadata {
			if al.isSensitiveField(key) {
				event.Metadata[key] = "[REDACTED]"
			} else if strValue, ok := value.(string); ok && al.containsSensitiveData(strValue) {
				event.Metadata[key] = "[REDACTED]"
			}
		}
	}

	// Redact PII if not explicitly allowed
	if !al.config.IncludePII {
		if al.looksLikeEmail(event.UserID) {
			event.UserID = al.hashPII(event.UserID)
		}
	}
}

// eventProcessor processes events asynchronously
func (al *AuditLogger) eventProcessor() {
	defer al.wg.Done()

	ticker := time.NewTicker(al.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case event := <-al.eventChan:
			al.writeEvent(event)
		case <-ticker.C:
			al.flush()
		case <-al.stopChan:
			// Drain remaining events
			for {
				select {
				case event := <-al.eventChan:
					al.writeEvent(event)
				default:
					return
				}
			}
		}
	}
}

// writeEvent writes an event to the configured outputs
func (al *AuditLogger) writeEvent(event *AuditEvent) {
	// Convert to JSON
	eventJSON, err := json.Marshal(event)
	if err != nil {
		al.logger.Error("Failed to marshal audit event", zap.Error(err))
		return
	}

	// Write to file if configured
	if al.fileWriter != nil {
		al.mu.Lock()
		al.fileWriter.Write(eventJSON)
		al.fileWriter.Write([]byte("\n"))
		al.mu.Unlock()
	}

	// Write to console if configured
	if al.config.EnableConsole {
		al.logger.Info("AUDIT", zap.String("event", string(eventJSON)))
	}

	// Write to syslog if configured (simplified implementation)
	if al.config.EnableSyslog {
		al.logger.Info("AUDIT", zap.Any("event", event))
	}
}

// Helper functions
func (al *AuditLogger) setupFileLogging() error {
	// Create directory if it doesn't exist
	dir := filepath.Dir(al.config.LogFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Open file for appending
	file, err := os.OpenFile(al.config.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	al.fileWriter = file
	return nil
}

func (al *AuditLogger) getClientIP(r *http.Request) string {
	// Check headers for real IP
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Extract from RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func (al *AuditLogger) getRequestID(ctx context.Context) string {
	if id := ctx.Value("request_id"); id != nil {
		if idStr, ok := id.(string); ok {
			return idStr
		}
	}
	return ""
}

func (al *AuditLogger) getRequestFromContext(ctx context.Context) *http.Request {
	if r := ctx.Value("http_request"); r != nil {
		if req, ok := r.(*http.Request); ok {
			return req
		}
	}
	return nil
}

func (al *AuditLogger) getSeverityForStatusCode(statusCode int) string {
	if statusCode >= 500 {
		return SeverityHigh
	} else if statusCode >= 400 {
		return SeverityMedium
	}
	return SeverityLow
}

func (al *AuditLogger) getOutcomeForStatusCode(statusCode int) string {
	if statusCode >= 200 && statusCode < 300 {
		return OutcomeSuccess
	} else if statusCode >= 400 && statusCode < 500 {
		return OutcomeFailure
	}
	return OutcomeError
}

func (al *AuditLogger) isSensitiveField(key string) bool {
	sensitiveFields := []string{"password", "token", "secret", "key", "auth", "credential"}
	keyLower := strings.ToLower(key)
	for _, field := range sensitiveFields {
		if strings.Contains(keyLower, field) {
			return true
		}
	}
	return false
}

func (al *AuditLogger) containsSensitiveData(value string) bool {
	// Simple heuristics for detecting sensitive data
	if len(value) > 20 && strings.ContainsAny(value, "=&") {
		return true // Looks like encoded data
	}
	return false
}

func (al *AuditLogger) redactURL(url string) string {
	// Remove query parameters that might contain sensitive data
	if idx := strings.Index(url, "?"); idx != -1 {
		return url[:idx] + "?[REDACTED]"
	}
	return url
}

func (al *AuditLogger) looksLikeEmail(value string) bool {
	return strings.Contains(value, "@") && strings.Contains(value, ".")
}

func (al *AuditLogger) hashPII(value string) string {
	// Get PII hashing key from environment with fallback
	key := os.Getenv("PHPFPM_MANAGER_PII_KEY")
	if key == "" {
		// Fallback to a default key (in production, this should be properly configured)
		key = "default-pii-key-change-in-production"
		al.logger.Warn("Using default PII hashing key - configure PHPFPM_MANAGER_PII_KEY in production")
	}

	// Create HMAC-SHA256 hash
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(value))
	hash := h.Sum(nil)

	// Return hex-encoded hash with prefix for identification
	return fmt.Sprintf("pii_hash_%s", hex.EncodeToString(hash))
}

func (al *AuditLogger) flush() {
	if al.fileWriter != nil {
		al.mu.Lock()
		al.fileWriter.Sync()
		al.mu.Unlock()
	}
}

func generateEventID() string {
	return fmt.Sprintf("audit_%d_%d", time.Now().Unix(), time.Now().Nanosecond()%1000000)
}

// Stop gracefully shuts down the audit logger
func (al *AuditLogger) Stop() {
	if al.config.AsyncLogging {
		close(al.stopChan)
		al.wg.Wait()
	}

	if al.fileWriter != nil {
		al.fileWriter.Close()
	}
}

// DefaultAuditConfig returns a sensible default audit configuration
func DefaultAuditConfig() *AuditConfig {
	return &AuditConfig{
		Enabled:           true,
		LogFile:           "/var/log/phpfpm-manager/audit.log",
		RotateSize:        100 * 1024 * 1024, // 100MB
		RotateAge:         30,                // 30 days
		RotateCount:       10,                // keep 10 files
		EnableSyslog:      false,
		EnableConsole:     false,
		LogLevel:          "info",
		EventTypes:        []string{}, // log all types
		ExcludeUserAgents: []string{"HealthChecker", "Prometheus"},
		ExcludeIPs:        []string{"127.0.0.1", "::1"},
		BufferSize:        1000,
		FlushInterval:     time.Second * 30,
		AsyncLogging:      true,
		IncludePII:        false,
		RedactSensitive:   true,
		ComplianceFormat:  "json",
		OrganizationID:    "",
	}
}
