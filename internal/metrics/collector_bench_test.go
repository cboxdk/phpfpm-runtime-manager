package metrics

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/config"
	"github.com/cboxdk/phpfpm-runtime-manager/internal/types"
	"go.uber.org/zap"
)

// BenchmarkMetricsCollection benchmarks the metrics collection performance
func BenchmarkMetricsCollection(b *testing.B) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	// Create test pools configuration
	pools := []config.PoolConfig{
		{
			Name:              "test-pool-1",
			FastCGIStatusPath: "http://localhost:9001/status",
			MaxWorkers:        20,
		},
		{
			Name:              "test-pool-2",
			FastCGIStatusPath: "http://localhost:9002/status",
			MaxWorkers:        30,
		},
	}

	monitoringConfig := config.MonitoringConfig{
		CollectInterval:     time.Second,
		CPUThreshold:        80.0,
		MemoryThreshold:     85.0,
		EnableOpcache:       true,
		EnableSystemMetrics: true,
	}

	collector, err := NewCollector(monitoringConfig, pools, logger)
	if err != nil {
		b.Fatalf("Failed to create collector: %v", err)
	}

	testCases := []struct {
		name         string
		metricsCount int
		poolCount    int
		concurrency  int
	}{
		{"SmallLoad", 50, 2, 5},
		{"MediumLoad", 200, 5, 10},
		{"HighLoad", 500, 10, 20},
		{"ExtremeLoad", 1000, 20, 50},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			benchmarkCollection(b, collector, tc.metricsCount, tc.concurrency)
		})
	}
}

func benchmarkCollection(b *testing.B, collector *Collector, metricsCount, concurrency int) {
	b.ResetTimer()
	b.ReportAllocs()

	// Use channels to coordinate goroutines
	start := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // Wait for signal to start

			for j := 0; j < b.N/concurrency; j++ {
				metricSet := generateBenchmarkMetrics(metricsCount)
				// Simulate metrics processing
				_ = metricSet
			}
		}()
	}

	close(start) // Signal all goroutines to start
	wg.Wait()

	dropCount, totalProcessed, dropRate := collector.GetBackpressureStats()
	b.ReportMetric(float64(dropCount), "dropped_metrics")
	b.ReportMetric(float64(totalProcessed), "total_processed")
	b.ReportMetric(dropRate, "drop_rate_percent")
}

// BenchmarkBatchProcessing benchmarks the batch processing functionality
func BenchmarkBatchProcessing(b *testing.B) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	batchSizes := []int{10, 25, 50, 100, 200}
	flushTimeouts := []time.Duration{
		1 * time.Second,
		5 * time.Second,
		10 * time.Second,
	}

	for _, batchSize := range batchSizes {
		for _, timeout := range flushTimeouts {
			name := fmt.Sprintf("BatchSize_%d_Timeout_%dms", batchSize, timeout.Milliseconds())
			b.Run(name, func(b *testing.B) {
				benchmarkBatchProcessor(b, batchSize, timeout, logger)
			})
		}
	}
}

func benchmarkBatchProcessor(b *testing.B, batchSize int, flushTimeout time.Duration, logger *zap.Logger) {
	processedCount := int64(0)
	var mu sync.Mutex

	processor := func(metrics []types.MetricSet) error {
		mu.Lock()
		processedCount += int64(len(metrics))
		mu.Unlock()
		return nil
	}

	batchProcessor := NewBatchProcessor(batchSize, flushTimeout, processor, logger)

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			metricSet := generateBenchmarkMetrics(10) // 10 metrics per set
			batchProcessor.AddMetrics("test", metricSet)
		}
	})

	// Give time for final flushes
	time.Sleep(flushTimeout + 100*time.Millisecond)

	mu.Lock()
	finalCount := processedCount
	mu.Unlock()

	b.ReportMetric(float64(finalCount), "processed_metrics")
	b.ReportMetric(float64(finalCount)/float64(b.N), "processing_efficiency")
}

// BenchmarkObjectPooling benchmarks the object pooling performance
func BenchmarkObjectPooling(b *testing.B) {
	b.Run("WithoutPools", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			// Simulate allocation without pools
			metricSet := &types.MetricSet{
				Metrics: make([]types.Metric, 20),
				Labels:  make(map[string]string, 4),
			}
			for j := range metricSet.Metrics {
				metricSet.Metrics[j] = types.Metric{
					Labels: make(map[string]string, 4),
				}
			}
			// Simulate clearing (what would happen in cleanup)
			for j := range metricSet.Metrics {
				for k := range metricSet.Metrics[j].Labels {
					delete(metricSet.Metrics[j].Labels, k)
				}
			}
		}
	})

	b.Run("WithPools", func(b *testing.B) {
		// Set up pools like in the collector
		var metricSetPool sync.Pool
		var metricPool sync.Pool
		var labelPool sync.Pool

		metricSetPool.New = func() interface{} {
			return &types.MetricSet{
				Metrics: make([]types.Metric, 0, 20),
				Labels:  make(map[string]string, 4),
			}
		}

		metricPool.New = func() interface{} {
			return &types.Metric{
				Labels: make(map[string]string, 4),
			}
		}

		labelPool.New = func() interface{} {
			return make(map[string]string, 4)
		}

		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			// Get from pool
			metricSet := metricSetPool.Get().(*types.MetricSet)
			metricSet.Metrics = metricSet.Metrics[:0]

			// Simulate usage
			for j := 0; j < 20; j++ {
				metric := metricPool.Get().(*types.Metric)
				labels := labelPool.Get().(map[string]string)

				// Use the objects
				metric.Labels = labels
				metricSet.Metrics = append(metricSet.Metrics, *metric)

				// Clean and return to pools
				for k := range labels {
					delete(labels, k)
				}
				labelPool.Put(labels)
				*metric = types.Metric{}
				metricPool.Put(metric)
			}

			// Clean and return metricSet
			for k := range metricSet.Labels {
				delete(metricSet.Labels, k)
			}
			metricSetPool.Put(metricSet)
		}
	})
}

// BenchmarkChannelBuffering benchmarks different channel buffer sizes
func BenchmarkChannelBuffering(b *testing.B) {
	bufferSizes := []int{100, 500, 1000, 2000, 5000}

	for _, bufferSize := range bufferSizes {
		b.Run(fmt.Sprintf("BufferSize_%d", bufferSize), func(b *testing.B) {
			benchmarkChannelBuffer(b, bufferSize)
		})
	}
}

func benchmarkChannelBuffer(b *testing.B, bufferSize int) {
	ch := make(chan types.MetricSet, bufferSize)

	// Consumer goroutine
	go func() {
		for range ch {
			// Consume metrics
		}
	}()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			metricSet := generateBenchmarkMetrics(10)
			select {
			case ch <- metricSet:
				// Successfully sent
			default:
				// Channel full - in real implementation this would trigger backpressure
			}
		}
	})

	close(ch)
}

// BenchmarkBackpressureHandling benchmarks backpressure handling performance
func BenchmarkBackpressureHandling(b *testing.B) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	pools := []config.PoolConfig{
		{Name: "test-pool", FastCGIStatusPath: "http://localhost:9000/status", MaxWorkers: 10},
	}

	monitoringConfig := config.MonitoringConfig{
		CollectInterval: 100 * time.Millisecond, // High frequency to trigger backpressure
	}

	collector, err := NewCollector(monitoringConfig, pools, logger)
	if err != nil {
		b.Fatalf("Failed to create collector: %v", err)
	}

	// Fill the channel to near capacity to trigger backpressure
	for i := 0; i < collector.backpressureThreshold; i++ {
		select {
		case collector.metrics <- generateBenchmarkMetrics(10):
		default:
			break
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			metricSet := generateBenchmarkMetrics(10)

			// Simulate the backpressure logic
			select {
			case collector.metrics <- metricSet:
				// Successfully sent
			default:
				// Simulate dropping metrics (backpressure applied)
				collector.returnMetricSetToPool(&metricSet)
			}
		}
	})

	dropCount, totalProcessed, dropRate := collector.GetBackpressureStats()
	b.ReportMetric(float64(dropCount), "dropped_metrics")
	b.ReportMetric(float64(totalProcessed), "total_processed")
	b.ReportMetric(dropRate, "drop_rate_percent")
}

// BenchmarkConcurrentCollection benchmarks concurrent metrics collection
func BenchmarkConcurrentCollection(b *testing.B) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	concurrencyLevels := []int{1, 5, 10, 20, 50}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("Concurrency_%d", concurrency), func(b *testing.B) {
			benchmarkConcurrentCollection(b, concurrency, logger)
		})
	}
}

func benchmarkConcurrentCollection(b *testing.B, concurrency int, logger *zap.Logger) {
	pools := make([]config.PoolConfig, concurrency)
	for i := 0; i < concurrency; i++ {
		pools[i] = config.PoolConfig{
			Name:              fmt.Sprintf("pool-%d", i),
			FastCGIStatusPath: fmt.Sprintf("http://localhost:%d/status", 9000+i),
			MaxWorkers:        10,
		}
	}

	monitoringConfig := config.MonitoringConfig{
		CollectInterval: time.Second,
	}

	_, err := NewCollector(monitoringConfig, pools, logger)
	if err != nil {
		b.Fatalf("Failed to create collector: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < b.N/concurrency; j++ {
				metricSet := generateBenchmarkMetrics(20)
				// Simulate collection processing
				_ = metricSet
			}
		}()
	}
	wg.Wait()
}

// Helper function to generate benchmark metrics
func generateBenchmarkMetrics(count int) types.MetricSet {
	metrics := make([]types.Metric, count)
	timestamp := time.Now()

	for i := 0; i < count; i++ {
		metrics[i] = types.Metric{
			Name:      fmt.Sprintf("benchmark_metric_%d", i%5),
			Value:     rand.Float64() * 100,
			Type:      types.MetricTypeGauge,
			Timestamp: timestamp,
			Labels: map[string]string{
				"pool":   fmt.Sprintf("pool_%d", i%3),
				"worker": fmt.Sprintf("worker_%d", i%10),
			},
		}
	}

	return types.MetricSet{
		Timestamp: timestamp,
		Metrics:   metrics,
		Labels:    map[string]string{"source": "benchmark"},
	}
}
