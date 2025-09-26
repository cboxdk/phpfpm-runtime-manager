package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/api"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/config"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/telemetry"
	"go.uber.org/zap"
)

// EventStorage implements telemetry.EventStorage for SQLite
type EventStorage struct {
	db     *sql.DB
	logger *zap.Logger
}

// NewEventStorage creates a new event storage instance
func NewEventStorage(db *sql.DB, logger *zap.Logger) *EventStorage {
	return &EventStorage{
		db:     db,
		logger: logger,
	}
}

// StoreEvent stores an event in the database
func (s *EventStorage) StoreEvent(ctx context.Context, event telemetry.Event) error {
	detailsJSON, err := json.Marshal(event.Details)
	if err != nil {
		return fmt.Errorf("failed to marshal event details: %w", err)
	}

	query := `
		INSERT INTO events (id, type, timestamp, pool, summary, details, correlation_id, severity)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = s.db.ExecContext(ctx, query,
		event.ID,
		string(event.Type),
		event.Timestamp,
		event.Pool,
		event.Summary,
		string(detailsJSON),
		event.CorrelationID,
		string(event.Severity),
	)

	if err != nil {
		s.logger.Error("Failed to store event",
			zap.String("event_id", event.ID),
			zap.Error(err))
		return fmt.Errorf("failed to store event: %w", err)
	}

	s.logger.Debug("Event stored successfully",
		zap.String("event_id", event.ID),
		zap.String("type", string(event.Type)))

	return nil
}

// GetEvents retrieves events from the database based on the filter
func (s *EventStorage) GetEvents(ctx context.Context, filter telemetry.EventFilter) ([]telemetry.Event, error) {
	query, args := s.buildEventQuery(filter)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	var events []telemetry.Event
	for rows.Next() {
		var event telemetry.Event
		var detailsJSON string
		var eventType, severity string
		var pool sql.NullString
		var correlationID sql.NullString

		err := rows.Scan(
			&event.ID,
			&eventType,
			&event.Timestamp,
			&pool,
			&event.Summary,
			&detailsJSON,
			&correlationID,
			&severity,
		)
		if err != nil {
			s.logger.Error("Failed to scan event row", zap.Error(err))
			continue
		}

		// Convert fields
		event.Type = telemetry.EventType(eventType)
		event.Severity = telemetry.EventSeverity(severity)
		if pool.Valid {
			event.Pool = pool.String
		}
		if correlationID.Valid {
			event.CorrelationID = correlationID.String
		}

		// Parse details JSON
		if err := json.Unmarshal([]byte(detailsJSON), &event.Details); err != nil {
			s.logger.Error("Failed to unmarshal event details",
				zap.String("event_id", event.ID),
				zap.Error(err))
			event.Details = make(map[string]interface{})
		}

		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating event rows: %w", err)
	}

	s.logger.Debug("Retrieved events",
		zap.Int("count", len(events)),
		zap.String("filter_pool", filter.Pool),
		zap.String("filter_type", string(filter.Type)))

	return events, nil
}

// GetEventsByTimeRange retrieves events within a specific time range for metrics annotations
func (s *EventStorage) GetEventsByTimeRange(ctx context.Context, startTime, endTime time.Time, pool string) ([]telemetry.Event, error) {
	filter := telemetry.EventFilter{
		StartTime: startTime,
		EndTime:   endTime,
		Pool:      pool,
		Limit:     config.DefaultAnnotationLimit, // Reasonable limit for annotations
	}

	return s.GetEvents(ctx, filter)
}

// GetEventAnnotations retrieves events as metric annotations for a specific time window
func (s *EventStorage) GetEventAnnotations(ctx context.Context, startTime, endTime time.Time, pool string) ([]api.EventAnnotation, error) {
	events, err := s.GetEventsByTimeRange(ctx, startTime, endTime, pool)
	if err != nil {
		return nil, err
	}

	var annotations []api.EventAnnotation
	for _, event := range events {
		annotation := api.EventAnnotation{
			Timestamp: event.Timestamp,
			Pool:      event.Pool,
			EventType: string(event.Type),
			Summary:   event.Summary,
			Severity:  string(event.Severity),
			EventID:   event.ID,
			Details:   event.Details,
		}
		annotations = append(annotations, annotation)
	}

	return annotations, nil
}

// buildEventQuery constructs a SQL query with filters
func (s *EventStorage) buildEventQuery(filter telemetry.EventFilter) (string, []interface{}) {
	query := `
		SELECT id, type, timestamp, pool, summary, details, correlation_id, severity
		FROM events
		WHERE 1=1
	`
	var args []interface{}

	// Time range filters
	if !filter.StartTime.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, filter.StartTime)
	}

	if !filter.EndTime.IsZero() {
		query += " AND timestamp <= ?"
		args = append(args, filter.EndTime)
	}

	// Pool filter
	if filter.Pool != "" {
		query += " AND pool = ?"
		args = append(args, filter.Pool)
	}

	// Type filter
	if filter.Type != "" {
		query += " AND type = ?"
		args = append(args, string(filter.Type))
	}

	// Severity filter
	if filter.Severity != "" {
		query += " AND severity = ?"
		args = append(args, string(filter.Severity))
	}

	// Order by timestamp descending
	query += " ORDER BY timestamp DESC"

	// Limit
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	return query, args
}

// CleanupOldEvents removes events older than the specified retention period
func (s *EventStorage) CleanupOldEvents(ctx context.Context, retentionPeriod time.Duration) (int64, error) {
	cutoffTime := time.Now().Add(-retentionPeriod)

	result, err := s.db.ExecContext(ctx,
		"DELETE FROM events WHERE timestamp < ?",
		cutoffTime,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup old events: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	s.logger.Info("Cleaned up old events",
		zap.Int64("rows_deleted", rowsAffected),
		zap.Duration("retention_period", retentionPeriod))

	return rowsAffected, nil
}

// GetEventStats returns statistics about stored events
func (s *EventStorage) GetEventStats(ctx context.Context) (api.EventStats, error) {
	var stats api.EventStats

	// Get total count
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events").Scan(&stats.TotalEvents)
	if err != nil {
		return stats, fmt.Errorf("failed to get total event count: %w", err)
	}

	// Get count by type
	rows, err := s.db.QueryContext(ctx, `
		SELECT type, COUNT(*)
		FROM events
		GROUP BY type
	`)
	if err != nil {
		return stats, fmt.Errorf("failed to get event counts by type: %w", err)
	}
	defer rows.Close()

	stats.EventsByType = make(map[string]int64)
	for rows.Next() {
		var eventType string
		var count int64
		if err := rows.Scan(&eventType, &count); err != nil {
			continue
		}
		stats.EventsByType[eventType] = count
	}

	// Get oldest and newest event timestamps
	err = s.db.QueryRowContext(ctx, "SELECT MIN(timestamp), MAX(timestamp) FROM events").
		Scan(&stats.OldestEvent, &stats.NewestEvent)
	if err != nil && err != sql.ErrNoRows {
		return stats, fmt.Errorf("failed to get event timestamp range: %w", err)
	}

	return stats, nil
}
