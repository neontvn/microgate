// Mock data for developing the dashboard UI before the Go backend APIs are ready.

export const mockServices = [
    {
        id: 'backend-9001',
        port: 9001,
        status: 'running',
        healthy: true,
        latency_ms: 45,
        last_check: '2026-02-21T15:52:00Z',
        error_rate: 0.002,
    },
    {
        id: 'backend-9002',
        port: 9002,
        status: 'running',
        healthy: true,
        latency_ms: 120,
        last_check: '2026-02-21T15:52:00Z',
        error_rate: 0.005,
    },
    {
        id: 'backend-9003',
        port: 9003,
        status: 'stopped',
        healthy: false,
        latency_ms: null,
        last_check: null,
        error_rate: null,
    },
    {
        id: 'backend-9004',
        port: 9004,
        status: 'crashed',
        healthy: false,
        latency_ms: null,
        last_check: '2026-02-21T15:50:30Z',
        error_rate: null,
    },
];

export const mockMetrics = {
    requests_per_minute: 142,
    avg_latency_ms: 47,
    error_rate: 0.003,
    healthy_backends: 2,
    total_backends: 4,
    uptime: '4h32m',
};

// Generate sparkline data (last 30 data points)
function generateSparkline(base, variance, points = 30) {
    return Array.from({ length: points }, () =>
        Math.max(0, base + (Math.random() - 0.5) * variance * 2)
    );
}

export const mockSparklines = {
    requests: generateSparkline(142, 40),
    latency: generateSparkline(47, 15),
    errors: generateSparkline(0.003, 0.002),
};

const methods = ['GET', 'POST', 'PUT', 'DELETE'];
const paths = [
    '/api/v1/users',
    '/api/v1/orders',
    '/api/v1/products',
    '/api/v1/auth/login',
    '/api/v1/health',
    '/api/v1/users/123',
    '/api/v1/orders/456/items',
];
const backends = [':9001', ':9002'];
const statuses = [200, 200, 200, 200, 200, 201, 204, 400, 404, 500, 502];

function randomId() {
    return Math.random().toString(36).substring(2, 10);
}

function generateLog(i) {
    const method = methods[Math.floor(Math.random() * methods.length)];
    const path = paths[Math.floor(Math.random() * paths.length)];
    const status = statuses[Math.floor(Math.random() * statuses.length)];
    const latency = status >= 500 ? 200 + Math.random() * 800 : 10 + Math.random() * 150;
    const backend = backends[Math.floor(Math.random() * backends.length)];

    const ts = new Date(Date.now() - i * 2000);

    return {
        id: randomId(),
        timestamp: ts.toISOString(),
        method,
        path,
        status,
        latency_ms: Math.round(latency),
        backend,
        client_ip: '192.168.1.' + Math.floor(Math.random() * 255),
        bytes_in: method === 'GET' ? 0 : Math.floor(Math.random() * 5000),
        bytes_out: Math.floor(Math.random() * 10000),
        request_headers: {
            'Authorization': 'Bearer ***',
            'Content-Type': 'application/json',
            'X-Request-ID': randomId(),
            'User-Agent': 'curl/7.88.1',
        },
        response_headers: {
            'Content-Type': 'application/json',
            'X-Request-ID': randomId(),
            'X-Response-Time': `${Math.round(latency)}ms`,
        },
    };
}

export const mockLogs = Array.from({ length: 50 }, (_, i) => generateLog(i));

export const mockProcessLogs = [
    '[2026-02-21 15:50:00] Starting test backend on :9001',
    '[2026-02-21 15:50:01] Server listening on :9001',
    '[2026-02-21 15:50:05] GET /api/v1/users 200 45ms',
    '[2026-02-21 15:50:06] POST /api/v1/orders 201 120ms',
    '[2026-02-21 15:50:07] GET /api/v1/products 200 32ms',
    '[2026-02-21 15:50:08] GET /api/v1/users/123 200 28ms',
    '[2026-02-21 15:50:10] GET /api/v1/health 200 2ms',
    '[2026-02-21 15:50:12] PUT /api/v1/users/123 200 67ms',
    '[2026-02-21 15:50:15] GET /api/v1/orders/456/items 500 890ms',
    '[2026-02-21 15:50:16] ERROR: upstream connection timeout',
    '[2026-02-21 15:50:18] GET /api/v1/users 200 41ms',
    '[2026-02-21 15:50:20] POST /api/v1/auth/login 200 156ms',
];
