// Package middleware provides HTTP middleware for rate limiting, auth, and metrics.
package middleware

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ---- Rate Limiter (Token Bucket) ----

// RateLimiter implements a per-IP token bucket rate limiter.
type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*tokenBucket
	rate     int           // tokens per interval
	interval time.Duration // refill interval
	burst    int           // max tokens
}

type tokenBucket struct {
	tokens   float64
	lastTime time.Time
}

// NewRateLimiter creates a rate limiter.
// rate: max requests per interval. burst: max burst size.
func NewRateLimiter(rate int, interval time.Duration, burst int) *RateLimiter {
	rl := &RateLimiter{
		buckets:  make(map[string]*tokenBucket),
		rate:     rate,
		interval: interval,
		burst:    burst,
	}
	// Clean stale buckets periodically.
	go func() {
		for {
			time.Sleep(10 * time.Minute)
			rl.mu.Lock()
			for ip, b := range rl.buckets {
				if time.Since(b.lastTime) > 10*time.Minute {
					delete(rl.buckets, ip)
				}
			}
			rl.mu.Unlock()
		}
	}()
	return rl
}

// Allow checks if a request from ip is allowed.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[ip]
	if !ok {
		b = &tokenBucket{
			tokens:   float64(rl.burst),
			lastTime: time.Now(),
		}
		rl.buckets[ip] = b
	}

	// Refill tokens based on elapsed time.
	elapsed := time.Since(b.lastTime).Seconds()
	b.tokens += elapsed * float64(rl.rate) / rl.interval.Seconds()
	if b.tokens > float64(rl.burst) {
		b.tokens = float64(rl.burst)
	}
	b.lastTime = time.Now()

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// RateLimit returns an HTTP middleware that rate-limits by client IP.
func RateLimit(rl *RateLimiter) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			if !rl.Allow(ip) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]string{
					"error": "rate limit exceeded, try again later",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ---- API Key Authentication ----

// Auth is a simple API key authenticator.
type Auth struct {
	keys map[string]bool
}

// NewAuth creates an Auth with the given API keys.
func NewAuth(keys []string) *Auth {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k != "" {
			m[k] = true
		}
	}
	return &Auth{keys: m}
}

// Authenticate checks the Authorization header for a valid Bearer token.
func (a *Auth) Authenticate(r *http.Request) bool {
	if len(a.keys) == 0 {
		return true // Auth disabled if no keys configured.
	}

	header := r.Header.Get("Authorization")
	if header == "" {
		return false
	}

	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return false
	}

	return a.keys[parts[1]]
}

// RequireAuth returns middleware that enforces API key authentication.
func RequireAuth(a *Auth) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !a.Authenticate(r) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{
					"error": "unauthorized: valid API key required",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ---- Metrics ----

// Metrics tracks request counts, latency, and LLM costs.
type Metrics struct {
	mu            sync.RWMutex
	totalRequests uint64
	totalErrors   uint64
	totalLatency  time.Duration
	llmCalls      uint64
	llmCostUSD    float64
	pathCounts    map[string]uint64
}

// NewMetrics creates a new Metrics tracker.
func NewMetrics() *Metrics {
	return &Metrics{
		pathCounts: make(map[string]uint64),
	}
}

// Record records a request.
func (m *Metrics) Record(path string, latency time.Duration, isError bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalRequests++
	m.totalLatency += latency
	m.pathCounts[path]++
	if isError {
		m.totalErrors++
	}
}

// RecordLLMCall records an LLM call with estimated cost.
func (m *Metrics) RecordLLMCall(costUSD float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.llmCalls++
	m.llmCostUSD += costUSD
}

// Snapshot returns a point-in-time copy of all metrics.
func (m *Metrics) Snapshot() MetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	avgLat := time.Duration(0)
	if m.totalRequests > 0 {
		avgLat = m.totalLatency / time.Duration(m.totalRequests)
	}

	paths := make(map[string]uint64, len(m.pathCounts))
	for k, v := range m.pathCounts {
		paths[k] = v
	}

	return MetricsSnapshot{
		TotalRequests: m.totalRequests,
		TotalErrors:   m.totalErrors,
		AvgLatencyMS:  float64(avgLat.Microseconds()) / 1000.0,
		LLMCalls:      m.llmCalls,
		LLMCostUSD:    m.llmCostUSD,
		PathCounts:    paths,
	}
}

// MetricsSnapshot is a point-in-time snapshot of metrics.
type MetricsSnapshot struct {
	TotalRequests uint64            `json:"total_requests"`
	TotalErrors   uint64            `json:"total_errors"`
	AvgLatencyMS  float64           `json:"avg_latency_ms"`
	LLMCalls      uint64            `json:"llm_calls"`
	LLMCostUSD    float64           `json:"llm_cost_usd"`
	PathCounts    map[string]uint64 `json:"path_counts"`
}

// MetricsMiddleware injects request counting and latency tracking.
func MetricsMiddleware(m *Metrics) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wr := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(wr, r)
			latency := time.Since(start)
			isError := wr.statusCode >= 400
			m.Record(r.URL.Path, latency, isError)
		})
	}
}

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (rr *responseRecorder) WriteHeader(code int) {
	rr.statusCode = code
	rr.ResponseWriter.WriteHeader(code)
}

// ---- Utilities ----

// clientIP extracts the client IP from a request.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	// Strip port from RemoteAddr.
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}

// ---- Logging Middleware ----

// RequestLog logs each request with method, path, status, and duration.
func RequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rr := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rr, r)
		log.Printf("[http] %s %s %d %s", r.Method, r.URL.Path, rr.statusCode, time.Since(start).Round(time.Microsecond))
	})
}
