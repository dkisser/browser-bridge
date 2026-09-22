import {
  afterAll,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
} from 'bun:test';
import { LocalServer } from '../src/local-server';
import { PairingManager } from '../src/pairing';

describe('LocalServer HTTP API', () => {
  let server: LocalServer;
  const port = 13002;
  let connected = false;
  let manualDisconnect = false;
  let tokenHash: string | undefined;

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
    tokenHash = undefined;
    const pairing = new PairingManager(
      () => tokenHash,
      (hash) => {
        tokenHash = hash;
      },
    );
    server = new LocalServer(
      port,
      {
        onCommand: () => {},
        onConnect: () => {},
        onDisconnect: () => {},
        cloud: mockCloud,
      },
      pairing,
    );
    server.start();
  });

  afterAll(() => {
    server.stop();
  });

  beforeEach(() => {
    connected = false;
    manualDisconnect = false;
  });

  it('GET /api/status returns cloud status and pairing state', async () => {
    const res = await fetch(`http://localhost:${port}/api/status`);
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.success).toBe(true);
    expect(body.data).toEqual({
      connected: false,
      browserId: 'b-test',
      serverUrl: 'ws://localhost:3001',
      manualDisconnect: false,
      paired: false,
      hasExtension: false,
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

  describe('CORS', () => {
    it('reflects chrome-extension origins', async () => {
      const res = await fetch(`http://localhost:${port}/api/status`, {
        headers: { Origin: 'chrome-extension://abcdefghijklmnopqrstuvwxyz' },
      });
      expect(res.status).toBe(200);
      expect(res.headers.get('access-control-allow-origin')).toBe(
        'chrome-extension://abcdefghijklmnopqrstuvwxyz',
      );
      expect(res.headers.get('vary')).toBe('Origin');
    });

    it('serves origin-less requests (curl/CLI) without CORS headers', async () => {
      const res = await fetch(`http://localhost:${port}/api/status`);
      expect(res.status).toBe(200);
      expect(res.headers.get('access-control-allow-origin')).toBeNull();
    });

    it('answers extension preflights with allow headers', async () => {
      const res = await fetch(`http://localhost:${port}/api/status`, {
        method: 'OPTIONS',
        headers: { Origin: 'chrome-extension://abcdefghijklmnop' },
      });
      expect(res.status).toBe(204);
      expect(res.headers.get('access-control-allow-origin')).toBe(
        'chrome-extension://abcdefghijklmnop',
      );
      expect(res.headers.get('access-control-allow-methods')).toContain('GET');
      expect(res.headers.get('access-control-allow-methods')).toContain('POST');
    });

    it('never sends Access-Control-Allow-Origin: *', async () => {
      const res = await fetch(`http://localhost:${port}/api/status`);
      expect(res.headers.get('access-control-allow-origin')).not.toBe('*');
    });

    it('rejects web-page origins on write routes even as simple requests', async () => {
      const res = await fetch(`http://localhost:${port}/api/disconnect`, {
        method: 'POST',
        headers: { Origin: 'https://evil.example' },
      });
      expect(res.status).toBe(403);
      const body = await res.json();
      expect(body.error).toBe('forbidden_origin');
    });
  });

  describe('pairing endpoints', () => {
    it('full flow: start → confirm → token, and status flips to paired', async () => {
      const startRes = await fetch(`http://localhost:${port}/api/pair/start`, {
        method: 'POST',
      });
      expect(startRes.status).toBe(200);
      const startBody = await startRes.json();
      expect(startBody.success).toBe(true);
      expect(startBody.data.code).toMatch(/^[0-9A-HJ-NP-TV-Z]{8}$/);
      expect(startBody.data.expiresIn).toBe(5 * 60 * 1000);

      const confirmRes = await fetch(
        `http://localhost:${port}/api/pair/confirm`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ code: startBody.data.code }),
        },
      );
      expect(confirmRes.status).toBe(200);
      const confirmBody = await confirmRes.json();
      expect(confirmBody.data.token).toMatch(/^[0-9a-f]{64}$/);

      const statusRes = await fetch(`http://localhost:${port}/api/status`);
      const statusBody = await statusRes.json();
      expect(statusBody.data.paired).toBe(true);
    });

    it('rejects a wrong code with 401 and attempts remaining', async () => {
      await fetch(`http://localhost:${port}/api/pair/start`, {
        method: 'POST',
      });
      const res = await fetch(`http://localhost:${port}/api/pair/confirm`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ code: 'ZZZZZZZZ' }),
      });
      expect(res.status).toBe(401);
      const body = await res.json();
      expect(body.success).toBe(false);
      expect(body.error).toBe('invalid_code');
      expect(body.attemptsRemaining).toBe(4);
    });

    it('rejects confirm without a pending code', async () => {
      const res = await fetch(`http://localhost:${port}/api/pair/confirm`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ code: 'ABCDEFGH' }),
      });
      expect(res.status).toBe(401);
      const body = await res.json();
      expect(body.error).toBe('invalid_code');
    });
  });

  describe('websocket upgrade gate', () => {
    function tryWs(
      url: string,
      protocols?: string[],
    ): Promise<{ opened: boolean }> {
      return new Promise((resolve) => {
        let opened = false;
        const ws = new WebSocket(url, protocols);
        const timer = setTimeout(() => {
          ws.close();
          resolve({ opened });
        }, 2000);
        ws.addEventListener('open', () => {
          opened = true;
          clearTimeout(timer);
          ws.close();
          resolve({ opened });
        });
        ws.addEventListener('close', () => {
          clearTimeout(timer);
          resolve({ opened });
        });
      });
    }

    it('rejects upgrade without a token', async () => {
      const { opened } = await tryWs(`ws://localhost:${port}/`);
      expect(opened).toBe(false);
    });

    it('rejects upgrade with a wrong token', async () => {
      const { opened } = await tryWs(`ws://localhost:${port}/`, [
        'ab'.repeat(32),
      ]);
      expect(opened).toBe(false);
    });

    it('accepts upgrade with the paired token in the subprotocol header', async () => {
      const startRes = await fetch(`http://localhost:${port}/api/pair/start`, {
        method: 'POST',
      });
      const { code } = (await startRes.json()).data;
      const confirmRes = await fetch(
        `http://localhost:${port}/api/pair/confirm`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ code }),
        },
      );
      const { token } = (await confirmRes.json()).data;

      const { opened } = await tryWs(`ws://localhost:${port}/`, [token]);
      expect(opened).toBe(true);
    });

    it('rejects a stale token after re-pairing', async () => {
      const startRes = await fetch(`http://localhost:${port}/api/pair/start`, {
        method: 'POST',
      });
      const { code } = (await startRes.json()).data;
      const confirmRes = await fetch(
        `http://localhost:${port}/api/pair/confirm`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ code }),
        },
      );
      const { token } = (await confirmRes.json()).data;

      // Re-pair: the previous token must no longer authenticate.
      const start2 = await fetch(`http://localhost:${port}/api/pair/start`, {
        method: 'POST',
      });
      const { code: code2 } = (await start2.json()).data;
      const confirm2 = await fetch(
        `http://localhost:${port}/api/pair/confirm`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ code: code2 }),
        },
      );
      const { token: token2 } = (await confirm2.json()).data;

      const stale = await tryWs(`ws://localhost:${port}/`, [token]);
      expect(stale.opened).toBe(false);
      const fresh = await tryWs(`ws://localhost:${port}/`, [token2]);
      expect(fresh.opened).toBe(true);
    });
  });
});
