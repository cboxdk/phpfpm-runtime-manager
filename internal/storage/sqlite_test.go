package storage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/config"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/types"
	"go.uber.org/zap/zaptest"
)

func TestNewSQLiteStorage(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "sqlite_storage_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name           string
		config         config.StorageConfig
		expectError    bool
		expectedErrMsg string
	}{
		{
			name: "valid configuration",
			config: config.StorageConfig{
				DatabasePath: filepath.Join(tempDir, "test.db"),
				Retention: config.RetentionConfig{
					Raw:    time.Hour,
					Minute: 24 * time.Hour,
					Hour:   7 * 24 * time.Hour,
				},
				Aggregation: config.AggregationConfig{
					Enabled:   true,
					Interval:  time.Minute,
					BatchSize: 100,
				},
			},
			expectError: false,
		},
		{
			name: "nested directory path",
			config: config.StorageConfig{
				DatabasePath: filepath.Join(tempDir, "nested", "dir", "test.db"),
				Retention: config.RetentionConfig{
					Raw:    time.Hour,
					Minute: 24 * time.Hour,
					Hour:   7 * 24 * time.Hour,
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage, err := NewSQLiteStorage(tt.config, logger)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if tt.expectedErrMsg != "" && err.Error() != tt.expectedErrMsg {
					t.Errorf("Expected error message '%s', got '%s'", tt.expectedErrMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if storage == nil {
				t.Error("Expected non-nil storage")
				return
			}

			// Verify database file was created
			if _, err := os.Stat(tt.config.DatabasePath); os.IsNotExist(err) {
				t.Error("Expected database file to be created")
			}

			// Clean up
			storage.DB().Close()
		})
	}
}

func TestSQLiteStorageStartStop(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "sqlite_storage_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := config.StorageConfig{
		DatabasePath: filepath.Join(tempDir, "test.db"),
		Retention: config.RetentionConfig{
			Raw:    time.Hour,
			Minute: 24 * time.Hour,
			Hour:   7 * 24 * time.Hour,
		},
		Aggregation: config.AggregationConfig{
			Enabled:   true,
			Interval:  100 * time.Millisecond, // Fast for testing
			BatchSize: 100,
		},
	}

	storage, err := NewSQLiteStorage(config, logger)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.DB().Close()

	ctx := context.Background()

	// Test start
	err = storage.Start(ctx)
	if err != nil {
		t.Errorf("Failed to start storage: %v", err)
	}

	if !storage.running {
		t.Error("Expected storage to be running")
	}

	// Test double start
	err = storage.Start(ctx)
	if err == nil {
		t.Error("Expected error when starting already running storage")
	}

	// Test stop
	err = storage.Stop(ctx)
	if err != nil {
		t.Errorf("Failed to stop storage: %v", err)
	}

	if storage.running {
		t.Error("Expected storage to not be running")
	}

	// Test double stop
	err = storage.Stop(ctx)
	if err != nil {
		t.Errorf("Unexpected error stopping already stopped storage: %v", err)
	}
}

func TestSQLiteStorageStore(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "sqlite_storage_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := config.StorageConfig{
		DatabasePath: filepath.Join(tempDir, "test.db"),
		Retention: config.RetentionConfig{
			Raw:    time.Hour,
			Minute: 24 * time.Hour,
			Hour:   7 * 24 * time.Hour,
		},
	}

	storage, err := NewSQLiteStorage(config, logger)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.DB().Close()

	ctx := context.Background()
	err = storage.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start storage: %v", err)
	}
	defer storage.Stop(ctx)

	// Test storing empty metrics set
	emptyMetrics := types.MetricSet{
		Timestamp: time.Now(),
		Metrics:   []types.Metric{},
	}

	err = storage.Store(ctx, emptyMetrics)
	if err != nil {
		t.Errorf("Unexpected error storing empty metrics: %v", err)
	}

	// Test storing metrics
	now := time.Now()
	metricSet := types.MetricSet{
		Timestamp: now,
		Metrics: []types.Metric{
			{
				Name:      "test_metric_1",
				Value:     42.0,
				Type:      types.MetricTypeGauge,
				Labels:    map[string]string{"pool": "test-pool"},
				Timestamp: now,
			},
			{
				Name:      "test_metric_2",
				Value:     100.0,
				Type:      types.MetricTypeCounter,
				Labels:    map[string]string{"pool": "test-pool", "worker": "1"},
				Timestamp: now,
			},
		},
	}

	err = storage.Store(ctx, metricSet)
	if err != nil {
		t.Errorf("Failed to store metrics: %v", err)
	}

	// Verify metrics were stored
	var count int
	err = storage.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM metrics").Scan(&count)
	if err != nil {
		t.Errorf("Failed to query stored metrics: %v", err)
	}

	if count != 2 {
		t.Errorf("Expected 2 stored metrics, got %d", count)
	}

	// Verify stored data
	rows, err := storage.DB().QueryContext(ctx, `
		SELECT metric_name, value, metric_type, labels, aggregation_level
		FROM metrics ORDER BY metric_name
	`)
	if err != nil {
		t.Errorf("Failed to query metrics: %v", err)
	}
	defer rows.Close()

	expectedMetrics := []struct {
		name       string
		value      float64
		metricType string
		labels     map[string]string
		aggLevel   string
	}{
		{"test_metric_1", 42.0, "gauge", map[string]string{"pool": "test-pool"}, "raw"},
		{"test_metric_2", 100.0, "counter", map[string]string{"pool": "test-pool", "worker": "1"}, "raw"},
	}

	i := 0
	for rows.Next() {
		var name, metricType, labelsJSON, aggLevel string
		var value float64

		err = rows.Scan(&name, &value, &metricType, &labelsJSON, &aggLevel)
		if err != nil {
			t.Errorf("Failed to scan row: %v", err)
			continue
		}

		if i >= len(expectedMetrics) {
			t.Errorf("Got more metrics than expected")
			break
		}

		expected := expectedMetrics[i]
		if name != expected.name {
			t.Errorf("Expected metric name '%s', got '%s'", expected.name, name)
		}
		if value != expected.value {
			t.Errorf("Expected value %f, got %f", expected.value, value)
		}
		if metricType != expected.metricType {
			t.Errorf("Expected type '%s', got '%s'", expected.metricType, metricType)
		}
		if aggLevel != expected.aggLevel {
			t.Errorf("Expected aggregation level '%s', got '%s'", expected.aggLevel, aggLevel)
		}

		// Parse and verify labels
		var labels map[string]string
		err = json.Unmarshal([]byte(labelsJSON), &labels)
		if err != nil {
			t.Errorf("Failed to unmarshal labels: %v", err)
		} else {
			for key, expectedValue := range expected.labels {
				if actualValue, exists := labels[key]; !exists {
					t.Errorf("Expected label '%s' not found", key)
				} else if actualValue != expectedValue {
					t.Errorf("Expected label '%s'='%s', got '%s'", key, expectedValue, actualValue)
				}
			}
		}

		i++
	}

	if i != len(expectedMetrics) {
		t.Errorf("Expected %d metrics, processed %d", len(expectedMetrics), i)
	}
}

func TestSQLiteStorageStoreNotRunning(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "sqlite_storage_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := config.StorageConfig{
		DatabasePath: filepath.Join(tempDir, "test.db"),
		Retention: config.RetentionConfig{
			Raw:    time.Hour,
			Minute: 24 * time.Hour,
			Hour:   7 * 24 * time.Hour,
		},
	}

	storage, err := NewSQLiteStorage(config, logger)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.DB().Close()

	ctx := context.Background()

	// Try to store without starting storage
	metricSet := types.MetricSet{
		Timestamp: time.Now(),
		Metrics: []types.Metric{
			{
				Name:      "test_metric",
				Value:     42.0,
				Type:      types.MetricTypeGauge,
				Labels:    map[string]string{},
				Timestamp: time.Now(),
			},
		},
	}

	err = storage.Store(ctx, metricSet)
	if err == nil {
		t.Error("Expected error when storing to non-running storage")
	}
}

func TestSQLiteStorageQuery(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "sqlite_storage_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := config.StorageConfig{
		DatabasePath: filepath.Join(tempDir, "test.db"),
		Retention: config.RetentionConfig{
			Raw:    time.Hour,
			Minute: 24 * time.Hour,
			Hour:   7 * 24 * time.Hour,
		},
	}

	storage, err := NewSQLiteStorage(config, logger)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.DB().Close()

	ctx := context.Background()
	err = storage.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start storage: %v", err)
	}
	defer storage.Stop(ctx)

	// Store test data
	baseTime := time.Now().Truncate(time.Hour)
	for i := 0; i < 10; i++ {
		timestamp := baseTime.Add(time.Duration(i) * time.Minute)
		metricSet := types.MetricSet{
			Timestamp: timestamp,
			Metrics: []types.Metric{
				{
					Name:      "cpu_usage",
					Value:     float64(10 + i*5), // 10, 15, 20, ... 55
					Type:      types.MetricTypeGauge,
					Labels:    map[string]string{"pool": "web"},
					Timestamp: timestamp,
				},
				{
					Name:      "memory_usage",
					Value:     float64(100 + i*10), // 100, 110, 120, ... 190
					Type:      types.MetricTypeGauge,
					Labels:    map[string]string{"pool": "web"},
					Timestamp: timestamp,
				},
				{
					Name:      "cpu_usage",
					Value:     float64(20 + i*3), // Different pool
					Type:      types.MetricTypeGauge,
					Labels:    map[string]string{"pool": "api"},
					Timestamp: timestamp,
				},
			},
		}

		err = storage.Store(ctx, metricSet)
		if err != nil {
			t.Fatalf("Failed to store test data: %v", err)
		}
	}

	tests := []struct {
		name            string
		query           types.Query
		expectedResults int
		expectedMin     float64
		expectedMax     float64
	}{
		{
			name: "simple metric query",
			query: types.Query{
				MetricName:  "cpu_usage",
				StartTime:   baseTime,
				EndTime:     baseTime.Add(time.Hour),
				Labels:      map[string]string{"pool": "web"},
				Aggregation: "",
			},
			expectedResults: 10,
			expectedMin:     10.0,
			expectedMax:     55.0,
		},
		{
			name: "aggregated query - avg",
			query: types.Query{
				MetricName:  "memory_usage",
				StartTime:   baseTime,
				EndTime:     baseTime.Add(time.Hour),
				Labels:      map[string]string{"pool": "web"},
				Aggregation: "avg",
				Interval:    time.Hour,
			},
			expectedResults: 1,
			expectedMin:     145.0, // Average of 100, 110, 120, ... 190
			expectedMax:     145.0,
		},
		{
			name: "different pool query",
			query: types.Query{
				MetricName:  "cpu_usage",
				StartTime:   baseTime,
				EndTime:     baseTime.Add(time.Hour),
				Labels:      map[string]string{"pool": "api"},
				Aggregation: "",
			},
			expectedResults: 10,
			expectedMin:     20.0,
			expectedMax:     47.0, // 20 + 9*3
		},
		{
			name: "time range query",
			query: types.Query{
				MetricName:  "cpu_usage",
				StartTime:   baseTime.Add(2 * time.Minute),
				EndTime:     baseTime.Add(5 * time.Minute),
				Labels:      map[string]string{"pool": "web"},
				Aggregation: "",
			},
			expectedResults: 4, // Minutes 2, 3, 4, 5
			expectedMin:     20.0,
			expectedMax:     35.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := storage.Query(ctx, tt.query)
			if err != nil {
				t.Errorf("Query failed: %v", err)
				return
			}

			if result == nil {
				t.Error("Expected non-nil result")
				return
			}

			if result.MetricName != tt.query.MetricName {
				t.Errorf("Expected metric name '%s', got '%s'", tt.query.MetricName, result.MetricName)
			}

			if len(result.Values) != tt.expectedResults {
				t.Errorf("Expected %d results, got %d", tt.expectedResults, len(result.Values))
			}

			if len(result.Values) > 0 {
				// Find min and max values
				min := result.Values[0].Value
				max := result.Values[0].Value
				for _, dp := range result.Values {
					if dp.Value < min {
						min = dp.Value
					}
					if dp.Value > max {
						max = dp.Value
					}
				}

				if min != tt.expectedMin {
					t.Errorf("Expected min value %f, got %f", tt.expectedMin, min)
				}
				if max != tt.expectedMax {
					t.Errorf("Expected max value %f, got %f", tt.expectedMax, max)
				}
			}
		})
	}
}

func TestSQLiteStorageQueryNotRunning(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "sqlite_storage_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := config.StorageConfig{
		DatabasePath: filepath.Join(tempDir, "test.db"),
		Retention: config.RetentionConfig{
			Raw:    time.Hour,
			Minute: 24 * time.Hour,
			Hour:   7 * 24 * time.Hour,
		},
	}

	storage, err := NewSQLiteStorage(config, logger)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.DB().Close()

	ctx := context.Background()

	// Try to query without starting storage
	query := types.Query{
		MetricName: "test_metric",
		StartTime:  time.Now().Add(-time.Hour),
		EndTime:    time.Now(),
	}

	_, err = storage.Query(ctx, query)
	if err == nil {
		t.Error("Expected error when querying non-running storage")
	}
}

func TestSQLiteStorageCleanup(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "sqlite_storage_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := config.StorageConfig{
		DatabasePath: filepath.Join(tempDir, "test.db"),
		Retention: config.RetentionConfig{
			Raw:    time.Minute, // Very short for testing
			Minute: 10 * time.Minute,
			Hour:   time.Hour,
		},
	}

	storage, err := NewSQLiteStorage(config, logger)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.DB().Close()

	ctx := context.Background()
	err = storage.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start storage: %v", err)
	}
	defer storage.Stop(ctx)

	// Store old data (beyond retention period)
	oldTime := time.Now().Add(-2 * time.Minute)     // Older than retention
	recentTime := time.Now().Add(-30 * time.Second) // Within retention

	// Insert old data
	_, err = storage.DB().ExecContext(ctx, `
		INSERT INTO metrics (timestamp, metric_name, value, metric_type, labels, aggregation_level)
		VALUES (?, 'old_metric', 42.0, 'gauge', '{}', 'raw')
	`, oldTime.Unix())
	if err != nil {
		t.Fatalf("Failed to insert old data: %v", err)
	}

	// Insert old minute data
	_, err = storage.DB().ExecContext(ctx, `
		INSERT INTO metrics_minute (timestamp, metric_name, value_avg, value_min, value_max, value_sum, sample_count, labels)
		VALUES (?, 'old_minute', 42.0, 42.0, 42.0, 42.0, 1, '{}')
	`, oldTime.Add(-15*time.Minute).Unix())
	if err != nil {
		t.Fatalf("Failed to insert old minute data: %v", err)
	}

	// Insert old hour data
	_, err = storage.DB().ExecContext(ctx, `
		INSERT INTO metrics_hour (timestamp, metric_name, value_avg, value_min, value_max, value_sum, sample_count, labels)
		VALUES (?, 'old_hour', 42.0, 42.0, 42.0, 42.0, 1, '{}')
	`, oldTime.Add(-2*time.Hour).Unix())
	if err != nil {
		t.Fatalf("Failed to insert old hour data: %v", err)
	}

	// Insert recent data
	_, err = storage.DB().ExecContext(ctx, `
		INSERT INTO metrics (timestamp, metric_name, value, metric_type, labels, aggregation_level)
		VALUES (?, 'recent_metric', 84.0, 'gauge', '{}', 'raw')
	`, recentTime.Unix())
	if err != nil {
		t.Fatalf("Failed to insert recent data: %v", err)
	}

	// Count before cleanup
	var beforeCount int
	err = storage.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM metrics").Scan(&beforeCount)
	if err != nil {
		t.Fatalf("Failed to count metrics before cleanup: %v", err)
	}

	if beforeCount != 2 {
		t.Errorf("Expected 2 metrics before cleanup, got %d", beforeCount)
	}

	// Run cleanup
	err = storage.Cleanup(ctx)
	if err != nil {
		t.Errorf("Cleanup failed: %v", err)
	}

	// Count after cleanup
	var afterCount int
	err = storage.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM metrics").Scan(&afterCount)
	if err != nil {
		t.Fatalf("Failed to count metrics after cleanup: %v", err)
	}

	if afterCount != 1 {
		t.Errorf("Expected 1 metric after cleanup (recent only), got %d", afterCount)
	}

	// Verify recent data is still there
	var metricName string
	err = storage.DB().QueryRowContext(ctx, "SELECT metric_name FROM metrics").Scan(&metricName)
	if err != nil {
		t.Errorf("Failed to get remaining metric: %v", err)
	}

	if metricName != "recent_metric" {
		t.Errorf("Expected 'recent_metric' to remain, got '%s'", metricName)
	}
}

func TestSQLiteStorageCleanupNotRunning(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "sqlite_storage_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := config.StorageConfig{
		DatabasePath: filepath.Join(tempDir, "test.db"),
		Retention: config.RetentionConfig{
			Raw:    time.Hour,
			Minute: 24 * time.Hour,
			Hour:   7 * 24 * time.Hour,
		},
	}

	storage, err := NewSQLiteStorage(config, logger)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.DB().Close()

	ctx := context.Background()

	// Try to cleanup without starting storage
	err = storage.Cleanup(ctx)
	if err == nil {
		t.Error("Expected error when cleaning up non-running storage")
	}
}

func TestBuildQuery(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "sqlite_storage_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := config.StorageConfig{
		DatabasePath: filepath.Join(tempDir, "test.db"),
		Retention: config.RetentionConfig{
			Raw:    time.Hour,
			Minute: 24 * time.Hour,
			Hour:   7 * 24 * time.Hour,
		},
	}

	storage, err := NewSQLiteStorage(config, logger)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.DB().Close()

	tests := []struct {
		name         string
		query        types.Query
		expectedSQL  string
		expectedArgs int
	}{
		{
			name: "simple query",
			query: types.Query{
				MetricName: "cpu_usage",
			},
			expectedSQL:  "SELECT timestamp, value FROM metrics WHERE metric_name = ? ORDER BY timestamp ASC",
			expectedArgs: 1,
		},
		{
			name: "query with time range",
			query: types.Query{
				MetricName: "cpu_usage",
				StartTime:  time.Unix(1000, 0),
				EndTime:    time.Unix(2000, 0),
			},
			expectedSQL:  "SELECT timestamp, value FROM metrics WHERE metric_name = ? AND timestamp >= ? AND timestamp <= ? ORDER BY timestamp ASC",
			expectedArgs: 3,
		},
		{
			name: "query with labels",
			query: types.Query{
				MetricName: "cpu_usage",
				Labels:     map[string]string{"pool": "web", "worker": "1"},
			},
			expectedSQL:  "SELECT timestamp, value FROM metrics WHERE metric_name = ? AND JSON_EXTRACT(labels, '$.pool') = ? AND JSON_EXTRACT(labels, '$.worker') = ? ORDER BY timestamp ASC",
			expectedArgs: 3,
		},
		{
			name: "aggregated query",
			query: types.Query{
				MetricName:  "cpu_usage",
				Aggregation: "avg",
			},
			expectedSQL:  "SELECT timestamp, AVG(value) as value FROM metrics WHERE metric_name = ? ORDER BY timestamp ASC",
			expectedArgs: 1,
		},
		{
			name: "query with interval grouping",
			query: types.Query{
				MetricName: "cpu_usage",
				Interval:   time.Minute,
			},
			expectedSQL:  "SELECT timestamp, value FROM metrics WHERE metric_name = ? GROUP BY (timestamp / ?) * ? ORDER BY timestamp ASC",
			expectedArgs: 3,
		},
		{
			name: "complex query",
			query: types.Query{
				MetricName:  "memory_usage",
				StartTime:   time.Unix(1000, 0),
				EndTime:     time.Unix(2000, 0),
				Labels:      map[string]string{"pool": "api"},
				Aggregation: "max",
				Interval:    5 * time.Minute,
			},
			expectedSQL:  "SELECT timestamp, MAX(value) as value FROM metrics WHERE metric_name = ? AND timestamp >= ? AND timestamp <= ? AND JSON_EXTRACT(labels, '$.pool') = ? GROUP BY (timestamp / ?) * ? ORDER BY timestamp ASC",
			expectedArgs: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := storage.buildQuery(tt.query)
			if err != nil {
				t.Errorf("buildQuery failed: %v", err)
				return
			}

			if sql != tt.expectedSQL {
				t.Errorf("Expected SQL:\n%s\nGot:\n%s", tt.expectedSQL, sql)
			}

			if len(args) != tt.expectedArgs {
				t.Errorf("Expected %d args, got %d", tt.expectedArgs, len(args))
			}

			// Verify first arg is always the metric name
			if len(args) > 0 {
				if metricName, ok := args[0].(string); !ok || metricName != tt.query.MetricName {
					t.Errorf("Expected first arg to be metric name '%s', got %v", tt.query.MetricName, args[0])
				}
			}
		})
	}
}

func TestAggregateMinuteData(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "sqlite_storage_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := config.StorageConfig{
		DatabasePath: filepath.Join(tempDir, "test.db"),
		Retention: config.RetentionConfig{
			Raw:    time.Hour,
			Minute: 24 * time.Hour,
			Hour:   7 * 24 * time.Hour,
		},
	}

	storage, err := NewSQLiteStorage(config, logger)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.DB().Close()

	ctx := context.Background()

	// Insert test data spanning multiple minutes
	baseTime := time.Now().Truncate(time.Hour)

	// Insert data for first minute
	for i := 0; i < 5; i++ {
		timestamp := baseTime.Add(time.Duration(i) * 10 * time.Second) // 0s, 10s, 20s, 30s, 40s
		_, err = storage.DB().ExecContext(ctx, `
			INSERT INTO metrics (timestamp, metric_name, value, metric_type, labels, aggregation_level)
			VALUES (?, 'test_metric', ?, 'gauge', '{"pool":"web"}', 'raw')
		`, timestamp.Unix(), float64(10+i)) // Values: 10, 11, 12, 13, 14
	}
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	// Insert data for second minute
	for i := 0; i < 3; i++ {
		timestamp := baseTime.Add(time.Minute).Add(time.Duration(i) * 15 * time.Second) // 1:00, 1:15, 1:30
		_, err = storage.DB().ExecContext(ctx, `
			INSERT INTO metrics (timestamp, metric_name, value, metric_type, labels, aggregation_level)
			VALUES (?, 'test_metric', ?, 'gauge', '{"pool":"web"}', 'raw')
		`, timestamp.Unix(), float64(20+i)) // Values: 20, 21, 22
	}
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	// Run minute aggregation
	err = storage.aggregateMinuteData(ctx)
	if err != nil {
		t.Errorf("aggregateMinuteData failed: %v", err)
	}

	// Verify aggregated data
	rows, err := storage.DB().QueryContext(ctx, `
		SELECT timestamp, metric_name, value_avg, value_min, value_max, value_sum, sample_count, labels
		FROM metrics_minute ORDER BY timestamp
	`)
	if err != nil {
		t.Fatalf("Failed to query minute aggregates: %v", err)
	}
	defer rows.Close()

	expectedAggregates := []struct {
		timestamp   int64
		metricName  string
		valueAvg    float64
		valueMin    float64
		valueMax    float64
		valueSum    float64
		sampleCount int64
		labels      string
	}{
		{
			timestamp:   baseTime.Truncate(time.Minute).Unix(),
			metricName:  "test_metric",
			valueAvg:    12.0, // (10+11+12+13+14)/5
			valueMin:    10.0,
			valueMax:    14.0,
			valueSum:    60.0,
			sampleCount: 5,
			labels:      `{"pool":"web"}`,
		},
		{
			timestamp:   baseTime.Add(time.Minute).Truncate(time.Minute).Unix(),
			metricName:  "test_metric",
			valueAvg:    21.0, // (20+21+22)/3
			valueMin:    20.0,
			valueMax:    22.0,
			valueSum:    63.0,
			sampleCount: 3,
			labels:      `{"pool":"web"}`,
		},
	}

	i := 0
	for rows.Next() {
		var timestamp int64
		var metricName, labels string
		var valueAvg, valueMin, valueMax, valueSum float64
		var sampleCount int64

		err = rows.Scan(&timestamp, &metricName, &valueAvg, &valueMin, &valueMax, &valueSum, &sampleCount, &labels)
		if err != nil {
			t.Errorf("Failed to scan aggregate row: %v", err)
			continue
		}

		if i >= len(expectedAggregates) {
			t.Errorf("Got more aggregates than expected")
			break
		}

		expected := expectedAggregates[i]
		if timestamp != expected.timestamp {
			t.Errorf("Expected timestamp %d, got %d", expected.timestamp, timestamp)
		}
		if metricName != expected.metricName {
			t.Errorf("Expected metric name '%s', got '%s'", expected.metricName, metricName)
		}
		if valueAvg != expected.valueAvg {
			t.Errorf("Expected avg %f, got %f", expected.valueAvg, valueAvg)
		}
		if valueMin != expected.valueMin {
			t.Errorf("Expected min %f, got %f", expected.valueMin, valueMin)
		}
		if valueMax != expected.valueMax {
			t.Errorf("Expected max %f, got %f", expected.valueMax, valueMax)
		}
		if valueSum != expected.valueSum {
			t.Errorf("Expected sum %f, got %f", expected.valueSum, valueSum)
		}
		if sampleCount != expected.sampleCount {
			t.Errorf("Expected sample count %d, got %d", expected.sampleCount, sampleCount)
		}

		i++
	}

	if i != len(expectedAggregates) {
		t.Errorf("Expected %d aggregates, processed %d", len(expectedAggregates), i)
	}
}

func TestAggregateHourData(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "sqlite_storage_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := config.StorageConfig{
		DatabasePath: filepath.Join(tempDir, "test.db"),
		Retention: config.RetentionConfig{
			Raw:    time.Hour,
			Minute: 24 * time.Hour,
			Hour:   7 * 24 * time.Hour,
		},
	}

	storage, err := NewSQLiteStorage(config, logger)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.DB().Close()

	ctx := context.Background()

	// Insert test minute aggregates for a completed hour (1 hour ago)
	baseTime := time.Now().Add(-2 * time.Hour).Truncate(time.Hour)

	// Insert minute aggregates for that completed hour
	for i := 0; i < 3; i++ {
		timestamp := baseTime.Add(time.Duration(i) * 20 * time.Minute) // 0:00, 0:20, 0:40
		_, err = storage.DB().ExecContext(ctx, `
			INSERT INTO metrics_minute (timestamp, metric_name, value_avg, value_min, value_max, value_sum, sample_count, labels)
			VALUES (?, 'test_metric', ?, ?, ?, ?, ?, '{"pool":"web"}')
		`, timestamp.Unix(), float64(10+i), float64(5+i), float64(15+i), float64(50+i*10), int64(5))
		// Avg: 10, 11, 12; Min: 5, 6, 7; Max: 15, 16, 17; Sum: 50, 60, 70; Count: 5 each
	}
	if err != nil {
		t.Fatalf("Failed to insert minute aggregates: %v", err)
	}

	// Run hour aggregation
	err = storage.aggregateHourData(ctx)
	if err != nil {
		t.Errorf("aggregateHourData failed: %v", err)
	}

	// Verify aggregated data
	rows, err := storage.DB().QueryContext(ctx, `
		SELECT timestamp, metric_name, value_avg, value_min, value_max, value_sum, sample_count, labels
		FROM metrics_hour ORDER BY timestamp
	`)
	if err != nil {
		t.Fatalf("Failed to query hour aggregates: %v", err)
	}
	defer rows.Close()

	expectedAggregates := []struct {
		timestamp   int64
		metricName  string
		valueAvg    float64
		valueMin    float64
		valueMax    float64
		valueSum    float64
		sampleCount int64
		labels      string
	}{
		{
			timestamp:   baseTime.Truncate(time.Hour).Unix(),
			metricName:  "test_metric",
			valueAvg:    11.0,  // (10+11+12)/3
			valueMin:    5.0,   // min of (5,6,7)
			valueMax:    17.0,  // max of (15,16,17)
			valueSum:    180.0, // 50+60+70
			sampleCount: 15,    // 5+5+5
			labels:      `{"pool":"web"}`,
		},
	}

	i := 0
	for rows.Next() {
		var timestamp int64
		var metricName, labels string
		var valueAvg, valueMin, valueMax, valueSum float64
		var sampleCount int64

		err = rows.Scan(&timestamp, &metricName, &valueAvg, &valueMin, &valueMax, &valueSum, &sampleCount, &labels)
		if err != nil {
			t.Errorf("Failed to scan hour aggregate row: %v", err)
			continue
		}

		if i >= len(expectedAggregates) {
			t.Errorf("Got more hour aggregates than expected")
			break
		}

		expected := expectedAggregates[i]
		if timestamp != expected.timestamp {
			t.Errorf("Expected timestamp %d, got %d", expected.timestamp, timestamp)
		}
		if metricName != expected.metricName {
			t.Errorf("Expected metric name '%s', got '%s'", expected.metricName, metricName)
		}
		if valueAvg != expected.valueAvg {
			t.Errorf("Expected avg %f, got %f", expected.valueAvg, valueAvg)
		}
		if valueMin != expected.valueMin {
			t.Errorf("Expected min %f, got %f", expected.valueMin, valueMin)
		}
		if valueMax != expected.valueMax {
			t.Errorf("Expected max %f, got %f", expected.valueMax, valueMax)
		}
		if valueSum != expected.valueSum {
			t.Errorf("Expected sum %f, got %f", expected.valueSum, valueSum)
		}
		if sampleCount != expected.sampleCount {
			t.Errorf("Expected sample count %d, got %d", expected.sampleCount, sampleCount)
		}

		i++
	}

	if i != len(expectedAggregates) {
		t.Errorf("Expected %d hour aggregates, processed %d", len(expectedAggregates), i)
	}
}
