// Package autoscaler provides intelligent PHP-FPM worker process scaling
// with machine learning baselines and emergency response capabilities.
//
// This package implements a comprehensive autoscaling system that:
// - Learns baseline performance patterns from historical data
// - Provides emergency scaling for traffic spikes and queue buildup
// - Maintains optimal worker configurations with hot reloads
// - Integrates with supervisor or direct PHP-FPM management
//
// The system operates in three phases:
// 1. Learning Phase: Collects baseline samples and builds confidence models
// 2. Confident Scaling: Uses learned baselines for intelligent scaling decisions
// 3. Emergency Response: Immediate scaling for critical conditions
package autoscaler

import "time"

// Scaling Constants define the core parameters for intelligent autoscaling behavior.
// These constants control baseline learning, confidence thresholds, and scaling factors.
const (
	// DefaultTargetSamples is the default number of baseline samples to collect before exiting learning phase.
	// The system needs sufficient samples to build a reliable baseline model for intelligent scaling.
	// Lower values mean faster transition out of learning but less reliable baselines.
	DefaultTargetSamples = 50

	// MaxBufferedSamples is the maximum number of samples to keep in the baseline buffer.
	// This prevents unbounded memory growth while maintaining sufficient historical data
	// for rolling baseline calculations and confidence scoring.
	MaxBufferedSamples = 200

	// LearningPhaseMaxCapacityPercent is the maximum percentage of system capacity used during learning.
	// During the learning phase, scaling is conservative to prevent overprovisioning
	// while the system builds confidence in its baseline models.
	LearningPhaseMaxCapacityPercent = 0.25

	// TargetQueueProcessTimeSeconds is the target time in seconds to process the entire queue.
	// This defines the maximum acceptable queue processing time before emergency scaling triggers.
	// Lower values result in more aggressive scaling for queue management.
	TargetQueueProcessTimeSeconds = 30.0

	// HighQueuePressureThreshold defines when queue pressure is considered high (jobs per worker)
	HighQueuePressureThreshold = 2.0

	// MinViableWorkers is the absolute minimum number of workers that must be running
	MinViableWorkers = 1

	// MaxWorkerMemoryMB is the maximum memory per worker limit for safety (1GB)
	MaxWorkerMemoryMB = 1024

	// MinWorkerMemoryMB is the minimum memory per worker for viability (16MB)
	MinWorkerMemoryMB = 16
)

// Confidence Thresholds define the machine learning confidence levels that determine
// scaling aggressiveness. Higher confidence enables more aggressive scaling decisions,
// while lower confidence maintains conservative scaling behavior.
const (
	// HighConfidenceThreshold for aggressive scaling decisions.
	// Above this threshold, the system has high confidence in its baseline models
	// and can make full scaling adjustments based on learned patterns.
	HighConfidenceThreshold = 0.8

	// MediumConfidenceThreshold for moderate scaling decisions
	MediumConfidenceThreshold = 0.5

	// MinimumConfidenceThreshold below which scaling is very conservative
	MinimumConfidenceThreshold = 0.3

	// ModerateScalingFactor for medium confidence scaling (70% of optimal change)
	ModerateScalingFactor = 0.7
)

// Timing Constants control the temporal behavior of the autoscaling system.
// These values balance responsiveness with stability, preventing oscillation
// while ensuring timely response to load changes.
const (
	// DefaultScaleUpCooldown is the minimum time between scale-up operations
	DefaultScaleUpCooldown = 30 * time.Second

	// DefaultScaleDownCooldown is the minimum time between scale-down operations
	DefaultScaleDownCooldown = 60 * time.Second

	// DefaultCollectInterval for baseline sample collection
	DefaultCollectInterval = 10 * time.Second

	// DefaultProcessingWindow for processing accumulated samples
	DefaultProcessingWindow = 60 * time.Second

	// DefaultScalingInterval for making scaling decisions
	DefaultScalingInterval = 30 * time.Second

	// DefaultGlobalCooldown between major scaling actions across all pools
	DefaultGlobalCooldown = 60 * time.Second

	// MemoryCacheTTL for system memory detection caching
	MemoryCacheTTL = 5 * time.Minute

	// MinLearningDuration minimum time to spend in learning phase
	MinLearningDuration = 2 * time.Minute
)

// Memory Constants define memory-related parameters for worker scaling calculations.
// These values ensure memory-aware scaling that prevents system overload
// while maximizing available resources for PHP-FPM workers.
const (
	// DefaultMemoryReservePercent is the percentage of system memory to reserve for OS
	DefaultMemoryReservePercent = 0.25

	// DefaultAvgWorkerMemoryMB is the conservative estimate for PHP-FPM worker memory usage
	DefaultAvgWorkerMemoryMB = 64

	// SystemMemoryMinMB is the minimum system memory required (512MB)
	SystemMemoryMinMB = 512

	// SystemMemoryMaxMB is the maximum system memory to consider (1TB)
	SystemMemoryMaxMB = 1024 * 1024

	// FallbackSystemMemoryGB when memory detection fails
	FallbackSystemMemoryGB = 4
)

// Worker Configuration Constants define the optimal ratios and limits for
// PHP-FPM worker process configuration. These constants ensure properly
// balanced pool configurations across different scaling scenarios.
const (
	// DefaultStartServersPercent is the percentage of max workers to start initially
	DefaultStartServersPercent = 0.25

	// DefaultMinSparePercent is the percentage of max workers to keep as minimum spare
	DefaultMinSparePercent = 0.125

	// DefaultMaxSparePercent is the percentage of max workers to allow as maximum spare
	DefaultMaxSparePercent = 0.50

	// DefaultMinWorkers is the minimum number of workers to always maintain
	DefaultMinWorkers = 2

	// MaxDecisionHistory is the maximum number of scaling decisions to track for oscillation detection
	MaxDecisionHistory = 10

	// MinConsistentSamplesRequired for confidence calculation
	MinConsistentSamplesRequired = 3

	// ExponentialMovingAverageAlpha is the smoothing factor for baseline metrics
	ExponentialMovingAverageAlpha = 0.1
)

// Buffer and Channel Constants control internal data structures and communication
// channels within the autoscaling system. These values prevent memory leaks
// while ensuring adequate buffering for smooth operation.
const (
	// DefaultMaxBuffer for sample collection
	DefaultMaxBuffer = 100

	// DefaultChannelBuffer for metrics channels
	DefaultChannelBuffer = 100

	// MaxMetricsChannelSize to prevent memory leaks
	MaxMetricsChannelSize = 1000
)

// Validation Constants define limits and constraints for configuration validation.
// These constants ensure safe operation by preventing invalid configurations
// that could destabilize the autoscaling system or PHP-FPM pools.
const (
	// MaxPoolNameLength for pool name validation
	MaxPoolNameLength = 64

	// MinPoolNameLength for pool name validation
	MinPoolNameLength = 1

	// MaxScalingInterval to prevent too frequent scaling
	MaxScalingInterval = 10 * time.Minute

	// MinScalingInterval to ensure responsive scaling
	MinScalingInterval = 5 * time.Second
)
