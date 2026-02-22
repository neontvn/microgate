import { useState } from 'react';
import ServicePanel from './components/ServicePanel';
import MetricsPanel from './components/MetricsPanel';
import RequestTable from './components/RequestTable';
import RequestDetail from './components/RequestDetail';
import ProcessLogs from './components/ProcessLogs';
import {
    mockServices,
    mockMetrics,
    mockSparklines,
    mockLogs,
    mockProcessLogs,
} from './data/mockData';

export default function App() {
    const [services, setServices] = useState(mockServices);
    const [logs] = useState(mockLogs);
    const [selectedRequest, setSelectedRequest] = useState(null);
    const [showProcessLogs, setShowProcessLogs] = useState(false);

    const handleStart = (id) => {
        setServices((prev) =>
            prev.map((s) =>
                s.id === id
                    ? { ...s, status: 'running', healthy: true, latency_ms: 50, error_rate: 0.001 }
                    : s
            )
        );
    };

    const handleStop = (id) => {
        setServices((prev) =>
            prev.map((s) =>
                s.id === id
                    ? { ...s, status: 'stopped', healthy: false, latency_ms: null, error_rate: null }
                    : s
            )
        );
    };

    const handleAddBackend = (data) => {
        const newService = {
            id: data.id,
            port: data.port,
            status: 'stopped',
            healthy: false,
            latency_ms: null,
            last_check: null,
            error_rate: null,
        };
        setServices((prev) => [...prev, newService]);
    };

    return (
        <div className="app">
            <header className="app-header">
                <div className="app-header-left">
                    <span className="app-logo">⚡ MicroGate</span>
                    <span className="app-version">v0.4.0</span>
                </div>
                <div className="app-header-right">
                    <span className="uptime-badge">Uptime: {mockMetrics.uptime}</span>
                    <button
                        className="btn btn-sm"
                        onClick={() => setShowProcessLogs(!showProcessLogs)}
                    >
                        {showProcessLogs ? 'Hide Logs' : 'Process Logs'}
                    </button>
                </div>
            </header>

            <main className="app-main">
                <ServicePanel
                    services={services}
                    onStart={handleStart}
                    onStop={handleStop}
                    onAdd={handleAddBackend}
                />

                <MetricsPanel metrics={mockMetrics} sparklines={mockSparklines} />

                {showProcessLogs && (
                    <ProcessLogs
                        logs={mockProcessLogs}
                        title="Backend :9001 Output"
                        onClear={() => { }}
                    />
                )}

                <RequestTable logs={logs} onSelectRequest={setSelectedRequest} />
            </main>

            <RequestDetail
                request={selectedRequest}
                onClose={() => setSelectedRequest(null)}
            />
        </div>
    );
}
