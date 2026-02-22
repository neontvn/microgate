import { useState, useEffect } from 'react';
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
    const [services, setServices] = useState([]);
    const [logs] = useState(mockLogs);
    const [selectedRequest, setSelectedRequest] = useState(null);
    const [showProcessLogs, setShowProcessLogs] = useState(false);

    // Fetch initial processes on mount
    useEffect(() => {
        fetch('/dashboard/api/processes')
            .then(res => res.json())
            .then(data => setServices(data.processes || []))
            .catch(err => console.error("Failed to fetch processes:", err));
    }, []);

    const handleStart = async (id) => {
        try {
            await fetch(`/dashboard/api/processes/${id}/start`, { method: 'POST' });
            // Optimistic update
            setServices(prev => prev.map(s => s.id === id ? { ...s, status: 'running' } : s));
        } catch (err) {
            console.error("Failed to start:", err);
        }
    };

    const handleStop = async (id) => {
        try {
            await fetch(`/dashboard/api/processes/${id}/stop`, { method: 'POST' });
            // Optimistic update
            setServices(prev => prev.map(s => s.id === id ? { ...s, status: 'stopped' } : s));
        } catch (err) {
            console.error("Failed to stop:", err);
        }
    };

    const handleAddBackend = async (data) => {
        try {
            await fetch('/dashboard/api/processes', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(data)
            });
            // Fetch fresh list
            const res = await fetch('/dashboard/api/processes');
            const json = await res.json();
            setServices(json.processes || []);
        } catch (err) {
            console.error("Failed to add backend:", err);
        }
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
