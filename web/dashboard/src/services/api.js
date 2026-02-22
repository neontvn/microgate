// Base path for Dashboard API.
// In development, this is proxied to the Go server (e.g. localhost:8080) by Vite.
// In production, the React app is served directly by the Go server, so this works relatively.
const API_BASE = '/dashboard/api';

/**
 * Fetches recent request logs
 * @param {Object} filters - Optional filters
 * @param {number} filters.limit - Max number of logs to return (default 50)
 * @param {number} filters.status - Filter by HTTP status code
 * @param {string} filters.path - Filter by request path (substring match)
 * @returns {Promise<Array>} Array of RequestLog objects
 */
export async function fetchLogs(filters = {}) {
    const params = new URLSearchParams();
    if (filters.limit) params.append('limit', filters.limit);
    if (filters.status) params.append('status', filters.status);
    if (filters.path) params.append('path', filters.path);

    const query = params.toString();
    const url = query ? `${API_BASE}/logs?${query}` : `${API_BASE}/logs`;
    
    const res = await fetch(url);
    if (!res.ok) {
        throw new Error(`Failed to fetch logs: ${res.statusText}`);
    }
    const data = await res.json();
    return data.logs || [];
}

/**
 * Fetches a single request log by ID
 * @param {string} id 
 * @returns {Promise<Object>} RequestLog object
 */
export async function fetchLogDetail(id) {
    const res = await fetch(`${API_BASE}/logs/${id}`);
    if (!res.ok) {
        throw new Error(`Failed to fetch log detail: ${res.statusText}`);
    }
    return res.json();
}

/**
 * Fetches all managed backend processes and their health status
 * @returns {Promise<Array>} Array of Process objects with health status
 */
export async function fetchProcesses() {
    const res = await fetch(`${API_BASE}/processes`);
    if (!res.ok) {
        throw new Error(`Failed to fetch processes: ${res.statusText}`);
    }
    const data = await res.json();
    return data.processes || [];
}

/**
 * Starts a managed process
 * @param {string} id - The process ID
 */
export async function startProcess(id) {
    const res = await fetch(`${API_BASE}/processes/${id}/start`, {
        method: 'POST'
    });
    if (!res.ok) {
        const errText = await res.text();
        throw new Error(errText || `Failed to start process: ${res.statusText}`);
    }
}

/**
 * Stops a managed process
 * @param {string} id - The process ID
 */
export async function stopProcess(id) {
    const res = await fetch(`${API_BASE}/processes/${id}/stop`, {
        method: 'POST'
    });
    if (!res.ok) {
        const errText = await res.text();
        throw new Error(errText || `Failed to stop process: ${res.statusText}`);
    }
}

/**
 * Adds a new process to be managed by the gateway
 * @param {Object} data 
 * @param {string} data.id
 * @param {string} data.command
 * @param {Array<string>} data.args
 * @param {number} data.port
 */
export async function addProcess(data) {
    const res = await fetch(`${API_BASE}/processes`, {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json'
        },
        body: JSON.stringify(data)
    });
    if (!res.ok) {
        const errText = await res.text();
        throw new Error(errText || `Failed to add process: ${res.statusText}`);
    }
}
