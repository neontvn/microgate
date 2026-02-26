# MicroGate Request Flow (Phase 5 - Adaptive Edition)

This document breaks down how a single HTTP request (e.g., `GET /api/v1/users`) travels through the API Gateway **after the Phase 5 adaptive traffic features are fully implemented**.

The gateway no longer just follows static rules—it constantly measures, learns, and retunes itself while traffic is flowing.

---

### Step 1: The Gateway Listens
The client sends the request to the configured gateway port (default `8080`). The Go HTTP Server accepts the incoming request. 

### Step 2: Running the Enhanced Middleware Gauntlet
The request must survive a chain of middleware, but now the gateway uses intelligent, dynamic enforcement rather than static configuration limits:

1. **RequestID (`middleware.RequestID()`):** Assigns a unique `X-Request-Id`.
2. **Traffic Recorder (`middleware.TrafficRecording()`): [NEW]** Before doing anything else, it records that a request has started for the `/api/v1` route. This middleware acts as a high-speed sensor, asynchronously pushing data points to the **Traffic Store** ring buffer without blocking the request path.
3. **Capture & Metrics:** Sets up the stopwatch for latency tracking and registers the incoming Prometheus hit.
4. **Adaptive Rate Limiting (`middleware.AdaptiveRateLimiter()`): [UPGRADED]** 
   - Instead of checking a hardcoded `10 req/sec` config, the limiter asks the background **Traffic Analyzer** for the current baseline of `/api/v1`.
   - The Analyzer says, *"Over the last hour, /api/v1 usually gets 5 req/sec. The dynamic threshold is 3x the baseline."*
   - Because 15 req/sec is the new learned ceiling, the request passes. If a sudden DDoS occurs, this limiter catches it automatically based on historical norms, dropping traffic with `HTTP 429`.
5. **Auth:** Authenticates the incoming headers.
6. **Auto-Tuning Circuit Breaker (`middleware.CircuitBreaker()`): [UPGRADED]** 
   - Instead of waiting for a flat "5 consecutive errors," the breaker checks the historic error baseline for the backend. 
   - If the normal error rate is incredibly low (0.01%), the breaker will trip much earlier to protect a suddenly fragile service, returning `HTTP 503`.

### Step 3: Performance-Weighted Load Balancing
Assuming the request passes all enforcement middleware, it hits the `Proxy` router to determine *where* it should go.

1. The gateway sees `/api/v1` maps to three backends: `9001`, `9002`, `9003`.
2. It asks the **WeightedLoadBalancer [NEW]:** *"Who's turn is it?"*
3. The LoadBalancer no longer blindly uses Round-Robin. It checks the live scores assigned by the background **Traffic Analyzer**.
   - `9001` has 15ms latency (Weight 0.70)
   - `9002` has 90ms latency (Weight 0.20)
   - `9003` has 200ms latency (Weight 0.10)
4. Knowing that `9001` is currently the most performant, the LoadBalancer uses weighted-random selection, actively steering the majority of the incoming traffic to the fastest, healthiest nodes.
5. The proxy fires the HTTP request to the chosen backend.

### Step 4: The Backend Responds
The backend processes the logic and replies with `HTTP 200 OK` and its JSON payload.

### Step 5: Recording the Intelligence (The Way Out)
The response travels backwards up the chain:

1. **Capture:** The stopwatch stops.
2. **Traffic Recorder: [NEW]** The recorder packages exactly how long the request took (e.g., `12ms`) and whether it was successful. It pushes this directly into the background **Traffic Store**.
3. **Traffic Analyzer (Background Process): [NEW]**
   - Completely outside the request flow, the Analyzer wakes up every few minutes. 
   - It reads all the new latency and error data the `TrafficRecorder` just dumped into the `TrafficStore`.
   - It computes a Z-score to check for anomalies. 
   - It re-calculates the baseline metrics and updates the weights for the `LoadBalancer`, `CircuitBreaker`, and `AdaptiveRateLimiter` so they are fully prepared for the *next* request.

Finally, the Gateway hands the pure `{"status": "ok"}` JSON back to the client.

---
The defining characteristic of Phase 5 is the **Feedback Loop**. The gateway uses the latency and success of the *current* request to instantly tune the routing efficiency of *future* requests!
