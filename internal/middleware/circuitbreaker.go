package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/tanmay/gateway/internal/analytics"
)

// Circuit breaker states
const (
	StateClosed   = iota // normal — requests flow through
	StateOpen            // tripped — all requests rejected
	StateHalfOpen        // testing — allow one request to check if backend recovered
)

// CircuitBreaker prevents cascading failures by tracking backend errors.
// If failures exceed the threshold, it "opens" and rejects requests
// until a timeout passes, then lets one request through to test recovery.
//
// When an Analyzer is set via SetAnalyzer(), the threshold is dynamically
// computed as 5× the baseline error rate (with a minimum floor of 5%).
type CircuitBreaker struct {
	state        int
	failureCount int
	threshold    int           // static failures before opening
	timeout      time.Duration // how long to stay open before trying again
	lastFailure  time.Time
	mu           sync.Mutex
	analyzer     *analytics.Analyzer // optional — enables dynamic thresholds
	totalCount   int                 // total requests in current window (for error rate)
}

// NewCircuitBreaker creates a circuit breaker.
// threshold = how many failures before opening (e.g., 5)
// timeout = how long to wait before trying again (e.g., 30s)
func NewCircuitBreaker(threshold int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:     StateClosed,
		threshold: threshold,
		timeout:   timeout,
	}
}

// SetAnalyzer enables dynamic threshold computation based on learned error baselines.
// When set, the circuit breaker opens when the error rate exceeds 5× the baseline,
// instead of using the static failure count threshold.
func (cb *CircuitBreaker) SetAnalyzer(a *analytics.Analyzer) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.analyzer = a
}

// shouldTrip decides whether the circuit should open.
// With an analyzer: uses dynamic error-rate threshold (5× baseline, min 5%).
// Without: uses the static failure count threshold.
func (cb *CircuitBreaker) shouldTrip(backend string) bool {
	if cb.analyzer != nil && cb.analyzer.HasSufficientData() && cb.totalCount > 0 {
		currentErrorRate := float64(cb.failureCount) / float64(cb.totalCount)
		dynamicThreshold := cb.dynamicThreshold(backend)
		return currentErrorRate > dynamicThreshold
	}
	// Fall back to static threshold
	return cb.failureCount >= cb.threshold
}

// dynamicThreshold computes the error-rate threshold for a backend based on its baseline.
func (cb *CircuitBreaker) dynamicThreshold(backend string) float64 {
	if cb.analyzer == nil {
		return 1.0 // never trip (effectively disabled)
	}

	baseline := cb.analyzer.GetBackendBaseline(backend)
	if baseline == nil || baseline.SampleSize < 2 {
		// Not enough data — use a generous default
		return 0.5
	}

	threshold := baseline.MeanErrorRate * 5.0
	if threshold < 0.05 {
		return 0.05 // minimum 5% threshold
	}
	return threshold
}

// Middleware returns the circuit breaker Middleware.
func (cb *CircuitBreaker) Middleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cb.mu.Lock()

			switch cb.state {
			case StateOpen:
				// Check if timeout has passed — if so, move to half-open
				if time.Since(cb.lastFailure) > cb.timeout {
					cb.state = StateHalfOpen
					cb.mu.Unlock()
					// Fall through to try one request
				} else {
					cb.mu.Unlock()
					http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
					return
				}

			case StateClosed, StateHalfOpen:
				cb.mu.Unlock()
				// Fall through to handle request
			}

			// Wrap response writer to capture status code
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(wrapped, r)

			// Identify backend from response headers (set by proxy)
			backend := w.Header().Get("X-Proxy-Backend")

			// Check if the request failed (5xx = backend error)
			cb.mu.Lock()
			cb.totalCount++
			if wrapped.statusCode >= 500 {
				cb.failureCount++
				cb.lastFailure = time.Now()

				if cb.state == StateHalfOpen {
					// Half-open test failed → back to open
					cb.state = StateOpen
				} else if cb.shouldTrip(backend) {
					// Too many failures → open the circuit
					cb.state = StateOpen
				}
			} else {
				// Success — reset everything
				cb.failureCount = 0
				cb.totalCount = 0
				cb.state = StateClosed
			}
			cb.mu.Unlock()
		})
	}
}
