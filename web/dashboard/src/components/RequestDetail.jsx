export default function RequestDetail({ request, onClose }) {
    if (!request) return null;

    const statusBadgeClass = (status) => {
        if (status >= 500) return 'badge-error';
        if (status >= 400) return 'badge-warning';
        return 'badge-success';
    };

    const curlCommand = `curl -X ${request.method} http://localhost:8080${request.path}${request.request_headers?.Authorization
            ? ` \\\n  -H 'Authorization: ${request.request_headers.Authorization}'`
            : ''
        }`;

    const copyToClipboard = () => {
        navigator.clipboard.writeText(curlCommand);
    };

    return (
        <>
            <div className="detail-overlay" onClick={onClose} />
            <div className="detail-panel">
                <div className="detail-header">
                    <h2>Request Detail</h2>
                    <button className="detail-close" onClick={onClose}>✕</button>
                </div>

                <div className="detail-body">
                    {/* Overview */}
                    <div className="detail-section">
                        <h3>Overview</h3>
                        <div className="detail-grid">
                            <div className="detail-field">
                                <div className="detail-field-label">Request ID</div>
                                <div className="detail-field-value">{request.id}</div>
                            </div>
                            <div className="detail-field">
                                <div className="detail-field-label">Status</div>
                                <div className="detail-field-value">
                                    <span className={`badge ${statusBadgeClass(request.status)}`}>
                                        {request.status}
                                    </span>
                                </div>
                            </div>
                            <div className="detail-field">
                                <div className="detail-field-label">Method</div>
                                <div className="detail-field-value">
                                    <span className={`badge-method ${request.method}`}>{request.method}</span>
                                </div>
                            </div>
                            <div className="detail-field">
                                <div className="detail-field-label">Latency</div>
                                <div className="detail-field-value">{request.latency_ms}ms</div>
                            </div>
                            <div className="detail-field">
                                <div className="detail-field-label">Path</div>
                                <div className="detail-field-value">{request.path}</div>
                            </div>
                            <div className="detail-field">
                                <div className="detail-field-label">Backend</div>
                                <div className="detail-field-value">{request.backend}</div>
                            </div>
                            <div className="detail-field">
                                <div className="detail-field-label">Client IP</div>
                                <div className="detail-field-value">{request.client_ip}</div>
                            </div>
                            <div className="detail-field">
                                <div className="detail-field-label">Timestamp</div>
                                <div className="detail-field-value">
                                    {new Date(request.timestamp).toLocaleString()}
                                </div>
                            </div>
                        </div>
                    </div>

                    {/* Transfer */}
                    <div className="detail-section">
                        <h3>Transfer</h3>
                        <div className="detail-grid">
                            <div className="detail-field">
                                <div className="detail-field-label">Bytes In</div>
                                <div className="detail-field-value">
                                    {request.bytes_in != null ? `${request.bytes_in} B` : '—'}
                                </div>
                            </div>
                            <div className="detail-field">
                                <div className="detail-field-label">Bytes Out</div>
                                <div className="detail-field-value">
                                    {request.bytes_out != null ? `${request.bytes_out} B` : '—'}
                                </div>
                            </div>
                        </div>
                    </div>

                    {/* Request Headers */}
                    {request.request_headers && (
                        <div className="detail-section">
                            <h3>Request Headers</h3>
                            <div className="detail-headers-list">
                                {Object.entries(request.request_headers).map(([key, value]) => (
                                    <div key={key}>
                                        <span className="detail-header-key">{key}:</span>{' '}
                                        <span className="detail-header-value">{value}</span>
                                    </div>
                                ))}
                            </div>
                        </div>
                    )}

                    {/* Response Headers */}
                    {request.response_headers && (
                        <div className="detail-section">
                            <h3>Response Headers</h3>
                            <div className="detail-headers-list">
                                {Object.entries(request.response_headers).map(([key, value]) => (
                                    <div key={key}>
                                        <span className="detail-header-key">{key}:</span>{' '}
                                        <span className="detail-header-value">{value}</span>
                                    </div>
                                ))}
                            </div>
                        </div>
                    )}

                    {/* Copy as cURL */}
                    <div className="detail-section">
                        <h3>Copy as cURL</h3>
                        <div className="detail-headers-list" style={{ whiteSpace: 'pre-wrap' }}>
                            {curlCommand}
                        </div>
                        <button className="btn btn-sm copy-curl-btn" onClick={copyToClipboard}>
                            📋 Copy to Clipboard
                        </button>
                    </div>
                </div>
            </div>
        </>
    );
}
