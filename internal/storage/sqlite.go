package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/config"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/types"
	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
)

// ConnectionPool manages database connection pool with health checks
type ConnectionPool struct {
	db           *sql.DB
	healthTicker *time.Ticker
	stats        PoolStats
	mu           sync.RWMutex
	logger       *zap.Logger
	config       PoolConfig
}

// PoolConfig contains connection pool configuration
type PoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	HealthInterval  time.Duration
}

// PoolStats tracks connection pool performance metrics
type PoolStats struct {
	ActiveConnections  int64
	IdleConnections    int64
	WaitCount          int64
	WaitDuration       time.Duration
	MaxIdleTime        time.Duration
	HealthChecks       int64
	FailedHealthChecks int64
	LastHealthCheck    time.Time
}

// SQLiteStorage implements the Storage interface using SQLite
type SQLiteStorage struct {
	config config.StorageConfig
	logger *zap.Logger
	pool   *ConnectionPool
	mu     sync.RWMutex

	// Background tasks
	cleanupTicker *time.Ticker
	aggTicker     *time.Ticker

	// State
	running bool

	// Performance optimization pools
	bufferPool sync.Pool // For JSON marshaling buffers
	stmtCache  map[string]*sql.Stmt
	stmtMu     sync.RWMutex
}

// MetricRow represents a metric row in the database
type MetricRow struct {
	ID               int64             `json:"id"`
	Timestamp        time.Time         `json:"timestamp"`
	MetricName       string            `json:"metric_name"`
	Value            float64           `json:"value"`
	MetricType       string            `json:"metric_type"`
	Labels           map[string]string `json:"labels"`
	AggregationLevel string            `json:"aggregation_level"` // raw, minute, hour
}

// NewConnectionPool creates a new optimized connection pool
func NewConnectionPool(databasePath string, logger *zap.Logger) (*ConnectionPool, error) {
	// Optimized DSN with performance settings
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=10000&_cache_size=10000&_synchronous=NORMAL&_temp_store=MEMORY", databasePath)

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enhanced connection pool configuration
	poolConfig := PoolConfig{
		MaxOpenConns:    25,            // Increased from 10 for better concurrency
		MaxIdleConns:    10,            // Increased from 5 to reduce connection churn
		ConnMaxLifetime: 2 * time.Hour, // Increased from 1 hour
		ConnMaxIdleTime: 30 * time.Minute,
		HealthInterval:  30 * time.Second,
	}

	// Configure database connection pool
	db.SetMaxOpenConns(poolConfig.MaxOpenConns)
	db.SetMaxIdleConns(poolConfig.MaxIdleConns)
	db.SetConnMaxLifetime(poolConfig.ConnMaxLifetime)
	db.SetConnMaxIdleTime(poolConfig.ConnMaxIdleTime)

	pool := &ConnectionPool{
		db:     db,
		config: poolConfig,
		logger: logger,
		stats:  PoolStats{LastHealthCheck: time.Now()},
	}

	// Start health check routine
	pool.startHealthCheck()

	logger.Info("Connection pool created",
		zap.Int("max_open_conns", poolConfig.MaxOpenConns),
		zap.Int("max_idle_conns", poolConfig.MaxIdleConns),
		zap.Duration("conn_max_lifetime", poolConfig.ConnMaxLifetime))

	return pool, nil
}

// startHealthCheck begins periodic health checking
func (p *ConnectionPool) startHealthCheck() {
	p.healthTicker = time.NewTicker(p.config.HealthInterval)
	go func() {
		for range p.healthTicker.C {
			p.performHealthCheck()
		}
	}()
}

// performHealthCheck executes a health check on the connection pool
func (p *ConnectionPool) performHealthCheck() {
	start := time.Now()

	p.mu.Lock()
	p.stats.HealthChecks++
	p.stats.LastHealthCheck = start
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Simple ping to verify connectivity
	if err := p.db.PingContext(ctx); err != nil {
		p.mu.Lock()
		p.stats.FailedHealthChecks++
		p.mu.Unlock()

		p.logger.Error("Connection pool health check failed", zap.Error(err))
		return
	}

	// Update connection statistics
	dbStats := p.db.Stats()
	p.mu.Lock()
	p.stats.ActiveConnections = int64(dbStats.OpenConnections - dbStats.Idle)
	p.stats.IdleConnections = int64(dbStats.Idle)
	p.stats.WaitCount = dbStats.WaitCount
	p.stats.WaitDuration = dbStats.WaitDuration
	p.stats.MaxIdleTime = time.Duration(dbStats.MaxIdleClosed)
	p.mu.Unlock()

	duration := time.Since(start)
	p.logger.Debug("Connection pool health check completed",
		zap.Duration("duration", duration),
		zap.Int64("active_connections", p.stats.ActiveConnections),
		zap.Int64("idle_connections", p.stats.IdleConnections))
}

// GetStats returns current pool statistics
func (p *ConnectionPool) GetStats() PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.stats
}

// Close shuts down the connection pool
func (p *ConnectionPool) Close() error {
	if p.healthTicker != nil {
		p.healthTicker.Stop()
	}
	return p.db.Close()
}

// NewSQLiteStorage creates a new SQLite storage backend
func NewSQLiteStorage(config config.StorageConfig, logger *zap.Logger) (*SQLiteStorage, error) {
	// Ensure directory exists
	dir := filepath.Dir(config.DatabasePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Initialize connection pool with enhanced configuration
	pool, err := NewConnectionPool(config.DatabasePath, logger.Named("connection-pool"))
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	s := &SQLiteStorage{
		config:    config,
		logger:    logger,
		pool:      pool,
		stmtCache: make(map[string]*sql.Stmt),
	}

	// Initialize buffer pool for JSON marshaling
	s.bufferPool.New = func() interface{} {
		return make([]byte, 0, 1024) // Start with 1KB capacity
	}

	// Initialize database schema
	if err := s.initSchema(); err != nil {
		s.pool.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return s, nil
}

// Start initializes the storage backend
func (s *SQLiteStorage) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("storage is already running")
	}
	s.running = true
	s.mu.Unlock()

	s.logger.Info("Starting SQLite storage backend",
		zap.String("database_path", s.config.DatabasePath))

	// Start cleanup ticker
	s.cleanupTicker = time.NewTicker(time.Hour)
	go s.cleanupLoop(ctx)

	// Start aggregation ticker if enabled
	if s.config.Aggregation.Enabled {
		s.aggTicker = time.NewTicker(s.config.Aggregation.Interval)
		go s.aggregationLoop(ctx)
	}

	return nil
}

// Stop closes the storage backend
func (s *SQLiteStorage) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	s.mu.Unlock()

	s.logger.Info("Stopping SQLite storage backend")

	// Stop tickers
	if s.cleanupTicker != nil {
		s.cleanupTicker.Stop()
	}
	if s.aggTicker != nil {
		s.aggTicker.Stop()
	}

	// Close prepared statements
	s.stmtMu.Lock()
	for _, stmt := range s.stmtCache {
		stmt.Close()
	}
	s.stmtMu.Unlock()

	// Close connection pool
	if s.pool != nil {
		return s.pool.Close()
	}

	return nil
}

// DB returns the underlying database connection for event storage
func (s *SQLiteStorage) DB() *sql.DB {
	return s.pool.db
}

// GetPoolStats returns current connection pool statistics
func (s *SQLiteStorage) GetPoolStats() PoolStats {
	return s.pool.GetStats()
}

// getOrCreateStmt returns a cached prepared statement or creates a new one
func (s *SQLiteStorage) getOrCreateStmt(query string) (*sql.Stmt, error) {
	s.stmtMu.RLock()
	if stmt, exists := s.stmtCache[query]; exists {
		s.stmtMu.RUnlock()
		return stmt, nil
	}
	s.stmtMu.RUnlock()

	s.stmtMu.Lock()
	defer s.stmtMu.Unlock()

	// Double-check after acquiring write lock
	if stmt, exists := s.stmtCache[query]; exists {
		return stmt, nil
	}

	// Create new prepared statement
	stmt, err := s.pool.db.Prepare(query)
	if err != nil {
		return nil, err
	}

	s.stmtCache[query] = stmt
	return stmt, nil
}

// Store persists a set of metrics with optimized batching and pooling
func (s *SQLiteStorage) Store(ctx context.Context, metrics types.MetricSet) error {
	if len(metrics.Metrics) == 0 {
		return nil
	}

	s.mu.RLock()
	db := s.pool.db
	running := s.running
	s.mu.RUnlock()

	if !running {
		return fmt.Errorf("storage is not running")
	}

	// Begin transaction for batch insert with optimized settings
	tx, err := db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelDefault,
		ReadOnly:  false,
	})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Use cached prepared statement
	insertQuery := `INSERT INTO metrics (timestamp, metric_name, value, metric_type, labels, aggregation_level) VALUES (?, ?, ?, ?, ?, ?)`
	stmt, err := tx.PrepareContext(ctx, insertQuery)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	// Optimize JSON marshaling with buffer pooling and caching
	jsonCache := make(map[string][]byte, len(metrics.Metrics)/4) // Estimate cache size
	var validMetrics []types.Metric
	var labelsJSONs [][]byte

	// Pre-allocate slices with known capacity
	validMetrics = make([]types.Metric, 0, len(metrics.Metrics))
	labelsJSONs = make([][]byte, 0, len(metrics.Metrics))

	// Process metrics with optimized JSON marshaling
	for _, metric := range metrics.Metrics {
		// Create efficient cache key
		labelsKey := fmt.Sprintf("%v", metric.Labels)

		var labelsJSON []byte
		if cached, exists := jsonCache[labelsKey]; exists {
			labelsJSON = cached
		} else {
			// Get buffer from pool
			buf := s.bufferPool.Get().([]byte)
			buf = buf[:0] // Reset length but keep capacity

			// Use more efficient JSON encoding
			labelsJSON, err = json.Marshal(metric.Labels)
			if err != nil {
				s.logger.Error("Failed to marshal metric labels",
					zap.String("metric", metric.Name),
					zap.Error(err))
				s.bufferPool.Put(buf)
				continue
			}

			// Copy to avoid buffer reuse issues
			jsonCopy := make([]byte, len(labelsJSON))
			copy(jsonCopy, labelsJSON)
			jsonCache[labelsKey] = jsonCopy

			// Return buffer to pool
			s.bufferPool.Put(buf)
			labelsJSON = jsonCopy
		}

		validMetrics = append(validMetrics, metric)
		labelsJSONs = append(labelsJSONs, labelsJSON)
	}

	// Batch insert with pre-processed data
	insertedCount := 0
	for i, metric := range validMetrics {
		_, err = stmt.ExecContext(ctx,
			metric.Timestamp.Unix(),
			metric.Name,
			metric.Value,
			string(metric.Type),
			string(labelsJSONs[i]),
			"raw",
		)
		if err != nil {
			s.logger.Error("Failed to insert metric",
				zap.String("metric", metric.Name),
				zap.Error(err))
			continue
		}
		insertedCount++
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	s.logger.Debug("Stored metrics batch",
		zap.Int("total_metrics", len(metrics.Metrics)),
		zap.Int("inserted_metrics", insertedCount),
		zap.Int("cached_labels", len(jsonCache)),
		zap.Time("timestamp", metrics.Timestamp))

	return nil
}

// Query retrieves metrics based on the given query
func (s *SQLiteStorage) Query(ctx context.Context, query types.Query) (*types.Result, error) {
	s.mu.RLock()
	db := s.pool.db
	running := s.running
	s.mu.RUnlock()

	if !running {
		return nil, fmt.Errorf("storage is not running")
	}

	// Build SQL query
	sqlQuery, args, err := s.buildQuery(query)
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	s.logger.Debug("Executing query",
		zap.String("sql", sqlQuery),
		zap.Any("args", args))

	rows, err := db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	var dataPoints []types.DataPoint
	for rows.Next() {
		var timestamp int64
		var value float64

		if err := rows.Scan(&timestamp, &value); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		dataPoints = append(dataPoints, types.DataPoint{
			Timestamp: time.Unix(timestamp, 0),
			Value:     value,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	result := &types.Result{
		MetricName: query.MetricName,
		Labels:     query.Labels,
		Values:     dataPoints,
	}

	s.logger.Debug("Query completed",
		zap.String("metric", query.MetricName),
		zap.Int("results", len(dataPoints)))

	return result, nil
}

// Cleanup removes expired metrics according to retention policies
func (s *SQLiteStorage) Cleanup(ctx context.Context) error {
	s.mu.RLock()
	db := s.pool.db
	running := s.running
	s.mu.RUnlock()

	if !running {
		return fmt.Errorf("storage is not running")
	}

	now := time.Now()

	// Clean up raw data
	rawCutoff := now.Add(-s.config.Retention.Raw)
	result, err := db.ExecContext(ctx,
		"DELETE FROM metrics WHERE aggregation_level = 'raw' AND timestamp < ?",
		rawCutoff.Unix())
	if err != nil {
		return fmt.Errorf("failed to cleanup raw data: %w", err)
	}

	if rowsAffected, err := result.RowsAffected(); err == nil {
		s.logger.Info("Cleaned up raw metrics",
			zap.Int64("rows_deleted", rowsAffected),
			zap.Time("cutoff", rawCutoff))
	}

	// Clean up minute aggregates
	minuteCutoff := now.Add(-s.config.Retention.Minute)
	result, err = db.ExecContext(ctx,
		"DELETE FROM metrics WHERE aggregation_level = 'minute' AND timestamp < ?",
		minuteCutoff.Unix())
	if err != nil {
		return fmt.Errorf("failed to cleanup minute data: %w", err)
	}

	if rowsAffected, err := result.RowsAffected(); err == nil {
		s.logger.Info("Cleaned up minute metrics",
			zap.Int64("rows_deleted", rowsAffected),
			zap.Time("cutoff", minuteCutoff))
	}

	// Clean up hour aggregates
	hourCutoff := now.Add(-s.config.Retention.Hour)
	result, err = db.ExecContext(ctx,
		"DELETE FROM metrics WHERE aggregation_level = 'hour' AND timestamp < ?",
		hourCutoff.Unix())
	if err != nil {
		return fmt.Errorf("failed to cleanup hour data: %w", err)
	}

	if rowsAffected, err := result.RowsAffected(); err == nil {
		s.logger.Info("Cleaned up hour metrics",
			zap.Int64("rows_deleted", rowsAffected),
			zap.Time("cutoff", hourCutoff))
	}

	// Vacuum database to reclaim space
	_, err = db.ExecContext(ctx, "VACUUM")
	if err != nil {
		s.logger.Error("Failed to vacuum database", zap.Error(err))
	} else {
		s.logger.Debug("Database vacuumed successfully")
	}

	return nil
}

// initSchema creates the database schema
func (s *SQLiteStorage) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS metrics (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp INTEGER NOT NULL,
		metric_name TEXT NOT NULL,
		value REAL NOT NULL,
		metric_type TEXT NOT NULL,
		labels TEXT NOT NULL DEFAULT '{}',
		aggregation_level TEXT NOT NULL DEFAULT 'raw',
		created_at INTEGER DEFAULT (strftime('%s', 'now'))
	);

	CREATE INDEX IF NOT EXISTS idx_metrics_timestamp ON metrics(timestamp);
	CREATE INDEX IF NOT EXISTS idx_metrics_name ON metrics(metric_name);
	CREATE INDEX IF NOT EXISTS idx_metrics_name_timestamp ON metrics(metric_name, timestamp);
	CREATE INDEX IF NOT EXISTS idx_metrics_aggregation ON metrics(aggregation_level);
	CREATE INDEX IF NOT EXISTS idx_metrics_compound ON metrics(metric_name, aggregation_level, timestamp);

	-- Aggregated metrics tables for better performance
	CREATE TABLE IF NOT EXISTS metrics_minute (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp INTEGER NOT NULL,
		metric_name TEXT NOT NULL,
		value_avg REAL NOT NULL,
		value_min REAL NOT NULL,
		value_max REAL NOT NULL,
		value_sum REAL NOT NULL,
		sample_count INTEGER NOT NULL,
		labels TEXT NOT NULL DEFAULT '{}',
		created_at INTEGER DEFAULT (strftime('%s', 'now'))
	);

	CREATE INDEX IF NOT EXISTS idx_metrics_minute_name_timestamp ON metrics_minute(metric_name, timestamp);

	CREATE TABLE IF NOT EXISTS metrics_hour (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp INTEGER NOT NULL,
		metric_name TEXT NOT NULL,
		value_avg REAL NOT NULL,
		value_min REAL NOT NULL,
		value_max REAL NOT NULL,
		value_sum REAL NOT NULL,
		sample_count INTEGER NOT NULL,
		labels TEXT NOT NULL DEFAULT '{}',
		created_at INTEGER DEFAULT (strftime('%s', 'now'))
	);

	CREATE INDEX IF NOT EXISTS idx_metrics_hour_name_timestamp ON metrics_hour(metric_name, timestamp);

	-- Events table for telemetry events and scaling events
	CREATE TABLE IF NOT EXISTS events (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		timestamp DATETIME NOT NULL,
		pool TEXT,
		summary TEXT NOT NULL,
		details TEXT NOT NULL, -- JSON blob
		correlation_id TEXT,
		severity TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Create indexes for efficient querying
	CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);
	CREATE INDEX IF NOT EXISTS idx_events_pool ON events(pool);
	CREATE INDEX IF NOT EXISTS idx_events_type ON events(type);
	CREATE INDEX IF NOT EXISTS idx_events_severity ON events(severity);
	CREATE INDEX IF NOT EXISTS idx_events_correlation_id ON events(correlation_id);
	CREATE INDEX IF NOT EXISTS idx_events_time_pool_type ON events(timestamp, pool, type);
	`

	_, err := s.pool.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	s.logger.Info("Database schema initialized successfully")
	return nil
}

// buildQuery constructs a SQL query from the given query parameters
func (s *SQLiteStorage) buildQuery(query types.Query) (string, []interface{}, error) {
	var sql string
	var args []interface{}

	// Base query
	baseSQL := "SELECT timestamp, "

	// Handle aggregation
	switch query.Aggregation {
	case "avg":
		baseSQL += "AVG(value) as value"
	case "sum":
		baseSQL += "SUM(value) as value"
	case "min":
		baseSQL += "MIN(value) as value"
	case "max":
		baseSQL += "MAX(value) as value"
	default:
		baseSQL += "value"
	}

	baseSQL += " FROM metrics WHERE metric_name = ?"
	args = append(args, query.MetricName)

	// Add time range constraints
	if !query.StartTime.IsZero() {
		baseSQL += " AND timestamp >= ?"
		args = append(args, query.StartTime.Unix())
	}

	if !query.EndTime.IsZero() {
		baseSQL += " AND timestamp <= ?"
		args = append(args, query.EndTime.Unix())
	}

	// Add label filters
	for key, value := range query.Labels {
		baseSQL += " AND JSON_EXTRACT(labels, '$." + key + "') = ?"
		args = append(args, value)
	}

	// Add grouping and ordering
	if query.Interval > 0 {
		intervalSeconds := int64(query.Interval.Seconds())
		baseSQL += " GROUP BY (timestamp / ?) * ?"
		args = append(args, intervalSeconds, intervalSeconds)
	}

	baseSQL += " ORDER BY timestamp ASC"

	sql = baseSQL

	return sql, args, nil
}

// cleanupLoop runs periodic cleanup
func (s *SQLiteStorage) cleanupLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.cleanupTicker.C:
			if err := s.Cleanup(ctx); err != nil {
				s.logger.Error("Cleanup failed", zap.Error(err))
			}
		}
	}
}

// aggregationLoop runs periodic data aggregation
func (s *SQLiteStorage) aggregationLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.aggTicker.C:
			if err := s.aggregateMinuteData(ctx); err != nil {
				s.logger.Error("Minute aggregation failed", zap.Error(err))
			}
			if err := s.aggregateHourData(ctx); err != nil {
				s.logger.Error("Hour aggregation failed", zap.Error(err))
			}
		}
	}
}

// aggregateMinuteData creates minute-level aggregates from raw data
func (s *SQLiteStorage) aggregateMinuteData(ctx context.Context) error {
	s.mu.RLock()
	db := s.pool.db
	s.mu.RUnlock()

	// Find the latest minute aggregate timestamp
	var lastMinute int64
	err := db.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(timestamp), 0) FROM metrics_minute").Scan(&lastMinute)
	if err != nil {
		return fmt.Errorf("failed to get last minute timestamp: %w", err)
	}

	// Aggregate data from the last processed minute to now
	now := time.Now()
	currentMinute := now.Truncate(time.Minute).Unix()

	if lastMinute >= currentMinute {
		// No new data to aggregate
		return nil
	}

	// Perform aggregation
	_, err = db.ExecContext(ctx, `
		INSERT INTO metrics_minute (timestamp, metric_name, value_avg, value_min, value_max, value_sum, sample_count, labels)
		SELECT
			(timestamp / 60) * 60 as minute_timestamp,
			metric_name,
			AVG(value) as value_avg,
			MIN(value) as value_min,
			MAX(value) as value_max,
			SUM(value) as value_sum,
			COUNT(*) as sample_count,
			labels
		FROM metrics
		WHERE aggregation_level = 'raw'
			AND timestamp > ?
			AND timestamp < ?
		GROUP BY minute_timestamp, metric_name, labels
	`, lastMinute, currentMinute)

	if err != nil {
		return fmt.Errorf("failed to aggregate minute data: %w", err)
	}

	s.logger.Debug("Aggregated minute data",
		zap.Int64("from_timestamp", lastMinute),
		zap.Int64("to_timestamp", currentMinute))

	return nil
}

// aggregateHourData creates hour-level aggregates from minute data
func (s *SQLiteStorage) aggregateHourData(ctx context.Context) error {
	s.mu.RLock()
	db := s.pool.db
	s.mu.RUnlock()

	// Find the latest hour aggregate timestamp
	var lastHour int64
	err := db.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(timestamp), 0) FROM metrics_hour").Scan(&lastHour)
	if err != nil {
		return fmt.Errorf("failed to get last hour timestamp: %w", err)
	}

	// Aggregate data from the last processed hour to now
	now := time.Now()
	currentHour := now.Truncate(time.Hour).Unix()

	if lastHour >= currentHour {
		// No new data to aggregate
		return nil
	}

	// Perform aggregation
	_, err = db.ExecContext(ctx, `
		INSERT INTO metrics_hour (timestamp, metric_name, value_avg, value_min, value_max, value_sum, sample_count, labels)
		SELECT
			(timestamp / 3600) * 3600 as hour_timestamp,
			metric_name,
			AVG(value_avg) as value_avg,
			MIN(value_min) as value_min,
			MAX(value_max) as value_max,
			SUM(value_sum) as value_sum,
			SUM(sample_count) as sample_count,
			labels
		FROM metrics_minute
		WHERE timestamp > ?
			AND timestamp < ?
		GROUP BY hour_timestamp, metric_name, labels
	`, lastHour, currentHour)

	if err != nil {
		return fmt.Errorf("failed to aggregate hour data: %w", err)
	}

	s.logger.Debug("Aggregated hour data",
		zap.Int64("from_timestamp", lastHour),
		zap.Int64("to_timestamp", currentHour))

	return nil
}
