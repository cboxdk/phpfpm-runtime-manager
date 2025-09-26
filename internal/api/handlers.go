package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/config"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/security"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/telemetry"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/types"
	"go.uber.org/zap"
)

// Server represents the API server
type Server struct {
	logger         *zap.Logger
	manager        ManagerInterface
	supervisor     SupervisorInterface
	collector      CollectorInterface
	eventStorage   EventStorageInterface
	securityConfig *config.SecurityConfig
	startTime      time.Time
	version        string
}

// ManagerInterface defines the interface for manager operations
type ManagerInterface interface {
	Reload(ctx context.Context) error
	Restart(ctx context.Context) error
	Health() types.HealthStatus
}

// SupervisorInterface defines the interface for supervisor operations
type SupervisorInterface interface {
	Health() types.HealthStatus
	GetPoolStatus(pool string) (*PoolStatus, error)
	GetAllPoolsStatus() ([]PoolStatus, error)
	ReloadPool(ctx context.Context, pool string) error
	ScalePool(ctx context.Context, pool string, workers int) error
	GetPoolScale(pool string) (int, error)
	ValidatePoolConfig(pool string) error
}

// CollectorInterface defines the interface for collector operations
type CollectorInterface interface {
	IsAutoscalingEnabled() bool
	IsAutoscalingPaused() bool
	PauseAutoscaling() error
	ResumeAutoscaling() error
	GetAutoscalingStatus() AutoscalingStatus
}

// EventStorageInterface defines the interface for event storage operations
type EventStorageInterface interface {
	GetEvents(ctx context.Context, filter telemetry.EventFilter) ([]telemetry.Event, error)
	GetEventAnnotations(ctx context.Context, startTime, endTime time.Time, pool string) ([]EventAnnotation, error)
	GetEventStats(ctx context.Context) (EventStats, error)
}

// EventAnnotation represents an event as a metric annotation
type EventAnnotation struct {
	Timestamp time.Time              `json:"timestamp"`
	Pool      string                 `json:"pool"`
	EventType string                 `json:"event_type"`
	Summary   string                 `json:"summary"`
	Severity  string                 `json:"severity"`
	EventID   string                 `json:"event_id"`
	Details   map[string]interface{} `json:"details"`
}

// EventStats represents statistics about stored events
type EventStats struct {
	TotalEvents  int64            `json:"total_events"`
	EventsByType map[string]int64 `json:"events_by_type"`
	OldestEvent  time.Time        `json:"oldest_event"`
	NewestEvent  time.Time        `json:"newest_event"`
}

// NewServer creates a new API server instance
func NewServer(logger *zap.Logger, manager ManagerInterface, supervisor SupervisorInterface, collector CollectorInterface, eventStorage EventStorageInterface, securityConfig *config.SecurityConfig, version string) *Server {
	return &Server{
		logger:         logger.Named("api"),
		manager:        manager,
		supervisor:     supervisor,
		collector:      collector,
		eventStorage:   eventStorage,
		securityConfig: securityConfig,
		startTime:      time.Now(),
		version:        version,
	}
}

// generateRequestID generates a unique request ID
func (s *Server) generateRequestID() string {
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}

// writeJSON writes a JSON response
func (s *Server) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error("Failed to encode JSON response", zap.Error(err))
	}
}

// writeError writes an error response
func (s *Server) writeError(w http.ResponseWriter, status int, message string, requestID string, details interface{}) {
	response := ErrorResponse{
		Error:     http.StatusText(status),
		Message:   message,
		RequestID: requestID,
		Timestamp: time.Now(),
		Details:   details,
	}
	s.writeJSON(w, status, response)
}

// parseJSON parses JSON request body
func (s *Server) parseJSON(r *http.Request, v interface{}) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(v)
}

// HandleStatus handles GET /api/v1/status
func (s *Server) HandleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "", nil)
		return
	}

	pools, err := s.supervisor.GetAllPoolsStatus()
	if err != nil {
		s.logger.Error("Failed to get pools status", zap.Error(err))
		s.writeError(w, http.StatusInternalServerError, "Failed to get pools status", s.generateRequestID(), nil)
		return
	}

	autoscaling := s.collector.GetAutoscalingStatus()
	health := s.manager.Health()

	status := "healthy"
	if health.Overall != types.HealthStateHealthy {
		status = string(health.Overall)
	}

	response := StatusResponse{
		Version:     s.version,
		Status:      status,
		Uptime:      time.Since(s.startTime).String(),
		Pools:       pools,
		Autoscaling: autoscaling,
		Configuration: ConfigurationStatus{
			Valid:         s.validateConfiguration(),
			LastValidated: &s.startTime,
		},
		LastUpdate: time.Now(),
	}

	s.writeJSON(w, http.StatusOK, response)
}

// HandleReload handles POST /api/v1/reload
func (s *Server) HandleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "", nil)
		return
	}

	requestID := s.generateRequestID()
	start := time.Now()

	var req ReloadRequest
	if r.ContentLength > 0 {
		if err := s.parseJSON(r, &req); err != nil {
			s.writeError(w, http.StatusBadRequest, "Invalid JSON request", requestID, err.Error())
			return
		}
	}

	ctx := r.Context()
	var errors map[string]string
	var poolsReloaded []string

	if len(req.Pools) == 0 {
		// Reload all pools via manager
		if err := s.manager.Reload(ctx); err != nil {
			s.logger.Error("Failed to reload configuration", zap.Error(err), zap.String("request_id", requestID))
			s.writeError(w, http.StatusInternalServerError, "Failed to reload configuration", requestID, err.Error())
			return
		}

		// Get all pool names
		allPools, err := s.supervisor.GetAllPoolsStatus()
		if err == nil {
			for _, pool := range allPools {
				poolsReloaded = append(poolsReloaded, pool.Name)
			}
		}
	} else {
		// Reload specific pools
		errors = make(map[string]string)
		for _, pool := range req.Pools {
			if err := s.supervisor.ReloadPool(ctx, pool); err != nil {
				s.logger.Error("Failed to reload pool", zap.String("pool", pool), zap.Error(err), zap.String("request_id", requestID))
				errors[pool] = err.Error()
			} else {
				poolsReloaded = append(poolsReloaded, pool)
			}
		}
	}

	success := len(errors) == 0
	message := "Reload completed successfully"
	if !success {
		message = fmt.Sprintf("Reload completed with %d errors", len(errors))
	}

	response := ReloadResponse{
		Success:       success,
		Message:       message,
		RequestID:     requestID,
		Timestamp:     time.Now(),
		PoolsReloaded: poolsReloaded,
		Errors:        errors,
		Duration:      time.Since(start).String(),
	}

	status := http.StatusOK
	if !success {
		status = http.StatusPartialContent
	}

	s.writeJSON(w, status, response)
}

// HandleRestart handles POST /api/v1/restart
func (s *Server) HandleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "", nil)
		return
	}

	requestID := s.generateRequestID()
	start := time.Now()

	ctx := r.Context()
	if err := s.manager.Restart(ctx); err != nil {
		s.logger.Error("Failed to restart", zap.Error(err), zap.String("request_id", requestID))
		s.writeError(w, http.StatusInternalServerError, "Failed to restart", requestID, err.Error())
		return
	}

	response := OperationResponse{
		Success:   true,
		Message:   "Restart completed successfully",
		RequestID: requestID,
		Timestamp: time.Now(),
		Data: map[string]string{
			"duration": time.Since(start).String(),
		},
	}

	s.writeJSON(w, http.StatusOK, response)
}

// HandlePools handles GET /api/v1/pools
func (s *Server) HandlePools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "", nil)
		return
	}

	pools, err := s.supervisor.GetAllPoolsStatus()
	if err != nil {
		s.logger.Error("Failed to get pools", zap.Error(err))
		s.writeError(w, http.StatusInternalServerError, "Failed to get pools", s.generateRequestID(), nil)
		return
	}

	response := PoolListResponse{
		Success:   true,
		Message:   "Pools retrieved successfully",
		Timestamp: time.Now(),
		Pools:     pools,
		Count:     len(pools),
	}

	s.writeJSON(w, http.StatusOK, response)
}

// HandlePoolDetail handles GET /api/v1/pools/{pool}
func (s *Server) HandlePoolDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "", nil)
		return
	}

	pool := s.extractPoolName(r.URL.Path)
	if pool == "" {
		s.writeError(w, http.StatusBadRequest, "Pool name is required", s.generateRequestID(), nil)
		return
	}

	poolStatus, err := s.supervisor.GetPoolStatus(pool)
	if err != nil {
		s.logger.Error("Failed to get pool status", zap.String("pool", pool), zap.Error(err))
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("Pool '%s' not found", pool), s.generateRequestID(), nil)
		return
	}

	response := OperationResponse{
		Success:   true,
		Message:   "Pool status retrieved successfully",
		RequestID: s.generateRequestID(),
		Timestamp: time.Now(),
		Data:      poolStatus,
	}

	s.writeJSON(w, http.StatusOK, response)
}

// HandlePoolReload handles POST /api/v1/pools/{pool}/reload
func (s *Server) HandlePoolReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "", nil)
		return
	}

	pool := s.extractPoolName(r.URL.Path)
	if pool == "" {
		s.writeError(w, http.StatusBadRequest, "Pool name is required", s.generateRequestID(), nil)
		return
	}

	requestID := s.generateRequestID()
	start := time.Now()

	ctx := r.Context()
	if err := s.supervisor.ReloadPool(ctx, pool); err != nil {
		s.logger.Error("Failed to reload pool", zap.String("pool", pool), zap.Error(err), zap.String("request_id", requestID))
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to reload pool '%s'", pool), requestID, err.Error())
		return
	}

	response := OperationResponse{
		Success:   true,
		Message:   fmt.Sprintf("Pool '%s' reloaded successfully", pool),
		RequestID: requestID,
		Timestamp: time.Now(),
		Data: map[string]string{
			"pool":     pool,
			"duration": time.Since(start).String(),
		},
	}

	s.writeJSON(w, http.StatusOK, response)
}

// HandlePoolScale handles POST /api/v1/pools/{pool}/scale and GET /api/v1/pools/{pool}/scale
func (s *Server) HandlePoolScale(w http.ResponseWriter, r *http.Request) {
	pool := s.extractPoolName(r.URL.Path)
	if pool == "" {
		s.writeError(w, http.StatusBadRequest, "Pool name is required", s.generateRequestID(), nil)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handlePoolScaleGet(w, r, pool)
	case http.MethodPost:
		s.handlePoolScalePost(w, r, pool)
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "", nil)
	}
}

func (s *Server) handlePoolScaleGet(w http.ResponseWriter, r *http.Request, pool string) {
	currentScale, err := s.supervisor.GetPoolScale(pool)
	if err != nil {
		s.logger.Error("Failed to get pool scale", zap.String("pool", pool), zap.Error(err))
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("Failed to get scale for pool '%s'", pool), s.generateRequestID(), nil)
		return
	}

	response := OperationResponse{
		Success:   true,
		Message:   "Pool scale retrieved successfully",
		RequestID: s.generateRequestID(),
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"pool":    pool,
			"workers": currentScale,
		},
	}

	s.writeJSON(w, http.StatusOK, response)
}

func (s *Server) handlePoolScalePost(w http.ResponseWriter, r *http.Request, pool string) {
	requestID := s.generateRequestID()
	start := time.Now()

	var req ScaleRequest
	if err := s.parseJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid JSON request", requestID, err.Error())
		return
	}

	if req.Workers < 1 {
		s.writeError(w, http.StatusBadRequest, "Worker count must be at least 1", requestID, nil)
		return
	}

	// Get current scale
	oldWorkers, err := s.supervisor.GetPoolScale(pool)
	if err != nil {
		s.logger.Error("Failed to get current pool scale", zap.String("pool", pool), zap.Error(err))
		oldWorkers = 0 // Unknown current scale
	}

	ctx := r.Context()
	if err := s.supervisor.ScalePool(ctx, pool, req.Workers); err != nil {
		s.logger.Error("Failed to scale pool",
			zap.String("pool", pool),
			zap.Int("workers", req.Workers),
			zap.Error(err),
			zap.String("request_id", requestID))
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to scale pool '%s'", pool), requestID, err.Error())
		return
	}

	response := ScaleResponse{
		Success:    true,
		Message:    fmt.Sprintf("Pool '%s' scaled successfully", pool),
		RequestID:  requestID,
		Timestamp:  time.Now(),
		Pool:       pool,
		OldWorkers: oldWorkers,
		NewWorkers: req.Workers,
		Duration:   time.Since(start).String(),
	}

	s.writeJSON(w, http.StatusOK, response)
}

// HandleAutoscaling handles GET /api/v1/autoscaling and POST /api/v1/autoscaling
func (s *Server) HandleAutoscaling(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAutoscalingGet(w, r)
	case http.MethodPost:
		s.handleAutoscalingPost(w, r)
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "", nil)
	}
}

func (s *Server) handleAutoscalingGet(w http.ResponseWriter, r *http.Request) {
	status := s.collector.GetAutoscalingStatus()

	response := OperationResponse{
		Success:   true,
		Message:   "Autoscaling status retrieved successfully",
		RequestID: s.generateRequestID(),
		Timestamp: time.Now(),
		Data:      status,
	}

	s.writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleAutoscalingPost(w http.ResponseWriter, r *http.Request) {
	requestID := s.generateRequestID()

	var req AutoscalingRequest
	if err := s.parseJSON(r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid JSON request", requestID, err.Error())
		return
	}

	var err error
	var message string

	switch strings.ToLower(req.Action) {
	case "pause":
		err = s.collector.PauseAutoscaling()
		message = "Autoscaling paused successfully"
	case "resume":
		err = s.collector.ResumeAutoscaling()
		message = "Autoscaling resumed successfully"
	default:
		s.writeError(w, http.StatusBadRequest, "Invalid action. Use 'pause' or 'resume'", requestID, nil)
		return
	}

	if err != nil {
		s.logger.Error("Failed to execute autoscaling action",
			zap.String("action", req.Action),
			zap.Error(err),
			zap.String("request_id", requestID))
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to %s autoscaling", req.Action), requestID, err.Error())
		return
	}

	response := OperationResponse{
		Success:   true,
		Message:   message,
		RequestID: requestID,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"action": req.Action,
			"reason": req.Reason,
		},
	}

	s.writeJSON(w, http.StatusOK, response)
}

// HandleConfigVerify handles POST /api/v1/config/verify
func (s *Server) HandleConfigVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "", nil)
		return
	}

	requestID := s.generateRequestID()

	var req ConfigVerifyRequest
	if r.ContentLength > 0 {
		if err := s.parseJSON(r, &req); err != nil {
			s.writeError(w, http.StatusBadRequest, "Invalid JSON request", requestID, err.Error())
			return
		}
	}

	// Get pools to verify
	pools := req.Pools
	if len(pools) == 0 {
		allPools, err := s.supervisor.GetAllPoolsStatus()
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get pools list", requestID, err.Error())
			return
		}
		for _, pool := range allPools {
			pools = append(pools, pool.Name)
		}
	}

	// Verify each pool
	var results []PoolVerifyResult
	totalIssues := 0
	for _, pool := range pools {
		result := PoolVerifyResult{
			Pool:  pool,
			Valid: true,
		}

		if err := s.supervisor.ValidatePoolConfig(pool); err != nil {
			result.Valid = false
			result.Issues = []string{err.Error()}
			totalIssues++
		}

		results = append(results, result)
	}

	validPools := 0
	for _, result := range results {
		if result.Valid {
			validPools++
		}
	}

	success := totalIssues == 0
	message := "Configuration verification completed"
	if !success {
		message = fmt.Sprintf("Configuration verification completed with %d issues", totalIssues)
	}

	response := ConfigVerifyResponse{
		Success:     success,
		Message:     message,
		RequestID:   requestID,
		Timestamp:   time.Now(),
		PoolResults: results,
		Summary: VerifySummary{
			TotalPools:   len(pools),
			ValidPools:   validPools,
			InvalidPools: len(pools) - validPools,
			IssuesFound:  totalIssues,
			IssuesFixed:  s.countFixableIssues(results),
		},
	}

	status := http.StatusOK
	if !success {
		status = http.StatusPartialContent
	}

	s.writeJSON(w, status, response)
}

// HandleHealth handles GET /api/v1/health
func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "", nil)
		return
	}

	health := s.manager.Health()

	status := "healthy"
	checks := map[string]string{
		"manager": string(health.Overall),
	}

	// Add pool health checks
	if health.Pools != nil {
		for pool, state := range health.Pools {
			checks[fmt.Sprintf("pool_%s", pool)] = string(state)
		}
	}

	response := HealthResponse{
		Status:    status,
		Version:   s.version,
		Timestamp: time.Now(),
		Uptime:    time.Since(s.startTime).String(),
		Checks:    checks,
	}

	httpStatus := http.StatusOK
	if health.Overall != types.HealthStateHealthy {
		httpStatus = http.StatusServiceUnavailable
		response.Status = "unhealthy"
	}

	s.writeJSON(w, httpStatus, response)
}

// extractPoolName extracts pool name from URL path with security validation
func (s *Server) extractPoolName(path string) string {
	// Extract pool name from paths like /api/v1/pools/{pool}/...
	parts := strings.Split(strings.Trim(path, "/"), "/")

	// Find the index of "pools"
	poolsIndex := -1
	for i, part := range parts {
		if part == "pools" {
			poolsIndex = i
			break
		}
	}

	// Pool name should be the next part after "pools"
	if poolsIndex >= 0 && poolsIndex+1 < len(parts) && parts[poolsIndex+1] != "" {
		poolName := parts[poolsIndex+1]

		// Validate pool name for security
		if err := security.ValidatePoolName(poolName, s.securityConfig); err != nil {
			s.logger.Warn("Invalid pool name in request",
				zap.String("pool_name", poolName),
				zap.String("path", path),
				zap.Error(err))
			return ""
		}

		return poolName
	}

	return ""
}

// SetupRoutes sets up the API routes on an existing HTTP mux
func (s *Server) SetupRoutes(mux *http.ServeMux) {
	// System endpoints
	mux.HandleFunc("/api/v1/status", s.HandleStatus)
	mux.HandleFunc("/api/v1/health", s.HandleHealth)
	mux.HandleFunc("/api/v1/reload", s.HandleReload)
	mux.HandleFunc("/api/v1/restart", s.HandleRestart)

	// Pool endpoints
	mux.HandleFunc("/api/v1/pools", s.HandlePools)
	mux.HandleFunc("/api/v1/pools/", s.routePoolEndpoints)

	// Autoscaling endpoints
	mux.HandleFunc("/api/v1/autoscaling", s.HandleAutoscaling)

	// Configuration endpoints
	mux.HandleFunc("/api/v1/config/verify", s.HandleConfigVerify)

	// Event endpoints
	mux.HandleFunc("/api/v1/events", s.HandleEvents)
	mux.HandleFunc("/api/v1/events/annotations", s.HandleEventAnnotations)
	mux.HandleFunc("/api/v1/events/stats", s.HandleEventStats)
}

// routePoolEndpoints routes pool-specific endpoints
func (s *Server) routePoolEndpoints(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	if strings.HasSuffix(path, "/reload") {
		s.HandlePoolReload(w, r)
	} else if strings.HasSuffix(path, "/scale") {
		s.HandlePoolScale(w, r)
	} else {
		// Pool detail endpoint
		s.HandlePoolDetail(w, r)
	}
}

// HandleEvents handles requests for retrieving events
func (s *Server) HandleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed", s.generateRequestID(), nil)
		return
	}

	if s.eventStorage == nil {
		s.writeError(w, http.StatusServiceUnavailable, "Event storage not available", s.generateRequestID(), nil)
		return
	}

	ctx := r.Context()
	query := r.URL.Query()

	// Parse filter parameters
	filter := telemetry.EventFilter{
		Limit: 100, // Default limit
	}

	if startTime := query.Get("start_time"); startTime != "" {
		if t, err := time.Parse(time.RFC3339, startTime); err == nil {
			filter.StartTime = t
		}
	}

	if endTime := query.Get("end_time"); endTime != "" {
		if t, err := time.Parse(time.RFC3339, endTime); err == nil {
			filter.EndTime = t
		}
	}

	if pool := query.Get("pool"); pool != "" {
		filter.Pool = pool
	}

	if eventType := query.Get("type"); eventType != "" {
		filter.Type = telemetry.EventType(eventType)
	}

	if severity := query.Get("severity"); severity != "" {
		filter.Severity = telemetry.EventSeverity(severity)
	}

	if limit := query.Get("limit"); limit != "" {
		if l, err := parseIntParam(limit); err == nil && l > 0 && l <= 1000 {
			filter.Limit = l
		}
	}

	events, err := s.eventStorage.GetEvents(ctx, filter)
	if err != nil {
		s.logger.Error("Failed to retrieve events", zap.Error(err))
		s.writeError(w, http.StatusInternalServerError, "Failed to retrieve events", s.generateRequestID(), nil)
		return
	}

	response := map[string]interface{}{
		"events": events,
		"count":  len(events),
		"filter": filter,
	}

	s.writeJSON(w, http.StatusOK, response)
}

// HandleEventAnnotations handles requests for retrieving event annotations for metrics
func (s *Server) HandleEventAnnotations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed", s.generateRequestID(), nil)
		return
	}

	if s.eventStorage == nil {
		s.writeError(w, http.StatusServiceUnavailable, "Event storage not available", s.generateRequestID(), nil)
		return
	}

	ctx := r.Context()
	query := r.URL.Query()

	// Parse required time range parameters
	startTimeStr := query.Get("start_time")
	endTimeStr := query.Get("end_time")

	if startTimeStr == "" || endTimeStr == "" {
		s.writeError(w, http.StatusBadRequest, "start_time and end_time parameters are required", s.generateRequestID(), nil)
		return
	}

	startTime, err := time.Parse(time.RFC3339, startTimeStr)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid start_time format, use RFC3339", s.generateRequestID(), nil)
		return
	}

	endTime, err := time.Parse(time.RFC3339, endTimeStr)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid end_time format, use RFC3339", s.generateRequestID(), nil)
		return
	}

	pool := query.Get("pool") // Optional pool filter

	annotations, err := s.eventStorage.GetEventAnnotations(ctx, startTime, endTime, pool)
	if err != nil {
		s.logger.Error("Failed to retrieve event annotations", zap.Error(err))
		s.writeError(w, http.StatusInternalServerError, "Failed to retrieve event annotations", s.generateRequestID(), nil)
		return
	}

	response := map[string]interface{}{
		"annotations": annotations,
		"count":       len(annotations),
		"time_range": map[string]interface{}{
			"start": startTime,
			"end":   endTime,
		},
		"pool": pool,
	}

	s.writeJSON(w, http.StatusOK, response)
}

// HandleEventStats handles requests for event statistics
func (s *Server) HandleEventStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed", s.generateRequestID(), nil)
		return
	}

	if s.eventStorage == nil {
		s.writeError(w, http.StatusServiceUnavailable, "Event storage not available", s.generateRequestID(), nil)
		return
	}

	ctx := r.Context()

	stats, err := s.eventStorage.GetEventStats(ctx)
	if err != nil {
		s.logger.Error("Failed to retrieve event statistics", zap.Error(err))
		s.writeError(w, http.StatusInternalServerError, "Failed to retrieve event statistics", s.generateRequestID(), nil)
		return
	}

	s.writeJSON(w, http.StatusOK, stats)
}

// parseIntParam parses an integer parameter from a string
func parseIntParam(value string) (int, error) {
	return strconv.Atoi(value)
}

// validateConfiguration performs basic configuration validation
func (s *Server) validateConfiguration() bool {
	if s.manager == nil {
		return false
	}

	// Check if all essential components are available and healthy
	if s.supervisor == nil || s.collector == nil {
		return false
	}

	// Check supervisor health
	health := s.supervisor.Health()
	if health.Overall == "unknown" || health.Overall == "unhealthy" {
		return false
	}

	return true
}

// countFixableIssues counts the number of issues that could potentially be auto-fixed
func (s *Server) countFixableIssues(results []PoolVerifyResult) int {
	fixableCount := 0

	for _, result := range results {
		for _, issue := range result.Issues {
			// Count issues that are typically auto-fixable
			if s.isFixableIssue(issue) {
				fixableCount++
			}
		}
	}

	return fixableCount
}

// isFixableIssue determines if an issue type is potentially auto-fixable
func (s *Server) isFixableIssue(issue string) bool {
	// Define patterns for auto-fixable issues
	fixablePatterns := []string{
		"configuration format",
		"missing default",
		"invalid syntax",
		"deprecated option",
	}

	issueLower := strings.ToLower(issue)
	for _, pattern := range fixablePatterns {
		if strings.Contains(issueLower, pattern) {
			return true
		}
	}

	return false
}
