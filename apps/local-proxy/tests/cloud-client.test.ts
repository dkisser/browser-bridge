import { afterAll, beforeAll, describe, expect, test } from 'bun:test';
import { CloudClient } from '../src/cloud-client';

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

    try {
      client.close();
      expect(client.isManualDisconnect).toBe(true);

      // connect will fail because there is no server, that is fine
      try {
        await client.connect();
      } catch {
        // expected to fail
      }

      expect(client.isManualDisconnect).toBe(false);
    } finally {
      // The failed connect schedules an automatic reconnect; close must
      // cancel it so no timers leak into later tests.
      client.close();
    }
  });

  test('does not schedule reconnect after manual close', async () => {
    const client = new CloudClient({
      serverUrl: 'ws://localhost:9998',
      apiToken: 'test-token',
      browserId: 'test-browser',
      onCommand: () => {},
    });

    try {
      await client.connect();
      expect(client.isManualDisconnect).toBe(false);

      client.close();
      expect(client.isManualDisconnect).toBe(true);

      // Poll past the first reconnect delay (1s). Fail fast if a reconnect
      // is ever scheduled instead of sleeping a fixed duration.
      const deadline = Date.now() + 2000;
      while (Date.now() < deadline) {
        expect(client.reconnectAttemptsForTest).toBe(0);
        await new Promise((resolve) => setTimeout(resolve, 50));
      }
      expect(client.reconnectAttemptsForTest).toBe(0);
    } finally {
      client.close();
    }
  });

  test('fires onConnect on initial connect and on reconnect', async () => {
    let calls = 0;
    const client = new CloudClient({
      serverUrl: 'ws://localhost:9998',
      apiToken: 'test-token',
      browserId: 'test-browser',
      onCommand: () => {},
      onConnect: () => {
        calls++;
      },
    });

    try {
      await client.connect();
      client.close();
      await client.connect();
      expect(calls).toBe(2);
    } finally {
      client.close();
    }
  });
});
