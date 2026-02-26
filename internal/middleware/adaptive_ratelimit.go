package middleware

import (
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/tanmay/gateway/internal/analytics"
)

// AdaptiveRateLimitConfig holds configuration for the adaptive rate limiter.
type AdaptiveRateLimitConfig struct {
	Enabled        bool
	Multiplier     float64       // allow up to N× normal traffic (default 3.0)
	MinLimit       float64       // never go below this (default 10)
	MaxLimit       float64       // never go above this (default 10000)
	LearningPeriod time.Duration // don't enforce adaptive limits until this much data (default 1h)
}

// AdaptiveRateLimiter wraps a static RateLimiter and dynamically adjusts
// per-route limits based on learned traffic baselines from the Analyzer.
type AdaptiveRateLimiter struct {
	static   *RateLimiter
	analyzer *analytics.Analyzer
	config   AdaptiveRateLimitConfig

	mu             sync.RWMutex
	routeLimiters  map[string]*RateLimiter // per-route rate limiters with adaptive limits
	lastRebalance  time.Time
}

// NewAdaptiveRateLimiter creates an adaptive rate limiter.
// If the analyzer has insufficient data, it falls back to the static limiter.
func NewAdaptiveRateLimiter(static *RateLimiter, analyzer *analytics.Analyzer, cfg AdaptiveRateLimitConfig) *AdaptiveRateLimiter {
	if cfg.Multiplier <= 0 {
		cfg.Multiplier = 3.0
	}
	if cfg.MinLimit <= 0 {
		cfg.MinLimit = 10
	}
	if cfg.MaxLimit <= 0 {
		cfg.MaxLimit = 10000
	}
	if cfg.LearningPeriod <= 0 {
		cfg.LearningPeriod = 1 * time.Hour
	}

	return &AdaptiveRateLimiter{
		static:        static,
		analyzer:      analyzer,
		config:        cfg,
		routeLimiters: make(map[string]*RateLimiter),
	}
}

// currentLimit computes the dynamic rate limit for a route based on analyzer baselines.
// Returns tokens-per-minute. The refill rate is derived by dividing by 60 to get per-second.
func (a *AdaptiveRateLimiter) currentLimit(route string) float64 {
	baseline := a.analyzer.GetRouteBaseline(route)
	if baseline == nil || baseline.SampleSize < 5 {
		return 0 // not enough data
	}

	// Dynamic limit = mean request rate per minute × multiplier
	limit := baseline.MeanRate * a.config.Multiplier

	// Clamp to configured bounds
	if limit < a.config.MinLimit {
		limit = a.config.MinLimit
	}
	if limit > a.config.MaxLimit {
		limit = a.config.MaxLimit
	}

	return limit
}

// rebalance updates per-route rate limiters based on current baselines.
func (a *AdaptiveRateLimiter) rebalance() {
	baselines := a.analyzer.GetAllRouteBaselines()

	a.mu.Lock()
	defer a.mu.Unlock()

	for route, baseline := range baselines {
		if baseline.SampleSize < 5 {
			continue
		}

		limit := baseline.MeanRate * a.config.Multiplier
		if limit < a.config.MinLimit {
			limit = a.config.MinLimit
		}
		if limit > a.config.MaxLimit {
			limit = a.config.MaxLimit
		}

		// maxTokens = burst capacity, refillRate = sustained rate per second
		refillRate := limit / 60.0

		existing, ok := a.routeLimiters[route]
		if !ok || existing.maxTokens != limit {
			a.routeLimiters[route] = NewRateLimiter(limit, refillRate)
			log.Printf("[adaptive-rl] route=%s limit=%.0f req/min (mean=%.1f × %.1f)",
				route, limit, baseline.MeanRate, a.config.Multiplier)
		}
	}

	a.lastRebalance = time.Now()
}

// Middleware returns the rate limiting middleware.
// Uses adaptive limits when sufficient data exists, falls back to static otherwise.
func (a *AdaptiveRateLimiter) Middleware(routeResolver func(path string) string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// If adaptive is disabled or not enough data yet, use static limiter
			if !a.config.Enabled || !a.analyzer.HasSufficientData() {
				a.static.Middleware()(next).ServeHTTP(w, r)
				return
			}

			// Periodically rebalance (every 5 minutes)
			a.mu.RLock()
			needsRebalance := time.Since(a.lastRebalance) > 5*time.Minute
			a.mu.RUnlock()
			if needsRebalance {
				a.rebalance()
			}

			// Resolve the route for this request path
			route := routeResolver(r.URL.Path)

			// Look up route-specific limiter
			a.mu.RLock()
			rl, ok := a.routeLimiters[route]
			a.mu.RUnlock()

			if !ok {
				// No adaptive data for this route — fall back to static
				a.static.Middleware()(next).ServeHTTP(w, r)
				return
			}

			// Check the adaptive rate limit
			ip, _, _ := net.SplitHostPort(r.RemoteAddr)
			rl.mu.Lock()
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
