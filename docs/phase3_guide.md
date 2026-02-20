# Phase 3: Observability, Health Checks & Load Balancing

## 🧠 Core Concept: Production Readiness

Your gateway works — but in production you need to **see** what's happening, know when backends are healthy, and distribute traffic across multiple instances.

```
                         ┌─ Backend A:9001 (healthy ✅)
Client → Gateway:8080 ──┤─ Backend B:9002 (healthy ✅)
         │               └─ Backend C:9003 (down ❌ → skip)
         │
         ├── /health          → health check endpoint
         ├── /metrics         → Prometheus metrics
         └── structured logs  → JSON logging
```

---

## Step 1: `internal/middleware/logging.go` — Structured JSON Logging

Replace `log.Printf` with structured JSON logs. Production systems need machine-parseable logs.

**Before:**
```
2026/02/18 17:45:00 GET /api/v1/users 200 45ms
```

**After:**
```json
{"timestamp":"2026-02-18T17:45:00Z","method":"GET","path":"/api/v1/users","status":200,"duration_ms":45,"client_ip":"192.168.1.5"}
```

**You'll need:**
1. A `logEntry` struct with fields: `Timestamp`, `Method`, `Path`, `Status`, `DurationMs`, `ClientIP`
2. Use `encoding/json` to marshal the struct
3. Write the JSON to `os.Stdout` (not `log.Printf` — avoids the timestamp prefix)

**Key Go concept**: `json.NewEncoder(os.Stdout).Encode(entry)` — streams JSON directly to stdout without allocating a string.

---

## Step 2: `internal/health/health.go` — Health Check Endpoint

Expose a `/health` endpoint that reports gateway + backend status.

**Response:**
```json
{
  "status": "healthy",
  "uptime": "2h15m30s",
  "backends": {
    "http://localhost:9001": "healthy",
    "http://localhost:9002": "unhealthy"
  }
}
```

**You'll need:**
1. A `HealthChecker` struct that holds the config and a start time
2. A `checkBackend(url string) bool` method — make an HTTP GET to the backend, return true if status 200
3. A `Handler()` method returning `http.HandlerFunc` that:
   - Checks each backend
   - Returns 200 if all healthy, 503 if any are down
   - Returns JSON with status, uptime, and per-backend health

**Key concept**: The `/health` endpoint is registered **outside** the middleware chain — you don't want health checks rate-limited or auth-gated.

---

## Step 3: `internal/health/checker.go` — Background Health Checks

Don't check backend health on every request — run checks **in the background** on a timer.

**You'll need:**
1. A `BackendStatus` struct: `URL string`, `Healthy bool`, `LastCheck time.Time`
2. A goroutine that runs every N seconds (configurable via `health_check_interval` in config)
3. Updates a `map[string]*BackendStatus` (protected by `sync.RWMutex`)
4. The health endpoint reads from this map instead of checking live

**Key Go concepts:**
- `time.NewTicker(interval)` — fires on a schedule
- `sync.RWMutex` — allows multiple readers OR one writer (better than `sync.Mutex` for read-heavy workloads)
- Goroutines — `go checker.Run()` starts the background loop

---

## Step 4: `internal/proxy/loadbalancer.go` — Load Balancing

Support multiple backends per route and distribute traffic.

**Update `config.yml`:**
```yaml
routes:
  - path: "/api/v1"
    backends:
      - "http://localhost:9001"
      - "http://localhost:9002"
      - "http://localhost:9003"
    strategy: "round-robin"
```

**You'll need:**
1. A `LoadBalancer` struct with: `backends []string`, `current uint64`, `mu sync.Mutex`
2. Three strategies:
   - **Round Robin**: cycle through backends using an atomic counter
   - **Random**: pick a random backend with `math/rand`
   - **Least Connections**: track active connections per backend (optional, advanced)
3. A `Next() string` method that returns the next backend URL based on strategy
4. Skip unhealthy backends (integrate with the health checker from Step 3)

**Key Go concept**: `sync/atomic.AddUint64(&counter, 1)` — lock-free incrementing for round-robin, much faster than mutex for this use case.

---

## Step 5: `internal/middleware/requestid.go` — Request Tracing

Assign a unique ID to every request for tracing across logs.

**You'll need:**
1. Generate a UUID using `crypto/rand` (or `github.com/google/uuid`)
2. Add it to the request context and response headers:
   - Set `X-Request-ID` response header
   - Add to request context so other middleware/handlers can access it
3. Update logging middleware to include the request ID in every log line

**Key Go concept**: `context.WithValue(r.Context(), key, requestID)` — attach data to the request that flows through all handlers.

---

## Step 6: Wire It Up in `cmd/gateway/main.go`

```go
// Register health endpoint (outside middleware chain)
http.Handle("/health", healthChecker.Handler())

// Chain middleware with request ID
handler := middleware.Chain(
    proxyHandler,
    middleware.RequestID(),
    middleware.Logging(),
    rateLimiter.Middleware(),
    auth.Middleware(),
    circuitBreaker.Middleware(),
)
```

Update `config.yml` to add health check and load balancing settings.

---

## Order to Code

| Order | File | Why this order |
|-------|------|---------------|
| 1 | `logging.go` (update) | Foundation — all other features need good logs |
| 2 | `health.go` | Simple HTTP endpoint, no dependencies |
| 3 | `checker.go` | Adds background goroutines, builds on health |
| 4 | `loadbalancer.go` | Most complex — integrates with health checker |
| 5 | `requestid.go` | Quick win — enhances logging |
| 6 | Update `main.go` | Wire it all together |

**Start with structured logging — it makes debugging everything else much easier!**
