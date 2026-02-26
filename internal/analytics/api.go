package analytics

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// AnalyticsAPI exposes REST endpoints for traffic intelligence data.
// These are registered outside the middleware chain so they aren't
// rate-limited or counted as regular traffic.
type AnalyticsAPI struct {
	analyzer *Analyzer
	store    TrafficStore
}

// NewAnalyticsAPI creates a new analytics API handler.
func NewAnalyticsAPI(analyzer *Analyzer, store TrafficStore) *AnalyticsAPI {
	return &AnalyticsAPI{
		analyzer: analyzer,
		store:    store,
	}
}

// Handler returns an http.Handler for the analytics API endpoints.
// Expected to be mounted at /analytics (caller strips prefix).
func (api *AnalyticsAPI) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/routes", api.handleRoutes)
	mux.HandleFunc("/routes/", api.handleRouteHistory) // /routes/{route}/history
	mux.HandleFunc("/anomalies", api.handleAnomalies)
	mux.HandleFunc("/backends", api.handleBackends)
	return mux
}

// routeSummary is the JSON response for a single route in GET /analytics/routes.
type routeSummary struct {
	Route            string  `json:"route"`
	AvgRate          float64 `json:"avg_rate"`
	AvgLatencyMs     float64 `json:"avg_latency_ms"`
	P99LatencyMs     float64 `json:"p99_latency_ms"`
	ErrorRate        float64 `json:"error_rate"`
	CurrentRateLimit float64 `json:"current_rate_limit"`
	Anomalies24h     int     `json:"anomalies_24h"`
}

// handleRoutes returns all known routes with current baselines.
// GET /analytics/routes
func (api *AnalyticsAPI) handleRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	baselines := api.analyzer.GetAllRouteBaselines()
	anomalies := api.analyzer.GetRecentAnomalies()

	// Count anomalies per route
	anomalyCounts := make(map[string]int)
	for _, a := range anomalies {
		anomalyCounts[a.Route]++
	}

	var summaries []routeSummary
	for route, b := range baselines {
		summaries = append(summaries, routeSummary{
			Route:            route,
			AvgRate:          b.MeanRate,
			AvgLatencyMs:     b.MeanLatencyMs,
			P99LatencyMs:     b.P99LatencyMs,
			ErrorRate:        b.MeanErrorRate,
			CurrentRateLimit: b.MeanRate * 3.0, // default multiplier
			Anomalies24h:     anomalyCounts[route],
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"routes": summaries,
	})
}

// historyPoint is a single data point in a route's time-series history.
type historyPoint struct {
	Timestamp    time.Time `json:"timestamp"`
	RequestCount int       `json:"request_count"`
	ErrorRate    float64   `json:"error_rate"`
	AvgLatencyMs float64  `json:"avg_latency_ms"`
	BytesIn      int64     `json:"bytes_in"`
	BytesOut     int64     `json:"bytes_out"`
}

// handleRouteHistory returns time-series data for a specific route.
// GET /analytics/routes/{route}/history
func (api *AnalyticsAPI) handleRouteHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract route from path: /routes/{route}/history
	// The path after /routes/ could be like "/api/v1/history"
	path := r.URL.Path // e.g., "/routes//api/v1/history"
	path = strings.TrimPrefix(path, "/routes/")
	// Remove trailing "/history"
	route := strings.TrimSuffix(path, "/history")
	if route == "" {
		http.Error(w, "route parameter required", http.StatusBadRequest)
		return
	}

	// Ensure route starts with /
	if !strings.HasPrefix(route, "/") {
		route = "/" + route
	}

	// Default to last 1 hour of data
	to := time.Now()
	from := to.Add(-1 * time.Hour)

	buckets := api.store.GetBuckets(route, from, to)

	points := make([]historyPoint, len(buckets))
	for i, b := range buckets {
		points[i] = historyPoint{
			Timestamp:    b.Timestamp,
			RequestCount: b.RequestCount,
			ErrorRate:    b.ErrorRate(),
			AvgLatencyMs: float64(b.AvgLatency()) / float64(time.Millisecond),
			BytesIn:      b.BytesIn,
			BytesOut:     b.BytesOut,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"route":   route,
		"from":    from,
		"to":      to,
		"history": points,
	})
}

// handleAnomalies returns recent anomalies with details.
// GET /analytics/anomalies
func (api *AnalyticsAPI) handleAnomalies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	anomalies := api.analyzer.GetRecentAnomalies()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"anomalies": anomalies,
		"count":     len(anomalies),
	})
}

// backendSummary is the JSON response for a single backend in GET /analytics/backends.
type backendSummary struct {
	Backend      string  `json:"backend"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	ErrorRate    float64 `json:"error_rate"`
	Weight       float64 `json:"weight"`
}

// WeightProvider returns current backend weights (implemented by WeightedLoadBalancer).
type WeightProvider interface {
	GetWeights() map[string]float64
}

// weightProvider is set externally to provide backend weights.
var weightProviderFn func() map[string]float64

// SetWeightProvider allows main.go to inject the weight provider function.
func (api *AnalyticsAPI) SetWeightProvider(fn func() map[string]float64) {
	weightProviderFn = fn
}

// handleBackends returns backend performance data and current weights.
// GET /analytics/backends
func (api *AnalyticsAPI) handleBackends(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	baselines := api.analyzer.GetAllBackendBaselines()

	// Get weights if available
	var weights map[string]float64
	if weightProviderFn != nil {
		weights = weightProviderFn()
	}

	var summaries []backendSummary
	for backend, b := range baselines {
		weight := 0.0
		if weights != nil {
			weight = weights[backend]
		}
		summaries = append(summaries, backendSummary{
			Backend:      backend,
			AvgLatencyMs: b.MeanLatencyMs,
			ErrorRate:    b.MeanErrorRate,
			Weight:       weight,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"backends": summaries,
	})
}
