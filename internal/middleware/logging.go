package middleware

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"time"
)

// responseWriter wraps http.ResponseWriter to capture the status code.
// Problem: Go's http.ResponseWriter doesn't let you read the status code
// after WriteHeader() is called. So we intercept it.
//
// By embedding http.ResponseWriter, this struct automatically satisfies
// the http.ResponseWriter interface — we only override what we need.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader intercepts the status code before passing it through.
// This is called by the handler (or Go itself) to set the HTTP status.
// We save it, then delegate to the real ResponseWriter.
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// logEntry is the structured log format for each request.
// Using JSON makes logs machine-parseable — tools like Datadog, Splunk,
// and ELK can ingest these directly without custom parsers.
type logEntry struct {
	Timestamp  string `json:"timestamp"`
	RequestID  string `json:"request_id,omitempty"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	Status     int    `json:"status"`
	DurationMs int64  `json:"duration_ms"`
	ClientIP   string `json:"client_ip"`
}

// Logging returns a Middleware that logs every request as structured JSON.
// Pattern: Middleware returns a func that returns a func — this is the
// standard Go middleware closure pattern.
//
// The outer func receives `next` (the handler to wrap).
// The inner func is the actual HTTP handler that runs per-request.
func Logging() Middleware {
	// Create a JSON encoder once — reused across all requests.
	// Writes directly to stdout, avoiding log.Printf's timestamp prefix.
	encoder := json.NewEncoder(os.Stdout)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Capture the start time BEFORE the request is processed
			start := time.Now()

			// Wrap the real ResponseWriter so we can read the status code later.
			// Default to 200 because Go sends 200 if WriteHeader is never called explicitly.
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			// Call the next handler in the chain — this is where the actual work happens.
			// Everything above this line is "before" logic, everything below is "after" logic.
			next.ServeHTTP(wrapped, r)

			// Extract client IP without port
			clientIP, _, _ := net.SplitHostPort(r.RemoteAddr)

			// Log as structured JSON after the request completes
			encoder.Encode(logEntry{
				Timestamp:  start.UTC().Format(time.RFC3339),
				RequestID:  GetRequestID(r.Context()),
				Method:     r.Method,
				Path:       r.URL.Path,
				Status:     wrapped.statusCode,
				DurationMs: time.Since(start).Milliseconds(),
				ClientIP:   clientIP,
			})
		})
	}
}
