package autoscaler

import (
	"context"
	"testing"
	"time"
)

func TestNewIntelligentScaler(t *testing.T) {
	scaler, err := NewIntelligentScaler("test-pool")
	if err != nil {
		t.Fatalf("Failed to create intelligent scaler: %v", err)
	}

	if scaler.poolName != "test-pool" {
		t.Errorf("Expected pool name 'test-pool', got '%s'", scaler.poolName)
	}

	if !scaler.baseline.IsLearning {
		t.Error("Expected scaler to start in learning mode")
	}

	if scaler.baseline.TargetSamples != 50 {
		t.Errorf("Expected 50 target samples, got %d", scaler.baseline.TargetSamples)
	}
}

func TestBaselineLearning(t *testing.T) {
	scaler, err := NewIntelligentScaler("test-pool")
	if err != nil {
		t.Fatalf("Failed to create intelligent scaler: %v", err)
	}

	// Add baseline samples
	for i := 0; i < 10; i++ {
		sample := BaselineSample{
			Timestamp:          time.Now().Add(time.Duration(i) * time.Second),
			ActiveWorkers:      2,
			RequestsPerSecond:  10.0,
			AvgMemoryPerWorker: 64.0,
			AvgCPUPerWorker:    5.0,
			AvgRequestDuration: 100 * time.Millisecond,
			QueueDepth:         0,
		}
		scaler.AddBaselineSample(sample)
	}

	// Check that baseline is being learned
	if scaler.baseline.SampleCount != 10 {
		t.Errorf("Expected 10 samples, got %d", scaler.baseline.SampleCount)
	}

	if scaler.baseline.OptimalMemoryPerWorker == 0 {
		t.Error("Expected baseline memory per worker to be set")
	}

	if scaler.baseline.ConfidenceScore == 0 {
		t.Error("Expected confidence score to be calculated")
	}

	t.Logf("Baseline after 10 samples: memory=%.1fMB, cpu=%.1f%%, confidence=%.3f",
		scaler.baseline.OptimalMemoryPerWorker,
		scaler.baseline.OptimalCPUPerWorker,
		scaler.baseline.ConfidenceScore)
}

func TestLearningPhaseScaling(t *testing.T) {
	scaler, err := NewIntelligentScaler("test-pool")
	if err != nil {
		t.Fatalf("Failed to create intelligent scaler: %v", err)
	}

	ctx := context.Background()

	// During learning phase with few samples, should start with 1 worker
	scaler.baseline.SampleCount = 2
	targetWorkers, err := scaler.CalculateTargetWorkers(ctx)
	if err != nil {
		t.Fatalf("Failed to calculate target workers: %v", err)
	}

	if targetWorkers != 1 {
		t.Errorf("Expected 1 worker during early learning, got %d", targetWorkers)
	}

	// With more samples and queue pressure, should scale up conservatively
	scaler.baseline.SampleCount = 10
	scaler.currentWorkers = 2
	scaler.queueMetrics = &QueueMetrics{
		Depth:              10,
		ProcessingVelocity: 2.0,
	}

	targetWorkers, err = scaler.CalculateTargetWorkers(ctx)
	if err != nil {
		t.Fatalf("Failed to calculate target workers: %v", err)
	}

	// Should scale up due to queue pressure but stay conservative during learning
	if targetWorkers <= 2 {
		t.Errorf("Expected scaling up due to queue pressure, got %d workers", targetWorkers)
	}

	// Should not exceed learning phase limits
	maxLearningWorkers := int(float64(scaler.maxWorkersLimit) * 0.25)
	if targetWorkers > maxLearningWorkers {
		t.Errorf("Expected learning phase scaling to stay under %d workers, got %d",
			maxLearningWorkers, targetWorkers)
	}

	t.Logf("Learning phase scaling: %d -> %d workers (queue depth: %d)",
		scaler.currentWorkers, targetWorkers, scaler.queueMetrics.Depth)
}

func TestConfidenceBasedScaling(t *testing.T) {
	scaler, err := NewIntelligentScaler("test-pool")
	if err != nil {
		t.Fatalf("Failed to create intelligent scaler: %v", err)
	}

	// Simulate learning completion
	scaler.baseline.IsLearning = false
	scaler.baseline.SampleCount = 60
	scaler.baseline.ConfidenceScore = 0.8 // High confidence
	scaler.baseline.OptimalMemoryPerWorker = 64.0
	scaler.baseline.OptimalCPUPerWorker = 5.0

	scaler.currentWorkers = 4
	scaler.activeWorkers = 2

	// High queue pressure with high confidence should allow aggressive scaling
	// 20 jobs with only 2 active workers = 10 jobs per worker (high pressure)
	scaler.queueMetrics = &QueueMetrics{
		Depth:                20,
		ProcessingVelocity:   0.5,              // Low processing velocity to trigger scaling
		EstimatedProcessTime: 40 * time.Second, // Takes longer than 30s target
	}

	// Ensure the system memory limit allows scaling up
	scaler.systemMemoryMB = 8192 // 8GB system
	scaler.maxWorkersLimit = 64  // Allow plenty of headroom for scaling

	ctx := context.Background()
	targetWorkers, err := scaler.CalculateTargetWorkers(ctx)
	if err != nil {
		t.Fatalf("Failed to calculate target workers: %v", err)
	}

	// Debug information
	t.Logf("Baseline optimal memory per worker: %.1f MB", scaler.baseline.OptimalMemoryPerWorker)
	t.Logf("System memory: %d MB, Max workers limit: %d", scaler.systemMemoryMB, scaler.maxWorkersLimit)
	t.Logf("Queue depth: %d, Processing velocity: %.1f", scaler.queueMetrics.Depth, scaler.queueMetrics.ProcessingVelocity)

	if targetWorkers <= scaler.currentWorkers {
		t.Errorf("Expected aggressive scaling up with high confidence and queue pressure, got %d workers", targetWorkers)
	}

	t.Logf("High confidence scaling: %d -> %d workers (confidence: %.2f)",
		scaler.currentWorkers, targetWorkers, scaler.baseline.ConfidenceScore)

	// Test low confidence scaling
	scaler.baseline.ConfidenceScore = 0.3 // Low confidence
	targetWorkers, err = scaler.CalculateTargetWorkers(ctx)
	if err != nil {
		t.Fatalf("Failed to calculate target workers: %v", err)
	}

	// Should be more conservative with low confidence
	conservativeIncrease := targetWorkers - scaler.currentWorkers
	if conservativeIncrease > 2 {
		t.Errorf("Expected conservative scaling with low confidence, got increase of %d workers", conservativeIncrease)
	}

	t.Logf("Low confidence scaling: %d -> %d workers (confidence: %.2f)",
		scaler.currentWorkers, targetWorkers, scaler.baseline.ConfidenceScore)
}

func TestHysteresisProtection(t *testing.T) {
	scaler, err := NewIntelligentScaler("test-pool")
	if err != nil {
		t.Fatalf("Failed to create intelligent scaler: %v", err)
	}

	// Set up short cooldown for testing
	scaler.hysteresis.ScaleUpCooldown = 1 * time.Second
	scaler.hysteresis.ScaleDownCooldown = 2 * time.Second

	scaler.currentWorkers = 5
	scaler.lastScaleTime = time.Now()
	scaler.lastScaleDirection = 1 // Last scaled up

	// Test cooldown prevention
	targetWorkers := scaler.applyHysteresisProtection(7) // Want to scale up
	if targetWorkers != scaler.currentWorkers {
		t.Errorf("Expected hysteresis to prevent scaling during cooldown, got %d workers", targetWorkers)
	}

	// Wait for cooldown to expire
	time.Sleep(1100 * time.Millisecond)

	targetWorkers = scaler.applyHysteresisProtection(7)
	if targetWorkers != 7 {
		t.Errorf("Expected scaling after cooldown, got %d workers instead of 7", targetWorkers)
	}

	// Test oscillation detection
	scaler.consecutiveDecisions = []int{1, -1, 1, -1}   // Alternating pattern
	targetWorkers = scaler.applyHysteresisProtection(3) // Want to scale down
	if targetWorkers != scaler.currentWorkers {
		t.Errorf("Expected oscillation protection to prevent scaling, got %d workers", targetWorkers)
	}

	t.Logf("Hysteresis protection working correctly")
}

func TestMemoryBasedConstraints(t *testing.T) {
	scaler, err := NewIntelligentScaler("test-pool")
	if err != nil {
		t.Fatalf("Failed to create intelligent scaler: %v", err)
	}

	// Test that scaling respects memory limits
	scaler.baseline.IsLearning = false
	scaler.baseline.OptimalMemoryPerWorker = 128.0 // High memory per worker
	scaler.currentWorkers = 2

	// System has limited memory
	originalMaxWorkers := scaler.maxWorkersLimit
	scaler.maxWorkersLimit = 10 // Limited by system memory

	optimalWorkers := scaler.calculateOptimalFromBaseline()
	if optimalWorkers > scaler.maxWorkersLimit {
		t.Errorf("Calculated workers (%d) exceeded memory-based limit (%d)",
			optimalWorkers, scaler.maxWorkersLimit)
	}

	scaler.maxWorkersLimit = originalMaxWorkers
	t.Logf("Memory constraints respected: optimal=%d, limit=%d", optimalWorkers, scaler.maxWorkersLimit)
}

func TestQueuePressureCalculation(t *testing.T) {
	scaler, err := NewIntelligentScaler("test-pool")
	if err != nil {
		t.Fatalf("Failed to create intelligent scaler: %v", err)
	}

	// Test various queue scenarios
	testCases := []struct {
		queueDepth         int
		processingVelocity float64
		currentWorkers     int
		expectedPressure   string // low, medium, high
	}{
		{0, 2.0, 2, "none"},   // No queue
		{5, 2.0, 2, "medium"}, // Moderate queue
		{20, 1.0, 2, "high"},  // High queue with low velocity
		{10, 10.0, 5, "low"},  // Queue with high processing velocity
	}

	for _, tc := range testCases {
		scaler.queueMetrics = &QueueMetrics{
			Depth:              tc.queueDepth,
			ProcessingVelocity: tc.processingVelocity,
		}
		scaler.currentWorkers = tc.currentWorkers
		scaler.activeWorkers = tc.currentWorkers

		optimalWorkers := scaler.calculateOptimalFromBaseline()

		t.Logf("Queue test: depth=%d, velocity=%.1f, workers=%d -> optimal=%d (%s pressure)",
			tc.queueDepth, tc.processingVelocity, tc.currentWorkers, optimalWorkers, tc.expectedPressure)
	}
}

func TestBaselineConfidenceCalculation(t *testing.T) {
	scaler, err := NewIntelligentScaler("test-pool")
	if err != nil {
		t.Fatalf("Failed to create intelligent scaler: %v", err)
	}

	// Add consistent samples (should increase confidence)
	consistentSamples := []BaselineSample{
		{Timestamp: time.Now(), AvgMemoryPerWorker: 64.0, AvgCPUPerWorker: 5.0, AvgRequestDuration: 100 * time.Millisecond},
		{Timestamp: time.Now(), AvgMemoryPerWorker: 65.0, AvgCPUPerWorker: 5.1, AvgRequestDuration: 105 * time.Millisecond},
		{Timestamp: time.Now(), AvgMemoryPerWorker: 63.0, AvgCPUPerWorker: 4.9, AvgRequestDuration: 95 * time.Millisecond},
		{Timestamp: time.Now(), AvgMemoryPerWorker: 64.5, AvgCPUPerWorker: 5.2, AvgRequestDuration: 102 * time.Millisecond},
		{Timestamp: time.Now(), AvgMemoryPerWorker: 64.2, AvgCPUPerWorker: 5.0, AvgRequestDuration: 98 * time.Millisecond},
	}

	for _, sample := range consistentSamples {
		scaler.AddBaselineSample(sample)
	}

	consistentConfidence := scaler.baseline.ConfidenceScore

	// Add variable samples (should decrease confidence)
	variableSamples := []BaselineSample{
		{Timestamp: time.Now(), AvgMemoryPerWorker: 32.0, AvgCPUPerWorker: 2.0, AvgRequestDuration: 50 * time.Millisecond},
		{Timestamp: time.Now(), AvgMemoryPerWorker: 128.0, AvgCPUPerWorker: 10.0, AvgRequestDuration: 200 * time.Millisecond},
		{Timestamp: time.Now(), AvgMemoryPerWorker: 96.0, AvgCPUPerWorker: 8.0, AvgRequestDuration: 300 * time.Millisecond},
	}

	for _, sample := range variableSamples {
		scaler.AddBaselineSample(sample)
	}

	variableConfidence := scaler.baseline.ConfidenceScore

	t.Logf("Confidence with consistent samples: %.3f", consistentConfidence)
	t.Logf("Confidence with variable samples: %.3f", variableConfidence)

	if variableConfidence >= consistentConfidence {
		t.Error("Expected confidence to decrease with variable samples")
	}
}
