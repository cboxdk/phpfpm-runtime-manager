package autoscaler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cboxdk/phpfpm-runtime-manager/internal/metrics"
	"go.uber.org/zap"
)

// BaselineCollector integrates with the metrics collector to feed baseline learning
type BaselineCollector struct {
	scaler    *IntelligentScaler
	logger    *zap.Logger
	collector *metrics.Collector

	// State management
	mu            sync.RWMutex
	isRunning     bool
	collectTicker *time.Ticker
	stopChan      chan struct{}

	// Sample aggregation
	sampleBuffer []WorkerSample
	bufferMutex  sync.RWMutex
	maxBuffer    int

	// Integration config
	collectInterval  time.Duration
	processingWindow time.Duration

	// Statistics and error tracking
	lastCollectTime time.Time
	successfulRuns  int
	failedRuns      int
	lastError       string
	lastErrorTime   time.Time
}

// WorkerSample represents a single measurement for baseline learning
type WorkerSample struct {
	Timestamp          time.Time     `json:"timestamp"`
	PoolName           string        `json:"pool_name"`
	ActiveWorkers      int           `json:"active_workers"`
	TotalWorkers       int           `json:"total_workers"`
	ProcessingVelocity float64       `json:"processing_velocity"`
	AvgMemoryPerWorker float64       `json:"avg_memory_per_worker"`
	AvgCPUPerWorker    float64       `json:"avg_cpu_per_worker"`
	AvgRequestDuration time.Duration `json:"avg_request_duration"`
	QueueDepth         int           `json:"queue_depth"`
	RequestsPerSecond  float64       `json:"requests_per_second"`
}

// NewBaselineCollector creates a new baseline learning collector
func NewBaselineCollector(scaler *IntelligentScaler, collector *metrics.Collector, logger *zap.Logger) *BaselineCollector {
	return &BaselineCollector{
		scaler:           scaler,
		logger:           logger,
		collector:        collector,
		maxBuffer:        DefaultMaxBuffer,
		collectInterval:  10 * time.Second, // Collect samples every 10 seconds
		processingWindow: 60 * time.Second, // Process samples every 60 seconds
		sampleBuffer:     make([]WorkerSample, 0, DefaultMaxBuffer),
		stopChan:         make(chan struct{}),
	}
}

// Start begins baseline learning data collection
func (bc *BaselineCollector) Start(ctx context.Context) error {
	bc.logger.Info("Starting baseline learning collector")

	if bc.isRunning {
		return fmt.Errorf("baseline collector is already running")
	}

	bc.isRunning = true
	bc.collectTicker = time.NewTicker(bc.collectInterval)

	// Start collection goroutine
	go bc.runCollectionLoop(ctx)

	// Start processing goroutine
	go bc.runProcessingLoop(ctx)

	return nil
}

// Stop halts baseline learning data collection
func (bc *BaselineCollector) Stop() error {
	bc.logger.Info("Stopping baseline learning collector")

	if !bc.isRunning {
		return nil
	}

	bc.isRunning = false

	if bc.collectTicker != nil {
		bc.collectTicker.Stop()
	}

	close(bc.stopChan)
	return nil
}

// runCollectionLoop collects samples at regular intervals
func (bc *BaselineCollector) runCollectionLoop(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			bc.logger.Error("Baseline collection loop panic", zap.Any("error", r))
		}
	}()

	for {
		select {
		case <-ctx.Done():
			bc.logger.Debug("Collection loop stopping due to context cancellation")
			return
		case <-bc.stopChan:
			bc.logger.Debug("Collection loop stopping due to stop signal")
			return
		case <-bc.collectTicker.C:
			if err := bc.collectSample(ctx); err != nil {
				bc.logger.Warn("Failed to collect baseline sample", zap.Error(err))
			}
		}
	}
}

// runProcessingLoop processes accumulated samples for baseline learning
func (bc *BaselineCollector) runProcessingLoop(ctx context.Context) {
	processingTicker := time.NewTicker(bc.processingWindow)
	defer processingTicker.Stop()

	defer func() {
		if r := recover(); r != nil {
			bc.logger.Error("Baseline processing loop panic", zap.Any("error", r))
		}
	}()

	for {
		select {
		case <-ctx.Done():
			bc.logger.Debug("Processing loop stopping due to context cancellation")
			return
		case <-bc.stopChan:
			bc.logger.Debug("Processing loop stopping due to stop signal")
			return
		case <-processingTicker.C:
			bc.processSamples(ctx)
		}
	}
}

// collectSample collects a single baseline learning sample
func (bc *BaselineCollector) collectSample(ctx context.Context) error {
	// This would normally integrate with the metrics collector
	// For now, we'll create a simulated sample
	// In a real implementation, this would get actual PHP-FPM process metrics

	sample := WorkerSample{
		Timestamp:          time.Now(),
		PoolName:           bc.scaler.poolName,
		ActiveWorkers:      bc.scaler.activeWorkers,
		TotalWorkers:       bc.scaler.currentWorkers,
		ProcessingVelocity: 0,                        // Will be calculated from actual metrics
		AvgMemoryPerWorker: DefaultAvgWorkerMemoryMB, // Will be measured from actual process memory usage
		AvgCPUPerWorker:    5,                        // Will be measured from actual process CPU usage
		AvgRequestDuration: 100 * time.Millisecond,   // From PHP-FPM process data
		QueueDepth:         0,                        // From PHP-FPM status
		RequestsPerSecond:  1.0,                      // Calculated from processing velocity
	}

	// In a real implementation, populate sample with actual metrics:
	// 1. Get current PHP-FPM process metrics from collector
	// 2. Calculate memory usage per worker from system process data
	// 3. Get CPU usage per worker from process CPU tracking
	// 4. Extract queue depth and request timing from PHP-FPM status

	bc.bufferMutex.Lock()
	bc.sampleBuffer = append(bc.sampleBuffer, sample)

	// Trim buffer if it exceeds max size
	if len(bc.sampleBuffer) > bc.maxBuffer {
		bc.sampleBuffer = bc.sampleBuffer[1:]
	}
	bc.bufferMutex.Unlock()

	bc.logger.Debug("Collected baseline sample",
		zap.String("pool", sample.PoolName),
		zap.Int("active_workers", sample.ActiveWorkers),
		zap.Int("total_workers", sample.TotalWorkers),
		zap.Float64("processing_velocity", sample.ProcessingVelocity),
	)

	return nil
}

// processSamples analyzes collected samples and feeds them to the intelligent scaler
func (bc *BaselineCollector) processSamples(ctx context.Context) {
	bc.bufferMutex.RLock()
	if len(bc.sampleBuffer) == 0 {
		bc.bufferMutex.RUnlock()
		return
	}

	// Create a copy of samples for processing
	samples := make([]WorkerSample, len(bc.sampleBuffer))
	copy(samples, bc.sampleBuffer)
	bc.bufferMutex.RUnlock()

	bc.logger.Debug("Processing baseline samples", zap.Int("count", len(samples)))

	// Analyze samples and create baseline samples for the scaler
	for _, sample := range samples {
		baselineSample := BaselineSample{
			Timestamp:          sample.Timestamp,
			ActiveWorkers:      sample.ActiveWorkers,
			RequestsPerSecond:  sample.RequestsPerSecond,
			AvgMemoryPerWorker: sample.AvgMemoryPerWorker,
			AvgCPUPerWorker:    sample.AvgCPUPerWorker,
			AvgRequestDuration: sample.AvgRequestDuration,
			QueueDepth:         sample.QueueDepth,
		}

		// Feed sample to intelligent scaler for baseline learning
		bc.scaler.AddBaselineSample(baselineSample)
	}

	// Clear processed samples
	bc.bufferMutex.Lock()
	bc.sampleBuffer = bc.sampleBuffer[:0]
	bc.bufferMutex.Unlock()

	bc.logger.Info("Processed baseline samples for learning",
		zap.Int("samples_processed", len(samples)),
		zap.String("pool", bc.scaler.poolName),
	)
}

// GetCollectionStatus returns the current status of baseline collection
// GetCollectionStatusTyped returns the collection status with type safety
func (bc *BaselineCollector) GetCollectionStatusTyped() CollectionStatusDetail {
	bc.bufferMutex.RLock()
	bufferCount := len(bc.sampleBuffer)
	bc.bufferMutex.RUnlock()

	bc.mu.RLock()
	defer bc.mu.RUnlock()

	return CollectionStatusDetail{
		IsRunning:       bc.isRunning,
		LastCollectTime: bc.lastCollectTime,
		CollectInterval: bc.collectInterval,
		TotalSamples:    bufferCount,
		SuccessfulRuns:  bc.successfulRuns,
		FailedRuns:      bc.failedRuns,
		LastError:       bc.lastError,
		LastErrorTime:   bc.lastErrorTime,
	}
}

// GetCollectionStatus returns the collection status (legacy interface{} version for backward compatibility)
func (bc *BaselineCollector) GetCollectionStatus() map[string]interface{} {
	status := bc.GetCollectionStatusTyped()

	return map[string]interface{}{
		"is_running":        status.IsRunning,
		"last_collect_time": status.LastCollectTime,
		"collect_interval":  status.CollectInterval.String(),
		"total_samples":     status.TotalSamples,
		"successful_runs":   status.SuccessfulRuns,
		"failed_runs":       status.FailedRuns,
		"last_error":        status.LastError,
		"last_error_time":   status.LastErrorTime,
	}
}

// IntegrateWithMetrics connects the baseline collector with the metrics system
// This method would be called to wire up actual metrics collection
func (bc *BaselineCollector) IntegrateWithMetrics(metricsSource chan *metrics.WorkerProcessMetrics) {
	go func() {
		for workerMetrics := range metricsSource {
			if workerMetrics == nil {
				continue
			}

			// Convert metrics to baseline sample
			sample := WorkerSample{
				Timestamp:          workerMetrics.Timestamp,
				PoolName:           workerMetrics.PoolName,
				ActiveWorkers:      workerMetrics.ActiveWorkers,
				TotalWorkers:       workerMetrics.TotalWorkers,
				ProcessingVelocity: workerMetrics.ProcessingVelocity,
				AvgRequestDuration: workerMetrics.AvgActiveRequestDuration,
				QueueDepth:         workerMetrics.QueueDepth,
				RequestsPerSecond:  workerMetrics.ProcessingVelocity, // Approximate
			}

			// Calculate memory and CPU per worker from process metrics
			if len(workerMetrics.ProcessMetrics) > 0 {
				var totalMemory float64
				var totalCPU float64
				activeCount := 0

				for _, proc := range workerMetrics.ProcessMetrics {
					if proc.IsActive {
						totalMemory += float64(proc.LastRequestMemory) / (1024 * 1024) // Convert to MB
						totalCPU += proc.LastRequestCPU
						activeCount++
					}
				}

				if activeCount > 0 {
					sample.AvgMemoryPerWorker = totalMemory / float64(activeCount)
					sample.AvgCPUPerWorker = totalCPU / float64(activeCount)
				}
			}

			// Add sample to buffer
			bc.bufferMutex.Lock()
			bc.sampleBuffer = append(bc.sampleBuffer, sample)

			// Trim buffer if it exceeds max size
			if len(bc.sampleBuffer) > bc.maxBuffer {
				bc.sampleBuffer = bc.sampleBuffer[1:]
			}
			bc.bufferMutex.Unlock()
		}
	}()
}
