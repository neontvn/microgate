package analytics

import (
	"sort"
	"sync"
	"time"
)

// TrafficEvent represents a single request data point captured by the traffic middleware.
type TrafficEvent struct {
	Route     string        // Normalized route path (e.g., "/api/v1")
	Backend   string        // Backend URL that handled the request
	Status    int           // HTTP response status code
	Latency   time.Duration // Request-response latency
	BytesIn   int64         // Request body size
	BytesOut  int64         // Response body size
	ClientIP  string        // Client IP address
	Timestamp time.Time     // When the request was received
}

// Bucket aggregates traffic for one route (or backend) during a 1-minute window.
type Bucket struct {
	Route        string        `json:"route"`
	Timestamp    time.Time     `json:"timestamp"`     // start of the 1-minute window
	RequestCount int           `json:"request_count"`
	ErrorCount   int           `json:"error_count"`   // status >= 500
	TotalLatency time.Duration `json:"total_latency"`
	MaxLatency   time.Duration `json:"max_latency"`
	BytesIn      int64         `json:"bytes_in"`
	BytesOut     int64         `json:"bytes_out"`
}

// AvgLatency returns the mean latency for this bucket.
func (b *Bucket) AvgLatency() time.Duration {
	if b.RequestCount == 0 {
		return 0
	}
	return b.TotalLatency / time.Duration(b.RequestCount)
}

// ErrorRate returns the fraction of requests that were errors (5xx).
func (b *Bucket) ErrorRate() float64 {
	if b.RequestCount == 0 {
		return 0
	}
	return float64(b.ErrorCount) / float64(b.RequestCount)
}

// TrafficStore persists traffic events into time-bucketed aggregates.
type TrafficStore interface {
	// Record adds a single traffic event to the appropriate bucket.
	Record(event TrafficEvent)
	// GetBuckets returns buckets for a route within [from, to), sorted by time.
	GetBuckets(route string, from, to time.Time) []Bucket
	// GetAllBuckets returns buckets for all routes within [from, to).
	GetAllBuckets(from, to time.Time) map[string][]Bucket
	// GetRoutes returns all known route names.
	GetRoutes() []string
	// GetBackendBuckets returns per-backend buckets within [from, to).
	GetBackendBuckets(from, to time.Time) map[string][]Bucket
}

// MemoryTrafficStore is the in-memory implementation of TrafficStore.
// Uses nested maps keyed by route/backend then minute-truncated timestamp.
type MemoryTrafficStore struct {
	mu        sync.RWMutex
	routes    map[string]map[time.Time]*Bucket // route -> minute -> bucket
	backends  map[string]map[time.Time]*Bucket // backend -> minute -> bucket
	retention time.Duration                     // how long to keep buckets
}

// NewMemoryTrafficStore creates a new in-memory traffic store.
// retention specifies how long to keep historical buckets (default 48h).
func NewMemoryTrafficStore(retention time.Duration) *MemoryTrafficStore {
	if retention <= 0 {
		retention = 48 * time.Hour
	}
	return &MemoryTrafficStore{
		routes:    make(map[string]map[time.Time]*Bucket),
		backends:  make(map[string]map[time.Time]*Bucket),
		retention: retention,
	}
}

// Record adds a TrafficEvent to the correct 1-minute bucket for both route and backend.
func (s *MemoryTrafficStore) Record(event TrafficEvent) {
	minute := event.Timestamp.Truncate(time.Minute)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.recordInto(s.routes, event.Route, minute, event)
	if event.Backend != "" {
		s.recordInto(s.backends, event.Backend, minute, event)
	}
}

// recordInto is the shared logic for inserting into a bucket map.
func (s *MemoryTrafficStore) recordInto(
	m map[string]map[time.Time]*Bucket, key string, minute time.Time, event TrafficEvent,
) {
	if m[key] == nil {
		m[key] = make(map[time.Time]*Bucket)
	}
	b, exists := m[key][minute]
	if !exists {
		b = &Bucket{
			Route:     key,
			Timestamp: minute,
		}
		m[key][minute] = b
	}
	b.RequestCount++
	b.TotalLatency += event.Latency
	if event.Latency > b.MaxLatency {
		b.MaxLatency = event.Latency
	}
	if event.Status >= 500 {
		b.ErrorCount++
	}
	b.BytesIn += event.BytesIn
	b.BytesOut += event.BytesOut
}

// GetBuckets returns sorted buckets for a single route within [from, to).
func (s *MemoryTrafficStore) GetBuckets(route string, from, to time.Time) []Bucket {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.collectBuckets(s.routes[route], from, to)
}

// GetAllBuckets returns buckets for all routes within [from, to).
func (s *MemoryTrafficStore) GetAllBuckets(from, to time.Time) map[string][]Bucket {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string][]Bucket, len(s.routes))
	for route, bucketMap := range s.routes {
		if buckets := s.collectBuckets(bucketMap, from, to); len(buckets) > 0 {
			result[route] = buckets
		}
	}
	return result
}

// GetRoutes returns all known route names.
func (s *MemoryTrafficStore) GetRoutes() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	routes := make([]string, 0, len(s.routes))
	for route := range s.routes {
		routes = append(routes, route)
	}
	sort.Strings(routes)
	return routes
}

// GetBackendBuckets returns per-backend buckets within [from, to).
func (s *MemoryTrafficStore) GetBackendBuckets(from, to time.Time) map[string][]Bucket {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string][]Bucket, len(s.backends))
	for backend, bucketMap := range s.backends {
		if buckets := s.collectBuckets(bucketMap, from, to); len(buckets) > 0 {
			result[backend] = buckets
		}
	}
	return result
}

// collectBuckets filters and sorts buckets from a timestamp map within [from, to).
// Must be called with at least a read lock held.
func (s *MemoryTrafficStore) collectBuckets(bucketMap map[time.Time]*Bucket, from, to time.Time) []Bucket {
	if bucketMap == nil {
		return nil
	}
	var result []Bucket
	for ts, b := range bucketMap {
		if !ts.Before(from) && ts.Before(to) {
			result = append(result, *b)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.Before(result[j].Timestamp)
	})
	return result
}

// StartCleanup launches a background goroutine that prunes expired buckets
// every 10 minutes.
func (s *MemoryTrafficStore) StartCleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	go func() {
		for range ticker.C {
			s.cleanup()
		}
	}()
}

// cleanup removes all buckets older than the retention period.
func (s *MemoryTrafficStore) cleanup() {
	cutoff := time.Now().Add(-s.retention)

	s.mu.Lock()
	defer s.mu.Unlock()

	pruneMap(s.routes, cutoff)
	pruneMap(s.backends, cutoff)
}

// pruneMap removes entries older than cutoff from a nested bucket map.
func pruneMap(m map[string]map[time.Time]*Bucket, cutoff time.Time) {
	for key, bucketMap := range m {
		for ts := range bucketMap {
			if ts.Before(cutoff) {
				delete(bucketMap, ts)
			}
		}
		// Remove the outer key if no buckets remain
		if len(bucketMap) == 0 {
			delete(m, key)
		}
	}
}
