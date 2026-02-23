import { useState } from 'react';
import useEventStream from './hooks/useEventStream';
import { startProcess, stopProcess, addProcess, fetchProcessLogs } from './services/api';
import ServicePanel from './components/ServicePanel';
import MetricsPanel from './components/MetricsPanel';
import RequestTable from './components/RequestTable';
import RequestDetail from './components/RequestDetail';
import ProcessLogs from './components/ProcessLogs';

const emptyMetrics = {
    requests_per_minute: 0,
    avg_latency_ms: 0,
    error_rate: 0,
    healthy_backends: 0,
    total_backends: 0,
    uptime: '0s',
    sparklines: { requests: [], latency: [], errors: [] },
};

export default function App() {
    const { services, logs, metrics, isConnected, error } = useEventStream();
    const [selectedRequest, setSelectedRequest] = useState(null);
    const [showProcessLogs, setShowProcessLogs] = useState(false);
    const [processLogs, setProcessLogs] = useState([]);
    const [processLogsId, setProcessLogsId] = useState(null);

    const m = metrics || emptyMetrics;
    const sparklines = m.sparklines || { requests: [], latency: [], errors: [] };

    const handleStart = async (id) => {
        try {
            await startProcess(id);
        } catch (err) {
            console.error("Failed to start:", err);
            alert(err.message);
        }
    };

    const handleStop = async (id) => {
        try {
            await stopProcess(id);
        } catch (err) {
            console.error("Failed to stop:", err);
            alert(err.message);
        }
    };

    const handleAddBackend = async (data) => {
        try {
            await addProcess(data);
        } catch (err) {
            console.error("Failed to add backend:", err);
            alert(err.message);
        }
    };

    const handleToggleProcessLogs = async () => {
        if (showProcessLogs) {
            setShowProcessLogs(false);
            return;
        }
        // Show logs for the first running process, or the first process
        const target = services.find(s => s.status === 'running') || services[0];
        if (target) {
            try {
                const lines = await fetchProcessLogs(target.id);
                setProcessLogs(lines);
                setProcessLogsId(target.id);
            } catch (err) {
                console.error("Failed to fetch process logs:", err);
                setProcessLogs([]);
            }
        } else {
            setProcessLogs([]);
            setProcessLogsId(null);
        }
        setShowProcessLogs(true);
    };

    const handleClearProcessLogs = () => {
        setProcessLogs([]);
    };

    return (
        <div className="app">
            <header className="app-header">
                <div className="app-header-left">
                    <span className="app-logo">⚡ MicroGate</span>
                    <span className="app-version">v0.4.0</span>
                    <span className={`connection-status ${isConnected ? 'connected' : 'disconnected'}`}>
                        {isConnected ? '🟢 Live' : '🔴 Reconnecting...'}
                    </span>
                    {error && <span className="error-text" style={{ color: 'red', marginLeft: '10px' }}>{error}</span>}
                </div>
                <div className="app-header-right">
                    <span className="uptime-badge">Uptime: {m.uptime}</span>
                    <button
                        className="btn btn-sm"
                        onClick={handleToggleProcessLogs}
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

                <MetricsPanel
                    metrics={m}
                    sparklines={sparklines}
                />

                {showProcessLogs && (
                    <ProcessLogs
                        logs={processLogs}
                        title={processLogsId ? `Logs: ${processLogsId}` : 'Backend Logs'}
                        onClear={handleClearProcessLogs}
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
