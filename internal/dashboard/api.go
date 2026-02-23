package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tanmay/gateway/internal/health"
	"github.com/tanmay/gateway/internal/proxy"
)

// API provides HTTP endpoints for the dashboard
type API struct {
	pm    *ProcessManager
	hc    *health.HealthChecker
	proxy *proxy.Proxy
	store *LogStore
	broker *Broker
}

// NewAPI creates a new dashboard API
func NewAPI(pm *ProcessManager, hc *health.HealthChecker, p *proxy.Proxy, store *LogStore, broker *Broker) *API {

	// Hook the log store to emit 'request' events
	store.OnAdd = func(log RequestLog) {
		broker.Broadcast("request", log)
	}

	return &API{
		pm:     pm,
		hc:     hc,
		proxy:  p,
		store:  store,
		broker: broker,
	}
}

// Handler returns an http.Handler with all routes configured
func (api *API) Handler() http.Handler {
	mux := http.NewServeMux()

	// Enable CORS for development
	corsHandler := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			h(w, r)
		}
	}

	mux.HandleFunc("/processes", corsHandler(api.handleProcesses))
	mux.HandleFunc("/processes/", corsHandler(api.handleProcessAction))
	mux.HandleFunc("/routes", corsHandler(api.handleRoutes))
	mux.HandleFunc("/metrics", corsHandler(api.handleMetrics))
	mux.HandleFunc("/logs", corsHandler(api.handleLogs))
	mux.HandleFunc("/logs/", corsHandler(api.handleLogDetail))

	// Server-Sent Events stream
	mux.HandleFunc("/stream", api.broker.StreamHandler())

	return mux
}

// handleProcesses handles GET /processes to list, and POST /processes to add
func (api *API) handleProcesses(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		list := api.pm.List()

		// Add health status to each process
		type ProcessWithHealth struct {
			ManagedProcess
			Healthy bool `json:"healthy"`
		}

		out := make([]ProcessWithHealth, 0, len(list))
		for _, p := range list {
			url := fmt.Sprintf("http://localhost:%d", p.Port) // Match the URL registered in config/HealthChecker exactly
			isHealthy := api.hc.IsHealthy(url)

			// If it's stopped/crashed, it shouldn't show as healthy even if HealthChecker hasn't marked it down yet
			if p.Status != StatusRunning {
				isHealthy = false
			}

			out = append(out, ProcessWithHealth{
				ManagedProcess: p,
				Healthy:        isHealthy,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"processes": out,
		})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			ID      string   `json:"id"`
			Command string   `json:"command"`
			Args    []string `json:"args"`
			Port    int      `json:"port"`
			Route   string   `json:"route"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if err := api.pm.Add(req.ID, req.Command, req.Args, req.Port); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}

		// Register the backend with the route's load balancer and health checker
		backendURL := fmt.Sprintf("http://localhost:%d", req.Port)
		if req.Route != "" {
			if err := api.proxy.AddBackend(req.Route, backendURL); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		api.hc.AddBackend(backendURL)

		w.WriteHeader(http.StatusCreated)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// handleProcessAction handles POST /processes/{id}/start, /processes/{id}/stop,
// and GET /processes/{id}/logs
func (api *API) handleProcessAction(w http.ResponseWriter, r *http.Request) {
	// Simple path parsing: /processes/{id}/{action}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/processes/"), "/")
	if len(parts) != 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	id := parts[0]
	action := parts[1]

	// GET /processes/{id}/logs
	if action == "logs" && r.Method == http.MethodGet {
		lines := 100
		if l := r.URL.Query().Get("lines"); l != "" {
			if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
				lines = parsed
			}
		}
		output, err := api.pm.Logs(id, lines)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"lines": output,
		})
		return
	}

	// POST actions: start, stop
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var err error
	switch action {
	case "start":
		err = api.pm.Start(id)
	case "stop":
		err = api.pm.Stop(id)
	default:
		http.Error(w, "Unknown action", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// handleLogs handles GET /logs to list recent request logs with optional filters
func (api *API) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()

	limit := 50
	if l := q.Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	status := 0
	if s := q.Get("status"); s != "" {
		if parsed, err := strconv.Atoi(s); err == nil {
			status = parsed
		}
	}

	path := q.Get("path")

	// If no specific filters, use Recent() for faster retrieval
	var logs []RequestLog
	if status == 0 && path == "" {
		logs = api.store.Recent(limit)
	} else {
		logs = api.store.Search(limit, status, path)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"logs": logs,
	})
}

// handleRoutes handles GET /routes to list available proxy route paths
func (api *API) handleRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"routes": api.proxy.RouteNames(),
	})
}

// handleMetrics handles GET /metrics to return real-time gateway metrics
func (api *API) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	snap := api.store.Metrics()
	healthy, total := api.hc.BackendCounts()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"requests_per_minute": snap.RequestsPerMinute,
		"avg_latency_ms":     snap.AvgLatencyMs,
		"error_rate":         snap.ErrorRate,
		"healthy_backends":   healthy,
		"total_backends":     total,
		"uptime":             api.hc.Uptime(),
		"sparklines":         snap.Sparklines,
	})
}

// StartMetricsBroadcast sends a metrics SSE event every interval.
func (api *API) StartMetricsBroadcast(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			snap := api.store.Metrics()
			healthy, total := api.hc.BackendCounts()
			api.broker.Broadcast("metrics", map[string]interface{}{
				"requests_per_minute": snap.RequestsPerMinute,
				"avg_latency_ms":     snap.AvgLatencyMs,
				"error_rate":         snap.ErrorRate,
				"healthy_backends":   healthy,
				"total_backends":     total,
				"uptime":             api.hc.Uptime(),
				"sparklines":         snap.Sparklines,
			})
		}
	}()
}

// handleLogDetail handles GET /logs/{id} to get a single log
func (api *API) handleLogDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/logs/")
	if id == "" {
		http.Error(w, "Log ID required", http.StatusBadRequest)
		return
	}

	log, found := api.store.GetByID(id)
	if !found {
		http.Error(w, "Log not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(log)
}
