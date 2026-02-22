import { useState, useMemo } from 'react';

export default function RequestTable({ logs, onSelectRequest }) {
    const [statusFilter, setStatusFilter] = useState('all');
    const [methodFilter, setMethodFilter] = useState('all');
    const [pathSearch, setPathSearch] = useState('');

    const filteredLogs = useMemo(() => {
        return logs.filter((log) => {
            if (statusFilter !== 'all') {
                if (statusFilter === '2xx' && (log.status < 200 || log.status >= 300)) return false;
                if (statusFilter === '4xx' && (log.status < 400 || log.status >= 500)) return false;
                if (statusFilter === '5xx' && log.status < 500) return false;
            }
            if (methodFilter !== 'all' && log.method !== methodFilter) return false;
            if (pathSearch && !log.path.toLowerCase().includes(pathSearch.toLowerCase())) return false;
            return true;
        });
    }, [logs, statusFilter, methodFilter, pathSearch]);

    const formatTime = (iso) => {
        const d = new Date(iso);
        return d.toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
    };

    const statusBadgeClass = (status) => {
        if (status >= 500) return 'badge-error';
        if (status >= 400) return 'badge-warning';
        return 'badge-success';
    };

    const latencyClass = (ms) => {
        if (ms < 100) return 'latency-fast';
        if (ms < 500) return 'latency-medium';
        return 'latency-slow';
    };

    const rowClass = (status) => {
        if (status >= 500) return 'status-5xx';
        if (status >= 400) return 'status-4xx';
        return '';
    };

    return (
        <div className="card request-table-wrapper">
            <div className="card-header">
                <span className="card-title">Request Log</span>
                <span className="text-muted text-sm mono">{filteredLogs.length} requests</span>
            </div>

            <div className="request-filters">
                <select
                    className="filter-select"
                    value={statusFilter}
                    onChange={(e) => setStatusFilter(e.target.value)}
                >
                    <option value="all">All Status</option>
                    <option value="2xx">2xx Success</option>
                    <option value="4xx">4xx Client Error</option>
                    <option value="5xx">5xx Server Error</option>
                </select>

                <select
                    className="filter-select"
                    value={methodFilter}
                    onChange={(e) => setMethodFilter(e.target.value)}
                >
                    <option value="all">All Methods</option>
                    <option value="GET">GET</option>
                    <option value="POST">POST</option>
                    <option value="PUT">PUT</option>
                    <option value="DELETE">DELETE</option>
                </select>

                <input
                    className="filter-input"
                    type="text"
                    placeholder="Search path..."
                    value={pathSearch}
                    onChange={(e) => setPathSearch(e.target.value)}
                />
            </div>

            <div className="request-table-scroll">
                <table className="request-table">
                    <thead>
                        <tr>
                            <th>Time</th>
                            <th>Method</th>
                            <th>Path</th>
                            <th>Status</th>
                            <th>Latency</th>
                            <th>Backend</th>
                        </tr>
                    </thead>
                    <tbody>
                        {filteredLogs.map((log, i) => (
                            <tr
                                key={log.id}
                                className={`${rowClass(log.status)} ${i === 0 ? 'new-row' : ''}`}
                                onClick={() => onSelectRequest(log)}
                            >
                                <td>{formatTime(log.timestamp)}</td>
                                <td>
                                    <span className={`badge-method ${log.method}`}>{log.method}</span>
                                </td>
                                <td>{log.path}</td>
                                <td>
                                    <span className={`badge ${statusBadgeClass(log.status)}`}>{log.status}</span>
                                </td>
                                <td>
                                    <span className={`latency-badge ${latencyClass(log.latency_ms)}`}>
                                        {log.latency_ms}ms
                                    </span>
                                </td>
                                <td style={{ color: 'var(--text-muted)' }}>{log.backend}</td>
                            </tr>
                        ))}
                        {filteredLogs.length === 0 && (
                            <tr>
                                <td colSpan={6} style={{ textAlign: 'center', padding: '24px', color: 'var(--text-muted)' }}>
                                    No requests match filters
                                </td>
                            </tr>
                        )}
                    </tbody>
                </table>
            </div>
        </div>
    );
}
