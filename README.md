# MicroGate

A production-grade API gateway built in Go. MicroGate sits between clients and your backend services, handling authentication, rate limiting, load balancing, circuit breaking, and observability — and it gets smarter over time by learning from live traffic patterns.

## Features

### Core Gateway
- **Reverse Proxy** — routes requests by URL path prefix to one or more backends
- **Load Balancing** — round-robin and random strategies for multi-backend routes
- **Health Checking** — periodic background checks skip unhealthy backends automatically
- **Authentication** — API key and JWT Bearer token validation
- **Rate Limiting** — token bucket algorithm with configurable capacity and refill rate
- **Circuit Breaker** — trips after N consecutive failures, auto-recovers after a timeout
- **Request IDs** — unique `X-Request-Id` header attached to every request for end-to-end tracing
- **Prometheus Metrics** — standard HTTP metrics exposed at `/metrics`
- **Graceful Shutdown** — drains in-flight requests and stops managed processes on `SIGTERM`

### Adaptive Intelligence
- **Traffic Recording** — every request is asynchronously sampled into 1-minute time buckets
- **Background Analyzer** — runs every 5 minutes to compute moving averages, standard deviations, and z-score anomaly detection per route and per backend
- **Adaptive Rate Limiter** — dynamically sets per-route limits at `mean_rate × multiplier` instead of a hardcoded number; falls back to static config during the learning period
- **Weighted Load Balancer** — routes more traffic to faster, healthier backends based on live latency scores; rebalances every 5 minutes
- **Auto-Tuning Circuit Breaker** — trip threshold scales with the backend's historical error baseline (e.g., a 0.1% normal error rate trips much earlier than a 2% normal rate)
- **Analytics REST API** — exposes all learned intelligence via dedicated endpoints

### Real-Time Dashboard
- React frontend served at `/dashboard/`
- Live request log table updated via Server-Sent Events
- Backend process manager — start/stop backends from the UI
- Health status stream for all registered backends

## Architecture

```
                         config.yml
                              │
                              ▼
Client ──HTTP──▶  GATEWAY SERVER (:8080)
                  │
                  ├── Middleware Chain
                  │   RequestID → Capture → Metrics → Traffic Recorder
                  │   → Logging → Adaptive Rate Limiter → Auth → Circuit Breaker
                  │
                  ├── Weighted Load Balancer
                  │   picks the fastest healthy backend per route
                  │
                  ├── Reverse Proxy
                  │   /api/v1 → :9001, :9002, :9003
                  │   /api/v2 → :9004
                  │
                  ├── Adaptive Intelligence Layer (background)
                  │   TrafficStore (48h ring buffer) ←→ Analyzer (every 5m)
                  │   Anomaly Channel → Prometheus alerts
                  │
                  ├── /health         — backend health status
                  ├── /metrics        — Prometheus scrape endpoint
                  ├── /analytics/...  — learned traffic intelligence
                  └── /dashboard/     — React UI + SSE streams
```

See [`docs/architecture.md`](docs/architecture.md) for full ASCII diagrams of both the static (Phase 1-4) and adaptive (Phase 5) architectures.

## Request Flow

1. **RequestID** — stamps a unique trace ID onto the request
2. **Traffic Recorder** — pushes an async data point to the TrafficStore ring buffer
3. **Capture** — wraps the ResponseWriter to measure latency and response size
4. **Metrics** — increments Prometheus counters
5. **Logging** — prints method + path to stdout
6. **Adaptive Rate Limiter** — checks current route baseline; rejects with `429` if above `mean × 3.0`
7. **Auth** — validates API key or JWT; rejects with `401` if missing or invalid
8. **Circuit Breaker** — rejects with `503` if the target backend is currently tripped
9. **Weighted Load Balancer** — picks the highest-scoring healthy backend
10. **Reverse Proxy** — forwards the request and streams the response back

On the way out, the Capture middleware records latency and broadcasts a `RequestLog` SSE event to the dashboard.

See [`docs/request_flow.md`](docs/request_flow.md) and [`docs/request_flow_adaptive.md`](docs/request_flow_adaptive.md) for detailed walkthroughs.

## Project Structure

```
.
├── cmd/
│   ├── gateway/         # Main gateway entrypoint
│   └── testbackend/     # Lightweight test backend server
├── internal/
│   ├── analytics/       # TrafficStore, Analyzer, Analytics REST API
│   ├── config/          # YAML config parsing
│   ├── dashboard/       # SSE broker, log store, process manager, API
│   ├── health/          # Background health checker
│   ├── middleware/       # RequestID, Capture, Metrics, Logging, RateLimit,
│   │                    # Auth, CircuitBreaker, TrafficRecorder, AdaptiveRateLimit
│   └── proxy/           # Reverse proxy, round-robin LB, weighted LB
├── web/dashboard/       # React frontend (built output in dist/)
├── docs/                # Architecture diagrams and phase guides
└── config.yml           # Gateway configuration
```

## Getting Started

### Prerequisites

- Go 1.23+
- Node.js (to build the dashboard frontend)

### Run the gateway

```bash
# Build the test backend binary
go build -o ./tmp/testbackend ./cmd/testbackend

# Start the gateway (reads config.yml)
go run ./cmd/gateway
```

The gateway starts on port `8080`. With the default config, it auto-starts backend processes on ports `9001` and `9002`.

### Run a manual test backend

```bash
go run cmd/testbackend/main.go -port 9001
```

### Send a request

```bash
curl -H "X-API-Key: key-abc123" http://localhost:8080/api/v1/hello
```

## Configuration

```yaml
server:
  port: 8080

routes:
  - path: "/api/v1"
    backends:
      - "http://localhost:9001"
      - "http://localhost:9002"
      - "http://localhost:9003"
    strategy: "round-robin"
  - path: "/api/v2"
    backend: "http://localhost:9004"

ratelimit:
  max_tokens: 10       # token bucket capacity
  refill_rate: 1.0     # tokens added per second

auth:
  api_keys:
    - "key-abc123"
    - "key-xyz789"
  jwt_secret: "my-super-secret-key"

circuitbreaker:
  threshold: 5    # consecutive failures before tripping
  timeout: 30     # seconds before attempting recovery

healthcheck:
  interval: 10    # seconds between health pings

dashboard:
  enabled: true
  log_capacity: 1000
  sse_buffer: 256

analytics:
  enabled: true
  bucket_interval: "1m"
  retention: "48h"
  analyzer_interval: "5m"

adaptive_rate_limit:
  enabled: true
  multiplier: 3.0         # allow up to 3× normal traffic
  min_limit: 10
  max_limit: 10000
  learning_period: "1h"   # use static limit until enough data is collected

weighted_lb:
  enabled: true
  rebalance_interval: "5m"

processes:
  - id: "backend-9001"
    command: "./tmp/testbackend"
    args: ["-port", "9001"]
    port: 9001
    auto_start: true
```

## API Endpoints

| Endpoint | Auth required | Description |
|---|---|---|
| `GET /health` | No | Backend health status |
| `GET /metrics` | No | Prometheus metrics |
| `GET /analytics/routes` | No | Per-route baselines and current adaptive limits |
| `GET /analytics/routes/{route}/history` | No | Time-series data for a route |
| `GET /analytics/anomalies` | No | Recent anomaly alerts |
| `GET /analytics/backends` | No | Backend performance + current weights |
| `GET /dashboard/` | No | React dashboard UI |
| `GET /dashboard/api/*` | No | Dashboard API (SSE streams, process management) |
| `ANY /*` | Yes | Proxied requests through middleware chain |

## Observability

- **Prometheus** — scrape `/metrics` for standard HTTP request counters and histograms, plus `gateway_anomalies_total{route,metric}` and `gateway_backend_weight{backend}` gauges
- **Structured logs** — request logs printed to stdout with method, path, status, latency, and request ID
- **Real-time dashboard** — SSE-powered live request table and backend health at `/dashboard/`
- **Analytics API** — query learned baselines, anomaly history, and backend weights via REST

## Dependencies

| Package | Purpose |
|---|---|
| `github.com/golang-jwt/jwt/v5` | JWT validation |
| `github.com/prometheus/client_golang` | Prometheus metrics |
| `gopkg.in/yaml.v3` | Config file parsing |

## How the Adaptive Layer Works

The adaptive intelligence layer runs entirely in the background without adding latency to the request path:

1. **Traffic Recorder** pushes each request's route, status, latency, and byte counts to a buffered channel. A background goroutine drains the channel into the in-memory `TrafficStore`.
2. **TrafficStore** organizes data into 1-minute buckets per route and per backend. Buckets older than 48 hours are automatically expired.
3. **Analyzer** wakes up every 5 minutes, reads the last hour of buckets, and computes moving averages and standard deviations for request rate, error rate, and latency. It detects anomalies using a z-score threshold of 3.0.
4. **Adaptive Rate Limiter** queries the analyzer's baseline at request time. If the current rate exceeds `mean × 3.0`, it rejects with `429`. During the first hour (learning period), it falls back to the static config limit.
5. **Weighted Load Balancer** recalculates backend scores every 5 minutes using `(1/latency) × (1 - error_rate)` and uses weighted-random selection to steer more traffic toward faster, more reliable backends.
6. **Circuit Breaker** uses the backend's learned error baseline to set its trip threshold dynamically, preventing false positives on backends with naturally higher error rates.

This creates a feedback loop: a slow backend gets less traffic → recovers → gradually earns back its weight.
