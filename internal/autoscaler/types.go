package autoscaler

import (
	"time"
)

// PoolStatusResponse represents the response structure for pool status queries
type PoolStatusResponse struct {
	IsRunning       bool                  `json:"is_running"`
	ScalingInterval time.Duration         `json:"scaling_interval"`
	GlobalCooldown  time.Duration         `json:"global_cooldown"`
	Pools           map[string]PoolDetail `json:"pools"`
}

// PoolDetail represents detailed information about a specific pool
type PoolDetail struct {
	CurrentWorkers int                    `json:"current_workers"`
	TargetWorkers  int                    `json:"target_workers"`
	LastScaleTime  time.Time              `json:"last_scale_time"`
	LastDecision   string                 `json:"last_decision"`
	Baseline       BaselineStatusDetail   `json:"baseline"`
	Collection     CollectionStatusDetail `json:"collection"`
}

// BaselineStatusDetail represents the baseline learning status
type BaselineStatusDetail struct {
	IsLearning               bool          `json:"is_learning"`
	SampleCount              int           `json:"sample_count"`
	ConfidenceScore          float64       `json:"confidence_score"`
	OptimalMemoryPerWorker   float64       `json:"optimal_memory_per_worker"`
	OptimalCPUPerWorker      float64       `json:"optimal_cpu_per_worker"`
	OptimalRequestDuration   time.Duration `json:"optimal_request_duration"`
	OptimalRequestsPerWorker float64       `json:"optimal_requests_per_worker"`
	LearningPhase            string        `json:"learning_phase"`
	MaxSamples               int           `json:"max_samples"`
}

// CollectionStatusDetail represents the metrics collection status
type CollectionStatusDetail struct {
	IsRunning       bool          `json:"is_running"`
	LastCollectTime time.Time     `json:"last_collect_time"`
	CollectInterval time.Duration `json:"collect_interval"`
	TotalSamples    int           `json:"total_samples"`
	SuccessfulRuns  int           `json:"successful_runs"`
	FailedRuns      int           `json:"failed_runs"`
	LastError       string        `json:"last_error,omitempty"`
	LastErrorTime   time.Time     `json:"last_error_time,omitempty"`
}

// ScalingMetrics represents comprehensive scaling metrics
type ScalingMetrics struct {
	PoolName           string    `json:"pool_name"`
	CurrentWorkers     int       `json:"current_workers"`
	TargetWorkers      int       `json:"target_workers"`
	ActiveWorkers      int       `json:"active_workers"`
	IdleWorkers        int       `json:"idle_workers"`
	QueueDepth         int       `json:"queue_depth"`
	ProcessingVelocity float64   `json:"processing_velocity"`
	UtilizationPercent float64   `json:"utilization_percent"`
	IsLearning         bool      `json:"is_learning"`
	ConfidenceScore    float64   `json:"confidence_score"`
	LastScaleTime      time.Time `json:"last_scale_time"`
	LastScaleDirection int       `json:"last_scale_direction"` // 1 for up, -1 for down, 0 for none
	LastDecision       string    `json:"last_decision"`
	Health             string    `json:"health"`
	Status             string    `json:"status"`
}

// EmergencyScalingEvent represents an emergency scaling event
type EmergencyScalingEvent struct {
	PoolName           string             `json:"pool_name"`
	Timestamp          time.Time          `json:"timestamp"`
	TriggerType        string             `json:"trigger_type"` // "queue_buildup", "full_utilization", "near_capacity"
	Condition          EmergencyCondition `json:"condition"`
	FromWorkers        int                `json:"from_workers"`
	ToWorkers          int                `json:"to_workers"`
	UtilizationPercent float64            `json:"utilization_percent"`
	QueueDepth         int                `json:"queue_depth"`
	Success            bool               `json:"success"`
	ErrorMessage       string             `json:"error_message,omitempty"`
	ExecutionTimeMs    int64              `json:"execution_time_ms"`
}

// SystemHealthStatus represents overall autoscaling system health
type SystemHealthStatus struct {
	Timestamp       time.Time             `json:"timestamp"`
	OverallHealth   string                `json:"overall_health"` // "healthy", "degraded", "unhealthy"
	ActivePools     int                   `json:"active_pools"`
	TotalPools      int                   `json:"total_pools"`
	RunningTime     time.Duration         `json:"running_time"`
	TotalScalingOps int64                 `json:"total_scaling_ops"`
	SuccessfulOps   int64                 `json:"successful_ops"`
	FailedOps       int64                 `json:"failed_ops"`
	Pools           map[string]PoolHealth `json:"pools"`
}

// PoolHealth represents health status of individual pools
type PoolHealth struct {
	Name            string    `json:"name"`
	Health          string    `json:"health"`
	Status          string    `json:"status"`
	CurrentWorkers  int       `json:"current_workers"`
	TargetWorkers   int       `json:"target_workers"`
	LastScaleTime   time.Time `json:"last_scale_time"`
	LastHealthCheck time.Time `json:"last_health_check"`
	ErrorCount      int       `json:"error_count"`
	LastError       string    `json:"last_error,omitempty"`
}
