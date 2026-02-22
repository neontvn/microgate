package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/tanmay/gateway/internal/config"
	"github.com/tanmay/gateway/internal/dashboard"
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

	// Chain middleware: RequestID → Metrics → Logging → RateLimit → Auth → CircuitBreaker → Proxy
	handler := middleware.Chain(
		proxyHandler,
		middleware.RequestID(),
		middleware.Metrics(),
		middleware.Logging(),
		rateLimiter.Middleware(),
		auth.Middleware(),
		circuitBreaker.Middleware(),
	)

	// Initialize dashboard process manager
	pm := dashboard.NewProcessManager()

	// Dynamically populate processes from config
	// All servers are added in stopped state by default
	for _, rawURL := range backendURLs {
		if parsedURL, err := url.Parse(rawURL); err == nil {
			portStr := parsedURL.Port()
			if portStr != "" {
				if port, err := strconv.Atoi(portStr); err == nil {
					id := fmt.Sprintf("backend-%d", port)
					pm.Add(id, "./tmp/testbackend", []string{"-port", portStr}, port)
				}
			}
		}
	}

	dashboardAPI := dashboard.NewAPI(pm, healthChecker)

	// Register routes
	mux := http.NewServeMux()
	mux.Handle("/health", healthChecker.Handler()) // outside middleware chain — no auth/rate limit
	mux.Handle("/metrics", promhttp.Handler())     // Prometheus metrics endpoint

	// Dashboard API (strip prefix to match dashboard internal routes)
	mux.Handle("/dashboard/api/", http.StripPrefix("/dashboard/api", dashboardAPI.Handler()))

	mux.Handle("/", handler) // everything else goes through middleware

	// Setup graceful shutdown
	serverCtx, stop := context.WithCancel(context.Background())
	defer stop()

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{Addr: addr, Handler: mux}

	// Wait for interrupt signal to gracefully shutdown the server
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan

		log.Println("Shutting down gateway and backend processes...")
		pm.StopAll() // Kill all managed processes

		// Shutdown the HTTP server
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
		stop()
	}()

	// Start the gateway server
	log.Printf("API Gateway starting on %s", addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("HTTP server ListenAndServe: %v", err)
	}

	<-serverCtx.Done()
	log.Println("Gateway shutdown complete")
}
