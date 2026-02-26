# MicroGate Architecture

## Before Adaptive Intelligence (Phase 1-4)

All middleware uses static, hardcoded configuration. The rate limiter has a fixed token
count, the circuit breaker has a fixed failure threshold, and the load balancer uses
simple round-robin — no awareness of actual traffic patterns or backend performance.

```
                          ┌─────────────────────────────────────────────┐
                          │              config.yml                     │
                          │  (static rate limits, fixed thresholds,     │
                          │   round-robin strategy)                     │
                          └──────────────────┬──────────────────────────┘
                                             │ loaded once at startup
                                             ▼
┌──────────┐         ┌──────────────────────────────────────────────────────────┐
│          │  HTTP   │                    GATEWAY SERVER (:8080)                │
│  Client  │────────▶│                                                          │
│          │         │  ┌────────────────────────────────────────────────────┐  │
└──────────┘         │  │              Middleware Chain                      │  │
                     │  │                                                    │  │
                     │  │  ┌──────────┐  ┌─────────┐  ┌────────────────┐   │  │
                     │  │  │Request ID│─▶│ Capture │─▶│ Prometheus     │   │  │
                     │  │  │          │  │ (logs)  │  │ Metrics        │   │  │
                     │  │  └──────────┘  └─────────┘  └───────┬────────┘   │  │
                     │  │                                      │            │  │
                     │  │  ┌──────────┐  ┌─────────┐  ┌───────▼────────┐   │  │
                     │  │  │ Logging  │─▶│  Rate   │─▶│     Auth       │   │  │
                     │  │  │          │  │ Limiter │  │ (API key/JWT)  │   │  │
                     │  │  └──────────┘  │ STATIC  │  └───────┬────────┘   │  │
                     │  │                │ 10 tok  │          │            │  │
                     │  │                └─────────┘  ┌───────▼────────┐   │  │
                     │  │                             │ Circuit Breaker│   │  │
                     │  │                             │ STATIC: 5 errs │   │  │
                     │  │                             └───────┬────────┘   │  │
                     │  └─────────────────────────────────────┼────────────┘  │
                     │                                        │               │
                     │  ┌─────────────────────────────────────▼────────────┐  │
                     │  │              Reverse Proxy                       │  │
                     │  │         (Round-Robin Load Balancer)              │  │
                     │  │                                                  │  │
                     │  │  /api/v1 ──▶ ┌──────────┐ ┌──────────┐         │  │
                     │  │              │ :9001    │ │ :9002    │ ...     │  │
                     │  │              │ equal    │ │ equal    │         │  │
                     │  │              └──────────┘ └──────────┘         │  │
                     │  │                                                  │  │
                     │  │  /api/v2 ──▶ ┌──────────┐                      │  │
                     │  │              │ :9004    │                      │  │
                     │  │              └──────────┘                      │  │
                     │  └──────────────────────────────────────────────────┘  │
                     │                                                        │
                     │  ┌────────────────────┐  ┌──────────────────────────┐  │
                     │  │ Health Checker      │  │ Dashboard (React + SSE) │  │
                     │  │ periodic GET /      │  │ /dashboard/             │  │
                     │  │ → healthy/unhealthy │  │ process mgmt, req logs  │  │
                     │  └────────────────────┘  └──────────────────────────┘  │
                     └──────────────────────────────────────────────────────────┘

Key limitations:
  • Rate limit is fixed (10 tokens) — doesn't adapt to normal traffic volume
  • Circuit breaker trips after exactly 5 errors — same for all backends
  • Load balancer is round-robin — ignores backend speed/health differences
  • No visibility into traffic patterns, anomalies, or backend performance
```

---

## After Adaptive Intelligence (Phase 5)

The adaptive layer sits between the raw request flow and the decision-making components.
A TrafficStore records every request, an Analyzer computes baselines and detects anomalies,
and the middleware/LB components dynamically adjust their behavior based on learned patterns.

```
                          ┌─────────────────────────────────────────────┐
                          │              config.yml                     │
                          │  + analytics, adaptive_rate_limit,          │
                          │    weighted_lb sections                     │
                          └──────────────────┬──────────────────────────┘
                                             │
                                             ▼
┌──────────┐         ┌──────────────────────────────────────────────────────────┐
│          │  HTTP   │                    GATEWAY SERVER (:8080)                │
│  Client  │────────▶│                                                          │
│          │         │  ┌────────────────────────────────────────────────────┐  │
└──────────┘         │  │              Middleware Chain                      │  │
                     │  │                                                    │  │
                     │  │  ┌──────────┐  ┌─────────┐  ┌────────────────┐   │  │
                     │  │  │Request ID│─▶│ Capture │─▶│ Prometheus     │   │  │
                     │  │  │          │  │ (logs)  │  │ Metrics        │   │  │
                     │  │  └──────────┘  └─────────┘  └───────┬────────┘   │  │
                     │  │                                      │            │  │
                     │  │           ┌──────────────────────────▼─────────┐  │  │
                     │  │           │  ★ Traffic Recorder (NEW)         │  │  │
                     │  │           │  records every request to store    │  │  │
                     │  │           │  async via buffered channel        │  │  │
                     │  │           └──────────────┬────────────────────┘  │  │
                     │  │                          │                       │  │
                     │  │  ┌──────────┐  ┌────────▼────────────────────┐  │  │
                     │  │  │ Logging  │─▶│ ★ Adaptive Rate Limiter    │  │  │
                     │  │  │          │  │   DYNAMIC: mean × 3.0      │  │  │
                     │  │  └──────────┘  │   falls back to static     │  │  │
                     │  │                │   during learning period    │  │  │
                     │  │                └──────────┬──────────────────┘  │  │
                     │  │                 ┌────────▼────────────────┐    │  │
                     │  │                 │       Auth              │    │  │
                     │  │                 │   (API key / JWT)       │    │  │
                     │  │                 └────────┬────────────────┘    │  │
                     │  │                 ┌────────▼────────────────┐    │  │
                     │  │                 │ ★ Auto-Tuning Circuit   │    │  │
                     │  │                 │   Breaker               │    │  │
                     │  │                 │   DYNAMIC: 5× baseline  │    │  │
                     │  │                 │   error rate per backend │    │  │
                     │  │                 └────────┬────────────────┘    │  │
                     │  └──────────────────────────┼────────────────────┘  │
                     │                             │                       │
                     │  ┌──────────────────────────▼────────────────────┐  │
                     │  │         ★ Weighted Load Balancer              │  │
                     │  │    (replaces round-robin for multi-backend)   │  │
                     │  │                                               │  │
                     │  │  /api/v1 ──▶ ┌──────────┐ ┌──────────┐      │  │
                     │  │              │ :9001    │ │ :9002    │ ...  │  │
                     │  │              │ wt: 0.55 │ │ wt: 0.30 │      │  │
                     │  │              └──────────┘ └──────────┘      │  │
                     │  │                                               │  │
                     │  │  /api/v2 ──▶ ┌──────────┐ (single backend   │  │
                     │  │              │ :9004    │  stays as-is)     │  │
                     │  │              └──────────┘                   │  │
                     │  └───────────────────────────────────────────────┘  │
                     │                                                     │
                     │  ┌────────────────────────────────────────────────┐  │
                     │  │          ★ ADAPTIVE INTELLIGENCE LAYER         │  │
                     │  │                                                │  │
                     │  │  ┌──────────────┐      ┌───────────────────┐  │  │
                     │  │  │ TrafficStore │◀────▶│    Analyzer       │  │  │
                     │  │  │ (1-min       │      │ (runs every 5m)   │  │  │
                     │  │  │  buckets,    │      │                   │  │  │
                     │  │  │  48h retain) │      │ • Moving averages │  │  │
                     │  │  │              │      │ • Std deviations  │  │  │
                     │  │  │ Per-route &  │      │ • Z-score anomaly │  │  │
                     │  │  │ per-backend  │      │   detection       │  │  │
                     │  │  │ aggregates   │      │ • Route baselines │  │  │
                     │  │  └──────────────┘      │ • Backend baselines│  │  │
                     │  │                        └─────────┬─────────┘  │  │
                     │  │                                  │             │  │
                     │  │                    ┌─────────────▼──────────┐  │  │
                     │  │                    │   Anomaly Channel      │  │  │
                     │  │                    │   (broadcasts alerts)  │  │  │
                     │  │                    └────────────────────────┘  │  │
                     │  └────────────────────────────────────────────────┘  │
                     │                                                     │
                     │  ┌─────────────────┐  ┌──────────────────────────┐  │
                     │  │ Health Checker   │  │ Dashboard (React + SSE) │  │
                     │  │ periodic GET /   │  │ /dashboard/             │  │
                     │  └─────────────────┘  └──────────────────────────┘  │
                     │                                                     │
                     │  ┌─────────────────────────────────────────────────┐│
                     │  │   ★ Analytics REST API  (/analytics/...)       ││
                     │  │   • GET /analytics/routes      (baselines)     ││
                     │  │   • GET /analytics/routes/{r}/history (series) ││
                     │  │   • GET /analytics/anomalies   (alerts)        ││
                     │  │   • GET /analytics/backends    (weights)       ││
                     │  └─────────────────────────────────────────────────┘│
                     └─────────────────────────────────────────────────────┘

★ = New in Phase 5

Data flow:
  1. Every request → TrafficRecorder → TrafficStore (async, non-blocking)
  2. Analyzer reads TrafficStore every 5 min → computes baselines + detects anomalies
  3. Adaptive Rate Limiter reads baselines → sets dynamic per-route limits
  4. Weighted LB reads baselines → adjusts traffic weights per backend
  5. Circuit Breaker reads baselines → sets dynamic error-rate thresholds
  6. Analytics API exposes all intelligence via REST endpoints

Feedback loops:
  • Slow backend → lower weight → less traffic → backend recovers → weight increases
  • Traffic spike → z-score anomaly → adaptive rate limiter tightens → spike mitigated
  • High error rate → circuit breaker opens sooner → failures contained
```
