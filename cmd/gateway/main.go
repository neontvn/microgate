package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/tanmay/gateway/internal/config"
	"github.com/tanmay/gateway/internal/health"
	"github.com/tanmay/gateway/internal/middleware"
	"github.com/tanmay/gateway/internal/proxy"
)

func main() {
	// Load configuration
	cfg, err := config.LoadConfig("config.yml")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// Collect all backend URLs for health checking
	var backendURLs []string
	for _, route := range cfg.Routes {
		backendURLs = append(backendURLs, route.GetBackends()...)
	}

	// Initialize health checker and start background checks
	healthChecker := health.NewHealthChecker(backendURLs)
	healthChecker.StartBackground(time.Duration(cfg.HealthCheck.Interval) * time.Second)

	// Create the reverse proxy handler (now with load balancing + health awareness)
	proxyHandler := proxy.NewProxy(cfg, healthChecker)

	// Initialize middleware
	rateLimiter := middleware.NewRateLimiter(cfg.RateLimit.MaxTokens, cfg.RateLimit.RefillRate)
	auth := middleware.NewAuth(cfg.Auth.APIKeys, cfg.Auth.JWTSecret)
	circuitBreaker := middleware.NewCircuitBreaker(cfg.CircuitBreaker.Threshold, time.Duration(cfg.CircuitBreaker.Timeout)*time.Second)

	// Chain middleware: RequestID → Logging → RateLimit → Auth → CircuitBreaker → Proxy
	handler := middleware.Chain(
		proxyHandler,
		middleware.RequestID(),
		middleware.Logging(),
		rateLimiter.Middleware(),
		auth.Middleware(),
		circuitBreaker.Middleware(),
	)

	// Register routes
	mux := http.NewServeMux()
	mux.Handle("/health", healthChecker.Handler()) // outside middleware chain — no auth/rate limit
	mux.Handle("/", handler)                       // everything else goes through middleware

	// Start the gateway server
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("API Gateway starting on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
