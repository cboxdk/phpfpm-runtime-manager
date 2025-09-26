package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/types"
)

// Additional handlers for the secure API server

// HealthHandler returns the overall health status of the system
func (s *Server) HealthHandler(ctx context.Context, req interface{}) (interface{}, error) {
	managerHealth := s.manager.Health()
	supervisorHealth := s.supervisor.Health()

	overallStatus := types.HealthStateHealthy
	if managerHealth.Overall != types.HealthStateHealthy || supervisorHealth.Overall != types.HealthStateHealthy {
		overallStatus = types.HealthStateUnhealthy
	}

	return map[string]interface{}{
		"status":    overallStatus,
		"timestamp": time.Now(),
		"components": map[string]interface{}{
			"manager":    managerHealth,
			"supervisor": supervisorHealth,
		},
		"uptime":  time.Since(s.startTime).String(),
		"version": s.version,
	}, nil
}

// ReadinessHandler returns readiness status for k8s readiness probes
func (s *Server) ReadinessHandler(ctx context.Context, req interface{}) (interface{}, error) {
	// Check if all critical components are ready
	managerHealth := s.manager.Health()
	supervisorHealth := s.supervisor.Health()

	ready := managerHealth.Overall == types.HealthStateHealthy && supervisorHealth.Overall == types.HealthStateHealthy

	status := "ready"
	if !ready {
		status = "not_ready"
	}

	return map[string]interface{}{
		"status":    status,
		"ready":     ready,
		"timestamp": time.Now(),
		"checks": map[string]bool{
			"manager":    managerHealth.Overall == types.HealthStateHealthy,
			"supervisor": supervisorHealth.Overall == types.HealthStateHealthy,
		},
	}, nil
}

// LivenessHandler returns liveness status for k8s liveness probes
func (s *Server) LivenessHandler(ctx context.Context, req interface{}) (interface{}, error) {
	// Simple liveness check - if we can respond, we're alive
	return map[string]interface{}{
		"status":    "alive",
		"timestamp": time.Now(),
		"uptime":    time.Since(s.startTime).String(),
	}, nil
}

// ListPoolsHandler returns a list of all PHP-FPM pools
func (s *Server) ListPoolsHandler(ctx context.Context, req interface{}) (interface{}, error) {
	pools, err := s.supervisor.GetAllPoolsStatus()
	if err != nil {
		return nil, NewError("pools_list_failed", "Failed to retrieve pools list").
			WithDetails(err.Error()).
			WithStatus(http.StatusInternalServerError).
			Build()
	}

	return map[string]interface{}{
		"pools":     pools,
		"count":     len(pools),
		"timestamp": time.Now(),
	}, nil
}

// PoolStatusHandler returns detailed status for specific pools
func (s *Server) PoolStatusHandler(ctx context.Context, req interface{}) (interface{}, error) {
	pools, err := s.supervisor.GetAllPoolsStatus()
	if err != nil {
		return nil, NewError("pool_status_failed", "Failed to retrieve pool status").
			WithDetails(err.Error()).
			WithStatus(http.StatusInternalServerError).
			Build()
	}

	return map[string]interface{}{
		"pools": pools,
		"summary": map[string]interface{}{
			"total":     len(pools),
			"healthy":   countHealthyPools(pools),
			"timestamp": time.Now(),
		},
	}, nil
}

// RestartHandler restarts the PHP-FPM manager
func (s *Server) RestartHandler(ctx context.Context, req interface{}) (interface{}, error) {
	if err := s.manager.Restart(ctx); err != nil {
		return nil, NewError("restart_failed", "Failed to restart service").
			WithDetails(err.Error()).
			WithStatus(http.StatusInternalServerError).
			Build()
	}

	return map[string]interface{}{
		"success":   true,
		"message":   "Service restart initiated successfully",
		"timestamp": time.Now(),
	}, nil
}

// ScalePoolHandler scales a specific PHP-FPM pool
func (s *Server) ScalePoolHandler(ctx context.Context, req interface{}) (interface{}, error) {
	scaleReq := req.(*ScaleRequest)

	if scaleReq.Workers <= 0 {
		return nil, NewError("invalid_request", "Worker count must be positive").
			WithStatus(http.StatusBadRequest).
			Build()
	}

	// For this simplified example, we'll extract pool name from URL path
	// In a full implementation, this would come from the request path or body
	poolName := "default" // Placeholder - should be extracted from URL path

	// Get current scale
	currentWorkers, err := s.supervisor.GetPoolScale(poolName)
	if err != nil {
		return nil, NewError("pool_not_found", "Pool not found or inaccessible").
			WithDetails(err.Error()).
			WithStatus(http.StatusNotFound).
			Build()
	}

	// Perform scaling
	if err := s.supervisor.ScalePool(ctx, poolName, scaleReq.Workers); err != nil {
		return nil, NewError("scale_failed", "Failed to scale pool").
			WithDetails(err.Error()).
			WithStatus(http.StatusInternalServerError).
			Build()
	}

	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Pool %s scaled from %d to %d workers",
			poolName, currentWorkers, scaleReq.Workers),
		"pool":             poolName,
		"previous_workers": currentWorkers,
		"current_workers":  scaleReq.Workers,
		"timestamp":        time.Now(),
	}, nil
}

// MetricsHandler returns system and pool metrics
func (s *Server) MetricsHandler(ctx context.Context, req interface{}) (interface{}, error) {
	// Get pool status for metrics
	pools, err := s.supervisor.GetAllPoolsStatus()
	if err != nil {
		return nil, NewError("metrics_failed", "Failed to retrieve metrics").
			WithDetails(err.Error()).
			WithStatus(http.StatusInternalServerError).
			Build()
	}

	// Calculate summary metrics
	totalPools := len(pools)
	healthyPools := countHealthyPools(pools)
	totalWorkers := 0
	activeWorkers := 0

	for _, pool := range pools {
		totalWorkers += pool.Workers.Total
		activeWorkers += pool.Workers.Active
	}

	autoscalingEnabled := false
	autoscalingPaused := false
	if s.collector != nil {
		autoscalingEnabled = s.collector.IsAutoscalingEnabled()
		autoscalingPaused = s.collector.IsAutoscalingPaused()
	}

	return map[string]interface{}{
		"summary": map[string]interface{}{
			"pools_total":         totalPools,
			"pools_healthy":       healthyPools,
			"workers_total":       totalWorkers,
			"workers_active":      activeWorkers,
			"autoscaling_enabled": autoscalingEnabled,
			"autoscaling_paused":  autoscalingPaused,
		},
		"pools":     pools,
		"timestamp": time.Now(),
		"uptime":    time.Since(s.startTime).String(),
	}, nil
}

// ConfigHandler returns current configuration information
func (s *Server) ConfigHandler(ctx context.Context, req interface{}) (interface{}, error) {
	// Return basic configuration information (without sensitive data)
	return map[string]interface{}{
		"version":    s.version,
		"start_time": s.startTime,
		"uptime":     time.Since(s.startTime).String(),
		"features": map[string]bool{
			"autoscaling":        s.collector != nil && s.collector.IsAutoscalingEnabled(),
			"metrics_collection": s.collector != nil,
			"event_storage":      s.eventStorage != nil,
		},
		"timestamp": time.Now(),
	}, nil
}

// Extended PoolStatus for simple response format
type SimplePoolStatus struct {
	Name          string    `json:"name"`
	Status        string    `json:"status"`
	Workers       int       `json:"workers"`
	ActiveWorkers int       `json:"active_workers"`
	MaxWorkers    int       `json:"max_workers"`
	StartTime     time.Time `json:"start_time"`
	Uptime        string    `json:"uptime"`
	Health        string    `json:"health"`
}

// Helper functions

func countHealthyPools(pools []PoolStatus) int {
	count := 0
	for _, pool := range pools {
		if pool.Health == types.HealthStateHealthy || pool.Status == "running" {
			count++
		}
	}
	return count
}
