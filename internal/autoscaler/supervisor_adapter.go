package autoscaler

import (
	"context"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/api"
)

// SupervisorAdapter adapts the supervisor to work with autoscaler
type SupervisorAdapter struct {
	supervisor Supervisor
}

// Supervisor interface defines what we need from the supervisor
type Supervisor interface {
	ScalePool(ctx context.Context, pool string, workers int) error
	ReloadPool(ctx context.Context, pool string) error
	GetPoolStatus(pool string) (*api.PoolStatus, error)
	GetPoolScale(pool string) (int, error)
}

// NewSupervisorAdapter creates a new adapter for supervisor integration
func NewSupervisorAdapter(supervisor Supervisor) *SupervisorAdapter {
	return &SupervisorAdapter{
		supervisor: supervisor,
	}
}

// ScalePool implements PHPFPMSupervisor interface
func (s *SupervisorAdapter) ScalePool(ctx context.Context, pool string, workers int) error {
	return s.supervisor.ScalePool(ctx, pool, workers)
}

// ReloadPool implements PHPFPMSupervisor interface
func (s *SupervisorAdapter) ReloadPool(ctx context.Context, pool string) error {
	return s.supervisor.ReloadPool(ctx, pool)
}

// GetPoolStatus implements PHPFPMSupervisor interface
func (s *SupervisorAdapter) GetPoolStatus(pool string) (*PoolStatusInfo, error) {
	status, err := s.supervisor.GetPoolStatus(pool)
	if err != nil {
		return nil, err
	}

	// Convert from api.PoolStatus to PoolStatusInfo
	return &PoolStatusInfo{
		Name:           status.Name,
		Status:         status.Status,
		Health:         string(status.Health),
		CurrentWorkers: status.Workers.Total,
		ActiveWorkers:  status.Workers.Active,
		IdleWorkers:    status.Workers.Idle,
		QueueDepth:     0,           // Would need to be populated from metrics
		LastScaleTime:  time.Time{}, // Would need to track this
		ConfigPath:     status.ConfigPath,
	}, nil
}

// GetPoolScale implements PHPFPMSupervisor interface
func (s *SupervisorAdapter) GetPoolScale(pool string) (int, error) {
	return s.supervisor.GetPoolScale(pool)
}

// SetupAutoscalerWithSupervisor creates a fully configured autoscaler with supervisor integration
func SetupAutoscalerWithSupervisor(supervisor Supervisor, cfg interface{}, metricsCollector interface{}, logger interface{}) (*Manager, error) {
	// This would be implemented based on the actual config and metrics types
	// For now, it's a placeholder showing the integration pattern
	return nil, nil
}
