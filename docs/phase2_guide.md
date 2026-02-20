# Phase 2: Middleware Pipeline & Core Features

## 🧠 Core Concept: What is Middleware?

```
Request → [Logging] → [Rate Limit] → [Auth] → [Circuit Breaker] → Proxy → Backend
Response ←    ←            ←           ←              ←              ←
```

Middleware wraps your handler. Each one can inspect/modify the request **before** it reaches the proxy, and the response **after**. They chain together like layers of an onion.

In Go, the pattern is one function signature:
```go
type Middleware func(http.Handler) http.Handler
```

It takes a handler, wraps it, returns a new handler. Dead simple, infinitely composable.

---

## Step 1: `internal/middleware/chain.go` — The Pipeline

We are creating a middleware here. Build a function that chains middleware together:

```go
func Chain(handler http.Handler, middlewares ...Middleware) http.Handler
```

**How it works**: Loop through middlewares **in reverse** and wrap the handler repeatedly:
```
Chain(proxy, logging, rateLimit, auth)
// becomes: logging(rateLimit(auth(proxy)))
// request flows: logging → rateLimit → auth → proxy
```

**You'll need:**
1. A `Middleware` type alias: `type Middleware func(http.Handler) http.Handler`
2. A `Chain` function that iterates in reverse and wraps

**This is ~10 lines of code.**

---

## Step 2: `internal/middleware/logging.go` — Request Logging

Log every request with method, path, status code, and duration.

**The challenge**: `http.ResponseWriter` doesn't expose the status code after `WriteHeader` is called. You need a **wrapper struct**.

**You'll need:**
1. A `responseWriter` struct that embeds `http.ResponseWriter` and captures `statusCode`
2. Override the `WriteHeader(code int)` method to save the code
3. A `Logging()` function returning `Middleware` that:
   - Records `time.Now()` before calling `next.ServeHTTP`
   - Logs method, path, status code, and `time.Since(start)` after

**Key Go concept**: Embedding — when you embed `http.ResponseWriter` in your struct, your struct automatically satisfies the `http.ResponseWriter` interface. You only override the methods you need to customize.

---

## Step 3: `internal/middleware/ratelimit.go` — Token Bucket

Token bucket algorithms:
- Fixed Window Counter: Count requests in fixed time blocks (e.g., per minute); reset counter when the window ends. Vulnerable to boundary bursts.
- Sliding Window Log: Store timestamps of requests in a queue; remove old timestamps when they fall outside the window. Accurate but memory-intensive.
- Sliding Window Counter: Combines fixed window counts with weighted averages from the previous window. Good accuracy, constant memory.
- Token Bucket: Each client has a bucket with N tokens; tokens refill at a fixed rate. Allows controlled bursts. Simple and widely used.
- Leaky Bucket: Requests are added to a queue (bucket) and processed at a constant rate. Smooths out traffic but can cause latency under heavy load.

Limit requests per client IP using the **token bucket algorithm**.

**How token bucket works:**
```
Bucket holds N tokens (e.g., 10)
Each request costs 1 token
Tokens refill at a fixed rate (e.g., 1/second)
No tokens left → 429 Too Many Requests
```

**You'll need:**
1. A `bucket` struct: `tokens float64`, `maxTokens float64`, `refillRate float64`, `lastRefill time.Time`
2. An `allow()` method that refills tokens based on elapsed time, then checks if ≥ 1 token is available
3. A `RateLimiter` struct with a `map[string]*bucket` (keyed by client IP) and a `sync.Mutex`
4. A `NewRateLimiter(maxTokens, refillRate float64)` constructor
5. The middleware function that:
   - Extracts client IP from `r.RemoteAddr`
   - Calls `allow(ip)` — if false, respond with `429`
   - If true, call `next.ServeHTTP`

**Key Go concepts**: `sync.Mutex` for thread safety (multiple requests hit the map concurrently), `time.Since` for token refill calculations.

---

## Step 4: `internal/middleware/auth.go` — API Key + JWT

Two auth modes: simple API key lookup, and JWT token validation.

**You'll need:**
1. An `Auth` struct holding `apiKeys map[string]bool` (valid keys)
2. The middleware function that:
   - Checks for `X-API-Key` header → look it up in the map
   - If no API key, check `Authorization: Bearer <token>` header
   - If neither present or invalid → respond with `401 Unauthorized`
   - If valid → call `next.ServeHTTP`
3. For JWT: use `github.com/golang-jwt/jwt/v5`
   - Parse the token with `jwt.Parse(tokenString, keyFunc)`
   - `keyFunc` returns the signing key to verify against
   - Check `token.Valid`

**Start with API key only**, add JWT after.

---

## Step 5: `internal/middleware/circuitbreaker.go` — Circuit Breaker

Protect backends from cascading failures.

**State machine:**
```
        success
   ┌──────────────┐
   ▼              │
CLOSED ──────→ OPEN ──────→ HALF-OPEN
 (normal)    (failures     (try one request)
              exceeded)        │
                    ┌──────────┘
                    │ success → CLOSED
                    │ failure → OPEN
```

**You'll need:**
1. Three constants: `StateClosed`, `StateOpen`, `StateHalfOpen`
2. A `CircuitBreaker` struct: `state`, `failureCount`, `threshold int`, `lastFailure time.Time`, `timeout time.Duration`, `mu sync.Mutex`
3. Logic:
   - **Closed**: Forward requests. If response status ≥ 500, increment failures. If failures ≥ threshold → switch to Open.
   - **Open**: Reject immediately with `503 Service Unavailable`. If `timeout` has passed → switch to Half-Open.
   - **Half-Open**: Allow one request through. Success → Closed. Failure → Open.
4. Reuse the `responseWriter` wrapper from logging to capture status codes.

---

## Step 6: Wire It Up in [cmd/gateway/main.go]
Update main.go to chain the middleware:

```go
handler := proxy.NewProxy(cfg)
handler = middleware.Chain(
    handler,
    middleware.Logging(),
    rateLimiter.Middleware(),
    auth.Middleware(),
    circuitBreaker.Middleware(),
)
```

