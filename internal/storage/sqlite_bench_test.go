package storage

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/config"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/types"
	"go.uber.org/zap"
)

// BenchmarkDatabaseConnectionPool benchmarks connection pool performance
func BenchmarkDatabaseConnectionPool(b *testing.B) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	testCases := []struct {
		name        string
		concurrency int
		batchSize   int
	}{
		{"LowConcurrency", 5, 100},
		{"MediumConcurrency", 15, 200},
		{"HighConcurrency", 25, 500},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			benchmarkConnectionPool(b, tc.concurrency, tc.batchSize, logger)
		})
	}
}

func benchmarkConnectionPool(b *testing.B, concurrency, batchSize int, logger *zap.Logger) {
	// Create temporary database
	dbPath := fmt.Sprintf("/tmp/bench_db_%d_%d.db", concurrency, time.Now().UnixNano())
	defer os.Remove(dbPath)

	config := config.StorageConfig{
		DatabasePath: dbPath,
		Retention: config.RetentionConfig{
			Raw:    24 * time.Hour,
			Minute: 7 * 24 * time.Hour,
			Hour:   365 * 24 * time.Hour,
		},
		Aggregation: config.AggregationConfig{
			Enabled:   false, // Disable for benchmarking
			Interval:  time.Minute,
			BatchSize: 1000,
		},
	}

	storage, err := NewSQLiteStorage(config, logger)
	if err != nil {
		b.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Stop(context.Background())

	if err := storage.Start(context.Background()); err != nil {
		b.Fatalf("Failed to start storage: %v", err)
	}

	// Generate test metrics
	metrics := generateTestMetrics(batchSize)

	b.ResetTimer()
	b.ReportAllocs()

	// Run benchmark with specified concurrency
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			if err := storage.Store(ctx, metrics); err != nil {
				b.Errorf("Store failed: %v", err)
			}
		}
	})

	// Report connection pool stats
	stats := storage.GetPoolStats()
	b.ReportMetric(float64(stats.ActiveConnections), "active_conns")
	b.ReportMetric(float64(stats.IdleConnections), "idle_conns")
	b.ReportMetric(float64(stats.HealthChecks), "health_checks")
	b.ReportMetric(float64(stats.FailedHealthChecks), "failed_health_checks")
}

// BenchmarkJSONMarshaling benchmarks JSON marshaling optimizations
func BenchmarkJSONMarshaling(b *testing.B) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	dbPath := fmt.Sprintf("/tmp/bench_json_%d.db", time.Now().UnixNano())
	defer os.Remove(dbPath)

	config := config.StorageConfig{
		DatabasePath: dbPath,
		Retention: config.RetentionConfig{
			Raw:    24 * time.Hour,
			Minute: 7 * 24 * time.Hour,
			Hour:   365 * 24 * time.Hour,
		},
	}

	storage, err := NewSQLiteStorage(config, logger)
	if err != nil {
		b.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Stop(context.Background())

	if err := storage.Start(context.Background()); err != nil {
		b.Fatalf("Failed to start storage: %v", err)
	}

	// Test with different numbers of unique label sets
	testCases := []struct {
		name          string
		uniqueLabels  int
		metricsPerSet int
		totalMetrics  int
	}{
		{"LowLabelDiversity", 5, 10, 100},
		{"MediumLabelDiversity", 20, 25, 500},
		{"HighLabelDiversity", 50, 50, 1000},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			metrics := generateTestMetricsWithLabels(tc.uniqueLabels, tc.metricsPerSet, tc.totalMetrics)
			ctx := context.Background()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				if err := storage.Store(ctx, metrics); err != nil {
					b.Errorf("Store failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkBatchProcessing benchmarks the batch processing optimizations
func BenchmarkBatchProcessing(b *testing.B) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	dbPath := fmt.Sprintf("/tmp/bench_batch_%d.db", time.Now().UnixNano())
	defer os.Remove(dbPath)

	config := config.StorageConfig{
		DatabasePath: dbPath,
		Retention: config.RetentionConfig{
			Raw:    24 * time.Hour,
			Minute: 7 * 24 * time.Hour,
			Hour:   365 * 24 * time.Hour,
		},
	}

	storage, err := NewSQLiteStorage(config, logger)
	if err != nil {
		b.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Stop(context.Background())

	if err := storage.Start(context.Background()); err != nil {
		b.Fatalf("Failed to start storage: %v", err)
	}

	// Test different batch sizes
	batchSizes := []int{10, 50, 100, 500, 1000}

	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("BatchSize_%d", batchSize), func(b *testing.B) {
			metrics := generateTestMetrics(batchSize)
			ctx := context.Background()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				if err := storage.Store(ctx, metrics); err != nil {
					b.Errorf("Store failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkConnectionPoolHealth benchmarks connection pool health check overhead
func BenchmarkConnectionPoolHealth(b *testing.B) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	dbPath := fmt.Sprintf("/tmp/bench_health_%d.db", time.Now().UnixNano())
	defer os.Remove(dbPath)

	pool, err := NewConnectionPool(dbPath, logger)
	if err != nil {
		b.Fatalf("Failed to create connection pool: %v", err)
	}
	defer pool.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		pool.performHealthCheck()
	}

	stats := pool.GetStats()
	b.ReportMetric(float64(stats.HealthChecks), "health_checks")
	b.ReportMetric(float64(stats.FailedHealthChecks), "failed_health_checks")
}

// Helper functions

func generateTestMetrics(count int) types.MetricSet {
	metrics := make([]types.Metric, count)
	timestamp := time.Now()

	for i := 0; i < count; i++ {
		metrics[i] = types.Metric{
			Name:      fmt.Sprintf("test_metric_%d", i%10), // Reuse metric names
			Value:     rand.Float64() * 100,
			Type:      types.MetricTypeGauge,
			Timestamp: timestamp,
			Labels: map[string]string{
				"pool":     fmt.Sprintf("pool_%d", i%5),
				"instance": fmt.Sprintf("instance_%d", i%3),
				"region":   fmt.Sprintf("region_%d", i%2),
			},
		}
	}

	return types.MetricSet{
		Timestamp: timestamp,
		Metrics:   metrics,
		Labels:    map[string]string{"benchmark": "true"},
	}
}

func generateTestMetricsWithLabels(uniqueLabels, metricsPerSet, totalMetrics int) types.MetricSet {
	metrics := make([]types.Metric, totalMetrics)
	timestamp := time.Now()

	// Generate unique label sets
	labelSets := make([]map[string]string, uniqueLabels)
	for i := 0; i < uniqueLabels; i++ {
		labelSets[i] = map[string]string{
			"pool":     fmt.Sprintf("pool_%d", i),
			"instance": fmt.Sprintf("instance_%d", i%10),
			"region":   fmt.Sprintf("region_%d", i%5),
			"env":      fmt.Sprintf("env_%d", i%3),
		}
	}

	for i := 0; i < totalMetrics; i++ {
		labelSetIndex := i % uniqueLabels
		metrics[i] = types.Metric{
			Name:      fmt.Sprintf("test_metric_%d", i%20),
			Value:     rand.Float64() * 100,
			Type:      types.MetricTypeGauge,
			Timestamp: timestamp,
			Labels:    labelSets[labelSetIndex],
		}
	}

	return types.MetricSet{
		Timestamp: timestamp,
		Metrics:   metrics,
		Labels:    map[string]string{"benchmark": "labels"},
	}
}

// BenchmarkMemoryAllocation benchmarks memory allocation patterns
func BenchmarkMemoryAllocation(b *testing.B) {
	b.Run("WithoutPools", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			// Simulate old allocation pattern
			metricSet := &types.MetricSet{
				Metrics: make([]types.Metric, 20),
				Labels:  make(map[string]string, 4),
			}
			for j := range metricSet.Metrics {
				metricSet.Metrics[j] = types.Metric{
					Labels: make(map[string]string, 4),
				}
			}
			// Simulate usage
			_ = metricSet
		}
	})

	b.Run("WithPools", func(b *testing.B) {
		// Simulate pool usage
		metricSetPool := &types.MetricSet{}
		metricPool := &types.Metric{}

		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			// Simulate pool-based allocation
			metricSet := metricSetPool
			metricSet.Metrics = make([]types.Metric, 20)
			for j := range metricSet.Metrics {
				metric := metricPool
				metric.Labels = make(map[string]string, 4)
				metricSet.Metrics[j] = *metric
			}
			// Simulate usage
			_ = metricSet
		}
	})
}
