package middleware

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/tanmay/gateway/internal/dashboard"
)

// responseCapture wraps http.ResponseWriter to capture the status code,
// byte size, and still support Hijacker/Flusher interfaces if needed (e.g. for SSE/WebSockets).
type responseCapture struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
}

// WriteHeader intercepts the status code before passing it through.
func (rw *responseCapture) WriteHeader(code int) {
	if rw.statusCode == 0 {
		rw.statusCode = code
		rw.ResponseWriter.WriteHeader(code)
	}
}

// Write intercepts the byte write to track response size.
func (rw *responseCapture) Write(b []byte) (int, error) {
	if rw.statusCode == 0 {
		rw.WriteHeader(http.StatusOK)
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += int64(n)
	return n, err
}

// Flush implements http.Flusher
func (rw *responseCapture) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker
func (rw *responseCapture) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, errors.New("http.Hijacker interface is not supported")
}

// Capture returns a Middleware that silently pushes request logs to the Dashboard LogStore
// via a background goroutine to avoid adding latency to the request processing path.
func Capture(store *dashboard.LogStore) Middleware {
	ch := make(chan dashboard.RequestLog, 256)

	// Background worker to consume logs and add to store
	go func() {
		for log := range ch {
			store.Add(log)
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap the response writer
			wrapped := &responseCapture{ResponseWriter: w, statusCode: 0}

			// Execute the rest of the chain
			next.ServeHTTP(wrapped, r)

			// If no status was explicitly set during the request, default to 200
			if wrapped.statusCode == 0 {
				wrapped.statusCode = http.StatusOK
			}

			clientIP, _, _ := net.SplitHostPort(r.RemoteAddr)
			if clientIP == "" {
				clientIP = r.RemoteAddr
			}

			// Try to identify backend from context or headers (if set by proxy)
			backend := w.Header().Get("X-Proxy-Backend")

			// Push log to channel anonymously
			select {
			case ch <- dashboard.RequestLog{
				ID:        GetRequestID(r.Context()),
				Timestamp: start.UTC(),
				Method:    r.Method,
				Path:      r.URL.Path,
				Status:    wrapped.statusCode,
				Latency:   time.Since(start),
				ClientIP:  clientIP,
				BytesOut:  wrapped.bytesWritten,
				BytesIn:   r.ContentLength, // Request Content-Length
				Backend:   backend,
			}:
			default:
				// Channel is full, we drop it rather than block the response.
				// This shouldn't happen unless under extreme immediate load
			}
		})
	}
}
