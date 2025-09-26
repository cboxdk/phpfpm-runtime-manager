package metrics

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/types"
)

// BenchmarkStringInterning benchmarks string interning performance
func BenchmarkStringInterning(b *testing.B) {
	poolNames := []string{"web-pool", "api-pool", "worker-pool", "web-pool", "api-pool"}
	metricNames := []string{"cpu_usage", "memory_usage", "request_count", "cpu_usage", "memory_usage"}

	b.Run("WithoutInterning", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			// Simulate repeated string usage without interning
			for j := 0; j < len(poolNames); j++ {
				_ = poolNames[j%len(poolNames)]
				_ = metricNames[j%len(metricNames)]
			}
		}
	})

	b.Run("WithInterning", func(b *testing.B) {
		interner := NewStringInterner(64)
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			// Simulate repeated string usage with interning
			for j := 0; j < len(poolNames); j++ {
				_ = interner.Intern(poolNames[j%len(poolNames)])
				_ = interner.Intern(metricNames[j%len(metricNames)])
			}
		}
	})
}

// BenchmarkJSONEncoding benchmarks JSON encoding performance
func BenchmarkJSONEncoding(b *testing.B) {
	labels := map[string]string{
		"pool":   "web-pool",
		"type":   "phpfpm",
		"status": "active",
		"region": "us-west-2",
	}

	b.Run("StandardJSON", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, err := json.Marshal(labels)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("PooledJSON", func(b *testing.B) {
		pool := NewJSONBufferPool()
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			result := pool.MarshalToPool(labels)
			result.Cleanup()
		}
	})

	b.Run("CachedJSON", func(b *testing.B) {
		pool := NewJSONBufferPool()
		cache := NewLabelJSONCache(pool)
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			_ = cache.GetOrEncode(labels)
		}
	})
}

// BenchmarkMetricCreation benchmarks metric creation with optimizations
func BenchmarkMetricCreation(b *testing.B) {
	timestamp := time.Now()

	b.Run("StandardCreation", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			labels := map[string]string{
				"pool": "web-pool",
				"type": "phpfpm",
			}

			metrics := []types.Metric{
				{
					Name:      "phpfpm_active_processes",
					Value:     15.0,
					Type:      types.MetricTypeGauge,
					Labels:    labels,
					Timestamp: timestamp,
				},
				{
					Name:      "phpfpm_idle_processes",
					Value:     5.0,
					Type:      types.MetricTypeGauge,
					Labels:    labels,
					Timestamp: timestamp,
				},
			}

			// Simulate usage
			_ = metrics
		}
	})

	b.Run("OptimizedCreation", func(b *testing.B) {
		interner := NewStringInterner(64)
		labelPool := &sync.Pool{
			New: func() interface{} {
				return make(map[string]string, 4)
			},
		}

		// Pre-intern common strings
		poolLabel := interner.Intern("pool")
		typeLabel := interner.Intern("type")
		poolName := interner.Intern("web-pool")
		typeName := interner.Intern("phpfpm")
		activeProcessesName := interner.Intern("phpfpm_active_processes")
		idleProcessesName := interner.Intern("phpfpm_idle_processes")

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			labels := labelPool.Get().(map[string]string)
			labels[poolLabel] = poolName
			labels[typeLabel] = typeName

			metrics := []types.Metric{
				{
					Name:      activeProcessesName,
					Value:     15.0,
					Type:      types.MetricTypeGauge,
					Labels:    labels,
					Timestamp: timestamp,
				},
				{
					Name:      idleProcessesName,
					Value:     5.0,
					Type:      types.MetricTypeGauge,
					Labels:    labels,
					Timestamp: timestamp,
				},
			}

			// Clean up
			for k := range labels {
				delete(labels, k)
			}
			labelPool.Put(labels)

			// Simulate usage
			_ = metrics
		}
	})
}

// BenchmarkCollectorPerformance benchmarks overall collector performance
func BenchmarkCollectorPerformance(b *testing.B) {
	// This would benchmark the complete collector performance
	// comparing before and after optimizations
	b.Run("CompleteOptimizations", func(b *testing.B) {
		b.Skip("Integration benchmark - would require full collector setup")
		// This benchmark would measure:
		// - String interning impact on metric creation
		// - JSON buffer pooling impact on serialization
		// - Label map pooling impact on allocation
		// - Overall throughput improvement
	})
}

// BenchmarkStringInternConcurrency tests concurrent string interning performance
func BenchmarkStringInternConcurrency(b *testing.B) {
	interner := NewStringInterner(256)
	strings := []string{
		"pool-1", "pool-2", "pool-3", "pool-4", "pool-5",
		"cpu_usage", "memory_usage", "disk_usage", "network_in", "network_out",
	}

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_ = interner.Intern(strings[i%len(strings)])
			i++
		}
	})
}

// BenchmarkJSONPoolConcurrency tests concurrent JSON pool performance
func BenchmarkJSONPoolConcurrency(b *testing.B) {
	pool := NewJSONBufferPool()
	testData := map[string]interface{}{
		"metric": "cpu_usage",
		"value":  42.5,
		"labels": map[string]string{
			"pool": "web-pool",
			"host": "server-01",
		},
		"timestamp": time.Now().Unix(),
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			result := pool.MarshalToPool(testData)
			result.Cleanup()
		}
	})
}
