package analytics

import (
	"log"
	"math"
	"sync"
	"time"
)

// Anomaly represents a detected traffic anomaly.
type Anomaly struct {
	Route     string    `json:"route"`
	Metric    string    `json:"metric"`    // "request_rate", "error_rate", "latency"
	Current   float64   `json:"current"`
	Mean      float64   `json:"mean"`
	StdDev    float64   `json:"std_dev"`
	ZScore    float64   `json:"z_score"`
	Timestamp time.Time `json:"timestamp"`
}

// RouteBaseline holds the computed baseline statistics for a single route.
type RouteBaseline struct {
	Route         string  `json:"route"`
	MeanRate      float64 `json:"mean_rate"`       // avg requests per minute
	StdDevRate    float64 `json:"std_dev_rate"`
	MeanErrorRate float64 `json:"mean_error_rate"`
	StdDevError   float64 `json:"std_dev_error"`
	MeanLatencyMs float64 `json:"mean_latency_ms"` // avg latency in milliseconds
	StdDevLatency float64 `json:"std_dev_latency"`
	P99LatencyMs  float64 `json:"p99_latency_ms"`
	SampleSize    int     `json:"sample_size"` // number of buckets used
}

// BackendBaseline holds computed baseline statistics for a single backend.
type BackendBaseline struct {
	Backend       string  `json:"backend"`
	MeanLatencyMs float64 `json:"mean_latency_ms"`
	MeanErrorRate float64 `json:"mean_error_rate"`
	StdDevError   float64 `json:"std_dev_error"`
	SampleSize    int     `json:"sample_size"`
}

// AnalyzerConfig configures the traffic analyzer.
type AnalyzerConfig struct {
	Interval       time.Duration // how often to recompute baselines (default 5m)
	Window         time.Duration // how far back to look for baselines (default 1h)
	ZScoreThreshold float64      // z-score threshold for anomaly detection (default 3.0)
}

// Analyzer computes traffic baselines and detects anomalies.
// It periodically reads from a TrafficStore, computes moving averages
// and standard deviations, and publishes anomalies to a channel.
type Analyzer struct {
	store     TrafficStore
	config    AnalyzerConfig
	startTime time.Time

	mu              sync.RWMutex
	routeBaselines  map[string]*RouteBaseline
	backendBaselines map[string]*BackendBaseline
	anomalies       []Anomaly // recent anomalies (last 24h)

	// AnomalyChannel publishes detected anomalies for other components to react.
	AnomalyChannel chan Anomaly
}

// NewAnalyzer creates a new traffic analyzer with the given store and config.
func NewAnalyzer(store TrafficStore, cfg AnalyzerConfig) *Analyzer {
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Minute
	}
	if cfg.Window <= 0 {
		cfg.Window = 1 * time.Hour
	}
	if cfg.ZScoreThreshold <= 0 {
		cfg.ZScoreThreshold = 3.0
	}

	return &Analyzer{
		store:            store,
		config:           cfg,
		startTime:        time.Now(),
		routeBaselines:   make(map[string]*RouteBaseline),
		backendBaselines: make(map[string]*BackendBaseline),
		AnomalyChannel:   make(chan Anomaly, 64),
	}
}

// Start launches the background analysis loop. It runs analyze() every config.Interval.
func (a *Analyzer) Start() {
	// Run an initial analysis immediately
	a.analyze()

	ticker := time.NewTicker(a.config.Interval)
	go func() {
		for range ticker.C {
			a.analyze()
		}
	}()
}

// HasSufficientData returns true if the analyzer has been running long enough
// to have meaningful baselines (at least one full analysis window).
func (a *Analyzer) HasSufficientData() bool {
	return time.Since(a.startTime) >= a.config.Window
}

// GetRouteBaseline returns the baseline for a specific route, or nil if unknown.
func (a *Analyzer) GetRouteBaseline(route string) *RouteBaseline {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if b, ok := a.routeBaselines[route]; ok {
		cp := *b
		return &cp
	}
	return nil
}

// GetAllRouteBaselines returns baselines for all known routes.
func (a *Analyzer) GetAllRouteBaselines() map[string]*RouteBaseline {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make(map[string]*RouteBaseline, len(a.routeBaselines))
	for k, v := range a.routeBaselines {
		cp := *v
		result[k] = &cp
	}
	return result
}

// GetBackendBaseline returns the baseline for a specific backend, or nil if unknown.
func (a *Analyzer) GetBackendBaseline(backend string) *BackendBaseline {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if b, ok := a.backendBaselines[backend]; ok {
		cp := *b
		return &cp
	}
	return nil
}

// GetAllBackendBaselines returns baselines for all known backends.
func (a *Analyzer) GetAllBackendBaselines() map[string]*BackendBaseline {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make(map[string]*BackendBaseline, len(a.backendBaselines))
	for k, v := range a.backendBaselines {
		cp := *v
		result[k] = &cp
	}
	return result
}

// GetRecentAnomalies returns anomalies detected in the last 24 hours.
func (a *Analyzer) GetRecentAnomalies() []Anomaly {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make([]Anomaly, len(a.anomalies))
	copy(result, a.anomalies)
	return result
}

// analyze recomputes all baselines and checks for anomalies.
func (a *Analyzer) analyze() {
	now := time.Now()
	from := now.Add(-a.config.Window)

	a.analyzeRoutes(from, now)
	a.analyzeBackends(from, now)
	a.pruneAnomalies()
}

// analyzeRoutes computes baselines for all routes and detects anomalies.
func (a *Analyzer) analyzeRoutes(from, to time.Time) {
	allBuckets := a.store.GetAllBuckets(from, to)

	a.mu.Lock()
	defer a.mu.Unlock()

	for route, buckets := range allBuckets {
		if len(buckets) < 2 {
			continue
		}

		// Collect per-bucket metrics
		rates := make([]float64, len(buckets))
		errorRates := make([]float64, len(buckets))
		latencies := make([]float64, len(buckets))

		for i, b := range buckets {
			rates[i] = float64(b.RequestCount)
			errorRates[i] = b.ErrorRate()
			latencies[i] = float64(b.AvgLatency()) / float64(time.Millisecond)
		}

		baseline := &RouteBaseline{
			Route:         route,
			MeanRate:      mean(rates),
			StdDevRate:    stddev(rates),
			MeanErrorRate: mean(errorRates),
			StdDevError:   stddev(errorRates),
			MeanLatencyMs: mean(latencies),
			StdDevLatency: stddev(latencies),
			P99LatencyMs:  percentile(latencies, 0.99),
			SampleSize:    len(buckets),
		}

		a.routeBaselines[route] = baseline

		// Check current bucket (most recent) for anomalies
		current := buckets[len(buckets)-1]
		currentRate := float64(current.RequestCount)
		currentErrorRate := current.ErrorRate()
		currentLatency := float64(current.AvgLatency()) / float64(time.Millisecond)

		a.checkAnomaly(route, "request_rate", currentRate, baseline.MeanRate, baseline.StdDevRate)
		a.checkAnomaly(route, "error_rate", currentErrorRate, baseline.MeanErrorRate, baseline.StdDevError)
		a.checkAnomaly(route, "latency", currentLatency, baseline.MeanLatencyMs, baseline.StdDevLatency)
	}
}

// analyzeBackends computes baselines for all backends.
func (a *Analyzer) analyzeBackends(from, to time.Time) {
	allBuckets := a.store.GetBackendBuckets(from, to)

	a.mu.Lock()
	defer a.mu.Unlock()

	for backend, buckets := range allBuckets {
		if len(buckets) < 2 {
			continue
		}

		errorRates := make([]float64, len(buckets))
		latencies := make([]float64, len(buckets))

		for i, b := range buckets {
			errorRates[i] = b.ErrorRate()
			latencies[i] = float64(b.AvgLatency()) / float64(time.Millisecond)
		}

		a.backendBaselines[backend] = &BackendBaseline{
			Backend:       backend,
			MeanLatencyMs: mean(latencies),
			MeanErrorRate: mean(errorRates),
			StdDevError:   stddev(errorRates),
			SampleSize:    len(buckets),
		}
	}
}

// checkAnomaly tests if a current value is anomalous and records it.
// Must be called with the write lock held.
func (a *Analyzer) checkAnomaly(route, metric string, current, mean, stddev float64) {
	if stddev == 0 || mean == 0 {
		return
	}

	zScore := (current - mean) / stddev
	if zScore > a.config.ZScoreThreshold {
		anomaly := Anomaly{
			Route:     route,
			Metric:    metric,
			Current:   current,
			Mean:      mean,
			StdDev:    stddev,
			ZScore:    zScore,
			Timestamp: time.Now(),
		}

		a.anomalies = append(a.anomalies, anomaly)

		log.Printf("[anomaly] route=%s metric=%s current=%.2f mean=%.2f z_score=%.2f",
			route, metric, current, mean, zScore)

		// Non-blocking publish to the anomaly channel
		select {
		case a.AnomalyChannel <- anomaly:
		default:
		}
	}
}

// pruneAnomalies removes anomalies older than 24 hours.
func (a *Analyzer) pruneAnomalies() {
	cutoff := time.Now().Add(-24 * time.Hour)

	a.mu.Lock()
	defer a.mu.Unlock()

	kept := a.anomalies[:0]
	for _, anom := range a.anomalies {
		if !anom.Timestamp.Before(cutoff) {
			kept = append(kept, anom)
		}
	}
	a.anomalies = kept
}

// --- Statistical helpers ---

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func stddev(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	m := mean(values)
	var sumSquares float64
	for _, v := range values {
		diff := v - m
		sumSquares += diff * diff
	}
	return math.Sqrt(sumSquares / float64(len(values)))
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	// Make a sorted copy
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sortFloat64s(sorted)

	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func sortFloat64s(a []float64) {
	// Simple insertion sort — fine for the small arrays we deal with
	for i := 1; i < len(a); i++ {
		key := a[i]
		j := i - 1
		for j >= 0 && a[j] > key {
			a[j+1] = a[j]
			j--
		}
		a[j+1] = key
	}
}
