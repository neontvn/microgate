package proxy

import (
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/tanmay/gateway/internal/analytics"
	"github.com/tanmay/gateway/internal/health"
)

// BackendSelector is the interface for choosing a backend to handle a request.
// Both LoadBalancer and WeightedLoadBalancer implement this.
type BackendSelector interface {
	Next() string
	AddBackend(url string)
}

// backendWeight holds the computed weight for a single backend.
type backendWeight struct {
	url    string
	weight float64
}

// WeightedLoadBalancer distributes traffic to backends proportional to their
// performance: lower latency and lower error rate = more traffic.
type WeightedLoadBalancer struct {
	backends      []string
	mu            sync.RWMutex
	weights       []backendWeight // sorted by backend URL for stability
	analyzer      *analytics.Analyzer
	healthChecker *health.HealthChecker
	rebalanceInterval time.Duration
}

// NewWeightedLoadBalancer creates a performance-weighted load balancer.
func NewWeightedLoadBalancer(
	backends []string,
	analyzer *analytics.Analyzer,
	hc *health.HealthChecker,
	rebalanceInterval time.Duration,
) *WeightedLoadBalancer {
	if rebalanceInterval <= 0 {
		rebalanceInterval = 5 * time.Minute
	}

	wlb := &WeightedLoadBalancer{
		backends:          backends,
		analyzer:          analyzer,
		healthChecker:     hc,
		rebalanceInterval: rebalanceInterval,
	}

	// Initialize with equal weights
	wlb.initEqualWeights()

	return wlb
}

// initEqualWeights sets all backends to equal weight.
func (wlb *WeightedLoadBalancer) initEqualWeights() {
	wlb.mu.Lock()
	defer wlb.mu.Unlock()

	w := 1.0 / float64(len(wlb.backends))
	wlb.weights = make([]backendWeight, len(wlb.backends))
	for i, b := range wlb.backends {
		wlb.weights[i] = backendWeight{url: b, weight: w}
	}
}

// StartRebalancing launches a background goroutine that periodically recomputes weights.
func (wlb *WeightedLoadBalancer) StartRebalancing() {
	// Initial rebalance
	wlb.Rebalance()

	ticker := time.NewTicker(wlb.rebalanceInterval)
	go func() {
		for range ticker.C {
			wlb.Rebalance()
		}
	}()
}

// Rebalance recomputes backend weights based on analyzer data.
func (wlb *WeightedLoadBalancer) Rebalance() {
	wlb.mu.RLock()
	backends := make([]string, len(wlb.backends))
	copy(backends, wlb.backends)
	wlb.mu.RUnlock()

	newWeights := make([]backendWeight, 0, len(backends))
	var totalWeight float64

	for _, backend := range backends {
		baseline := wlb.analyzer.GetBackendBaseline(backend)
		var w float64
		if baseline == nil || baseline.SampleSize < 2 {
			w = 1.0 // equal weight if no data
		} else {
			w = computeWeight(baseline.MeanLatencyMs, baseline.MeanErrorRate)
		}
		newWeights = append(newWeights, backendWeight{url: backend, weight: w})
		totalWeight += w
	}

	// Normalize weights to sum to 1.0
	if totalWeight > 0 {
		for i := range newWeights {
			newWeights[i].weight /= totalWeight
		}
	}

	wlb.mu.Lock()
	wlb.weights = newWeights
	wlb.mu.Unlock()

	// Log the new weights
	for _, w := range newWeights {
		log.Printf("[weighted-lb] %s → weight=%.3f", w.url, w.weight)
	}
}

// computeWeight calculates a backend's weight from its latency and error rate.
// Lower latency + lower error rate = higher weight.
func computeWeight(avgLatencyMs float64, errorRate float64) float64 {
	// Prevent division by zero: use a minimum latency of 1ms
	if avgLatencyMs < 1 {
		avgLatencyMs = 1
	}

	latencyScore := 1.0 / avgLatencyMs
	reliabilityScore := 1.0 - errorRate
	if reliabilityScore < 0.01 {
		reliabilityScore = 0.01 // don't zero out completely
	}

	return latencyScore * reliabilityScore
}

// Next selects a backend using weighted random selection.
// Skips unhealthy backends if a health checker is configured.
func (wlb *WeightedLoadBalancer) Next() string {
	wlb.mu.RLock()
	weights := make([]backendWeight, len(wlb.weights))
	copy(weights, wlb.weights)
	wlb.mu.RUnlock()

	// Filter to healthy backends only
	var healthy []backendWeight
	var totalWeight float64
	for _, w := range weights {
		if wlb.healthChecker == nil || wlb.healthChecker.IsHealthy(w.url) {
			healthy = append(healthy, w)
			totalWeight += w.weight
		}
	}

	// If all are unhealthy, fall back to all backends (let circuit breaker handle)
	if len(healthy) == 0 {
		healthy = weights
		totalWeight = 0
		for _, w := range healthy {
			totalWeight += w.weight
		}
	}

	if len(healthy) == 0 {
		return ""
	}

	// Weighted random selection
	r := rand.Float64() * totalWeight
	var cumulative float64
	for _, w := range healthy {
		cumulative += w.weight
		if r <= cumulative {
			return w.url
		}
	}

	// Fallback (rounding edge case)
	return healthy[len(healthy)-1].url
}

// AddBackend registers a new backend URL at runtime.
func (wlb *WeightedLoadBalancer) AddBackend(url string) {
	wlb.mu.Lock()
	defer wlb.mu.Unlock()

	wlb.backends = append(wlb.backends, url)
	// Add with average weight of existing backends
	avgWeight := 1.0 / float64(len(wlb.backends))
	wlb.weights = append(wlb.weights, backendWeight{url: url, weight: avgWeight})
}

// GetWeights returns a snapshot of current backend weights (for API/debugging).
func (wlb *WeightedLoadBalancer) GetWeights() map[string]float64 {
	wlb.mu.RLock()
	defer wlb.mu.RUnlock()

	result := make(map[string]float64, len(wlb.weights))
	for _, w := range wlb.weights {
		result[w.url] = w.weight
	}
	return result
}
