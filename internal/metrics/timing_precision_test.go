package metrics

import (
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

// TestTimingPrecisionImprovements demonstrerer forbedret timing kun med polling
func TestTimingPrecisionImprovements(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := &TimingPrecisionExample{
		logger:   logger,
		lastSeen: make(map[int]*ProcessSnapshot),
	}

	t.Run("RequestBoundaryDetection", func(t *testing.T) {
		// Simulér 3 polls med request transitions
		baseTime := time.Now()

		// Poll 1: Process i Idle state
		poll1Time := baseTime
		poll1Processes := []PHPFPMProcess{
			{PID: 1001, State: ProcessStateIdle, RequestDuration: 0, RequestURI: ""},
		}

		events1 := tracker.DetectRequestBoundaries(poll1Processes, poll1Time)
		if len(events1) != 0 {
			t.Errorf("Poll 1: Expected 0 events for idle process, got %d", len(events1))
		}

		// Poll 2: Process starter request (Idle -> Running)
		poll2Time := baseTime.Add(100 * time.Millisecond)
		poll2Processes := []PHPFPMProcess{
			{PID: 1001, State: ProcessStateRunning, RequestDuration: 50, RequestURI: "/api/users"},
		}

		events2 := tracker.DetectRequestBoundaries(poll2Processes, poll2Time)
		if len(events2) != 1 {
			t.Fatalf("Poll 2: Expected 1 event for state transition, got %d", len(events2))
		}

		requestStart := events2[0]
		if requestStart.Type != RequestStart {
			t.Errorf("Expected RequestStart event, got %v", requestStart.Type)
		}
		if requestStart.PID != 1001 {
			t.Errorf("Expected PID 1001, got %d", requestStart.PID)
		}

		// Tjek at timing er estimeret mellem polls
		expectedTime := poll1Time.Add(50 * time.Millisecond) // Midpoint
		timeDiff := requestStart.EstimatedTime.Sub(expectedTime)
		if absDuration(timeDiff) > 25*time.Millisecond {
			t.Errorf("Timing estimation off by %v, expected around %v", timeDiff, expectedTime)
		}

		t.Logf("✅ Request start detected at %v (confidence: %.2f)",
			requestStart.EstimatedTime.Format("15:04:05.000"), requestStart.Confidence)

		// Poll 3: Request fortsætter
		poll3Time := baseTime.Add(200 * time.Millisecond)
		poll3Processes := []PHPFPMProcess{
			{PID: 1001, State: ProcessStateRunning, RequestDuration: 150, RequestURI: "/api/users"},
		}

		events3 := tracker.DetectRequestBoundaries(poll3Processes, poll3Time)
		if len(events3) != 0 {
			t.Errorf("Poll 3: Expected 0 events for continuing request, got %d", len(events3))
		}

		// Poll 4: Request færdig, ny request starter (RequestDuration reset)
		poll4Time := baseTime.Add(300 * time.Millisecond)
		poll4Processes := []PHPFPMProcess{
			{PID: 1001, State: ProcessStateRunning, RequestDuration: 25, RequestURI: "/api/products"},
		}

		events4 := tracker.DetectRequestBoundaries(poll4Processes, poll4Time)
		if len(events4) != 2 {
			t.Fatalf("Poll 4: Expected 2 events (end + start), got %d", len(events4))
		}

		// Første event skal være RequestEnd
		if events4[0].Type != RequestEnd {
			t.Errorf("First event should be RequestEnd, got %v", events4[0].Type)
		}

		// Andet event skal være RequestStart
		if events4[1].Type != RequestStart {
			t.Errorf("Second event should be RequestStart, got %v", events4[1].Type)
		}

		t.Logf("✅ Request boundary detected: End at %v, Start at %v",
			events4[0].EstimatedTime.Format("15:04:05.000"),
			events4[1].EstimatedTime.Format("15:04:05.000"))
	})

	t.Run("AdaptivePollingInterval", func(t *testing.T) {
		// Test forskellige scenarier for polling interval
		testCases := []struct {
			name          string
			activeWorkers int
			totalWorkers  int
			queueDepth    int
			recentEvents  int
			expectedRange [2]time.Duration // min, max
		}{
			{
				name:          "Low Activity",
				activeWorkers: 1,
				totalWorkers:  10,
				queueDepth:    0,
				recentEvents:  0,
				expectedRange: [2]time.Duration{300 * time.Millisecond, 500 * time.Millisecond},
			},
			{
				name:          "High Activity",
				activeWorkers: 8,
				totalWorkers:  10,
				queueDepth:    15,
				recentEvents:  5,
				expectedRange: [2]time.Duration{10 * time.Millisecond, 50 * time.Millisecond},
			},
			{
				name:          "Medium Activity",
				activeWorkers: 5,
				totalWorkers:  10,
				queueDepth:    3,
				recentEvents:  2,
				expectedRange: [2]time.Duration{75 * time.Millisecond, 150 * time.Millisecond},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				metrics := &WorkerProcessMetrics{
					ActiveWorkers: tc.activeWorkers,
					TotalWorkers:  tc.totalWorkers,
					QueueDepth:    tc.queueDepth,
				}

				// Simulér recent events
				recentEvents := make([]RequestEvent, tc.recentEvents)

				interval := tracker.GetOptimalPollingInterval(metrics, recentEvents)

				if interval < tc.expectedRange[0] || interval > tc.expectedRange[1] {
					t.Errorf("Polling interval %v outside expected range [%v, %v]",
						interval, tc.expectedRange[0], tc.expectedRange[1])
				}

				t.Logf("✅ %s: Polling interval %v (activity: %d/%d, queue: %d)",
					tc.name, interval, tc.activeWorkers, tc.totalWorkers, tc.queueDepth)
			})
		}
	})

	t.Run("TimingConfidenceCalculation", func(t *testing.T) {
		// Test confidence beregning baseret på poll interval
		testIntervals := []struct {
			interval      time.Duration
			minConfidence float64
		}{
			{10 * time.Millisecond, 0.90},
			{50 * time.Millisecond, 0.80},
			{100 * time.Millisecond, 0.70},
			{200 * time.Millisecond, 0.60},
			{500 * time.Millisecond, 0.45},
		}

		for _, test := range testIntervals {
			confidence := calculateConfidence(test.interval)
			if confidence < test.minConfidence {
				t.Errorf("Confidence %.2f too low for interval %v (expected >= %.2f)",
					confidence, test.interval, test.minConfidence)
			}

			t.Logf("✅ Interval %v: Confidence %.1f%%", test.interval, confidence*100)
		}
	})
}

// Helper function for absolute duration difference
func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
