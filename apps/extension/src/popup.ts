const statusDot = document.getElementById('statusDot') as HTMLDivElement;
const statusLabel = document.getElementById('statusLabel') as HTMLSpanElement;
const browserIdDiv = document.getElementById('browserId') as HTMLDivElement;
const connectButton = document.getElementById('connect') as HTMLButtonElement;
const pingButton = document.getElementById('ping') as HTMLButtonElement;
const resultDiv = document.getElementById('result') as HTMLDivElement;

async function checkStatus(): Promise<void> {
  try {
    const response = await chrome.runtime.sendMessage({ type: 'ping' });
    const connected = response?.connected ?? false;
    updateStatus(connected);
  } catch {
    updateStatus(false);
  }
}

function updateStatus(connected: boolean): void {
  statusDot.className = `dot ${connected ? 'connected' : 'disconnected'}`;
  statusLabel.textContent = connected ? 'Connected' : 'Disconnected';
}

connectButton.addEventListener('click', async () => {
  try {
    await chrome.runtime.sendMessage({ type: 'connect' });
    await checkStatus();
    resultDiv.textContent = 'Connection initiated';
  } catch (error) {
    resultDiv.textContent = `Error: ${String(error)}`;
  }
});

pingButton.addEventListener('click', async () => {
  try {
    const response = await chrome.runtime.sendMessage({ type: 'ping' });
    resultDiv.textContent = `Response: ${JSON.stringify(response)}`;
  } catch (error) {
    resultDiv.textContent = `Error: ${String(error)}`;
  }
});

checkStatus();
