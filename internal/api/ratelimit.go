package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// RateLimiter implements token bucket rate limiting with multiple strategies
type RateLimiter struct {
	mu       sync.RWMutex
	buckets  map[string]*TokenBucket
	config   *RateLimitConfig
	logger   *zap.Logger
	cleanup  *time.Ticker
	stopChan chan struct{}
}

// RateLimitConfig defines rate limiting configuration
type RateLimitConfig struct {
	// Global rate limits
	GlobalRequestsPerSecond int           `json:"global_requests_per_second"`
	GlobalBurstSize         int           `json:"global_burst_size"`
	GlobalWindowDuration    time.Duration `json:"global_window_duration"`

	// Per-IP rate limits
	IPRequestsPerSecond int           `json:"ip_requests_per_second"`
	IPBurstSize         int           `json:"ip_burst_size"`
	IPWindowDuration    time.Duration `json:"ip_window_duration"`

	// Per-endpoint rate limits
	EndpointLimits map[string]EndpointLimit `json:"endpoint_limits"`

	// Cleanup and configuration
	CleanupInterval  time.Duration `json:"cleanup_interval"`
	BucketTTL        time.Duration `json:"bucket_ttl"`
	EnableHeaderInfo bool          `json:"enable_header_info"`
	TrustedProxies   []string      `json:"trusted_proxies"`
	WhitelistedIPs   []string      `json:"whitelisted_ips"`
	MaxMemoryBuckets int           `json:"max_memory_buckets"`
}

// EndpointLimit defines rate limits for specific API endpoints
type EndpointLimit struct {
	RequestsPerSecond int           `json:"requests_per_second"`
	BurstSize         int           `json:"burst_size"`
	WindowDuration    time.Duration `json:"window_duration"`
}

// TokenBucket implements token bucket algorithm for rate limiting
type TokenBucket struct {
	mu               sync.Mutex
	tokens           float64
	capacity         float64
	refillRate       float64
	lastRefill       time.Time
	windowStart      time.Time
	windowDuration   time.Duration
	requestsInWindow int
	maxRequests      int
}

// RateLimitResult contains the result of a rate limit check
type RateLimitResult struct {
	Allowed         bool          `json:"allowed"`
	Remaining       int           `json:"remaining"`
	ResetTime       time.Time     `json:"reset_time"`
	RetryAfter      time.Duration `json:"retry_after"`
	LimitType       string        `json:"limit_type"`
	WindowRemaining time.Duration `json:"window_remaining"`
}

// NewRateLimiter creates a new rate limiter with the given configuration
func NewRateLimiter(config *RateLimitConfig, logger *zap.Logger) *RateLimiter {
	rl := &RateLimiter{
		buckets:  make(map[string]*TokenBucket),
		config:   config,
		logger:   logger,
		stopChan: make(chan struct{}),
	}

	// Start cleanup routine
	if config.CleanupInterval > 0 {
		rl.cleanup = time.NewTicker(config.CleanupInterval)
		go rl.cleanupRoutine()
	}

	return rl
}

// NewTokenBucket creates a new token bucket with the given parameters
func NewTokenBucket(capacity, refillRate float64, windowDuration time.Duration, maxRequests int) *TokenBucket {
	now := time.Now()
	return &TokenBucket{
		tokens:           capacity,
		capacity:         capacity,
		refillRate:       refillRate,
		lastRefill:       now,
		windowStart:      now,
		windowDuration:   windowDuration,
		requestsInWindow: 0,
		maxRequests:      maxRequests,
	}
}

// TryConsume attempts to consume tokens from the bucket
func (tb *TokenBucket) TryConsume(tokens float64) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()

	// Refill tokens based on time elapsed
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.tokens = min(tb.capacity, tb.tokens+elapsed*tb.refillRate)
	tb.lastRefill = now

	// Check sliding window if enabled
	if tb.windowDuration > 0 {
		// Reset window if expired
		if now.Sub(tb.windowStart) >= tb.windowDuration {
			tb.windowStart = now
			tb.requestsInWindow = 0
		}

		// Check window limit
		if tb.requestsInWindow >= tb.maxRequests {
			return false
		}
	}

	// Check token availability
	if tb.tokens >= tokens {
		tb.tokens -= tokens
		if tb.windowDuration > 0 {
			tb.requestsInWindow++
		}
		return true
	}

	return false
}

// GetStatus returns current bucket status
func (tb *TokenBucket) GetStatus() (remaining int, resetTime time.Time, windowRemaining time.Duration) {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()

	// Calculate remaining tokens
	elapsed := now.Sub(tb.lastRefill).Seconds()
	currentTokens := min(tb.capacity, tb.tokens+elapsed*tb.refillRate)
	remaining = int(currentTokens)

	// Calculate reset time (when bucket will be full)
	if currentTokens < tb.capacity {
		tokensNeeded := tb.capacity - currentTokens
		secondsToFull := tokensNeeded / tb.refillRate
		resetTime = now.Add(time.Duration(secondsToFull * float64(time.Second)))
	} else {
		resetTime = now
	}

	// Calculate window remaining time
	if tb.windowDuration > 0 {
		windowRemaining = tb.windowDuration - now.Sub(tb.windowStart)
		if windowRemaining < 0 {
			windowRemaining = 0
		}
	}

	return remaining, resetTime, windowRemaining
}

// CheckRateLimit checks if a request should be allowed based on multiple criteria
func (rl *RateLimiter) CheckRateLimit(ctx context.Context, clientIP, endpoint string) *RateLimitResult {
	// Check if IP is whitelisted
	if rl.isWhitelisted(clientIP) {
		return &RateLimitResult{
			Allowed:   true,
			Remaining: 999999,
			ResetTime: time.Now().Add(time.Hour),
			LimitType: "whitelisted",
		}
	}

	// Check global rate limit
	if rl.config.GlobalRequestsPerSecond > 0 {
		globalKey := "global"
		if !rl.checkBucket(globalKey, "global", 1) {
			return rl.createDeniedResult("global")
		}
	}

	// Check per-IP rate limit
	if rl.config.IPRequestsPerSecond > 0 {
		ipKey := fmt.Sprintf("ip:%s", clientIP)
		if !rl.checkBucket(ipKey, "ip", 1) {
			return rl.createDeniedResult("ip")
		}
	}

	// Check per-endpoint rate limit
	if endpointLimit, exists := rl.config.EndpointLimits[endpoint]; exists {
		endpointKey := fmt.Sprintf("endpoint:%s", endpoint)
		if !rl.checkBucketWithConfig(endpointKey, "endpoint", 1,
			float64(endpointLimit.BurstSize), float64(endpointLimit.RequestsPerSecond),
			endpointLimit.WindowDuration, endpointLimit.RequestsPerSecond) {
			return rl.createDeniedResult("endpoint")
		}
	}

	// All checks passed
	return &RateLimitResult{
		Allowed:   true,
		LimitType: "allowed",
	}
}

// checkBucket checks and consumes from a bucket with default IP configuration
func (rl *RateLimiter) checkBucket(key, bucketType string, tokens float64) bool {
	return rl.checkBucketWithConfig(key, bucketType, tokens,
		float64(rl.config.IPBurstSize), float64(rl.config.IPRequestsPerSecond),
		rl.config.IPWindowDuration, rl.config.IPRequestsPerSecond)
}

// checkBucketWithConfig checks and consumes from a bucket with custom configuration
func (rl *RateLimiter) checkBucketWithConfig(key, bucketType string, tokens, capacity, refillRate float64, windowDuration time.Duration, maxRequests int) bool {
	rl.mu.Lock()
	bucket, exists := rl.buckets[key]
	if !exists {
		bucket = NewTokenBucket(capacity, refillRate, windowDuration, maxRequests)
		rl.buckets[key] = bucket

		// Check memory limits
		if len(rl.buckets) > rl.config.MaxMemoryBuckets {
			rl.cleanupOldBuckets()
		}
	}
	rl.mu.Unlock()

	return bucket.TryConsume(tokens)
}

// createDeniedResult creates a rate limit denied result
func (rl *RateLimiter) createDeniedResult(limitType string) *RateLimitResult {
	var bucket *TokenBucket
	var key string

	switch limitType {
	case "global":
		key = "global"
	case "ip":
		// For denied result, we need to find the bucket, but this is simplified
		key = "ip:unknown"
	case "endpoint":
		key = "endpoint:unknown"
	}

	rl.mu.RLock()
	bucket = rl.buckets[key]
	rl.mu.RUnlock()

	if bucket != nil {
		remaining, resetTime, windowRemaining := bucket.GetStatus()
		retryAfter := time.Until(resetTime)
		if windowRemaining > 0 && windowRemaining < retryAfter {
			retryAfter = windowRemaining
		}

		return &RateLimitResult{
			Allowed:         false,
			Remaining:       remaining,
			ResetTime:       resetTime,
			RetryAfter:      retryAfter,
			LimitType:       limitType,
			WindowRemaining: windowRemaining,
		}
	}

	// Fallback result
	return &RateLimitResult{
		Allowed:    false,
		Remaining:  0,
		ResetTime:  time.Now().Add(time.Minute),
		RetryAfter: time.Minute,
		LimitType:  limitType,
	}
}

// isWhitelisted checks if an IP is in the whitelist
func (rl *RateLimiter) isWhitelisted(clientIP string) bool {
	for _, whitelistedIP := range rl.config.WhitelistedIPs {
		if clientIP == whitelistedIP {
			return true
		}
		// Support CIDR notation
		if _, network, err := net.ParseCIDR(whitelistedIP); err == nil {
			if ip := net.ParseIP(clientIP); ip != nil {
				if network.Contains(ip) {
					return true
				}
			}
		}
	}
	return false
}

// cleanupRoutine periodically cleans up old buckets
func (rl *RateLimiter) cleanupRoutine() {
	defer rl.cleanup.Stop()

	for {
		select {
		case <-rl.cleanup.C:
			rl.cleanupOldBuckets()
		case <-rl.stopChan:
			return
		}
	}
}

// cleanupOldBuckets removes old unused buckets to prevent memory leaks
func (rl *RateLimiter) cleanupOldBuckets() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for key, bucket := range rl.buckets {
		bucket.mu.Lock()
		lastUsed := bucket.lastRefill
		bucket.mu.Unlock()

		if now.Sub(lastUsed) > rl.config.BucketTTL {
			delete(rl.buckets, key)
		}
	}

	rl.logger.Debug("Rate limit cleanup completed",
		zap.Int("remaining_buckets", len(rl.buckets)))
}

// getClientIP extracts the real client IP considering trusted proxies
func (rl *RateLimiter) getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header if behind trusted proxy
	if rl.isTrustedProxy(r.RemoteAddr) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// Get the first IP in the chain
			ips := strings.Split(xff, ",")
			return strings.TrimSpace(ips[0])
		}
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return strings.TrimSpace(xri)
		}
	}

	// Extract IP from RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// isTrustedProxy checks if the request comes from a trusted proxy
func (rl *RateLimiter) isTrustedProxy(remoteAddr string) bool {
	ip, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		ip = remoteAddr
	}

	for _, trustedProxy := range rl.config.TrustedProxies {
		if ip == trustedProxy {
			return true
		}
		// Support CIDR notation
		if _, network, err := net.ParseCIDR(trustedProxy); err == nil {
			if clientIP := net.ParseIP(ip); clientIP != nil {
				if network.Contains(clientIP) {
					return true
				}
			}
		}
	}
	return false
}

// Stop stops the rate limiter cleanup routine
func (rl *RateLimiter) Stop() {
	close(rl.stopChan)
}

// RateLimitMiddleware creates HTTP middleware for rate limiting
func (rl *RateLimiter) RateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIP := rl.getClientIP(r)
		endpoint := r.URL.Path

		result := rl.CheckRateLimit(r.Context(), clientIP, endpoint)

		// Add rate limit headers if enabled
		if rl.config.EnableHeaderInfo {
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(rl.config.IPRequestsPerSecond))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(result.ResetTime.Unix(), 10))
			if !result.Allowed {
				w.Header().Set("Retry-After", strconv.Itoa(int(result.RetryAfter.Seconds())))
			}
		}

		if !result.Allowed {
			// Log rate limit violation
			rl.logger.Warn("Rate limit exceeded",
				zap.String("client_ip", clientIP),
				zap.String("endpoint", endpoint),
				zap.String("limit_type", result.LimitType),
				zap.Duration("retry_after", result.RetryAfter))

			w.WriteHeader(http.StatusTooManyRequests)

			// Return structured error response
			errorResponse := map[string]interface{}{
				"error": map[string]interface{}{
					"code":        "rate_limited",
					"message":     "Rate limit exceeded",
					"limit_type":  result.LimitType,
					"retry_after": int(result.RetryAfter.Seconds()),
					"reset_time":  result.ResetTime.Unix(),
				},
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(errorResponse)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// DefaultRateLimitConfig returns a sensible default configuration
func DefaultRateLimitConfig() *RateLimitConfig {
	return &RateLimitConfig{
		GlobalRequestsPerSecond: 1000,
		GlobalBurstSize:         100,
		GlobalWindowDuration:    time.Minute,

		IPRequestsPerSecond: 60,
		IPBurstSize:         10,
		IPWindowDuration:    time.Minute,

		EndpointLimits: map[string]EndpointLimit{
			"/api/v1/reload": {
				RequestsPerSecond: 5,
				BurstSize:         2,
				WindowDuration:    time.Minute,
			},
			"/api/v1/restart": {
				RequestsPerSecond: 2,
				BurstSize:         1,
				WindowDuration:    time.Minute * 5,
			},
			"/api/v1/pools/scale": {
				RequestsPerSecond: 10,
				BurstSize:         3,
				WindowDuration:    time.Minute,
			},
		},

		CleanupInterval:  time.Minute * 5,
		BucketTTL:        time.Minute * 10,
		EnableHeaderInfo: true,
		TrustedProxies:   []string{"127.0.0.1", "::1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
		WhitelistedIPs:   []string{},
		MaxMemoryBuckets: 10000,
	}
}

// helper function for min
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
