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
	"github.com/tanmay/gateway/internal/analytics"
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

	// --- Phase 5: Adaptive Traffic Intelligence ---

	// Collect route prefixes for traffic normalization
	var routePrefixes []string
	for _, route := range cfg.Routes {
		routePrefixes = append(routePrefixes, route.Path)
	}

	// Initialize TrafficStore and TrafficRecorder
	var trafficStore analytics.TrafficStore
	var trafficRecorder *middleware.TrafficRecorder
	var analyzer *analytics.Analyzer
	var analyticsAPI *analytics.AnalyticsAPI

	if cfg.Analytics.Enabled {
		retention, _ := time.ParseDuration(cfg.Analytics.Retention)
		if retention <= 0 {
			retention = 48 * time.Hour
		}
		trafficStore = analytics.NewMemoryTrafficStore(retention)
		trafficStore.(*analytics.MemoryTrafficStore).StartCleanup()

		trafficRecorder = middleware.NewTrafficRecorder(trafficStore, routePrefixes)

		// Initialize the Analyzer
		analyzerInterval, _ := time.ParseDuration(cfg.Analytics.AnalyzerInterval)
		analyzer = analytics.NewAnalyzer(trafficStore, analytics.AnalyzerConfig{
			Interval:        analyzerInterval,
			Window:          1 * time.Hour,
			ZScoreThreshold: 3.0,
		})
		analyzer.Start()
		log.Println("[init] Traffic analyzer started")

		// Wire analyzer into circuit breaker for dynamic thresholds
		circuitBreaker.SetAnalyzer(analyzer)

		// Initialize analytics REST API
		analyticsAPI = analytics.NewAnalyticsAPI(analyzer, trafficStore)
	}

	// Build the rate limiting middleware (static or adaptive)
	var rateLimitMiddleware middleware.Middleware
	if cfg.AdaptiveRateLimit.Enabled && analyzer != nil {
		learningPeriod, _ := time.ParseDuration(cfg.AdaptiveRateLimit.LearningPeriod)
		adaptiveRL := middleware.NewAdaptiveRateLimiter(rateLimiter, analyzer, middleware.AdaptiveRateLimitConfig{
			Enabled:        true,
			Multiplier:     cfg.AdaptiveRateLimit.Multiplier,
			MinLimit:       cfg.AdaptiveRateLimit.MinLimit,
			MaxLimit:       cfg.AdaptiveRateLimit.MaxLimit,
			LearningPeriod: learningPeriod,
		})

		// Route resolver: maps a full path to its normalized route prefix
		routeResolver := func(path string) string {
			if trafficRecorder != nil {
				return trafficRecorder.NormalizeRoute(path)
			}
			return path
		}

		rateLimitMiddleware = adaptiveRL.Middleware(routeResolver)
		log.Println("[init] Adaptive rate limiter enabled")
	} else {
		rateLimitMiddleware = rateLimiter.Middleware()
	}

	// Set up weighted load balancers if enabled
	var weightedLBs []*proxy.WeightedLoadBalancer
	if cfg.WeightedLB.Enabled && analyzer != nil {
		rebalanceInterval, _ := time.ParseDuration(cfg.WeightedLB.RebalanceInterval)
		for _, route := range cfg.Routes {
			backends := route.GetBackends()
			if len(backends) <= 1 {
				continue // no point in weighted LB for single backend
			}

			wlb := proxy.NewWeightedLoadBalancer(backends, analyzer, healthChecker, rebalanceInterval)
			wlb.StartRebalancing()
			proxyHandler.SetRouteSelector(route.Path, wlb)
			weightedLBs = append(weightedLBs, wlb)
			log.Printf("[init] Weighted LB enabled for %s", route.Path)
		}

		// Provide weight data to analytics API
		if analyticsAPI != nil && len(weightedLBs) > 0 {
			analyticsAPI.SetWeightProvider(func() map[string]float64 {
				allWeights := make(map[string]float64)
				for _, wlb := range weightedLBs {
					for backend, weight := range wlb.GetWeights() {
						allWeights[backend] = weight
					}
				}
				return allWeights
			})
		}
	}

	// Build middleware chain
	middlewares := []middleware.Middleware{
		middleware.RequestID(),
		middleware.Capture(logStore),
		middleware.Metrics(),
	}

	// Add traffic recording middleware if analytics is enabled
	if trafficRecorder != nil {
		middlewares = append(middlewares, trafficRecorder.Middleware())
	}

	middlewares = append(middlewares,
		middleware.Logging(),
		rateLimitMiddleware,
		auth.Middleware(),
		circuitBreaker.Middleware(),
	)

	handler := middleware.Chain(proxyHandler, middlewares...)

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
	dashboardAPI.StartMetricsBroadcast(5 * time.Second)

	// Register routes
	mux := http.NewServeMux()
	mux.Handle("/health", healthChecker.Handler()) // outside middleware chain — no auth/rate limit
	mux.Handle("/metrics", promhttp.Handler())     // Prometheus metrics endpoint

	// Analytics API (outside middleware chain)
	if analyticsAPI != nil {
		log.Println("[init] Analytics API enabled at /analytics/")
		mux.Handle("/analytics/", http.StripPrefix("/analytics", analyticsAPI.Handler()))
	}

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
