import { LOCAL_WS_PORT } from '@browser-bridge/shared';

const API_BASE = `http://localhost:${LOCAL_WS_PORT}`;
const POLL_INTERVAL_MS = 5000;

const statusDot = document.getElementById('statusDot') as HTMLDivElement;
const statusLabel = document.getElementById('statusLabel') as HTMLSpanElement;
const browserIdDiv = document.getElementById('browserId') as HTMLDivElement;
const cloudSwitch = document.getElementById('cloudSwitch') as HTMLInputElement;
const messageDiv = document.getElementById('message') as HTMLDivElement;

interface StatusResponse {
  success: boolean;
  data?: {
    connected: boolean;
    browserId: string;
    serverUrl: string;
    manualDisconnect: boolean;
  };
  error?: string;
}

function updateStatus(connected: boolean, browserId?: string): void {
  statusDot.className = `dot ${connected ? 'connected' : 'disconnected'}`;
  statusLabel.textContent = connected ? 'Connected' : 'Disconnected';
  if (browserId) browserIdDiv.textContent = browserId;
}

function setMessage(text: string): void {
  messageDiv.textContent = text;
}

async function fetchStatus(): Promise<StatusResponse> {
  try {
    const response = await fetch(`${API_BASE}/api/status`);
    return (await response.json()) as StatusResponse;
  } catch {
    return { success: false, error: 'Local proxy unreachable' };
  }
}

async function setCloudConnection(connect: boolean): Promise<void> {
  const endpoint = connect ? '/api/connect' : '/api/disconnect';
  try {
    const response = await fetch(`${API_BASE}${endpoint}`, { method: 'POST' });
    const result = (await response.json()) as StatusResponse;
    if (!result.success) {
      setMessage(result.error ?? 'Request failed');
    }
  } catch {
    setMessage('Local proxy unreachable');
  }
}

async function refresh(): Promise<void> {
  const result = await fetchStatus();
  if (result.success && result.data) {
    cloudSwitch.checked = result.data.connected;
    updateStatus(result.data.connected, result.data.browserId);
    setMessage('');
    cloudSwitch.disabled = false;
  } else {
    setMessage(result.error ?? 'Unknown error');
    cloudSwitch.disabled = true;
  }
}

cloudSwitch.addEventListener('change', async () => {
  cloudSwitch.disabled = true;
  await setCloudConnection(cloudSwitch.checked);
  await refresh();
});

refresh();
const poll = setInterval(refresh, POLL_INTERVAL_MS);

window.addEventListener('unload', () => {
  clearInterval(poll);
});
