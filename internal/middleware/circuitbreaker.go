package middleware

import (
	"net/http"
	"sync"
	"time"
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
type CircuitBreaker struct {
	state        int
	failureCount int
	threshold    int           // failures before opening
	timeout      time.Duration // how long to stay open before trying again
	lastFailure  time.Time
	mu           sync.Mutex
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

			// Check if the request failed (5xx = backend error)
			cb.mu.Lock()
			if wrapped.statusCode >= 500 {
				cb.failureCount++
				cb.lastFailure = time.Now()

				if cb.state == StateHalfOpen {
					// Half-open test failed → back to open
					cb.state = StateOpen
				} else if cb.failureCount >= cb.threshold {
					// Too many failures → open the circuit
					cb.state = StateOpen
				}
			} else {
				// Success — reset everything
				cb.failureCount = 0
				cb.state = StateClosed
			}
			cb.mu.Unlock()
		})
	}
}
