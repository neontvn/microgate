package dashboard

import (
	"sync"
	"time"
)

// RequestLog represents a single HTTP request processed by the gateway
type RequestLog struct {
	ID        string        `json:"id"`
	Timestamp time.Time     `json:"timestamp"`
	Method    string        `json:"method"`
	Path      string        `json:"path"`
	Status    int           `json:"status"`
	Latency   time.Duration `json:"latency_ms"`
	ClientIP  string        `json:"client_ip"`
	BytesIn   int64         `json:"bytes_in"`
	BytesOut  int64         `json:"bytes_out"`
	Backend   string        `json:"backend"`
	Error     string        `json:"error,omitempty"`
}

// LogStore is a thread-safe ring buffer for storing recent request logs
type LogStore struct {
	logs  []RequestLog
	mu    sync.RWMutex
	size  int
	index int
	count int

	// OnAdd is an optional hook to fire when a new log is received
	OnAdd func(log RequestLog)
}

// NewLogStore creates a new LogStore with the specified capacity
func NewLogStore(capacity int) *LogStore {
	if capacity <= 0 {
		capacity = 1000 // default capacity
	}
	return &LogStore{
		logs: make([]RequestLog, capacity),
		size: capacity,
	}
}

// Add inserts a new request log into the ring buffer
func (s *LogStore) Add(log RequestLog) {
	s.mu.Lock()
	s.logs[s.index] = log
	s.index = (s.index + 1) % s.size
	if s.count < s.size {
		s.count++
	}
	s.mu.Unlock()

	// Fire event hook outside the lock
	if s.OnAdd != nil {
		s.OnAdd(log)
	}
}

// Recent returns the n most recent request logs, ordered newest to oldest
func (s *LogStore) Recent(n int) []RequestLog {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if n > s.count {
		n = s.count
	}
	if n <= 0 {
		return []RequestLog{}
	}

	result := make([]RequestLog, n)

	// The most recent item is at index-1 (wrapping around)
	startIdx := s.index - 1
	if startIdx < 0 {
		startIdx = s.size - 1
	}

	for i := 0; i < n; i++ {
		idx := startIdx - i
		if idx < 0 {
			idx += s.size
		}
		result[i] = s.logs[idx]
	}

	return result
}

// GetByID retrieves a specific log by its ID
func (s *LogStore) GetByID(id string) (RequestLog, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Search backwards from the most recent
	startIdx := s.index - 1
	if startIdx < 0 {
		startIdx = s.size - 1
	}

	for i := 0; i < s.count; i++ {
		idx := startIdx - i
		if idx < 0 {
			idx += s.size
		}
		if s.logs[idx].ID == id {
			return s.logs[idx], true
		}
	}

	return RequestLog{}, false
}

// Search filters logs based on criteria, returning up to limit results
func (s *LogStore) Search(limit int, status int, path string) []RequestLog {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 50 // default limit
	}

	result := make([]RequestLog, 0, limit)

	startIdx := s.index - 1
	if startIdx < 0 {
		startIdx = s.size - 1
	}

	for i := 0; i < s.count && len(result) < limit; i++ {
		idx := startIdx - i
		if idx < 0 {
			idx += s.size
		}

		log := s.logs[idx]

		// Apply filters
		if status > 0 && log.Status != status {
			continue
		}

		if path != "" && log.Path != path && !containsString(log.Path, path) {
			continue
		}

		result = append(result, log)
	}

	return result
}

// SparklineData holds 30 time-bucketed data points for charting
type SparklineData struct {
	Requests []float64 `json:"requests"`
	Latency  []float64 `json:"latency"`
	Errors   []float64 `json:"errors"`
}

// MetricsSnapshot holds computed gateway metrics
type MetricsSnapshot struct {
	RequestsPerMinute int           `json:"requests_per_minute"`
	AvgLatencyMs      int           `json:"avg_latency_ms"`
	ErrorRate         float64       `json:"error_rate"`
	Sparklines        SparklineData `json:"sparklines"`
}

// Metrics computes real-time metrics from the log store.
// RPM, avg latency, and error rate are computed from the last 60 seconds.
// Sparklines are 30 one-minute buckets covering the last 30 minutes.
func (s *LogStore) Metrics() MetricsSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	oneMinuteAgo := now.Add(-60 * time.Second)
	thirtyMinutesAgo := now.Add(-30 * time.Minute)

	// Summary stats for last 60 seconds
	var recentCount int
	var recentErrors int
	var recentLatencySum time.Duration

	// Sparkline buckets: 30 one-minute windows
	const bucketCount = 30
	bucketRequests := make([]float64, bucketCount)
	bucketLatencySum := make([]float64, bucketCount)
	bucketLatencyCount := make([]int, bucketCount)
	bucketErrors := make([]float64, bucketCount)

	// Scan all entries in the ring buffer
	startIdx := s.index - 1
	if startIdx < 0 {
		startIdx = s.size - 1
	}

	for i := 0; i < s.count; i++ {
		idx := startIdx - i
		if idx < 0 {
			idx += s.size
		}
		log := s.logs[idx]

		// Skip zero-value entries (unfilled ring buffer slots)
		if log.Timestamp.IsZero() {
			continue
		}

		// Summary metrics: last 60 seconds
		if log.Timestamp.After(oneMinuteAgo) {
			recentCount++
			recentLatencySum += log.Latency
			if log.Status >= 500 {
				recentErrors++
			}
		}

		// Sparklines: last 30 minutes
		if log.Timestamp.After(thirtyMinutesAgo) {
			elapsed := now.Sub(log.Timestamp)
			bucket := int(elapsed.Minutes())
			if bucket >= bucketCount {
				bucket = bucketCount - 1
			}
			// Invert so index 0 = oldest, 29 = most recent
			bucket = bucketCount - 1 - bucket

			bucketRequests[bucket]++
			bucketLatencySum[bucket] += float64(log.Latency.Milliseconds())
			bucketLatencyCount[bucket]++
			if log.Status >= 500 {
				bucketErrors[bucket]++
			}
		}
	}

	// Compute summary
	snap := MetricsSnapshot{
		RequestsPerMinute: recentCount,
	}
	if recentCount > 0 {
		snap.AvgLatencyMs = int(recentLatencySum.Milliseconds()) / recentCount
		snap.ErrorRate = float64(recentErrors) / float64(recentCount)
	}

	// Compute sparkline averages for latency
	sparkLatency := make([]float64, bucketCount)
	for i := 0; i < bucketCount; i++ {
		if bucketLatencyCount[i] > 0 {
			sparkLatency[i] = bucketLatencySum[i] / float64(bucketLatencyCount[i])
		}
	}

	snap.Sparklines = SparklineData{
		Requests: bucketRequests,
		Latency:  sparkLatency,
		Errors:   bucketErrors,
	}

	return snap
}

func containsString(s, substr string) bool {
	// Basic substring check for the path filter
	return len(s) >= len(substr) && func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	}()
}
