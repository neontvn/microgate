# Phase 4: Gateway Dashboard — Real-Time Monitoring & Control Panel

## 🧠 Core Concept: See and Control Everything Your Gateway Does

Your gateway handles requests, but right now you can only see what's happening through terminal logs and Prometheus metrics. Phase 4 adds a **React dashboard** for real-time monitoring, request inspection, and backend process management — then wires it to live gateway data.

```
┌──────────────────────────────────────────────────────────────┐
│  MicroGate Dashboard (React)                                  │
│                                                               │
│  ┌─── Service Status ───────────┐  ┌─── Live Metrics ─────┐  │
│  │ ● Backend A :9001  healthy    │  │  Requests/min: ██▓ 142│  │
│  │   [Stop]                      │  │  Avg Latency:  █▓░  47│  │
│  │ ● Backend B :9002  healthy    │  │  Error Rate:   ▓░░ 0.3│  │
│  │   [Stop]                      │  └──────────────────────┘  │
│  │ ○ Backend C :9003  stopped    │                             │
│  │   [Start]                     │                             │
│  │                               │                             │
│  │ [+ Add New Backend]           │                             │
│  └───────────────────────────────┘                             │
│                                                               │
│  ┌─── Request Log ───────────────────────────────────────┐   │
│  │ ID      Method  Path         Status  Latency  Backend  │   │
│  │ a3f2... GET     /api/users   200     45ms     :9001    │   │
│  │ b7c1... POST    /api/orders  201     120ms    :9002    │   │
│  │ c9d4... GET     /api/users   500     890ms    :9001 ⚠️ │   │
│  └────────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────┘
```

**Build order philosophy:** Start with the UI using mock data so you can see what you're building. Then replace mocks with real APIs one by one.

---

## Step 1: React Dashboard with Mock Data

**Directory:** `web/dashboard/`

Scaffold the React app and build the full UI with hardcoded mock data. This lets you nail the design before touching any Go code.

### Setup:
```bash
cd web
npx -y create-vite@latest dashboard -- --template react
cd dashboard
npm install
```

### Components to build:

#### `src/components/ServicePanel.jsx`
Displays each backend with its health status and Start/Stop controls.

**Mock data:**
```js
const mockServices = [
  { id: "backend-9001", port: 9001, status: "running", healthy: true, latency_ms: 45 },
  { id: "backend-9002", port: 9002, status: "running", healthy: true, latency_ms: 120 },
  { id: "backend-9003", port: 9003, status: "stopped", healthy: false, latency_ms: null },
];
```

**UI elements:**
- Green pulsing dot for healthy + running, red for unhealthy, grey for stopped
- **Start/Stop button** per backend (calls mock handler for now)
- **"+ Add Backend"** button opens a modal with port and command inputs
- Latency shown as a colored badge (green < 100ms, yellow < 500ms, red > 500ms)

#### `src/components/MetricsPanel.jsx`
Live metrics overview with mini charts.

**Mock data:**
```js
const mockMetrics = {
  requests_per_minute: 142,
  avg_latency_ms: 47,
  error_rate: 0.003,
  healthy_backends: 2,
  total_backends: 3,
  uptime: "4h32m",
};
```

**UI elements:**
- Large number displays for key metrics
- Mini sparkline charts showing last 30 data points (use a simple SVG polyline or a lightweight chart library like recharts)
- Color shifts based on health (green → yellow → red as error rate climbs)

#### `src/components/RequestTable.jsx`
Scrollable table of recent requests, color-coded by status.

**Mock data:**
```js
const mockLogs = [
  { id: "a3f2c1d4", timestamp: "2026-02-21T15:52:30Z", method: "GET", path: "/api/users", status: 200, latency_ms: 45, backend: ":9001" },
  { id: "b7c1e2f5", timestamp: "2026-02-21T15:52:31Z", method: "POST", path: "/api/orders", status: 201, latency_ms: 120, backend: ":9002" },
  { id: "c9d4f3a6", timestamp: "2026-02-21T15:52:32Z", method: "GET", path: "/api/users", status: 500, latency_ms: 890, backend: ":9001" },
];
```

**UI elements:**
- Color-coded rows: green for 2xx, yellow for 4xx, red for 5xx
- Click a row → slide-out detail panel with full request/response headers
- Filter bar: dropdown for status codes, search box for path, method selector
- Auto-scroll with pause on hover

#### `src/components/RequestDetail.jsx`
Slide-out panel when clicking a request row.

**Shows:**
- Full request/response metadata (headers, timing, backend, bytes)
- Latency breakdown visualization
- Copy request as `curl` command button

#### `src/components/ProcessLogs.jsx`
Scrollable panel showing stdout/stderr from a selected backend.

**Mock data:**
```js
const mockProcessLogs = [
  "[2026-02-21 15:52:00] Server started on :9001",
  "[2026-02-21 15:52:01] GET /api/users 200 45ms",
  "[2026-02-21 15:52:02] POST /api/orders 201 120ms",
];
```

**UI elements:**
- Terminal-style dark background with monospace text
- Auto-scroll to bottom, pause on scroll up
- Clear button

#### `src/App.jsx`
Main layout — assembles all panels in a responsive grid:
```
┌──────────────────┬──────────────────┐
│  ServicePanel     │  MetricsPanel    │
│  (with controls)  │  (with charts)   │
├──────────────────┴──────────────────┤
│  RequestTable                        │
│  (with filters + click-to-inspect)   │
└──────────────────────────────────────┘
```

### Design requirements:
- **Dark theme** — dark background, high-contrast text, colored accents
- **Responsive grid** — CSS Grid or Flexbox for the 2-column top + full-width bottom layout
- **Animations** — pulse animation for health dots, smooth slide-in for detail panel, fade-in for new table rows
- **Typography** — use Inter or Roboto from Google Fonts

### Verification for this step:
```bash
cd web/dashboard
npm run dev
```
Open `http://localhost:5173` — the full dashboard should render with mock data, all interactions should work (filters, click-to-inspect, start/stop buttons).

---

## Step 2: Dashboard REST API (Go)

**File:** `internal/dashboard/api.go`

Build the API endpoints that the React app will call. Test each with `curl` before connecting the frontend.

### Request Logs API

**File:** `internal/dashboard/store.go` (the data layer)

1. A `RequestLog` struct:
   ```go
   type RequestLog struct {
       ID         string        `json:"id"`
       Timestamp  time.Time     `json:"timestamp"`
       Method     string        `json:"method"`
       Path       string        `json:"path"`
       Status     int           `json:"status"`
       Latency    time.Duration `json:"latency_ms"`
       ClientIP   string        `json:"client_ip"`
       BytesIn    int64         `json:"bytes_in"`
       BytesOut   int64         `json:"bytes_out"`
       Backend    string        `json:"backend"`
       Error      string        `json:"error,omitempty"`
   }
   ```
2. A `LogStore` using a **ring buffer** (fixed memory, oldest entries evicted):
   ```go
   type LogStore struct {
       logs  []RequestLog
       mu    sync.RWMutex
       size  int
       index int
   }
   ```
3. Methods: `Add()`, `Recent(n int)`, `Search(filter Filter)`, `GetByID(id string)`

### Endpoints:

| Endpoint | Description |
|---|---|
| `GET /dashboard/api/logs?limit=50&status=500&path=/api` | Recent requests with filters |
| `GET /dashboard/api/logs/{id}` | Full detail for a single request |
| `GET /dashboard/api/services` | Backend health status (from health checker) |
| `GET /dashboard/api/metrics` | Aggregated gateway stats |

**Key concept:** Register these outside the middleware chain so dashboard traffic isn't rate-limited or logged as regular traffic.

---

## Step 3: Request Capture Middleware

**File:** `internal/middleware/capture.go`

Middleware that records every request/response into the log store.

**What to build:**
1. A `responseCapture` wrapper around `http.ResponseWriter` that captures:
   - Status code (override `WriteHeader`)
   - Bytes written (override `Write`)
2. Middleware that records start time, wraps the response writer, calls `next.ServeHTTP()`, then builds a `RequestLog` and sends it to the store via a **buffered channel**:
   ```go
   func CaptureMiddleware(store *LogStore) Middleware {
       ch := make(chan RequestLog, 256)
       go func() {
           for log := range ch {
               store.Add(log)
           }
       }()
       return func(next http.Handler) http.Handler {
           // ... capture and push to ch
       }
   }
   ```

**Key concept:** Don't block the request path. The channel + goroutine pattern ensures zero latency overhead.

---

## Step 4: Live Updates with Server-Sent Events (SSE)

**File:** `internal/dashboard/sse.go`

Push real-time updates to the React app instead of polling.

**What to build:**
1. `GET /dashboard/api/stream` — SSE endpoint:
   ```go
   func (d *Dashboard) StreamHandler() http.HandlerFunc {
       return func(w http.ResponseWriter, r *http.Request) {
           w.Header().Set("Content-Type", "text/event-stream")
           w.Header().Set("Cache-Control", "no-cache")
           w.Header().Set("Connection", "keep-alive")

           flusher := w.(http.Flusher)
           ch := d.Subscribe()
           defer d.Unsubscribe(ch)

           for {
               select {
               case event := <-ch:
                   fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, event.JSON)
                   flusher.Flush()
               case <-r.Context().Done():
                   return
               }
           }
       }
   }
   ```
2. Event types:
   - `event: request` — new request logged
   - `event: service` — backend health changed
   - `event: metrics` — periodic metric snapshot (every 5 seconds)
   - `event: process` — backend started/stopped/crashed

### Connect React to SSE:
```jsx
// src/hooks/useEventStream.js
function useEventStream(url) {
    const [logs, setLogs] = useState([]);
    const [services, setServices] = useState([]);

    useEffect(() => {
        const source = new EventSource(url);
        source.addEventListener('request', (e) => {
            setLogs(prev => [JSON.parse(e.data), ...prev].slice(0, 200));
        });
        source.addEventListener('service', (e) => {
            updateServices(JSON.parse(e.data));
        });
        return () => source.close();
    }, [url]);

    return { logs, services };
}
```

---

## Step 5: Process Manager

**File:** `internal/dashboard/process.go`

Manage backend processes from the dashboard — start, stop, and monitor without using the terminal.

**What to build:**
1. A `ManagedProcess` struct:
   ```go
   type ManagedProcess struct {
       ID        string             `json:"id"`
       Command   string             `json:"command"`
       Args      []string           `json:"args"`
       Port      int                `json:"port"`
       Status    string             `json:"status"`    // "running", "stopped", "crashed"
       PID       int                `json:"pid,omitempty"`
       StartedAt *time.Time         `json:"started_at,omitempty"`
       cmd       *exec.Cmd
       cancel    context.CancelFunc
   }
   ```
2. A `ProcessManager` with methods:
   - `Start(id string) error` — starts via `exec.CommandContext`, captures stdout/stderr
   - `Stop(id string) error` — sends `SIGTERM`, waits 5s, then `SIGKILL`
   - `List() []ManagedProcess` — all processes with status
   - `Logs(id string, lines int) []string` — recent output from a process
3. **Crash detection:** goroutine per process calls `cmd.Wait()` — if it exits unexpectedly, mark as `"crashed"` and fire SSE event

### Process API:

| Endpoint | Description |
|---|---|
| `GET /dashboard/api/processes` | List all managed processes |
| `POST /dashboard/api/processes` | Add a new backend |
| `POST /dashboard/api/processes/{id}/start` | Start a stopped process |
| `POST /dashboard/api/processes/{id}/stop` | Stop a running process |
| `GET /dashboard/api/processes/{id}/logs?lines=50` | Process stdout/stderr |

### Config:
```yaml
processes:
  - id: "backend-9001"
    command: "go"
    args: ["run", "cmd/testbackend/main.go", "-port", "9001"]
    port: 9001
    auto_start: true
  - id: "backend-9002"
    command: "go"
    args: ["run", "cmd/testbackend/main.go", "-port", "9002"]
    port: 9002
    auto_start: true
  - id: "backend-9003"
    command: "go"
    args: ["run", "cmd/testbackend/main.go", "-port", "9003"]
    port: 9003
    auto_start: false
```

With `auto_start: true`, the gateway replaces `start.sh` — it boots its own backends on startup.

**Key Go concepts:**
- `exec.CommandContext(ctx, command, args...)` — context-based process lifecycle
- `cmd.StdoutPipe()` + `bufio.Scanner` — stream output line by line
- `cmd.Process.Signal(syscall.SIGTERM)` — graceful shutdown

---

## Step 6: Connect React to Live APIs

Replace all mock data in the React app with real API calls.

**What to update:**

1. **Create API service:** `src/services/api.js`
   ```js
   const API_BASE = '/dashboard/api';

   export async function fetchLogs(filters = {}) {
       const params = new URLSearchParams(filters);
       const res = await fetch(`${API_BASE}/logs?${params}`);
       return res.json();
   }

   export async function fetchServices() { /* ... */ }
   export async function fetchMetrics() { /* ... */ }
   export async function fetchProcesses() { /* ... */ }
   export async function startProcess(id) {
       return fetch(`${API_BASE}/processes/${id}/start`, { method: 'POST' });
   }
   export async function stopProcess(id) {
       return fetch(`${API_BASE}/processes/${id}/stop`, { method: 'POST' });
   }
   ```

2. **Replace mock data** in each component with `useEffect` + `useState` fetching from the API

3. **Add SSE hook:** `src/hooks/useEventStream.js` — connect to `/dashboard/api/stream` for live updates

4. **Proxy setup** in `vite.config.js` for development:
   ```js
   export default defineConfig({
       server: {
           proxy: {
               '/dashboard/api': 'http://localhost:8080'
           }
       }
   });
   ```

---

## Step 7: Wire It Up in `cmd/gateway/main.go`

**What to update:**
1. Initialize `LogStore`, `ProcessManager`, and `Dashboard`
2. Start `auto_start` processes on boot
3. Add capture middleware to the chain:
   ```go
   handler := middleware.Chain(
       proxyHandler,
       middleware.RequestID(),
       middleware.Capture(logStore),         // NEW — captures every request
       middleware.Logging(),
       rateLimiter.Middleware(),
       auth.Middleware(),
       circuitBreaker.Middleware(),
   )
   ```
4. Register dashboard endpoints (outside middleware chain):
   ```go
   http.Handle("/dashboard/api/", dashboardAPI.Handler())
   http.Handle("/dashboard/", http.FileServer(http.Dir("web/dashboard/dist")))
   ```
5. On shutdown (`SIGTERM`/`SIGINT`), stop all managed processes gracefully

**Update `config.yml`:**
```yaml
dashboard:
  enabled: true
  log_capacity: 1000
  sse_buffer: 256

processes:
  - id: "backend-9001"
    command: "go"
    args: ["run", "cmd/testbackend/main.go", "-port", "9001"]
    port: 9001
    auto_start: true
  - id: "backend-9002"
    command: "go"
    args: ["run", "cmd/testbackend/main.go", "-port", "9002"]
    port: 9002
    auto_start: true
  - id: "backend-9003"
    command: "go"
    args: ["run", "cmd/testbackend/main.go", "-port", "9003"]
    port: 9003
    auto_start: false
```

---

## Recommended Build Order

| Order | Step | What you build | Verification |
|-------|------|----------------|-------------|
| 1 | React Dashboard (Step 1) | Full UI with mock data | `npm run dev` → dashboard renders at localhost:5173 |
| 2 | REST API (Step 2) + Store | Go endpoints + ring buffer | `curl /dashboard/api/logs` returns JSON |
| 3 | Capture Middleware (Step 3) | Record requests | Gateway traffic shows up in `/dashboard/api/logs` |
| 4 | SSE (Step 4) | Real-time push | `curl /dashboard/api/stream` streams events |
| 5 | Process Manager (Step 5) | Start/stop backends | `POST /processes/{id}/stop` kills a backend |
| 6 | Connect React (Step 6) | Replace mocks with APIs | Dashboard shows live data from the gateway |
| 7 | Wire Up (Step 7) | Integrate everything | Full end-to-end flow works |

**Start with the React dashboard — seeing the UI first makes everything else more motivating and easier to test.**

---

## Verification

### Dashboard UI
- [ ] React app renders with mock data — all panels visible
- [ ] Click a request row → detail panel slides out
- [ ] Filters work — filter by status, path, method
- [ ] Auto-scroll works, pauses on hover
- [ ] Dark theme with proper styling and animations

### Live Data
- [ ] Send requests to gateway → they appear in the dashboard in real-time via SSE
- [ ] `/dashboard/api/services` shows correct health status for all backends
- [ ] Kill a backend → dashboard shows it turn red within one health check interval
- [ ] Dashboard itself doesn't appear in the request logs (excluded from capture)

### Process Management
- [ ] Start a stopped backend from dashboard → health check turns green
- [ ] Stop a running backend from dashboard → traffic reroutes automatically
- [ ] Add a new backend via dashboard → it appears in the service panel
- [ ] Backend crashes (`kill -9`) → dashboard shows "crashed" status
- [ ] Gateway shutdown (`Ctrl+C`) → all managed processes stop cleanly
- [ ] Gateway startup with `auto_start: true` → backends start automatically
