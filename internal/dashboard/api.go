package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/tanmay/gateway/internal/health"
)

// API provides HTTP endpoints for the dashboard
type API struct {
	pm *ProcessManager
	hc *health.HealthChecker
}

// NewAPI creates a new dashboard API
func NewAPI(pm *ProcessManager, hc *health.HealthChecker) *API {
	return &API{
		pm: pm,
		hc: hc,
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
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if err := api.pm.Add(req.ID, req.Command, req.Args, req.Port); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}

		w.WriteHeader(http.StatusCreated)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// handleProcessAction handles POST /processes/{id}/start and /processes/{id}/stop
func (api *API) handleProcessAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Simple path parsing: /processes/{id}/{action}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/processes/"), "/")
	if len(parts) != 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	id := parts[0]
	action := parts[1]

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
