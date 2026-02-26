package proxy

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"sync"

	"github.com/tanmay/gateway/internal/config"
	"github.com/tanmay/gateway/internal/health"
)

// Proxy routes requests to backends based on configured path prefixes.
// Each route gets a backend selector (LoadBalancer or WeightedLoadBalancer).
// Backends can be added at runtime.
type Proxy struct {
	mux    *http.ServeMux
	routes map[string]BackendSelector // path → backend selector
	mu     sync.RWMutex              // protects routes map
}

// NewProxy creates a Proxy that routes requests to backends
// based on the configured path prefixes.
func NewProxy(cfg *config.Config, hc *health.HealthChecker) *Proxy {
	mux := http.NewServeMux()
	routes := make(map[string]BackendSelector)

	p := &Proxy{
		mux:    mux,
		routes: routes,
	}

	for _, route := range cfg.Routes {
		backends := route.GetBackends()
		lb := NewLoadBalancer(backends, route.Strategy, hc)
		routes[route.Path] = lb

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
			rp := httputil.NewSingleHostReverseProxy(targetURL)
			originalDirector := rp.Director
			rp.Director = func(req *http.Request) {
				originalDirector(req)
				req.Header.Set("X-Forwarded-Host", req.Host)
				req.Header.Set("X-Gateway", "tanmay-gateway")
				log.Printf("[proxy] %s %s → %s", req.Method, req.URL.Path, backend)
			}

			rp.ServeHTTP(w, r)
		})

		mux.Handle(route.Path+"/", handler)
		log.Printf("[init] Route registered: %s → %v (strategy: %s)", route.Path, backends, route.Strategy)
	}

	return p
}

// ServeHTTP implements http.Handler by delegating to the internal mux.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mux.ServeHTTP(w, r)
}

// AddBackend registers a new backend URL with the backend selector for the given route.
func (p *Proxy) AddBackend(routePath, backendURL string) error {
	p.mu.RLock()
	selector, ok := p.routes[routePath]
	p.mu.RUnlock()

	if !ok {
		return fmt.Errorf("route %q not found", routePath)
	}

	selector.AddBackend(backendURL)
	log.Printf("[proxy] Backend added dynamically: %s → %s", routePath, backendURL)
	return nil
}

// SetRouteSelector replaces the backend selector for a specific route.
// Used during startup to swap in a WeightedLoadBalancer when enabled.
func (p *Proxy) SetRouteSelector(routePath string, selector BackendSelector) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.routes[routePath] = selector
}

// RouteNames returns a sorted list of all configured route paths.
func (p *Proxy) RouteNames() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	names := make([]string, 0, len(p.routes))
	for name := range p.routes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
