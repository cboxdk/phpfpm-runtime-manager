package api

import (
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/types"
)

// API Response Types

// StatusResponse represents the system status
type StatusResponse struct {
	Version       string              `json:"version"`
	Status        string              `json:"status"`
	Uptime        string              `json:"uptime"`
	Pools         []PoolStatus        `json:"pools"`
	Autoscaling   AutoscalingStatus   `json:"autoscaling"`
	Configuration ConfigurationStatus `json:"configuration"`
	LastUpdate    time.Time           `json:"last_update"`
}

// PoolStatus represents the status of a single pool
type PoolStatus struct {
	Name            string            `json:"name"`
	Status          string            `json:"status"`
	Workers         WorkerStatus      `json:"workers"`
	Health          types.HealthState `json:"health"`
	ConfigPath      string            `json:"config_path"`
	FastCGIEndpoint string            `json:"fastcgi_endpoint"`
	LastReload      *time.Time        `json:"last_reload,omitempty"`
}

// WorkerStatus represents worker information for a pool
type WorkerStatus struct {
	Active  int `json:"active"`
	Idle    int `json:"idle"`
	Total   int `json:"total"`
	Target  int `json:"target"`
	Maximum int `json:"maximum"`
}

// AutoscalingStatus represents autoscaling state
type AutoscalingStatus struct {
	Enabled       bool              `json:"enabled"`
	Paused        bool              `json:"paused"`
	LastAction    string            `json:"last_action,omitempty"`
	LastUpdate    time.Time         `json:"last_update"`
	Configuration AutoscalingConfig `json:"configuration"`
}

// AutoscalingConfig represents autoscaling configuration
type AutoscalingConfig struct {
	CPUThreshold      float64 `json:"cpu_threshold"`
	MemoryThreshold   float64 `json:"memory_threshold"`
	ScaleUpCooldown   string  `json:"scale_up_cooldown"`
	ScaleDownCooldown string  `json:"scale_down_cooldown"`
	MinWorkers        int     `json:"min_workers"`
	MaxWorkers        int     `json:"max_workers"`
}

// ConfigurationStatus represents configuration status
type ConfigurationStatus struct {
	Valid         bool       `json:"valid"`
	LastValidated *time.Time `json:"last_validated,omitempty"`
	LastModified  *time.Time `json:"last_modified,omitempty"`
	Issues        []string   `json:"issues,omitempty"`
}

// API Request Types

// ReloadRequest represents a reload operation request
type ReloadRequest struct {
	Pools []string `json:"pools,omitempty"` // Empty means all pools
	Force bool     `json:"force,omitempty"` // Force reload even if no changes detected
}

// ScaleRequest represents a scaling operation request
type ScaleRequest struct {
	Workers int    `json:"workers"`
	Reason  string `json:"reason,omitempty"`
}

// AutoscalingRequest represents autoscaling control request
type AutoscalingRequest struct {
	Action string `json:"action"` // "pause", "resume", "enable", "disable"
	Reason string `json:"reason,omitempty"`
}

// ConfigVerifyRequest represents config verification request
type ConfigVerifyRequest struct {
	Pools []string `json:"pools,omitempty"` // Empty means all pools
	Fix   bool     `json:"fix,omitempty"`   // Attempt to fix common issues
}

// API Response Types

// OperationResponse represents a generic operation response
type OperationResponse struct {
	Success   bool        `json:"success"`
	Message   string      `json:"message"`
	RequestID string      `json:"request_id,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data,omitempty"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error     string      `json:"error"`
	Message   string      `json:"message"`
	RequestID string      `json:"request_id,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
	Details   interface{} `json:"details,omitempty"`
}

// ReloadResponse represents reload operation response
type ReloadResponse struct {
	Success       bool              `json:"success"`
	Message       string            `json:"message"`
	RequestID     string            `json:"request_id"`
	Timestamp     time.Time         `json:"timestamp"`
	PoolsReloaded []string          `json:"pools_reloaded"`
	Errors        map[string]string `json:"errors,omitempty"`
	Duration      string            `json:"duration"`
}

// ScaleResponse represents scaling operation response
type ScaleResponse struct {
	Success    bool      `json:"success"`
	Message    string    `json:"message"`
	RequestID  string    `json:"request_id"`
	Timestamp  time.Time `json:"timestamp"`
	Pool       string    `json:"pool"`
	OldWorkers int       `json:"old_workers"`
	NewWorkers int       `json:"new_workers"`
	Duration   string    `json:"duration"`
}

// ConfigVerifyResponse represents config verification response
type ConfigVerifyResponse struct {
	Success     bool               `json:"success"`
	Message     string             `json:"message"`
	RequestID   string             `json:"request_id"`
	Timestamp   time.Time          `json:"timestamp"`
	PoolResults []PoolVerifyResult `json:"pool_results"`
	Summary     VerifySummary      `json:"summary"`
}

// PoolVerifyResult represents verification result for a single pool
type PoolVerifyResult struct {
	Pool     string   `json:"pool"`
	Valid    bool     `json:"valid"`
	Issues   []string `json:"issues,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	Fixed    []string `json:"fixed,omitempty"`
}

// VerifySummary represents overall verification summary
type VerifySummary struct {
	TotalPools   int `json:"total_pools"`
	ValidPools   int `json:"valid_pools"`
	InvalidPools int `json:"invalid_pools"`
	IssuesFound  int `json:"issues_found"`
	IssuesFixed  int `json:"issues_fixed"`
}

// PoolListResponse represents list of pools response
type PoolListResponse struct {
	Success   bool         `json:"success"`
	Message   string       `json:"message"`
	Timestamp time.Time    `json:"timestamp"`
	Pools     []PoolStatus `json:"pools"`
	Count     int          `json:"count"`
}

// Health check response
type HealthResponse struct {
	Status    string            `json:"status"`
	Version   string            `json:"version"`
	Timestamp time.Time         `json:"timestamp"`
	Uptime    string            `json:"uptime"`
	Checks    map[string]string `json:"checks"`
}
