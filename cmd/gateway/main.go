package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
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

	// Initialize dashboard process manager, log store, and SSE broker early so middleware can use it
	pm := dashboard.NewProcessManager()
	logStore := dashboard.NewLogStore(1000)
	broker := dashboard.NewBroker()

	// Hook ProcessManager events to the SSE broker
	pm.OnStateChange = func(p dashboard.ManagedProcess) {
		broker.Broadcast("process", p)
	}

	// Chain middleware: RequestID → Metrics → Capture → Logging → RateLimit → Auth → CircuitBreaker → Proxy
	handler := middleware.Chain(
		proxyHandler,
		middleware.RequestID(),
		middleware.Capture(logStore),
		middleware.Metrics(),
		middleware.Logging(),
		rateLimiter.Middleware(),
		auth.Middleware(),
		circuitBreaker.Middleware(),
	)

	// Populate managed processes from config
	for _, procCfg := range cfg.Processes {
		if err := pm.Add(procCfg.ID, procCfg.Command, procCfg.Args, procCfg.Port); err != nil {
			log.Printf("Error adding process %s: %v", procCfg.ID, err)
			continue
		}
		if procCfg.AutoStart {
			if err := pm.Start(procCfg.ID); err != nil {
				log.Printf("Failed to auto-start process %s: %v", procCfg.ID, err)
			} else {
				log.Printf("Auto-started process %s on port %d", procCfg.ID, procCfg.Port)
			}
		}
	}

	// Hook HealthChecker events to the SSE broker
	healthChecker.OnStateChange = func(url string, isHealthy bool) {
		broker.Broadcast("service", map[string]interface{}{
			"url":     url,
			"healthy": isHealthy,
		})
	}

	dashboardAPI := dashboard.NewAPI(pm, healthChecker, proxyHandler, logStore, broker)

	// Register routes
	mux := http.NewServeMux()
	mux.Handle("/health", healthChecker.Handler()) // outside middleware chain — no auth/rate limit
	mux.Handle("/metrics", promhttp.Handler())     // Prometheus metrics endpoint

	// Dashboard
	if cfg.Dashboard.Enabled {
		log.Println("Dashboard enabled - UI hosted at /dashboard/")
		mux.Handle("/dashboard/api/", http.StripPrefix("/dashboard/api", dashboardAPI.Handler()))
		// Serve React frontend (ensure trailing slash matches React router/assets if applicable)
		mux.Handle("/dashboard/", http.StripPrefix("/dashboard/", http.FileServer(http.Dir("web/dashboard/dist"))))
	}

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
