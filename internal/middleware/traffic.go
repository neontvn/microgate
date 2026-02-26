package middleware

import (
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/tanmay/gateway/internal/analytics"
)

// TrafficRecorder captures per-request metrics and writes them to a TrafficStore
// asynchronously via a buffered channel, following the same pattern as Capture middleware.
type TrafficRecorder struct {
	events chan analytics.TrafficEvent
	store  analytics.TrafficStore
	routes []string // known route prefixes, sorted longest-first for matching
}

// NewTrafficRecorder creates a TrafficRecorder with the given store and known route prefixes.
// It starts a background goroutine to drain events into the store.
func NewTrafficRecorder(store analytics.TrafficStore, routePrefixes []string) *TrafficRecorder {
	// Sort prefixes longest-first so we match the most specific route
	sorted := make([]string, len(routePrefixes))
	copy(sorted, routePrefixes)
	sort.Slice(sorted, func(i, j int) bool {
		return len(sorted[i]) > len(sorted[j])
	})

	tr := &TrafficRecorder{
		events: make(chan analytics.TrafficEvent, 256),
		store:  store,
		routes: sorted,
	}

	// Background worker drains events into the store
	go func() {
		for event := range tr.events {
			tr.store.Record(event)
		}
	}()

	return tr
}

// NormalizeRoute matches a request path to its configured route prefix.
// Returns the matched prefix (e.g., "/api/v1") or the raw path if no match.
func (tr *TrafficRecorder) NormalizeRoute(path string) string {
	for _, prefix := range tr.routes {
		if strings.HasPrefix(path, prefix+"/") || path == prefix {
			return prefix
		}
	}
	return path
}

// Middleware returns a Middleware that records traffic events for every request.
func (tr *TrafficRecorder) Middleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Reuse the responseCapture wrapper from capture.go
			wrapped := &responseCapture{ResponseWriter: w, statusCode: 0}

			next.ServeHTTP(wrapped, r)

			if wrapped.statusCode == 0 {
				wrapped.statusCode = http.StatusOK
			}

			clientIP, _, _ := net.SplitHostPort(r.RemoteAddr)
			if clientIP == "" {
				clientIP = r.RemoteAddr
			}

			backend := w.Header().Get("X-Proxy-Backend")

			select {
			case tr.events <- analytics.TrafficEvent{
				Route:     tr.NormalizeRoute(r.URL.Path),
				Backend:   backend,
				Status:    wrapped.statusCode,
				Latency:   time.Since(start),
				BytesIn:   r.ContentLength,
				BytesOut:  wrapped.bytesWritten,
				ClientIP:  clientIP,
				Timestamp: start.UTC(),
			}:
			default:
				// Drop event if channel is full rather than blocking the response
			}
		})
	}
}
