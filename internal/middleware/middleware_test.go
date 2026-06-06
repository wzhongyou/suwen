package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiter_Allow(t *testing.T) {
	rl := NewRateLimiter(2, time.Second, 3)

	// Should allow burst up to burst size.
	for i := 0; i < 3; i++ {
		if !rl.Allow("127.0.0.1") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	// 4th request should be denied.
	if rl.Allow("127.0.0.1") {
		t.Fatal("4th request should be denied")
	}
}

func TestRateLimiter_Middleware(t *testing.T) {
	rl := NewRateLimiter(1, time.Hour, 1)

	handler := RateLimit(rl)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request passes.
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Second request blocked.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
}

func TestAuth_NoKeys(t *testing.T) {
	a := NewAuth(nil)
	req := httptest.NewRequest("GET", "/", nil)
	if !a.Authenticate(req) {
		t.Fatal("auth with no keys should pass")
	}
}

func TestAuth_ValidKey(t *testing.T) {
	a := NewAuth([]string{"secret-key"})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer secret-key")

	if !a.Authenticate(req) {
		t.Fatal("valid key should pass")
	}
}

func TestAuth_InvalidKey(t *testing.T) {
	a := NewAuth([]string{"secret-key"})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")

	if a.Authenticate(req) {
		t.Fatal("invalid key should fail")
	}
}

func TestAuth_MissingHeader(t *testing.T) {
	a := NewAuth([]string{"secret-key"})

	req := httptest.NewRequest("GET", "/", nil)
	if a.Authenticate(req) {
		t.Fatal("missing header should fail")
	}
}

func TestAuth_Middleware(t *testing.T) {
	a := NewAuth([]string{"test-key"})

	handler := RequireAuth(a)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// No auth header.
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	// Valid auth.
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestMetrics_Record(t *testing.T) {
	m := NewMetrics()

	m.Record("/api/v1/search", 100*time.Millisecond, false)
	m.Record("/api/v1/search", 200*time.Millisecond, false)
	m.Record("/api/v1/search", 300*time.Millisecond, true)

	snap := m.Snapshot()
	if snap.TotalRequests != 3 {
		t.Fatalf("expected 3 requests, got %d", snap.TotalRequests)
	}
	if snap.TotalErrors != 1 {
		t.Fatalf("expected 1 error, got %d", snap.TotalErrors)
	}
	if snap.AvgLatencyMS < 190 || snap.AvgLatencyMS > 210 {
		t.Fatalf("expected ~200ms avg latency, got %.2f", snap.AvgLatencyMS)
	}
}

func TestMetrics_LLMCost(t *testing.T) {
	m := NewMetrics()

	m.RecordLLMCall(0.001)
	m.RecordLLMCall(0.002)

	snap := m.Snapshot()
	if snap.LLMCalls != 2 {
		t.Fatalf("expected 2 LLM calls, got %d", snap.LLMCalls)
	}
	if snap.LLMCostUSD != 0.003 {
		t.Fatalf("expected $0.003 cost, got $%.6f", snap.LLMCostUSD)
	}
}

func TestMetricsMiddleware(t *testing.T) {
	m := NewMetrics()
	handler := MetricsMiddleware(m)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/search", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	snap := m.Snapshot()
	if snap.TotalRequests != 1 {
		t.Fatalf("expected 1 request, got %d", snap.TotalRequests)
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name     string
		xff      string
		xri      string
		remote   string
		expected string
	}{
		{"xff", "10.0.0.1, 10.0.0.2", "", "", "10.0.0.1"},
		{"xri", "", "10.0.0.3", "", "10.0.0.3"},
		{"remote", "", "", "10.0.0.4:56789", "10.0.0.4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.xri != "" {
				req.Header.Set("X-Real-IP", tt.xri)
			}
			req.RemoteAddr = tt.remote
			if got := clientIP(req); got != tt.expected {
				t.Fatalf("expected %s, got %s", tt.expected, got)
			}
		})
	}
}

func TestRequestLog(t *testing.T) {
	handler := RequestLog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
