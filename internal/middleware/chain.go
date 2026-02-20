package middleware

import "net/http"

// Middleware is a function that wraps an http.Handler with additional behavior.
type Middleware func(http.Handler) http.Handler

// Chain applies middlewares to a handler in the order they are provided.
// Chain(handler, A, B, C) produces A(B(C(handler))).
// Request flow: A → B → C → handler.
func Chain(handler http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}
