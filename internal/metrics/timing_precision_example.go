package metrics

import (
	"time"

	"go.uber.org/zap"
)

// TimingPrecisionExample viser hvordan vi kan forbedre timing kun med polling
type TimingPrecisionExample struct {
	logger   *zap.Logger
	lastSeen map[int]*ProcessSnapshot
}

type ProcessSnapshot struct {
	PID             int
	State           PHPFPMProcessState
	RequestDuration int64 // milliseconds
	RequestURI      string
	Timestamp       time.Time
}

// LØSNING 1: REQUEST BOUNDARY DETECTION
// Detect når RequestDuration resetter = ny request starter
func (t *TimingPrecisionExample) DetectRequestBoundaries(processes []PHPFPMProcess, pollTime time.Time) []RequestEvent {
	var events []RequestEvent

	for _, process := range processes {
		last, exists := t.lastSeen[process.PID]

		if exists {
			// CASE 1: RequestDuration decreased = Previous request ended, new started
			if process.RequestDuration < last.RequestDuration {
				// Previous request finished between polls
				prevRequestEnd := RequestEvent{
					PID:           process.PID,
					Type:          RequestEnd,
					EstimatedTime: estimateEventTime(last, pollTime, 0.7), // 70% through interval
					RequestURI:    last.RequestURI,
					Duration:      time.Duration(last.RequestDuration) * time.Millisecond,
					Confidence:    calculateConfidence(pollTime.Sub(last.Timestamp)),
				}
				events = append(events, prevRequestEnd)

				// New request started
				newRequestStart := RequestEvent{
					PID:           process.PID,
					Type:          RequestStart,
					EstimatedTime: estimateEventTime(last, pollTime, 0.8), // 80% through interval
					RequestURI:    process.RequestURI,
					Confidence:    calculateConfidence(pollTime.Sub(last.Timestamp)),
				}
				events = append(events, newRequestStart)
			}

			// CASE 2: State change from Idle to Running = Request start
			if last.State == ProcessStateIdle &&
				(process.State == ProcessStateRunning || process.State == ProcessStateReading) {
				requestStart := RequestEvent{
					PID:           process.PID,
					Type:          RequestStart,
					EstimatedTime: estimateEventTime(last, pollTime, 0.5), // Midpoint estimate
					RequestURI:    process.RequestURI,
					Confidence:    calculateConfidence(pollTime.Sub(last.Timestamp)),
				}
				events = append(events, requestStart)
			}

			// CASE 3: State change from Running to Idle = Request end
			if (last.State == ProcessStateRunning || last.State == ProcessStateFinishing) &&
				process.State == ProcessStateIdle {
				requestEnd := RequestEvent{
					PID:           process.PID,
					Type:          RequestEnd,
					EstimatedTime: estimateEventTime(last, pollTime, 0.5),
					RequestURI:    last.RequestURI,
					Duration:      time.Duration(process.RequestDuration) * time.Millisecond,
					Confidence:    calculateConfidence(pollTime.Sub(last.Timestamp)),
				}
				events = append(events, requestEnd)
			}
		}

		// Update tracking
		t.lastSeen[process.PID] = &ProcessSnapshot{
			PID:             process.PID,
			State:           process.State,
			RequestDuration: process.RequestDuration,
			RequestURI:      process.RequestURI,
			Timestamp:       pollTime,
		}
	}

	return events
}

// LØSNING 2: ADAPTIVE POLLING FREQUENCY
func (t *TimingPrecisionExample) GetOptimalPollingInterval(metrics *WorkerProcessMetrics, recentEvents []RequestEvent) time.Duration {
	baseInterval := 200 * time.Millisecond

	// Factor 1: Current activity level
	activityMultiplier := 1.0
	if metrics.ActiveWorkers > 0 {
		utilizationRate := float64(metrics.ActiveWorkers) / float64(metrics.TotalWorkers)
		switch {
		case utilizationRate >= 0.8:
			activityMultiplier = 0.25 // 4x faster (50ms)
		case utilizationRate >= 0.5:
			activityMultiplier = 0.5 // 2x faster (100ms)
		case utilizationRate >= 0.2:
			activityMultiplier = 0.75 // 1.3x faster (150ms)
		default:
			activityMultiplier = 2.0 // 2x slower (400ms)
		}
	}

	// Factor 2: Queue pressure
	queueMultiplier := 1.0
	if metrics.QueueDepth > 5 {
		queueMultiplier = 0.5 // 2x faster når queue bygger op
	}

	// Factor 3: Recent event density
	eventMultiplier := 1.0
	if len(recentEvents) > 3 {
		eventMultiplier = 0.3 // 3x faster når mange events detekteres
	}

	// Kombinér faktorer
	finalMultiplier := activityMultiplier * queueMultiplier * eventMultiplier
	newInterval := time.Duration(float64(baseInterval) * finalMultiplier)

	// Begræns til fornuftige værdier
	if newInterval < 10*time.Millisecond {
		return 10 * time.Millisecond // Minimum 10ms
	}
	if newInterval > 2*time.Second {
		return 2 * time.Second // Maximum 2s
	}

	return newInterval
}

// LØSNING 3: SYSTEM-LEVEL TIMING CORRELATION
func (t *TimingPrecisionExample) CorrelateWithSystemMetrics(phpfpmEvents []RequestEvent, pid int) *PreciseExecutionTiming {
	// Hent system-level data fra /proc/PID/stat
	sysStats := readProcStats(pid)

	timing := &PreciseExecutionTiming{
		PID: pid,
	}

	// Kombinér PHP-FPM events med system timing
	for _, event := range phpfpmEvents {
		if event.PID == pid {
			switch event.Type {
			case RequestStart:
				timing.StartTime = event.EstimatedTime
				timing.StartCPUTime = sysStats.CPUTime
				timing.StartMemory = sysStats.Memory

			case RequestEnd:
				timing.EndTime = event.EstimatedTime
				timing.EndCPUTime = sysStats.CPUTime
				timing.EndMemory = sysStats.Memory

				// Beregn præcise resource metrics
				timing.ActualCPUUsage = timing.EndCPUTime - timing.StartCPUTime
				timing.PeakMemoryUsage = maxInt64(timing.StartMemory, timing.EndMemory)
				timing.ExecutionDuration = timing.EndTime.Sub(timing.StartTime)
			}
		}
	}

	return timing
}

// Helper functions
func estimateEventTime(lastSnapshot *ProcessSnapshot, pollTime time.Time, fraction float64) time.Time {
	interval := pollTime.Sub(lastSnapshot.Timestamp)
	return lastSnapshot.Timestamp.Add(time.Duration(float64(interval) * fraction))
}

func calculateConfidence(pollInterval time.Duration) float64 {
	switch {
	case pollInterval <= 20*time.Millisecond:
		return 0.95
	case pollInterval <= 50*time.Millisecond:
		return 0.85
	case pollInterval <= 100*time.Millisecond:
		return 0.75
	case pollInterval <= 200*time.Millisecond:
		return 0.65
	default:
		return 0.50
	}
}

// Data structures
type RequestEventType int

const (
	RequestStart RequestEventType = iota
	RequestEnd
)

type RequestEvent struct {
	PID           int
	Type          RequestEventType
	EstimatedTime time.Time
	RequestURI    string
	Duration      time.Duration
	Confidence    float64
}

type PreciseExecutionTiming struct {
	PID               int
	StartTime         time.Time
	EndTime           time.Time
	StartCPUTime      int64
	EndCPUTime        int64
	StartMemory       int64
	EndMemory         int64
	ActualCPUUsage    int64
	PeakMemoryUsage   int64
	ExecutionDuration time.Duration
}

// System stats reading (placeholder)
type SystemStats struct {
	CPUTime int64
	Memory  int64
}

func readProcStats(pid int) *SystemStats {
	// Implementation would read /proc/PID/stat and /proc/PID/status
	return &SystemStats{}
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
