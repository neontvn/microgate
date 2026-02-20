package proxy

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/tanmay/gateway/internal/config"
	"github.com/tanmay/gateway/internal/health"
)

// NewProxy creates an http.Handler that routes requests to backends
// based on the configured path prefixes.
// Each route gets a load balancer if it has multiple backends.
func NewProxy(cfg *config.Config, hc *health.HealthChecker) http.Handler {
	mux := http.NewServeMux()

	for _, route := range cfg.Routes {
		backends := route.GetBackends()
		lb := NewLoadBalancer(backends, route.Strategy, hc)

		// Create a handler that picks a backend per-request via the load balancer
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			backend := lb.Next()
			if backend == "" {
				http.Error(w, "No healthy backends available", http.StatusServiceUnavailable)
				return
			}

			targetURL, err := url.Parse(backend)
			if err != nil {
				http.Error(w, "Bad backend URL", http.StatusInternalServerError)
				return
			}

			// Create a reverse proxy for the selected backend
			proxy := httputil.NewSingleHostReverseProxy(targetURL)
			originalDirector := proxy.Director
			proxy.Director = func(req *http.Request) {
				originalDirector(req)
				req.Header.Set("X-Forwarded-Host", req.Host)
				req.Header.Set("X-Gateway", "tanmay-gateway")
				log.Printf("[proxy] %s %s → %s", req.Method, req.URL.Path, backend)
			}

			proxy.ServeHTTP(w, r)
		})

		mux.Handle(route.Path+"/", handler)
		log.Printf("[init] Route registered: %s → %v (strategy: %s)", route.Path, backends, route.Strategy)
	}

	return mux
}
