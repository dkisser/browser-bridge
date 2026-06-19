import { GlobalRegistrator } from '@happy-dom/global-registrator';
import { describe, expect, it, jest } from 'bun:test';

GlobalRegistrator.register();

describe('popup cloud switch', () => {
  it('fetches status on load and sets checkbox state', async () => {
    const mockFetch = jest.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        success: true,
        data: {
          connected: true,
          browserId: 'b-123',
          serverUrl: 'ws://localhost:3001',
          manualDisconnect: false,
        },
      }),
    });
    globalThis.fetch = mockFetch as unknown as typeof fetch;

    document.body.innerHTML = `
      <div id="statusDot" class="dot disconnected"></div>
      <span id="statusLabel">Disconnected</span>
      <div id="browserId"></div>
      <input id="cloudSwitch" type="checkbox" />
      <div id="message"></div>
    `;

    await import('./popup');
    await new Promise((resolve) => setTimeout(resolve, 10));

    const checkbox = document.getElementById('cloudSwitch') as HTMLInputElement;
    expect(checkbox.checked).toBe(true);
    expect(document.getElementById('statusLabel')?.textContent).toBe(
      'Connected',
    );
    expect(mockFetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/status'),
    );
  });
});
