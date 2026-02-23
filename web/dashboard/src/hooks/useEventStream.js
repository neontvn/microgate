import { useState, useEffect } from 'react';
import { fetchLogs, fetchProcesses, fetchMetrics } from '../services/api';

/**
 * Custom hook to connect to the Server-Sent Events stream
 * and manage real-time state for logs and processes/services.
 * 
 * @param {string} url - The URL to the SSE endpoint
 */
export default function useEventStream(url = '/dashboard/api/stream') {
    const [logs, setLogs] = useState([]);
    const [services, setServices] = useState([]);
    const [metrics, setMetrics] = useState(null);
    const [isConnected, setIsConnected] = useState(false);
    const [lastError, setLastError] = useState(null);

    // Initial data fetch
    useEffect(() => {
        let mounted = true;

        async function fetchInitialData() {
            try {
                const [initialLogs, initialServices, initialMetrics] = await Promise.all([
                    fetchLogs({ limit: 50 }),
                    fetchProcesses(),
                    fetchMetrics(),
                ]);

                if (mounted) {
                    setLogs(initialLogs);
                    setServices(initialServices);
                    setMetrics(initialMetrics);
                }
            } catch (err) {
                console.error("Failed to fetch initial data:", err);
                if (mounted) setLastError(err.message);
            }
        }

        fetchInitialData();
        return () => { mounted = false; };
    }, []);

    // SSE Connection Setup
    useEffect(() => {
        const source = new EventSource(url);

        source.onopen = () => {
            setIsConnected(true);
            setLastError(null);
            console.log("SSE Connection opened");
        };

        source.onerror = (err) => {
            console.error("SSE Error:", err);
            setIsConnected(false);
            setLastError("Lost connection to live stream. Reconnecting...");
        };

        // Listen for new requests
        source.addEventListener('request', (e) => {
            try {
                const newLog = JSON.parse(e.data);
                setLogs(prev => {
                    // Prepend the new log, capping at 200 items to avoid memory leaks
                    const updated = [newLog, ...prev];
                    return updated.length > 200 ? updated.slice(0, 200) : updated;
                });
            } catch (err) {
                console.error("Error parsing request event:", err);
            }
        });

        // Listen for process state changes (running, stopped, crashed)
        source.addEventListener('process', (e) => {
            try {
                const processEvent = JSON.parse(e.data);
                setServices(prev => {
                    const idx = prev.findIndex(p => p.id === processEvent.id);
                    if (idx === -1) {
                        // Brand new process we didn't know about yet
                        return [...prev, { ...processEvent, healthy: false }];
                    }
                    // Update existing process
                    const updated = [...prev];
                    updated[idx] = { ...updated[idx], ...processEvent };
                    return updated;
                });
            } catch (err) {
                console.error("Error parsing process event:", err);
            }
        });

        // Listen for service health changes (healthy/unhealthy from health checker)
        source.addEventListener('service', (e) => {
            try {
                const serviceEvent = JSON.parse(e.data);
                // serviceEvent looks like: { url: "http://localhost:9001", healthy: true }

                setServices(prev => {
                    // Find the process this belongs to based on port/URL
                    // Let's extract port from URL (not perfect but works for localhost format)
                    const urlObj = new URL(serviceEvent.url);
                    const port = parseInt(urlObj.port, 10);

                    const idx = prev.findIndex(p => p.port === port);
                    if (idx === -1) return prev; // Not found

                    const updated = [...prev];
                    updated[idx] = { ...updated[idx], healthy: serviceEvent.healthy };
                    return updated;
                });
            } catch (err) {
                console.error("Error parsing service event:", err);
            }
        });

        // Listen for periodic metrics snapshots
        source.addEventListener('metrics', (e) => {
            try {
                const metricsEvent = JSON.parse(e.data);
                setMetrics(metricsEvent);
            } catch (err) {
                console.error("Error parsing metrics event:", err);
            }
        });

        return () => {
            console.log("Closing SSE connection");
            source.close();
            setIsConnected(false);
        };
    }, [url]);

    return {
        logs,
        services,
        metrics,
        isConnected,
        error: lastError
    };
}
