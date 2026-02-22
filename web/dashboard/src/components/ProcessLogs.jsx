import { useRef, useEffect } from 'react';

export default function ProcessLogs({ logs, title, onClear }) {
    const scrollRef = useRef(null);

    useEffect(() => {
        if (scrollRef.current) {
            scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
        }
    }, [logs]);

    return (
        <div className="card" style={{ padding: 'var(--space-md)' }}>
            <div className="card-header" style={{ marginBottom: 'var(--space-sm)' }}>
                <span className="card-title">{title || 'Process Output'}</span>
                {onClear && (
                    <button className="btn btn-sm" onClick={onClear}>
                        Clear
                    </button>
                )}
            </div>
            <div className="process-logs" ref={scrollRef}>
                {logs.map((line, i) => (
                    <div key={i} className="log-line">
                        {formatLogLine(line)}
                    </div>
                ))}
                {logs.length === 0 && (
                    <div className="text-muted" style={{ padding: '8px 0' }}>
                        No output yet
                    </div>
                )}
            </div>
        </div>
    );
}

function formatLogLine(line) {
    // Highlight timestamps in brackets
    const match = line.match(/^(\[.*?\])\s(.*)$/);
    if (match) {
        return (
            <>
                <span className="timestamp">{match[1]}</span> {colorizeContent(match[2])}
            </>
        );
    }
    return colorizeContent(line);
}

function colorizeContent(text) {
    // Colorize ERROR
    if (text.startsWith('ERROR')) {
        return <span style={{ color: 'var(--status-error)' }}>{text}</span>;
    }
    // Colorize status codes in log output
    if (/\b[45]\d{2}\b/.test(text)) {
        return <span style={{ color: 'var(--status-warning)' }}>{text}</span>;
    }
    return text;
}
