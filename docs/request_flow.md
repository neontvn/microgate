# MicroGate Request Flow

Here is the exact journey of a single HTTP request (like `GET /api/v1/users`) as it travels through the API Gateway from the moment a user initiates a request until they get their JSON response back.

---

### Step 1: The Gateway Listens
The client sends the request to the configured gateway port (default `8080`). The Go HTTP Server running in `main.go` accepts the incoming request. 

Because the path `/api/v1/users` doesn't match the dedicated `/health`, `/metrics`, or `/dashboard/api/` hardcoded routes, the request is tossed into the **Middleware Chain handler**.

### Step 2: Running the Middleware Gauntlet
Before the proxy attempts to forward the request, it must pass through the chain of middleware configured in `main.go`. It processes through them in sequence:

1. **RequestID (`middleware.RequestID()`):** It attaches a unique `X-Request-Id` (e.g., `uuid-4f2a`) header to the request so it can be tracked across the entire system.
2. **Capture (`middleware.Capture()`):** It wraps the `ResponseWriter` in a custom `responseCapture` object. This sits quietly during the request phase, but acts as a stopwatch to measure latency and byte count later during the response phase.
3. **Metrics (`middleware.Metrics()`):** Registers that an incoming request arrived for Prometheus observability.
4. **Logging (`middleware.Logging()`):** Prints `[http] GET /api/v1/users` to the terminal console for debugging and visibility.
5. **Rate Limiting (`middleware.RateLimiter()`):** Uses a token bucket algorithm. If the client has fewer tokens per second than the limit, they pass. If not, it rejects the request immediately with `HTTP 429 Too Many Requests`.
6. **Auth (`middleware.Auth()`):** Checks if the request contains a valid `api_key` or `Authorization: Bearer <jwt-secret>` header. If neither exists or matches the keys configured in `config.yml`, it violently rejects the request with `HTTP 401 Unauthorized`.
7. **Circuit Breaker (`middleware.CircuitBreaker()`):** It checks the health history of the proxy destination. Is the target backend known to be throwing `500`s constantly right now? If the circuit is "Open" (tripped), it stops the request here with `HTTP 503 Service Unavailable`, refusing to overload the dying server.

### Step 3: Load Balancing & Proxying
Assuming the request survived all 7 layers of middleware, it hits the core engine: `proxy.NewProxy()`.

1. The gateway checks its routing table (`cfg.Routes`). It sees `/api/v1` routes to three `backends`: e.g., `9001`, `9002`, and `9003`.
2. It asks the **LoadBalancer:** *"Who's turn is it?"*
3. The LoadBalancer checks the **HealthChecker**. It observes that `9002` failed its last health ping and is sick. It skips `9002`.
4. Configured for `"round-robin"`, the LoadBalancer rotates to the next healthy backend in line: `9003`.
5. The gateway builds an **HTTP Reverse Proxy** pointing to `http://localhost:9003`, strips specific internal headers, adds an `X-Forwarded-Host: localhost:8080` header, and fires the actual HTTP request directly to the backend process on port `9003`.

### Step 4: The Backend Responds
1. The backend on port `9003` processes the traffic and replies with `HTTP 200 OK` and a payload like `{"status": "ok"}`.
2. The proxy receives the response.

### Step 5: Unwinding the Chain (The Way Out)
The response now travels *backwards* up the middleware chain. All the middleware layers complete their wrap-up tasks:

1. **Metrics:** Notes that the request finished and logs it as a success for Prometheus.
2. **Capture:** The stopwatch stops! It records that the request took `14ms` and generated `18 bytes` of data. It instantly packages this into a JSON `RequestLog` object and fires it over an asynchronous channel to the `LogStore`, which immediately broadcasts an SSE event to the React Dashboard so the Request Table updates in real-time.

Finally, the Gateway hands the pure `{"status": "ok"}` JSON payload back to the client over the original HTTP connection.
