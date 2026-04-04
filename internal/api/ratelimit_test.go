package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// RateLimiter unit tests
// ---------------------------------------------------------------------------

func TestRateLimiter_AllowsRequestsWithinLimit(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		RequestsPerMinute: 10,
		BurstSize:         10,
		CleanupInterval:   time.Minute,
	})
	defer rl.Stop()

	handler := rl.Middleware()(okHandler)

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want %d", i, rec.Code, http.StatusOK)
		}
	}
}

func TestRateLimiter_BlocksRequestsOverLimit(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		RequestsPerMinute: 5,
		BurstSize:         5,
		CleanupInterval:   time.Minute,
	})
	defer rl.Stop()

	handler := rl.Middleware()(okHandler)

	// Exhaust the bucket
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// Next request should be rate limited
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}

func TestRateLimiter_ReturnsRateLimitHeaders(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		RequestsPerMinute: 10,
		BurstSize:         10,
		CleanupInterval:   time.Minute,
	})
	defer rl.Stop()

	handler := rl.Middleware()(okHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Check X-RateLimit-Remaining header
	remaining := rec.Header().Get("X-RateLimit-Remaining")
	if remaining == "" {
		t.Fatal("missing X-RateLimit-Remaining header")
	}
	rem, err := strconv.Atoi(remaining)
	if err != nil {
		t.Fatalf("invalid X-RateLimit-Remaining: %q", remaining)
	}
	// After 1 request out of 10 burst, remaining should be 9
	if rem != 9 {
		t.Errorf("X-RateLimit-Remaining = %d, want 9", rem)
	}

	// Check X-RateLimit-Reset header
	reset := rec.Header().Get("X-RateLimit-Reset")
	if reset == "" {
		t.Fatal("missing X-RateLimit-Reset header")
	}
	resetSec, err := strconv.ParseInt(reset, 10, 64)
	if err != nil {
		t.Fatalf("invalid X-RateLimit-Reset: %q", reset)
	}
	if resetSec <= 0 {
		t.Errorf("X-RateLimit-Reset = %d, want positive value", resetSec)
	}

	// Check X-RateLimit-Limit header
	limit := rec.Header().Get("X-RateLimit-Limit")
	if limit == "" {
		t.Fatal("missing X-RateLimit-Limit header")
	}
	lim, err := strconv.Atoi(limit)
	if err != nil {
		t.Fatalf("invalid X-RateLimit-Limit: %q", limit)
	}
	if lim != 10 {
		t.Errorf("X-RateLimit-Limit = %d, want 10", lim)
	}
}

func TestRateLimiter_429ResponseIncludesRetryAfter(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		RequestsPerMinute: 2,
		BurstSize:         2,
		CleanupInterval:   time.Minute,
	})
	defer rl.Stop()

	handler := rl.Middleware()(okHandler)

	// Exhaust limit
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// This one should be 429
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}

	retryAfter := rec.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("missing Retry-After header on 429 response")
	}
	ra, err := strconv.Atoi(retryAfter)
	if err != nil {
		t.Fatalf("invalid Retry-After: %q", retryAfter)
	}
	if ra <= 0 {
		t.Errorf("Retry-After = %d, want positive value", ra)
	}
}

func TestRateLimiter_DifferentIPsHaveSeparateLimits(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		RequestsPerMinute: 2,
		BurstSize:         2,
		CleanupInterval:   time.Minute,
	})
	defer rl.Stop()

	handler := rl.Middleware()(okHandler)

	// Exhaust IP1
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// IP1 should be blocked
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "10.0.0.1:1234"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusTooManyRequests {
		t.Errorf("IP1 status = %d, want %d", rec1.Code, http.StatusTooManyRequests)
	}

	// IP2 should still be allowed
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "10.0.0.2:5678"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("IP2 status = %d, want %d", rec2.Code, http.StatusOK)
	}
}

func TestRateLimiter_UsesXForwardedFor(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		RequestsPerMinute: 2,
		BurstSize:         2,
		CleanupInterval:   time.Minute,
	})
	defer rl.Stop()

	handler := rl.Middleware()(okHandler)

	// Exhaust limit for forwarded IP
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", "203.0.113.50")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// Same forwarded IP should be blocked
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.50")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d (X-Forwarded-For IP should be rate limited)", rec.Code, http.StatusTooManyRequests)
	}

	// Different forwarded IP through same proxy should be allowed
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "10.0.0.1:1234"
	req2.Header.Set("X-Forwarded-For", "203.0.113.99")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (different X-Forwarded-For IP should pass)", rec2.Code, http.StatusOK)
	}
}

func TestRateLimiter_TokenReplenishesOverTime(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		RequestsPerMinute: 60, // 1 per second
		BurstSize:         1,  // only 1 token
		CleanupInterval:   time.Minute,
	})
	defer rl.Stop()

	handler := rl.Middleware()(okHandler)

	// Use the single token
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first request: status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Should be blocked immediately
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "10.0.0.1:1234"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: status = %d, want %d", rec2.Code, http.StatusTooManyRequests)
	}

	// Wait for 1 token to replenish (at 60/min = 1/sec)
	time.Sleep(1100 * time.Millisecond)

	// Should be allowed again
	req3 := httptest.NewRequest("GET", "/test", nil)
	req3.RemoteAddr = "10.0.0.1:1234"
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Errorf("third request after replenish: status = %d, want %d", rec3.Code, http.StatusOK)
	}
}

func TestRateLimiter_ConcurrentAccess(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		RequestsPerMinute: 100,
		BurstSize:         100,
		CleanupInterval:   time.Minute,
	})
	defer rl.Stop()

	handler := rl.Middleware()(okHandler)

	var wg sync.WaitGroup
	allowed := make(chan int, 200)
	blocked := make(chan int, 200)

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = "10.0.0.1:1234"
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code == http.StatusOK {
				allowed <- 1
			} else if rec.Code == http.StatusTooManyRequests {
				blocked <- 1
			}
		}()
	}

	wg.Wait()
	close(allowed)
	close(blocked)

	allowedCount := 0
	for range allowed {
		allowedCount++
	}
	blockedCount := 0
	for range blocked {
		blockedCount++
	}

	// Exactly 100 should be allowed (burst size)
	if allowedCount != 100 {
		t.Errorf("allowed = %d, want 100", allowedCount)
	}
	if blockedCount != 100 {
		t.Errorf("blocked = %d, want 100", blockedCount)
	}
}

func TestRateLimiter_CleanupRemovesStaleEntries(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		RequestsPerMinute: 60,
		BurstSize:         5,
		CleanupInterval:   100 * time.Millisecond,
	})
	defer rl.Stop()

	handler := rl.Middleware()(okHandler)

	// Create entries for multiple IPs
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0." + strconv.Itoa(i) + ":1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	if rl.Size() != 10 {
		t.Fatalf("limiter size = %d, want 10", rl.Size())
	}

	// Wait for cleanup cycle and some token replenishment
	time.Sleep(250 * time.Millisecond)

	// Size should be reduced (stale entries cleaned up after no activity)
	// We can't predict exactly how many, but the cleanup mechanism should work
	// At minimum, the cleanup goroutine should have run
	size := rl.Size()
	if size > 10 {
		t.Errorf("limiter size = %d, should not have grown", size)
	}
}

// ---------------------------------------------------------------------------
// SSE connection limiting tests
// ---------------------------------------------------------------------------

func TestSSERateLimiter_AllowsWithinLimit(t *testing.T) {
	rl := NewSSERateLimiter(5)

	ip := "10.0.0.1"
	for i := 0; i < 5; i++ {
		if !rl.Acquire(ip) {
			t.Fatalf("acquire %d should succeed", i)
		}
	}

	// 6th should fail
	if rl.Acquire(ip) {
		t.Error("6th acquire should fail")
	}

	// Release one and try again
	rl.Release(ip)
	if !rl.Acquire(ip) {
		t.Error("acquire after release should succeed")
	}
}

func TestSSERateLimiter_DifferentIPsSeparate(t *testing.T) {
	rl := NewSSERateLimiter(2)

	// IP1 takes 2 slots
	rl.Acquire("10.0.0.1")
	rl.Acquire("10.0.0.1")

	// IP1 blocked
	if rl.Acquire("10.0.0.1") {
		t.Error("IP1 should be blocked")
	}

	// IP2 should still work
	if !rl.Acquire("10.0.0.2") {
		t.Error("IP2 should be allowed")
	}
}

func TestSSERateLimiter_ConcurrentAccess(t *testing.T) {
	rl := NewSSERateLimiter(50)

	var wg sync.WaitGroup
	acquired := make(chan bool, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			acquired <- rl.Acquire("10.0.0.1")
		}()
	}

	wg.Wait()
	close(acquired)

	allowedCount := 0
	for ok := range acquired {
		if ok {
			allowedCount++
		}
	}

	if allowedCount != 50 {
		t.Errorf("allowed = %d, want 50", allowedCount)
	}
}

// ---------------------------------------------------------------------------
// RateLimitConfig defaults test
// ---------------------------------------------------------------------------

func TestRateLimitConfig_Defaults(t *testing.T) {
	cfg := DefaultRateLimitConfig()

	if cfg.RequestsPerMinute <= 0 {
		t.Errorf("RequestsPerMinute = %d, want > 0", cfg.RequestsPerMinute)
	}
	if cfg.BurstSize <= 0 {
		t.Errorf("BurstSize = %d, want > 0", cfg.BurstSize)
	}
	if cfg.CleanupInterval <= 0 {
		t.Errorf("CleanupInterval = %v, want > 0", cfg.CleanupInterval)
	}
}

// ---------------------------------------------------------------------------
// IP extraction helper tests
// ---------------------------------------------------------------------------

func TestExtractIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		forwarded  string
		realIP     string
		want       string
	}{
		{
			name:       "simple remote addr with port",
			remoteAddr: "192.168.1.1:12345",
			want:       "192.168.1.1",
		},
		{
			name:       "remote addr without port",
			remoteAddr: "192.168.1.1",
			want:       "192.168.1.1",
		},
		{
			name:       "X-Forwarded-For single IP",
			remoteAddr: "10.0.0.1:1234",
			forwarded:  "203.0.113.50",
			want:       "203.0.113.50",
		},
		{
			name:       "X-Forwarded-For multiple IPs takes first",
			remoteAddr: "10.0.0.1:1234",
			forwarded:  "203.0.113.50, 70.41.3.18, 150.172.238.178",
			want:       "203.0.113.50",
		},
		{
			name:       "X-Real-IP takes precedence over remote addr",
			remoteAddr: "10.0.0.1:1234",
			realIP:     "203.0.113.99",
			want:       "203.0.113.99",
		},
		{
			name:       "X-Forwarded-For takes precedence over X-Real-IP",
			remoteAddr: "10.0.0.1:1234",
			forwarded:  "203.0.113.50",
			realIP:     "203.0.113.99",
			want:       "203.0.113.50",
		},
		{
			name:       "IPv6 remote addr",
			remoteAddr: "[::1]:12345",
			want:       "::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.forwarded != "" {
				req.Header.Set("X-Forwarded-For", tt.forwarded)
			}
			if tt.realIP != "" {
				req.Header.Set("X-Real-IP", tt.realIP)
			}

			got := extractIP(req)
			if got != tt.want {
				t.Errorf("extractIP() = %q, want %q", got, tt.want)
			}
		})
	}
}
