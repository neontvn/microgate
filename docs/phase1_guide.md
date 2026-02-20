# Phase 1: Build a Reverse Proxy
## 🧠 Core Concept: What is a Reverse Proxy?

```
Client → [API Gateway :8080] → Backend Server :9001
               ↑
         You are building this
```

A reverse proxy sits between clients and your backend servers. It receives requests, looks at the URL path, and forwards them to the correct backend. Think **Nginx** or **AWS API Gateway**.

---

## Step 1: Creating `config.yaml`

Define what routes your gateway knows about. Start simple:

```yaml
server:
  port: 8080

routes:
  - path: "/api/v1"
    backend: "http://localhost:9001"
  - path: "/api/v2"
    backend: "http://localhost:9002"
```

**Concept**: The gateway reads this at startup to know which path prefixes map to which backends.

---

## Step 2: Creating `internal/config/config.go`

Parses the YAML file into Go structs. You'll need:

1. A `Route` struct with `Path` and `Backend` fields (use `yaml` struct tags)
2. A `ServerConfig` struct with the `Port`
3. A `Config` struct combining both
4. A `LoadConfig(filename string) (*Config, error)` function that:
   - Reads the file with `os.ReadFile`
   - Unmarshals YAML with `gopkg.in/yaml.v3`

---

## Step 3: Creating `cmd/testbackend/main.go`

A tiny HTTP server to proxy requests **to**:

1. Accept a `-port` flag (use `flag` package)
2. Register a catch-all handler `"/"`
3. Return a JSON response like: `{"message": "Hello from backend", "port": 9001, "path": "/api/v1/hello"}`
4. Log each incoming request

**Why?** You need something at `localhost:9001` to proxy to. This lets you verify the gateway works.

Run it with: `go run cmd/testbackend/main.go -port 9001`

---

## Step 4: Creating `internal/proxy/proxy.go`

This is the heart of Phase 1. Build a reverse proxy:

1. Create a `NewProxy(cfg *config.Config) http.Handler` function
2. For each route in config, create an `httputil.ReverseProxy`:
   - Parse the backend URL with `url.Parse`
   - Use `httputil.NewSingleHostReverseProxy(targetURL)`
3. Use an `http.ServeMux` to register each route's path prefix → its reverse proxy
4. The `Director` function in `ReverseProxy` controls how the outgoing request is modified — this is where you rewrite headers, paths, etc.

**Key Go concepts**:
- `httputil.ReverseProxy` — Go's built-in reverse proxy. It handles copying request body, headers, and streaming the response back
- `http.ServeMux` — Go's built-in URL router. `mux.Handle("/api/v1/", proxy)` catches all requests under that prefix
- `Director` function — Called before forwarding. You set `req.URL.Host`, `req.URL.Scheme`, and optionally `req.Header`

---

## Step 5: Creating `cmd/gateway/main.go`

Wire everything together:

1. Call `config.LoadConfig("config.yaml")`
2. Call `proxy.NewProxy(cfg)` to get the handler
3. Start `http.ListenAndServe(":8080", handler)`
4. Log that the gateway is running

---

## 🧪 Test It

```bash
# Terminal 1: Start a test backend
go run cmd/testbackend/main.go -port 9001

# Terminal 2: Start the gateway
go run cmd/gateway/main.go

# Terminal 3: Test it!
curl http://localhost:8080/api/v1/hello
# Should return the JSON from your test backend
```

---
