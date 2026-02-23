package proxy

import (
	"math/rand"
	"sync"
	"sync/atomic"

	"github.com/tanmay/gateway/internal/health"
)

// LoadBalancer distributes requests across multiple backends.
// Supports round-robin and random strategies, and skips unhealthy backends.
type LoadBalancer struct {
	backends      []string
	mu            sync.RWMutex // protects backends slice
	strategy      string
	counter       uint64 // atomic counter for round-robin
	healthChecker *health.HealthChecker
}

// NewLoadBalancer creates a load balancer for the given backends.
// strategy: "round-robin" (default) or "random"
func NewLoadBalancer(backends []string, strategy string, hc *health.HealthChecker) *LoadBalancer {
	if strategy == "" {
		strategy = "round-robin"
	}
	return &LoadBalancer{
		backends:      backends,
		strategy:      strategy,
		healthChecker: hc,
	}
}

// AddBackend registers a new backend URL with this load balancer at runtime.
func (lb *LoadBalancer) AddBackend(url string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.backends = append(lb.backends, url)
}

// Next returns the next backend URL based on the load balancing strategy.
// Skips unhealthy backends if a health checker is configured.
// Returns empty string if no healthy backends are available.
func (lb *LoadBalancer) Next() string {
	healthy := lb.healthyBackends()
	if len(healthy) == 0 {
		return ""
	}

	switch lb.strategy {
	case "random":
		return healthy[rand.Intn(len(healthy))]
	default: // round-robin
		idx := atomic.AddUint64(&lb.counter, 1)
		return healthy[idx%uint64(len(healthy))]
	}
}

// healthyBackends returns only backends that are currently healthy.
// If no health checker is configured, returns all backends.
func (lb *LoadBalancer) healthyBackends() []string {
	lb.mu.RLock()
	backends := make([]string, len(lb.backends))
	copy(backends, lb.backends)
	lb.mu.RUnlock()

	if lb.healthChecker == nil {
		return backends
	}

	var healthy []string
	for _, backend := range backends {
		if lb.healthChecker.IsHealthy(backend) {
			healthy = append(healthy, backend)
		}
	}

	// If all backends are unhealthy, return all of them as fallback
	// (let the circuit breaker handle failures instead of blocking everything)
	if len(healthy) == 0 {
		return backends
	}
	return healthy
}
