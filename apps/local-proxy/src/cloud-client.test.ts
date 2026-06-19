import { describe, test, expect } from 'bun:test';
import { CloudClient } from './cloud-client';

describe('CloudClient', () => {
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
});
