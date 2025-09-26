package metrics

import (
	"sync"
)

// StringInterner provides string interning to reduce memory allocations
// for frequently used strings like pool names, metric names, and labels.
type StringInterner struct {
	mu    sync.RWMutex
	cache map[string]string
}

// NewStringInterner creates a new string interner with pre-allocated capacity.
func NewStringInterner(capacity int) *StringInterner {
	return &StringInterner{
		cache: make(map[string]string, capacity),
	}
}

// Intern returns a canonical instance of the string, reducing memory
// allocations for duplicate strings. This is particularly effective
// for metric names, pool names, and label keys that are repeated frequently.
//
// Performance characteristics:
//   - First occurrence: Allocates and stores string
//   - Subsequent occurrences: Returns cached string (zero allocation)
//   - Thread-safe through RWMutex with optimized read path
//
// Example usage:
//
//	poolName := interner.Intern("web-pool")  // Allocated once
//	poolName2 := interner.Intern("web-pool") // Returns cached string
func (si *StringInterner) Intern(s string) string {
	// Fast path: read-only lookup
	si.mu.RLock()
	if cached, exists := si.cache[s]; exists {
		si.mu.RUnlock()
		return cached
	}
	si.mu.RUnlock()

	// Slow path: write to cache
	si.mu.Lock()
	defer si.mu.Unlock()

	// Double-check in case another goroutine added it
	if cached, exists := si.cache[s]; exists {
		return cached
	}

	// Make a copy to ensure we own the string
	// This prevents memory leaks if the input string is part of a larger slice
	interned := string([]byte(s))
	si.cache[interned] = interned
	return interned
}

// Size returns the number of interned strings.
func (si *StringInterner) Size() int {
	si.mu.RLock()
	defer si.mu.RUnlock()
	return len(si.cache)
}

// Clear removes all interned strings. Use with caution as this will
// invalidate all previously returned string references.
func (si *StringInterner) Clear() {
	si.mu.Lock()
	defer si.mu.Unlock()
	si.cache = make(map[string]string, len(si.cache))
}

// CommonMetricNames provides pre-interned strings for frequently used metric names
// to reduce allocation overhead in hot paths.
type CommonMetricNames struct {
	interner *StringInterner

	// Pool metrics
	ActiveProcesses     string
	IdleProcesses       string
	TotalProcesses      string
	AcceptedConnections string
	SlowRequests        string
	RequestDuration     string

	// System metrics
	CPUUsage    string
	MemoryUsage string
	DiskUsage   string
	NetworkIn   string
	NetworkOut  string

	// Label keys
	PoolLabel   string
	TypeLabel   string
	StatusLabel string
}

// NewCommonMetricNames creates pre-interned common metric names for optimal performance.
func NewCommonMetricNames(interner *StringInterner) *CommonMetricNames {
	return &CommonMetricNames{
		interner: interner,

		// Pre-intern common metric names
		ActiveProcesses:     interner.Intern("active_processes"),
		IdleProcesses:       interner.Intern("idle_processes"),
		TotalProcesses:      interner.Intern("total_processes"),
		AcceptedConnections: interner.Intern("accepted_connections"),
		SlowRequests:        interner.Intern("slow_requests"),
		RequestDuration:     interner.Intern("request_duration"),

		CPUUsage:    interner.Intern("cpu_usage"),
		MemoryUsage: interner.Intern("memory_usage"),
		DiskUsage:   interner.Intern("disk_usage"),
		NetworkIn:   interner.Intern("network_in"),
		NetworkOut:  interner.Intern("network_out"),

		PoolLabel:   interner.Intern("pool"),
		TypeLabel:   interner.Intern("type"),
		StatusLabel: interner.Intern("status"),
	}
}

// GlobalStringInterner provides a package-level interner for common strings
var GlobalStringInterner = NewStringInterner(256)

// GlobalMetricNames provides pre-interned common metric names
var GlobalMetricNames = NewCommonMetricNames(GlobalStringInterner)
