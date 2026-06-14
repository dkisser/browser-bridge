import { WEBSOCKET_PORT } from '@my/shared';

const pingButton = document.getElementById('ping') as HTMLButtonElement;
const resultDiv = document.getElementById('result') as HTMLDivElement;

pingButton.addEventListener('click', async () => {
  try {
    const response = await chrome.runtime.sendMessage({ type: 'ping' });
    resultDiv.textContent = `Response: ${JSON.stringify(response)}`;
  } catch (error) {
    resultDiv.textContent = `Error: ${String(error)}`;
  }
});

console.log('Popup loaded. WebSocket port:', WEBSOCKET_PORT);
