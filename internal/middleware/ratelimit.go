package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// bucket represents a token bucket for a single client.
// Tokens are consumed per request and refill over time.
type bucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens added per second
	lastRefill time.Time
}

// refill adds tokens based on how much time has passed since last refill.
// Tokens are capped at maxTokens — you can't stockpile beyond the limit.
func (b *bucket) refill() {
	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * b.refillRate
	if b.tokens > b.maxTokens {
		b.tokens = b.maxTokens
	}
	b.lastRefill = now
}

// allow checks if a request is permitted.
// Refills tokens first, then tries to consume one.
func (b *bucket) allow() bool {
	b.refill()
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// RateLimiter holds a bucket per client IP.
// The mutex protects the map from concurrent access — multiple
// goroutines (requests) hit this simultaneously.
type RateLimiter struct {
	buckets    map[string]*bucket
	maxTokens  float64
	refillRate float64
	mu         sync.Mutex
}

// NewRateLimiter creates a rate limiter.
// maxTokens = burst size (e.g., 10 requests)
// refillRate = sustained rate (e.g., 1.0 = 1 token/sec)
func NewRateLimiter(maxTokens, refillRate float64) *RateLimiter {
	return &RateLimiter{
		buckets:    make(map[string]*bucket),
		maxTokens:  maxTokens,
		refillRate: refillRate,
	}
}

// getBucket returns the bucket for a given IP, creating one if needed.
func (rl *RateLimiter) getBucket(ip string) *bucket {
	if b, exists := rl.buckets[ip]; exists {
		return b
	}
	b := &bucket{
		tokens:     rl.maxTokens,
		maxTokens:  rl.maxTokens,
		refillRate: rl.refillRate,
		lastRefill: time.Now(),
	}
	rl.buckets[ip] = b
	return b
}

// Middleware returns the rate limiting Middleware.
// Extracts client IP, checks the bucket, returns 429 if over limit.
func (rl *RateLimiter) Middleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Lock because multiple goroutines access the buckets map
			rl.mu.Lock()
			ip, _, _ := net.SplitHostPort(r.RemoteAddr)
			b := rl.getBucket(ip)
			allowed := b.allow()
			rl.mu.Unlock()

			if !allowed {
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
