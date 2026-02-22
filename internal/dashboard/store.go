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
