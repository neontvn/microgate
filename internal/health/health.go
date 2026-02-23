package health

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// BackendStatus tracks the health of a single backend.
type BackendStatus struct {
	URL       string    `json:"url"`
	Healthy   bool      `json:"healthy"`
	LastCheck time.Time `json:"last_check"`
}

// HealthChecker monitors backend health and exposes a /health endpoint.
// It runs background checks on a timer and caches the results so that
// the /health endpoint doesn't need to probe backends on every request.
type HealthChecker struct {
	backends      map[string]*BackendStatus
	mu            sync.RWMutex
	startTime     time.Time
	client        *http.Client
	OnStateChange func(url string, isHealthy bool) // hook for SSE updates
}

// NewHealthChecker creates a HealthChecker for the given backend URLs.
func NewHealthChecker(backendURLs []string) *HealthChecker {
	backends := make(map[string]*BackendStatus)
	for _, url := range backendURLs {
		backends[url] = &BackendStatus{
			URL:     url,
			Healthy: true, // assume healthy until first check
		}
	}

	return &HealthChecker{
		backends:  backends,
		startTime: time.Now(),
		client: &http.Client{
			Timeout: 5 * time.Second, // don't hang on slow backends
		},
	}
}

// checkBackend makes an HTTP GET to the backend and returns true if it responds 200.
func (hc *HealthChecker) checkBackend(url string) bool {
	resp, err := hc.client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// RunChecks performs a one-time health check of all backends.
// Updates the cached status for each backend.
func (hc *HealthChecker) RunChecks() {
	for url := range hc.backends {
		healthy := hc.checkBackend(url)

		hc.mu.Lock()
		wasHealthy := hc.backends[url].Healthy
		hc.backends[url].Healthy = healthy
		hc.backends[url].LastCheck = time.Now()
		hc.mu.Unlock()

		// Fire event outside the lock, but only if state changed
		if hc.OnStateChange != nil && wasHealthy != healthy {
			hc.OnStateChange(url, healthy)
		}
	}
}

// StartBackground launches a goroutine that checks backends on a timer.
// Uses time.NewTicker to fire every `interval` duration.
// The goroutine runs until the program exits.
func (hc *HealthChecker) StartBackground(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		// Run an initial check immediately
		hc.RunChecks()
		for range ticker.C {
			hc.RunChecks()
		}
	}()
}

// AddBackend registers a new backend URL for health monitoring at runtime.
// It will be picked up on the next health check tick.
func (hc *HealthChecker) AddBackend(url string) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	if _, exists := hc.backends[url]; !exists {
		hc.backends[url] = &BackendStatus{
			URL:     url,
			Healthy: false, // unknown until first check
		}
	}
}

// IsHealthy returns whether a specific backend is currently healthy.
// Uses RLock (read lock) so multiple goroutines can check simultaneously
// without blocking each other — only writes need an exclusive lock.
func (hc *HealthChecker) IsHealthy(url string) bool {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	if status, exists := hc.backends[url]; exists {
		return status.Healthy
	}
	return false
}

// healthResponse is the JSON structure returned by the /health endpoint.
type healthResponse struct {
	Status   string                    `json:"status"`
	Uptime   string                    `json:"uptime"`
	Backends map[string]*BackendStatus `json:"backends"`
}

// Handler returns an http.HandlerFunc for the /health endpoint.
// Returns 200 if all backends are healthy, 503 if any are down.
func (hc *HealthChecker) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hc.mu.RLock()
		defer hc.mu.RUnlock()

		// Check if all backends are healthy
		allHealthy := true
		for _, status := range hc.backends {
			if !status.Healthy {
				allHealthy = false
				break
			}
		}

		// Build response
		resp := healthResponse{
			Status:   "healthy",
			Uptime:   time.Since(hc.startTime).Round(time.Second).String(),
			Backends: hc.backends,
		}

		if !allHealthy {
			resp.Status = "degraded"
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}
