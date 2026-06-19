import {
  afterAll,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
} from 'bun:test';
import { LocalServer } from './local-server';

describe('LocalServer HTTP API', () => {
  let server: LocalServer;
  const port = 13002;
  let connected = false;
  let manualDisconnect = false;

  const mockCloud = {
    isConnected: () => connected,
    isManualDisconnect: () => manualDisconnect,
    connect: async () => {
      connected = true;
    },
    disconnect: () => {
      connected = false;
      manualDisconnect = true;
    },
    browserId: 'b-test',
    serverUrl: 'ws://localhost:3001',
  };

  beforeAll(() => {
    server = new LocalServer(port, {
      onCommand: () => {},
      onConnect: () => {},
      onDisconnect: () => {},
      cloud: mockCloud,
    });
    server.start();
  });

  afterAll(() => {
    server.stop();
  });

  beforeEach(() => {
    connected = false;
    manualDisconnect = false;
  });

  it('GET /api/status returns cloud status', async () => {
    const res = await fetch(`http://localhost:${port}/api/status`);
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.success).toBe(true);
    expect(body.data).toEqual({
      connected: false,
      browserId: 'b-test',
      serverUrl: 'ws://localhost:3001',
      manualDisconnect: false,
    });
  });

  it('POST /api/connect updates connected state', async () => {
    const res = await fetch(`http://localhost:${port}/api/connect`, {
      method: 'POST',
    });
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.success).toBe(true);
    expect(body.data.connected).toBe(true);
  });

  it('POST /api/disconnect updates connected state', async () => {
    connected = true;
    const res = await fetch(`http://localhost:${port}/api/disconnect`, {
      method: 'POST',
    });
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.success).toBe(true);
    expect(body.data.connected).toBe(false);
    expect(manualDisconnect).toBe(true);
  });

  it('returns CORS headers for popup origin', async () => {
    const res = await fetch(`http://localhost:${port}/api/status`, {
      method: 'OPTIONS',
    });
    expect(res.headers.get('access-control-allow-origin')).toBe('*');
    expect(res.headers.get('access-control-allow-methods')).toContain('GET');
    expect(res.headers.get('access-control-allow-methods')).toContain('POST');
  });
});
