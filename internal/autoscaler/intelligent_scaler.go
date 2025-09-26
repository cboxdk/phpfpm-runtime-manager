package autoscaler

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"
)

// WorkerState represents the current state of a PHP-FPM worker
type WorkerState string

const (
	WorkerStateIdle     WorkerState = "idle"
	WorkerStateActive   WorkerState = "active"
	WorkerStateDying    WorkerState = "dying"
	WorkerStateStarting WorkerState = "starting"
)

// WorkerMetrics captures detailed metrics for a single PHP-FPM worker
type WorkerMetrics struct {
	PID                    int           `json:"pid"`
	State                  WorkerState   `json:"state"`
	StartTime              time.Time     `json:"start_time"`
	LastRequestTime        time.Time     `json:"last_request_time"`
	RequestCount           int64         `json:"request_count"`
	CPUTime                time.Duration `json:"cpu_time"`
	MemoryUsageMB          float64       `json:"memory_usage_mb"`
	LastRequestDuration    time.Duration `json:"last_request_duration"`
	AverageRequestDuration time.Duration `json:"average_request_duration"`
}

// BaselineSample represents a learning sample during the baseline phase
type BaselineSample struct {
	Timestamp          time.Time     `json:"timestamp"`
	ActiveWorkers      int           `json:"active_workers"`
	RequestsPerSecond  float64       `json:"requests_per_second"`
	AvgMemoryPerWorker float64       `json:"avg_memory_per_worker"`
	AvgCPUPerWorker    float64       `json:"avg_cpu_per_worker"`
	AvgRequestDuration time.Duration `json:"avg_request_duration"`
	QueueDepth         int           `json:"queue_depth"`
}

// ScalingBaseline contains learned baseline metrics for intelligent scaling
type ScalingBaseline struct {
	// Learning state
	IsLearning        bool      `json:"is_learning"`
	SampleCount       int       `json:"sample_count"`
	TargetSamples     int       `json:"target_samples"`
	LearningStartTime time.Time `json:"learning_start_time"`

	// Baseline metrics (learned from actual workload)
	OptimalMemoryPerWorker   float64       `json:"optimal_memory_per_worker"`
	OptimalCPUPerWorker      float64       `json:"optimal_cpu_per_worker"`
	OptimalRequestDuration   time.Duration `json:"optimal_request_duration"`
	OptimalRequestsPerWorker float64       `json:"optimal_requests_per_worker"`

	// Confidence metrics
	ConfidenceScore      float64   `json:"confidence_score"` // 0.0 to 1.0
	LastConfidenceUpdate time.Time `json:"last_confidence_update"`

	// Variance tracking for confidence calculation
	MemoryVariance   float64       `json:"memory_variance"`
	CPUVariance      float64       `json:"cpu_variance"`
	DurationVariance time.Duration `json:"duration_variance"`

	// Historical samples (limited buffer)
	RecentSamples []BaselineSample `json:"recent_samples"`
	MaxSamples    int              `json:"max_samples"`

	mu sync.RWMutex
}

// QueueMetrics represents queue depth and processing velocity metrics
type QueueMetrics struct {
	Depth                int           `json:"depth"`
	OldestJobAge         time.Duration `json:"oldest_job_age"`
	ProcessingVelocity   float64       `json:"processing_velocity"` // jobs per second
	AverageWaitTime      time.Duration `json:"average_wait_time"`
	EstimatedProcessTime time.Duration `json:"estimated_process_time"`
}

// HysteresisConfig controls scaling oscillation protection
type HysteresisConfig struct {
	ScaleUpCooldown           time.Duration `json:"scale_up_cooldown"`
	ScaleDownCooldown         time.Duration `json:"scale_down_cooldown"`
	MinConfidenceThreshold    float64       `json:"min_confidence_threshold"`
	ConsistentSamplesRequired int           `json:"consistent_samples_required"`
}

// IntelligentScaler implements adaptive baseline learning with confidence-based scaling.
//
// The IntelligentScaler uses a sophisticated three-phase approach to autoscaling:
//
//  1. Learning Phase: Collects 50+ samples to establish baseline performance patterns.
//     During this phase, scaling is conservative and limited to 25% of system capacity.
//
//  2. Confidence-Based Scaling: Uses learned patterns to make scaling decisions with
//     varying aggressiveness based on confidence in the data:
//     - High confidence (>0.8): Aggressive scaling up to full system capacity
//     - Medium confidence (0.5-0.8): Moderate scaling with 70% of calculated change
//     - Low confidence (<0.5): Conservative scaling with minimal changes
//
//  3. Hysteresis Protection: Prevents oscillations through cooldown periods and
//     oscillation detection based on consecutive scaling decisions.
//
// The scaler continuously learns from worker metrics including:
//   - Memory usage per worker (actual measurements from PHP-FPM processes)
//   - CPU utilization per worker
//   - Request duration patterns
//   - Queue depth and processing velocity
//
// Key algorithms:
//   - Exponential moving averages for baseline stability
//   - Queue pressure detection using jobs-per-worker ratios
//   - Memory-aware scaling respecting system resource constraints
//   - Confidence scoring based on metric variance and sample consistency
type IntelligentScaler struct {
	// Configuration
	poolName   string
	config     WorkerCalculationConfig
	hysteresis HysteresisConfig

	// State management
	baseline             *ScalingBaseline
	lastScaleTime        time.Time
	lastScaleDirection   int   // -1: down, 0: none, 1: up
	consecutiveDecisions []int // Track recent scaling decisions

	// Current metrics
	currentWorkers int
	activeWorkers  int
	workerMetrics  map[int]*WorkerMetrics
	queueMetrics   *QueueMetrics

	// System constraints
	systemMemoryMB  int
	maxWorkersLimit int

	// Thread safety
	mu sync.RWMutex
}

// NewIntelligentScaler creates a new intelligent autoscaler with baseline learning
func NewIntelligentScaler(poolName string) (*IntelligentScaler, error) {
	// Validate pool name
	if err := ValidatePoolName(poolName); err != nil {
		return nil, fmt.Errorf("invalid pool name: %w", err)
	}

	systemMemoryMB, err := getSystemMemory()
	if err != nil {
		return nil, fmt.Errorf("failed to get system memory: %w", err)
	}

	config := DefaultWorkerCalculationConfig()
	maxWorkers, err := GetMemoryBasedMaxWorkers()
	if err != nil {
		return nil, fmt.Errorf("failed to calculate max workers: %w", err)
	}

	return &IntelligentScaler{
		poolName: poolName,
		config:   config,
		hysteresis: HysteresisConfig{
			ScaleUpCooldown:           DefaultScaleUpCooldown,
			ScaleDownCooldown:         DefaultScaleDownCooldown,
			MinConfidenceThreshold:    MinimumConfidenceThreshold,
			ConsistentSamplesRequired: MinConsistentSamplesRequired,
		},
		baseline: &ScalingBaseline{
			IsLearning:        true,
			TargetSamples:     DefaultTargetSamples,
			LearningStartTime: time.Now(),
			MaxSamples:        MaxBufferedSamples,
			RecentSamples:     make([]BaselineSample, 0, MaxBufferedSamples),
		},
		workerMetrics:        make(map[int]*WorkerMetrics),
		systemMemoryMB:       systemMemoryMB,
		maxWorkersLimit:      maxWorkers,
		consecutiveDecisions: make([]int, 0, 10),
	}, nil
}

// UpdateWorkerMetrics updates metrics for a specific worker
func (is *IntelligentScaler) UpdateWorkerMetrics(metrics *WorkerMetrics) {
	is.mu.Lock()
	defer is.mu.Unlock()

	is.workerMetrics[metrics.PID] = metrics

	// Count active workers
	activeCount := 0
	for _, m := range is.workerMetrics {
		if m.State == WorkerStateActive {
			activeCount++
		}
	}
	is.activeWorkers = activeCount
	is.currentWorkers = len(is.workerMetrics)
}

// UpdateQueueMetrics updates queue depth and processing velocity
func (is *IntelligentScaler) UpdateQueueMetrics(metrics *QueueMetrics) {
	is.mu.Lock()
	defer is.mu.Unlock()

	is.queueMetrics = metrics
}

// UpdateWorkerCount updates the current and active worker counts
func (is *IntelligentScaler) UpdateWorkerCount(current, active int) {
	is.mu.Lock()
	defer is.mu.Unlock()

	is.currentWorkers = current
	is.activeWorkers = active
}

// AddBaselineSample adds a new learning sample during the baseline phase
func (is *IntelligentScaler) AddBaselineSample(sample BaselineSample) {
	is.baseline.mu.Lock()
	defer is.baseline.mu.Unlock()

	// Add sample to recent samples using ring buffer for efficiency
	is.addSampleToRingBuffer(sample)

	is.baseline.SampleCount++

	// Update baseline metrics using exponential moving average
	alpha := 0.1 // Smoothing factor
	if is.baseline.SampleCount == 1 {
		// First sample - initialize values
		is.baseline.OptimalMemoryPerWorker = sample.AvgMemoryPerWorker
		is.baseline.OptimalCPUPerWorker = sample.AvgCPUPerWorker
		is.baseline.OptimalRequestDuration = sample.AvgRequestDuration
		is.baseline.OptimalRequestsPerWorker = sample.RequestsPerSecond
	} else {
		// Update with exponential moving average
		is.baseline.OptimalMemoryPerWorker = alpha*sample.AvgMemoryPerWorker + (1-alpha)*is.baseline.OptimalMemoryPerWorker
		is.baseline.OptimalCPUPerWorker = alpha*sample.AvgCPUPerWorker + (1-alpha)*is.baseline.OptimalCPUPerWorker
		is.baseline.OptimalRequestDuration = time.Duration(alpha*float64(sample.AvgRequestDuration) + (1-alpha)*float64(is.baseline.OptimalRequestDuration))
		is.baseline.OptimalRequestsPerWorker = alpha*sample.RequestsPerSecond + (1-alpha)*is.baseline.OptimalRequestsPerWorker
	}

	// Calculate variance for confidence scoring
	is.updateVarianceMetrics()

	// Update confidence score
	is.updateConfidenceScore()

	// Check if learning phase should end
	if is.baseline.SampleCount >= is.baseline.TargetSamples && is.baseline.ConfidenceScore > is.hysteresis.MinConfidenceThreshold {
		is.baseline.IsLearning = false
	}
}

// updateVarianceMetrics calculates variance for confidence scoring
func (is *IntelligentScaler) updateVarianceMetrics() {
	samples := is.baseline.RecentSamples
	if len(samples) < 2 {
		return
	}

	// Calculate variance for memory usage
	var memSum, memSumSq float64
	var cpuSum, cpuSumSq float64
	var durSum, durSumSq float64

	for _, sample := range samples {
		memSum += sample.AvgMemoryPerWorker
		memSumSq += sample.AvgMemoryPerWorker * sample.AvgMemoryPerWorker

		cpuSum += sample.AvgCPUPerWorker
		cpuSumSq += sample.AvgCPUPerWorker * sample.AvgCPUPerWorker

		durFloat := float64(sample.AvgRequestDuration)
		durSum += durFloat
		durSumSq += durFloat * durFloat
	}

	n := float64(len(samples))

	is.baseline.MemoryVariance = (memSumSq - (memSum*memSum)/n) / (n - 1)
	is.baseline.CPUVariance = (cpuSumSq - (cpuSum*cpuSum)/n) / (n - 1)

	durVariance := (durSumSq - (durSum*durSum)/n) / (n - 1)
	is.baseline.DurationVariance = time.Duration(durVariance)
}

// updateConfidenceScore calculates confidence based on sample consistency
func (is *IntelligentScaler) updateConfidenceScore() {
	if is.baseline.SampleCount < 3 {
		is.baseline.ConfidenceScore = 0.0
		return
	}

	// Base confidence from sample count (more samples = higher confidence)
	sampleConfidence := math.Min(float64(is.baseline.SampleCount)/float64(is.baseline.TargetSamples), 1.0)

	// Variance confidence (lower variance = higher confidence)
	memoryCV := math.Sqrt(is.baseline.MemoryVariance) / is.baseline.OptimalMemoryPerWorker
	cpuCV := math.Sqrt(is.baseline.CPUVariance) / is.baseline.OptimalCPUPerWorker

	// Convert coefficient of variation to confidence (lower CV = higher confidence)
	varianceConfidence := math.Max(0.0, 1.0-(memoryCV+cpuCV)/2.0)

	// Time-based confidence (longer learning period = higher confidence)
	learningDuration := time.Since(is.baseline.LearningStartTime)
	timeConfidence := math.Min(float64(learningDuration)/(5*time.Minute).Seconds(), 1.0)

	// Weighted average of confidence factors
	is.baseline.ConfidenceScore = 0.5*sampleConfidence + 0.3*varianceConfidence + 0.2*timeConfidence
	is.baseline.LastConfidenceUpdate = time.Now()
}

// CalculateTargetWorkers determines the optimal number of workers based on current conditions
func (is *IntelligentScaler) CalculateTargetWorkers(ctx context.Context) (int, error) {
	is.mu.RLock()
	defer is.mu.RUnlock()

	// During learning phase, start with 1 worker and grow conservatively
	if is.baseline.IsLearning {
		return is.calculateLearningPhaseTarget()
	}

	// Post-learning: use confidence-based scaling
	return is.calculateConfidenceBasedTarget()
}

// calculateLearningPhaseTarget handles scaling during the initial learning phase
func (is *IntelligentScaler) calculateLearningPhaseTarget() (int, error) {
	// Start with 1 worker to establish baseline
	if is.baseline.SampleCount < 5 {
		return 1, nil
	}

	// Gradually increase to gather diverse samples
	// But never exceed 25% of max capacity during learning
	maxLearningWorkers := int(float64(is.maxWorkersLimit) * 0.25)
	if maxLearningWorkers < 2 {
		maxLearningWorkers = 2
	}

	// Scale based on queue pressure during learning
	if is.queueMetrics != nil && is.queueMetrics.Depth > 0 {
		queuePressure := float64(is.queueMetrics.Depth) / math.Max(float64(is.activeWorkers), 1.0)

		if queuePressure > 3.0 && is.currentWorkers < maxLearningWorkers {
			return is.currentWorkers + 1, nil
		}
	}

	return is.currentWorkers, nil
}

// calculateConfidenceBasedTarget uses learned baseline for intelligent scaling decisions
func (is *IntelligentScaler) calculateConfidenceBasedTarget() (int, error) {
	if is.queueMetrics == nil {
		return is.currentWorkers, nil
	}

	confidence := is.baseline.ConfidenceScore

	// Calculate optimal workers based on learned baseline
	optimalWorkers := is.calculateOptimalFromBaseline()

	// Apply confidence-based scaling aggressiveness
	targetWorkers := is.applyConfidenceScaling(optimalWorkers, confidence)

	// Apply hysteresis protection
	targetWorkers = is.applyHysteresisProtection(targetWorkers)

	// Apply system constraints
	if targetWorkers > is.maxWorkersLimit {
		targetWorkers = is.maxWorkersLimit
	}
	if targetWorkers < 1 {
		targetWorkers = 1
	}

	return targetWorkers, nil
}

// calculateOptimalFromBaseline calculates optimal workers based on learned metrics
func (is *IntelligentScaler) calculateOptimalFromBaseline() int {
	// Calculate workers needed based on queue depth and processing velocity
	queueBasedWorkers := is.currentWorkers // Start with current workers
	if is.queueMetrics != nil && is.queueMetrics.Depth > 0 {
		if is.queueMetrics.ProcessingVelocity > 0 {
			timeToProcess := float64(is.queueMetrics.Depth) / is.queueMetrics.ProcessingVelocity
			targetProcessTime := TargetQueueProcessTimeSeconds // Target: process queue in seconds

			if timeToProcess > targetProcessTime {
				queueBasedWorkers = int(math.Ceil(timeToProcess / targetProcessTime * float64(is.currentWorkers)))
			}
		} else {
			// No processing velocity data, scale based on queue depth
			queuePressure := float64(is.queueMetrics.Depth) / math.Max(float64(is.activeWorkers), 1.0)
			if queuePressure > 2.0 {
				queueBasedWorkers = int(math.Ceil(queuePressure))
			}
		}
	}

	// Calculate workers based on memory constraints using learned baseline
	memoryBasedWorkers := is.maxWorkersLimit
	if is.baseline.OptimalMemoryPerWorker > 0 {
		availableMemory := float64(is.systemMemoryMB) * 0.75 // Reserve 25% for OS
		memoryBasedWorkers = int(availableMemory / is.baseline.OptimalMemoryPerWorker)
	}

	// Use the more conservative estimate
	return int(math.Min(float64(queueBasedWorkers), float64(memoryBasedWorkers)))
}

// applyConfidenceScaling adjusts scaling aggressiveness based on confidence
func (is *IntelligentScaler) applyConfidenceScaling(optimalWorkers int, confidence float64) int {
	currentWorkers := is.currentWorkers

	// High confidence allows aggressive scaling
	if confidence > 0.8 {
		return optimalWorkers
	}

	// Medium confidence allows moderate scaling
	if confidence > 0.5 {
		delta := optimalWorkers - currentWorkers
		moderateChange := int(float64(delta) * 0.7) // Scale 70% of optimal change
		return currentWorkers + moderateChange
	}

	// Low confidence allows only conservative scaling
	if optimalWorkers > currentWorkers {
		return currentWorkers + 1 // Scale up by 1
	} else if optimalWorkers < currentWorkers {
		return currentWorkers - 1 // Scale down by 1
	}

	return currentWorkers
}

// applyHysteresisProtection prevents rapid scaling oscillations
func (is *IntelligentScaler) applyHysteresisProtection(targetWorkers int) int {
	now := time.Now()
	currentWorkers := is.currentWorkers

	// Determine scaling direction
	var direction int
	if targetWorkers > currentWorkers {
		direction = 1 // Scale up
	} else if targetWorkers < currentWorkers {
		direction = -1 // Scale down
	} else {
		direction = 0 // No change
	}

	// Check cooldown periods
	timeSinceLastScale := now.Sub(is.lastScaleTime)

	if direction == 1 && timeSinceLastScale < is.hysteresis.ScaleUpCooldown {
		return currentWorkers // Still in scale-up cooldown
	}

	if direction == -1 && timeSinceLastScale < is.hysteresis.ScaleDownCooldown {
		return currentWorkers // Still in scale-down cooldown
	}

	// Check for oscillation pattern
	if is.isOscillating(direction) {
		return currentWorkers // Prevent oscillation
	}

	// Update decision history
	if direction != 0 {
		is.consecutiveDecisions = append(is.consecutiveDecisions, direction)
		if len(is.consecutiveDecisions) > 10 {
			is.consecutiveDecisions = is.consecutiveDecisions[1:]
		}
		is.lastScaleDirection = direction
		is.lastScaleTime = now
	}

	return targetWorkers
}

// isOscillating detects rapid scaling oscillations
func (is *IntelligentScaler) isOscillating(currentDirection int) bool {
	if len(is.consecutiveDecisions) < 4 {
		return false
	}

	// Check for alternating pattern in recent decisions
	recent := is.consecutiveDecisions[len(is.consecutiveDecisions)-3:]
	alternating := 0

	for i := 1; i < len(recent); i++ {
		if recent[i] != recent[i-1] {
			alternating++
		}
	}

	// If most recent decisions alternate, we're oscillating
	return alternating >= 2
}

// GetBaselineStatus returns the current baseline learning status
// GetBaselineStatusTyped returns the baseline status with type safety
func (is *IntelligentScaler) GetBaselineStatusTyped() BaselineStatusDetail {
	is.baseline.mu.RLock()
	defer is.baseline.mu.RUnlock()

	learningPhase := "initial"
	if is.baseline.SampleCount > 0 {
		if is.baseline.IsLearning {
			learningPhase = "learning"
		} else {
			learningPhase = "completed"
		}
	}

	return BaselineStatusDetail{
		IsLearning:               is.baseline.IsLearning,
		SampleCount:              is.baseline.SampleCount,
		ConfidenceScore:          is.baseline.ConfidenceScore,
		OptimalMemoryPerWorker:   is.baseline.OptimalMemoryPerWorker,
		OptimalCPUPerWorker:      is.baseline.OptimalCPUPerWorker,
		OptimalRequestDuration:   is.baseline.OptimalRequestDuration,
		OptimalRequestsPerWorker: is.baseline.OptimalRequestsPerWorker,
		LearningPhase:            learningPhase,
		MaxSamples:               is.baseline.MaxSamples,
	}
}

// GetBaselineStatus returns the baseline status (legacy interface{} version for backward compatibility)
func (is *IntelligentScaler) GetBaselineStatus() map[string]interface{} {
	status := is.GetBaselineStatusTyped()

	legacyStatus := map[string]interface{}{
		"is_learning":                 status.IsLearning,
		"sample_count":                status.SampleCount,
		"confidence_score":            status.ConfidenceScore,
		"learning_phase":              status.LearningPhase,
		"optimal_memory_per_worker":   status.OptimalMemoryPerWorker,
		"optimal_cpu_per_worker":      status.OptimalCPUPerWorker,
		"optimal_request_duration":    status.OptimalRequestDuration.String(),
		"optimal_requests_per_worker": status.OptimalRequestsPerWorker,
		"max_samples":                 status.MaxSamples,
	}

	return legacyStatus
}

// addSampleToRingBuffer efficiently adds a sample to the ring buffer without expensive slice operations
func (is *IntelligentScaler) addSampleToRingBuffer(sample BaselineSample) {
	if is.baseline.RecentSamples == nil {
		is.baseline.RecentSamples = make([]BaselineSample, 0, is.baseline.MaxSamples)
	}

	if len(is.baseline.RecentSamples) < is.baseline.MaxSamples {
		// Buffer not full yet, simple append
		is.baseline.RecentSamples = append(is.baseline.RecentSamples, sample)
	} else {
		// Buffer full, use ring buffer behavior - overwrite oldest sample
		// This avoids expensive copy operations and maintains constant O(1) time complexity
		oldestIndex := is.baseline.SampleCount % is.baseline.MaxSamples
		is.baseline.RecentSamples[oldestIndex] = sample
	}
}
