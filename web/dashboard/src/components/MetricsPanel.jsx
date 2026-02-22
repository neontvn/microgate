export default function MetricsPanel({ metrics, sparklines }) {
    return (
        <div className="card">
            <div className="card-header">
                <span className="card-title">Live Metrics</span>
                <span className="badge badge-neutral">{metrics.uptime} uptime</span>
            </div>

            <div className="metrics-grid">
                <MetricCard
                    label="Requests / min"
                    value={metrics.requests_per_minute}
                    colorClass="good"
                    data={sparklines.requests}
                    color="var(--status-healthy)"
                />
                <MetricCard
                    label="Avg Latency"
                    value={`${metrics.avg_latency_ms}ms`}
                    colorClass={metrics.avg_latency_ms < 100 ? 'good' : metrics.avg_latency_ms < 500 ? 'warn' : 'bad'}
                    data={sparklines.latency}
                    color="var(--accent-secondary)"
                />
                <MetricCard
                    label="Error Rate"
                    value={`${(metrics.error_rate * 100).toFixed(2)}%`}
                    colorClass={metrics.error_rate < 0.01 ? 'good' : metrics.error_rate < 0.05 ? 'warn' : 'bad'}
                    data={sparklines.errors}
                    color="var(--status-warning)"
                />
                <MetricCard
                    label="Backends"
                    value={`${metrics.healthy_backends} / ${metrics.total_backends}`}
                    colorClass={metrics.healthy_backends === metrics.total_backends ? 'good' : 'warn'}
                />
            </div>
        </div>
    );
}

function MetricCard({ label, value, colorClass, data, color }) {
    return (
        <div className="metric-item">
            <div className="metric-label">{label}</div>
            <div className={`metric-value ${colorClass}`}>{value}</div>
            {data && <Sparkline data={data} color={color} />}
        </div>
    );
}

function Sparkline({ data, color }) {
    const width = 200;
    const height = 30;
    const max = Math.max(...data);
    const min = Math.min(...data);
    const range = max - min || 1;

    const points = data
        .map((val, i) => {
            const x = (i / (data.length - 1)) * width;
            const y = height - ((val - min) / range) * height;
            return `${x},${y}`;
        })
        .join(' ');

    // Area fill (closed path)
    const areaPoints = `0,${height} ${points} ${width},${height}`;

    return (
        <div className="metric-sparkline">
            <svg viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none">
                <polyline className="area" points={areaPoints} fill={color} />
                <polyline points={points} stroke={color} />
            </svg>
        </div>
    );
}
