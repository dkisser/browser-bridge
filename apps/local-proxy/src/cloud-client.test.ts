import { afterAll, beforeAll, describe, test, expect } from 'bun:test';
import { CloudClient } from './cloud-client';

describe('CloudClient', () => {
  let server: ReturnType<typeof Bun.serve>;

  beforeAll(() => {
    server = Bun.serve({
      port: 9998,
      fetch(req, svc) {
        if (svc.upgrade(req, { data: undefined })) return;
        return new Response('ok', { status: 200 });
      },
      websocket: {
        open() {},
        message() {},
        close() {},
      },
    });
  });

  afterAll(() => {
    server.stop();
  });

  test('sets manualDisconnect to true when close() is called', () => {
    const client = new CloudClient({
      serverUrl: 'ws://localhost:9999',
      apiToken: 'test-token',
      browserId: 'test-browser',
      onCommand: () => {},
    });

    expect(client.isManualDisconnect).toBe(false);
    client.close();
    expect(client.isManualDisconnect).toBe(true);
  });

  test('resets manualDisconnect to false when connect() is called', async () => {
    const client = new CloudClient({
      serverUrl: 'ws://localhost:9999',
      apiToken: 'test-token',
      browserId: 'test-browser',
      onCommand: () => {},
    });

    client.close();
    expect(client.isManualDisconnect).toBe(true);

    // connect will fail because there is no server, that is fine
    try {
      await client.connect();
    } catch {
      // expected to fail
    }

    expect(client.isManualDisconnect).toBe(false);
  });

  test('does not schedule reconnect after manual close', async () => {
    const client = new CloudClient({
      serverUrl: 'ws://localhost:9998',
      apiToken: 'test-token',
      browserId: 'test-browser',
      onCommand: () => {},
    });

    await client.connect();
    expect(client.isManualDisconnect).toBe(false);

    client.close();
    expect(client.isManualDisconnect).toBe(true);

    // Wait longer than the first reconnect delay (1s)
    await new Promise((resolve) => setTimeout(resolve, 1500));
    expect(client.reconnectAttemptsForTest).toBe(0);
  });
});
