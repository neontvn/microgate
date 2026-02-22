import { useState } from 'react';

export default function ServicePanel({ services, onStart, onStop, onAdd }) {
    const [showModal, setShowModal] = useState(false);
    const [selectedLogs, setSelectedLogs] = useState(null);

    const getStatusClass = (svc) => {
        if (svc.status === 'crashed') return 'crashed';
        if (svc.status === 'stopped') return 'stopped';
        return svc.healthy ? 'healthy' : 'unhealthy';
    };

    const getStatusLabel = (svc) => {
        if (svc.status === 'crashed') return 'crashed';
        if (svc.status === 'stopped') return 'stopped';
        return svc.healthy ? 'healthy' : 'unhealthy';
    };

    const formatLatency = (ms) => {
        if (ms == null) return '—';
        return `${ms}ms`;
    };

    const latencyClass = (ms) => {
        if (ms == null) return '';
        if (ms < 100) return 'latency-fast';
        if (ms < 500) return 'latency-medium';
        return 'latency-slow';
    };

    return (
        <div className="card">
            <div className="card-header">
                <span className="card-title">Services</span>
                <button className="btn btn-sm btn-primary" onClick={() => setShowModal(true)}>
                    + Add Backend
                </button>
            </div>

            <div className="service-list">
                {services.map((svc) => (
                    <div key={svc.id} className="service-item">
                        <span className={`status-dot ${getStatusClass(svc)}`} />
                        <div className="service-info">
                            <span className="service-name">:{svc.port}</span>
                            <div className="service-meta">
                                <span>{getStatusLabel(svc)}</span>
                                {svc.latency_ms != null && (
                                    <span className={latencyClass(svc.latency_ms)}>
                                        {formatLatency(svc.latency_ms)}
                                    </span>
                                )}
                                {svc.error_rate != null && (
                                    <span>{(svc.error_rate * 100).toFixed(1)}% err</span>
                                )}
                            </div>
                        </div>

                        <div className="service-actions">
                            {svc.status === 'running' && (
                                <button className="btn btn-sm btn-danger" onClick={() => onStop(svc.id)}>
                                    Stop
                                </button>
                            )}
                            {(svc.status === 'stopped' || svc.status === 'crashed') && (
                                <button className="btn btn-sm btn-success" onClick={() => onStart(svc.id)}>
                                    {svc.status === 'crashed' ? 'Restart' : 'Start'}
                                </button>
                            )}
                        </div>
                    </div>
                ))}
            </div>

            {showModal && (
                <AddBackendModal
                    onClose={() => setShowModal(false)}
                    onAdd={(data) => {
                        onAdd(data);
                        setShowModal(false);
                    }}
                />
            )}
        </div>
    );
}

function AddBackendModal({ onClose, onAdd }) {
    const [port, setPort] = useState('');
    const [command, setCommand] = useState('./tmp/testbackend');
    const [args, setArgs] = useState('-port');

    const handleSubmit = (e) => {
        e.preventDefault();
        onAdd({
            id: `backend-${port}`,
            port: parseInt(port, 10),
            command,
            args: `${args} ${port}`.split(' '),
        });
    };

    return (
        <div className="modal-overlay" onClick={onClose}>
            <div className="modal" onClick={(e) => e.stopPropagation()}>
                <h2>Add New Backend</h2>
                <form onSubmit={handleSubmit}>
                    <div className="modal-field">
                        <label>Port</label>
                        <input
                            type="number"
                            value={port}
                            onChange={(e) => setPort(e.target.value)}
                            placeholder="9005"
                            required
                        />
                    </div>
                    <div className="modal-field">
                        <label>Command</label>
                        <input
                            type="text"
                            value={command}
                            onChange={(e) => setCommand(e.target.value)}
                            placeholder="./tmp/testbackend"
                        />
                    </div>
                    <div className="modal-field">
                        <label>Arguments</label>
                        <input
                            type="text"
                            value={args}
                            onChange={(e) => setArgs(e.target.value)}
                            placeholder="-port"
                        />
                    </div>
                    <div className="modal-actions">
                        <button type="button" className="btn" onClick={onClose}>
                            Cancel
                        </button>
                        <button type="submit" className="btn btn-primary">
                            Add Backend
                        </button>
                    </div>
                </form>
            </div>
        </div>
    );
}
