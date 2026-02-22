import { useState } from 'react';
import useEventStream from './hooks/useEventStream';
import { startProcess, stopProcess, addProcess } from './services/api';
import ServicePanel from './components/ServicePanel';
import MetricsPanel from './components/MetricsPanel';
import RequestTable from './components/RequestTable';
import RequestDetail from './components/RequestDetail';
import ProcessLogs from './components/ProcessLogs';
import { mockMetrics, mockSparklines, mockProcessLogs } from './data/mockData';

export default function App() {
    const { services, logs, isConnected, error } = useEventStream();
    const [selectedRequest, setSelectedRequest] = useState(null);
    const [showProcessLogs, setShowProcessLogs] = useState(false);

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

                <MetricsPanel
                    metrics={mockMetrics}
                    sparklines={mockSparklines}
                />

                {showProcessLogs && (
                    <ProcessLogs
                        logs={mockProcessLogs}
                        title="Backend Logs"
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
