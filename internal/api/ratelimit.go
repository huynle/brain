package api

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimitConfig configures the token bucket rate limiter.
type RateLimitConfig struct {
	// RequestsPerMinute is the sustained rate of allowed requests per IP.
	RequestsPerMinute int
	// BurstSize is the maximum number of requests allowed in a burst.
	BurstSize int
	// CleanupInterval is how often stale buckets are removed from memory.
	CleanupInterval time.Duration
}

// DefaultRateLimitConfig returns sensible defaults for general API usage.
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerMinute: 100,
		BurstSize:         100,
		CleanupInterval:   5 * time.Minute,
	}
}

// tokenBucket implements a per-key token bucket.
type tokenBucket struct {
	tokens   float64
	maxToken float64
	rate     float64 // tokens per second
	lastTime time.Time
}

// allow checks if a request is allowed and consumes a token if so.
// Returns (allowed, remaining tokens, seconds until next token).
func (b *tokenBucket) allow(now time.Time) (bool, int, int) {
	elapsed := now.Sub(b.lastTime).Seconds()
	b.tokens += elapsed * b.rate
	if b.tokens > b.maxToken {
		b.tokens = b.maxToken
	}
	b.lastTime = now

	resetSec := 1 // minimum 1 second

	if b.tokens >= 1 {
		b.tokens--
		remaining := int(b.tokens)
		if remaining <= 0 {
			resetSec = int(math.Ceil(1.0 / b.rate))
		}
		return true, remaining, resetSec
	}

	// Denied — calculate time until next token
	deficit := 1.0 - b.tokens
	resetSec = int(math.Ceil(deficit / b.rate))
	if resetSec <= 0 {
		resetSec = 1
	}
	return false, 0, resetSec
}

// RateLimiter provides per-IP token bucket rate limiting.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	config  RateLimitConfig
	stopCh  chan struct{}
}

// NewRateLimiter creates and starts a RateLimiter with background cleanup.
func NewRateLimiter(cfg RateLimitConfig) *RateLimiter {
	rl := &RateLimiter{
		buckets: make(map[string]*tokenBucket),
		config:  cfg,
		stopCh:  make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

// Stop stops the background cleanup goroutine.
func (rl *RateLimiter) Stop() {
	select {
	case <-rl.stopCh:
		// already stopped
	default:
		close(rl.stopCh)
	}
}

// Size returns the number of tracked IPs (for testing/monitoring).
func (rl *RateLimiter) Size() int {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return len(rl.buckets)
}

// getBucket returns or creates a token bucket for the given key.
func (rl *RateLimiter) getBucket(key string) *tokenBucket {
	b, ok := rl.buckets[key]
	if !ok {
		b = &tokenBucket{
			tokens:   float64(rl.config.BurstSize),
			maxToken: float64(rl.config.BurstSize),
			rate:     float64(rl.config.RequestsPerMinute) / 60.0,
			lastTime: time.Now(),
		}
		rl.buckets[key] = b
	}
	return b
}

// Middleware returns an http middleware that enforces rate limits per IP.
func (rl *RateLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractIP(r)

			rl.mu.Lock()
			bucket := rl.getBucket(ip)
			allowed, remaining, resetSec := bucket.allow(time.Now())
			rl.mu.Unlock()

			// Always set rate limit headers
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(rl.config.BurstSize))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.Itoa(resetSec))

			if !allowed {
				w.Header().Set("Retry-After", strconv.Itoa(resetSec))
				WriteError(w, http.StatusTooManyRequests,
					"Too Many Requests",
					"Rate limit exceeded. Try again later.",
				)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// cleanupLoop periodically removes stale entries (buckets at max capacity).
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rl.stopCh:
			return
		case now := <-ticker.C:
			rl.mu.Lock()
			for key, b := range rl.buckets {
				// Replenish tokens to check if bucket is full
				elapsed := now.Sub(b.lastTime).Seconds()
				projected := b.tokens + elapsed*b.rate
				if projected >= b.maxToken {
					// Bucket is at max — no recent activity, safe to remove
					delete(rl.buckets, key)
				}
			}
			rl.mu.Unlock()
		}
	}
}

// extractIP extracts the client IP from the request, respecting proxy headers.
// Priority: X-Forwarded-For (first IP) > X-Real-IP > RemoteAddr.
func extractIP(r *http.Request) string {
	// X-Forwarded-For: client, proxy1, proxy2
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		ip := strings.TrimSpace(parts[0])
		if ip != "" {
			return ip
		}
	}

	// X-Real-IP
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return strings.TrimSpace(realIP)
	}

	// Fall back to RemoteAddr (strip port)
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---------------------------------------------------------------------------
// SSE connection limiter
// ---------------------------------------------------------------------------

// SSERateLimiter limits concurrent SSE connections per IP.
type SSERateLimiter struct {
	mu          sync.Mutex
	connections map[string]int
	maxPerIP    int
}

// NewSSERateLimiter creates a limiter for concurrent SSE connections.
func NewSSERateLimiter(maxPerIP int) *SSERateLimiter {
	return &SSERateLimiter{
		connections: make(map[string]int),
		maxPerIP:    maxPerIP,
	}
}

// Acquire attempts to claim an SSE connection slot for the given IP.
// Returns true if the connection is allowed, false if the limit is reached.
func (s *SSERateLimiter) Acquire(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.connections[ip]
	if current >= s.maxPerIP {
		return false
	}
	s.connections[ip] = current + 1
	return true
}

// Release frees an SSE connection slot for the given IP.
func (s *SSERateLimiter) Release(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.connections[ip]
	if current <= 1 {
		delete(s.connections, ip)
	} else {
		s.connections[ip] = current - 1
	}
}
