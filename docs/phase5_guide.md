# Phase 5: Adaptive Traffic Gateway — Learning From Traffic Patterns

## 🧠 Core Concept: A Gateway That Gets Smarter Over Time

Static gateways use hardcoded rules — "rate limit at 100 req/min", "round-robin across backends". But real traffic is dynamic. An **adaptive gateway** observes its own traffic, detects patterns (and anomalies), and auto-tunes its own behavior.

```
                    Requests flow in
                         │
                  ┌──────┴───────┐
                  │  MicroGate    │──→ serves traffic normally
                  └──────┬───────┘
                         │
                   records metrics
                         │
               ┌─────────┴──────────┐
               │   Traffic Store     │  per-route, per-minute stats
               │   (Redis/SQLite)    │  in a time-series format
               └─────────┬──────────┘
                         │
                  analyzer reads
                         │
               ┌─────────┴──────────┐
               │  Traffic Analyzer   │  runs every N minutes
               │                     │
               │  • Computes baselines│
               │  • Detects anomalies│
               │  • Adjusts configs  │
               └─────────────────────┘
```

Your gateway already handles auth, rate limiting, circuit breaking, load balancing, and observability. Phase 5 makes it **data-driven** — thresholds adjust automatically based on what the gateway sees.

---

## Step 1: Traffic Recording Middleware

**File:** `internal/middleware/traffic.go`

Record per-route metrics on every request. This is the data foundation everything else builds on.

**What to build:**
1. A `TrafficRecorder` struct that wraps a `TrafficStore` interface
2. Middleware that captures per-request data:
   - Route path (normalized, e.g., `/api/users` not `/api/users/123`)
   - Response status code
   - Response latency (from `time.Now()` before/after `next.ServeHTTP()`)
   - Request size (bytes)
   - Response size (bytes)
   - Client IP
3. Write each data point to the traffic store asynchronously (buffered channel → background goroutine)

**Key concept:** Don't block the request path with storage writes. Use a buffered channel:
```go
type TrafficRecorder struct {
    events chan TrafficEvent
    store  TrafficStore
}

func (t *TrafficRecorder) Start() {
    go func() {
        for event := range t.events {
            t.store.Record(event)
        }
    }()
}
```

The middleware pushes events to the channel, the background goroutine drains and persists them.

---

## Step 2: Traffic Store

**File:** `internal/analytics/store.go`

Store time-bucketed traffic metrics. Each "bucket" is a 1-minute window for a specific route.

**What to build:**
1. A `TrafficStore` interface:
   ```go
   type TrafficStore interface {
       Record(event TrafficEvent)
       GetBuckets(route string, from, to time.Time) []Bucket
       GetRoutes() []string
   }
   ```
2. A `Bucket` struct (one per route per minute):
   ```go
   type Bucket struct {
       Route       string
       Timestamp   time.Time    // start of the 1-minute window
       RequestCount int
       ErrorCount   int
       TotalLatency time.Duration
       MaxLatency   time.Duration
       BytesIn      int64
       BytesOut     int64
   }
   ```
3. Start with an **in-memory** implementation using `map[string][]Bucket` with a `sync.RWMutex`
4. Expire old buckets (keep last 24-48 hours) with a background cleanup goroutine

**Key Go concepts:**
- `time.Truncate(time.Minute)` — snap a timestamp to its minute boundary for bucketing
- `sync.RWMutex` — readers (analyzer) don't block each other, only writers (recorder) take exclusive locks

**Optional upgrade:** Swap in Redis sorted sets later for persistence across restarts. The interface keeps this clean.

---

## Step 3: Traffic Analyzer

**File:** `internal/analytics/analyzer.go`

The brain of the adaptive gateway. Runs on a timer, reads traffic data, computes baselines, and detects anomalies.

**What to build:**
1. An `Analyzer` struct that runs every N minutes (configurable, default 5 min)
2. For each route, compute:
   - **Moving average** request rate (last 1 hour)
   - **Moving average** error rate
   - **Moving average** latency (mean and p99)
   - **Standard deviation** of each metric
3. **Anomaly detection** using z-score:
   ```go
   func isAnomaly(current, mean, stddev float64, threshold float64) bool {
       if stddev == 0 {
           return false
       }
       zScore := (current - mean) / stddev
       return zScore > threshold // threshold = 3.0 is standard
   }
   ```
4. When an anomaly is detected:
   - Log a structured alert: `{"type":"anomaly","route":"/api/users","metric":"request_rate","current":500,"mean":100,"z_score":4.2}`
   - Update Prometheus metric: `gateway_anomalies_total{route="/api/users",metric="request_rate"}`
   - Publish to an `AnomalyChannel` for other components to react

**Key concept:** The z-score tells you "how many standard deviations away from normal is this?" A z-score of 3+ means there's a ~0.3% chance this is normal traffic — almost certainly anomalous.

---

## Step 4: Adaptive Rate Limiter

**File:** `internal/middleware/adaptive_ratelimit.go`

Replace static rate limits with dynamic ones that adjust based on learned traffic patterns.

**What to build:**
1. An `AdaptiveRateLimiter` struct that wraps your existing rate limiter
2. It reads baselines from the analyzer:
   ```go
   // Instead of hardcoded: limit = 100 req/min
   // Dynamic: limit = mean_rate * multiplier
   func (a *AdaptiveRateLimiter) currentLimit(route string) int {
       baseline := a.analyzer.GetBaseline(route)
       return int(baseline.MeanRate * a.config.Multiplier) // e.g., 3x normal
   }
   ```
3. Config support:
   ```yaml
   adaptive_rate_limit:
     enabled: true
     multiplier: 3.0        # allow up to 3x normal traffic
     min_limit: 10           # never go below 10 req/min
     max_limit: 10000        # never go above 10000 req/min
     learning_period: "1h"   # don't enforce until 1h of data collected
   ```
4. **Fallback:** If there's not enough data yet (cold start), use the static limit from config

**Key concept:** The `learning_period` is critical — you can't adapt without data. During cold start, the gateway uses your existing static rate limiter. Once it has enough history, it switches to dynamic limits.

---

## Step 5: Performance-Weighted Load Balancer

**File:** `internal/proxy/weighted_lb.go`

Replace round-robin with weights based on actual backend performance.

**What to build:**
1. A `WeightedLoadBalancer` struct that reads performance data from the analyzer
2. Every N minutes, recompute weights:
   ```go
   // Lower latency + lower error rate = higher weight
   func computeWeight(avgLatency time.Duration, errorRate float64) float64 {
       latencyScore := 1.0 / avgLatency.Seconds()         // faster = higher
       reliabilityScore := 1.0 - errorRate                  // fewer errors = higher
       return latencyScore * reliabilityScore
   }
   ```
3. Use weighted random selection:
   ```
   Backend A: weight 0.6  → gets ~60% of traffic
   Backend B: weight 0.3  → gets ~30% of traffic
   Backend C: weight 0.1  → gets ~10% of traffic
   ```
4. Skip backends whose circuit breaker is open (integrate with existing circuit breaker)
5. Expose weights via Prometheus: `gateway_backend_weight{backend="http://localhost:9001"}`

**Key concept:** This is a feedback loop — as a backend slows down, it gets less traffic, which helps it recover. When it recovers, it naturally gets more traffic again.

---

## Step 6: Auto-Tuning Circuit Breaker

**File:** Update `internal/middleware/circuitbreaker.go`

Instead of a hardcoded error threshold, learn what "normal" error rate is per backend.

**What to build:**
1. Add a method that reads the baseline error rate from the analyzer
2. Set the circuit breaker threshold dynamically:
   ```go
   // Instead of: open after 5 errors
   // Dynamic: open when error rate exceeds 5x the baseline
   func (cb *CircuitBreaker) dynamicThreshold(backend string) float64 {
       baseline := cb.analyzer.GetBaseline(backend)
       threshold := baseline.MeanErrorRate * 5.0
       if threshold < 0.05 {
           return 0.05 // minimum 5% threshold
       }
       return threshold
   }
   ```
3. This prevents false positives: an endpoint with a normal 2% error rate won't trigger at 3%, but one with a normal 0.1% rate will trigger at 0.5%

---

## Step 7: Analytics Dashboard API

**File:** `internal/analytics/api.go`

Expose the learned traffic intelligence via REST endpoints.

**What to build:**
1. `GET /analytics/routes` — list all known routes with current baselines:
   ```json
   [
     {
       "route": "/api/users",
       "avg_rate": 120.5,
       "avg_latency_ms": 45,
       "p99_latency_ms": 210,
       "error_rate": 0.002,
       "current_rate_limit": 361,
       "anomalies_24h": 2
     }
   ]
   ```
2. `GET /analytics/routes/{route}/history` — time-series data for a specific route (for charting)
3. `GET /analytics/anomalies` — recent anomalies with details
4. `GET /analytics/backends` — backend performance + current weights:
   ```json
   [
     {"backend": "http://localhost:9001", "avg_latency_ms": 50, "error_rate": 0.001, "weight": 0.6},
     {"backend": "http://localhost:9002", "avg_latency_ms": 120, "error_rate": 0.005, "weight": 0.3},
     {"backend": "http://localhost:9003", "avg_latency_ms": 200, "error_rate": 0.02, "weight": 0.1}
   ]
   ```

**Key concept:** Register these endpoints outside the middleware chain (like `/health`), so they aren't rate-limited or counted as regular traffic.

---

## Step 8: Wire It Up in `cmd/gateway/main.go`

**What to update:**
1. Initialize the `TrafficStore` and `TrafficRecorder`
2. Start the `Analyzer` as a background goroutine
3. Pass the analyzer to the adaptive rate limiter and weighted load balancer
4. Add the traffic recording middleware to the chain:
   ```go
   handler := middleware.Chain(
       proxyHandler,
       middleware.RequestID(),
       middleware.TrafficRecording(recorder),  // NEW — records every request
       middleware.Logging(),
       adaptiveRateLimiter.Middleware(),        // UPDATED — dynamic limits
       auth.Middleware(),
       circuitBreaker.Middleware(),             // UPDATED — dynamic thresholds
   )
   ```
5. Register analytics endpoints: `/analytics/routes`, `/analytics/anomalies`, `/analytics/backends`

**Update `config.yml`:**
```yaml
analytics:
  enabled: true
  bucket_interval: "1m"       # 1-minute time buckets
  retention: "48h"            # keep 48 hours of history
  analyzer_interval: "5m"     # recompute baselines every 5 min

adaptive_rate_limit:
  enabled: true
  multiplier: 3.0
  min_limit: 10
  max_limit: 10000
  learning_period: "1h"

weighted_lb:
  enabled: true
  rebalance_interval: "5m"
```

---

## Recommended Build Order

| Order | File | Why this order |
|-------|------|---------------|
| 1 | `analytics/store.go` | Data foundation — everything reads from this |
| 2 | `middleware/traffic.go` | Feeds data into the store |
| 3 | `analytics/analyzer.go` | Core brain — computes baselines and anomalies |
| 4 | `middleware/adaptive_ratelimit.go` | First consumer of analyzer data |
| 5 | `proxy/weighted_lb.go` | Second consumer — performance-weighted routing |
| 6 | Update `circuitbreaker.go` | Third consumer — dynamic thresholds |
| 7 | `analytics/api.go` | Expose insights via REST |
| 8 | Update `main.go` + `config.yml` | Wire it all together |

**Start with the traffic store and recorder — you can't analyze what you don't measure.**

---

## Verification

- [ ] Send traffic to the gateway → `/analytics/routes` shows learned baselines
- [ ] Send a traffic spike (10x normal) → anomaly detected and logged
- [ ] Adaptive rate limiter allows normal traffic, blocks anomalous spikes
- [ ] Slow down one backend artificially → weighted LB shifts traffic away from it
- [ ] Backend recovers → weighted LB gradually restores traffic to it
- [ ] Circuit breaker thresholds differ per route based on learned error baselines
- [ ] Grafana dashboard shows: traffic patterns per route, anomalies over time, backend weights, adaptive rate limits
