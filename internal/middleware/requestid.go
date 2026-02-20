package middleware

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
)

// requestIDKey is a private type for context keys to avoid collisions.
// Using a custom type (not string) ensures no other package can accidentally
// overwrite this context value.
type requestIDKey struct{}

// generateID creates a short unique ID using crypto/rand.
// Format: 8 hex characters (4 random bytes), e.g. "a1b2c3d4"
// This is simpler than a full UUID but sufficient for request tracing.
func generateID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// RequestID returns a Middleware that assigns a unique ID to every request.
// The ID is:
//   - Set as the X-Request-ID response header (for the client)
//   - Stored in the request context (for other middleware/handlers to access)
//   - If the client already sends X-Request-ID, we reuse it (for distributed tracing)
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Reuse client-provided request ID if present (supports distributed tracing)
			requestID := r.Header.Get("X-Request-ID")
			if requestID == "" {
				requestID = generateID()
			}

			// Add to response headers so the client can see the ID
			w.Header().Set("X-Request-ID", requestID)

			// Store in request context so downstream handlers can access it
			ctx := context.WithValue(r.Context(), requestIDKey{}, requestID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetRequestID extracts the request ID from the context.
// Returns empty string if no request ID is set.
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}
