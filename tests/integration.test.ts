import { afterAll, beforeAll, describe, expect, it } from 'bun:test';
import { ApiKeyAuthProvider } from '@browser-bridge/shared/auth';
import { BrowserServer } from '../apps/bridge-core/src/browser-server';
import { PairingManager } from '../apps/bridge-core/src/pairing';
import { Router } from '../apps/bridge-core/src/router';
import { InboundServer } from '../apps/bridge-core/src/server/inbound';
import { ConnectionRegistry } from '../apps/bridge-core/src/server/registry';
import { StateManager } from '../apps/bridge-core/src/state';

const WS_PORT = 3080;
const EXT_PORT = 3081;
const TEST_API_KEY = 'test-key-123';

function wsWithAuth(url: string, key: string): WebSocket {
  const opts = { headers: { Authorization: `Bearer ${key}` } };
  return new WebSocket(url, opts as never);
}

describe('Integration: CLI → bridge-core → extension', () => {
  let inbound: ReturnType<typeof InboundServer.prototype.start>;
  let browser: ReturnType<typeof BrowserServer.prototype.start>;
  let state: StateManager;
  let token: string;

  beforeAll(async () => {
    // Use an in-memory state by stubbing homedir via process env. We can't
    // easily redirect StateManager's config file location in this test, so
    // we accept that ~/.browser-bridge/config.json will be created/used; the
    // pairing hash we set below overrides whatever StateManager loaded.
    state = new StateManager();
    let pairingHash: string | undefined;
    const pairing = new PairingManager(
      () => pairingHash,
      (hash) => {
        pairingHash = hash;
      },
    );

    const registry = new ConnectionRegistry();
    const browserServer = new BrowserServer({
      port: EXT_PORT,
      getRouter: () => router,
      pairing,
    });
    const router = new Router(state, browserServer, registry);

    browser = browserServer.start();
    inbound = new InboundServer({
      port: WS_PORT,
      authProvider: new ApiKeyAuthProvider({ [TEST_API_KEY]: 'test-user' }),
      router,
      registry,
    }).start();

    // Pair an extension so it can connect on EXT_PORT.
    const start = await fetch(`http://localhost:${EXT_PORT}/api/pair/start`, {
      method: 'POST',
    });
    const { data: startData } = (await start.json()) as {
      data: { code: string };
    };
    const confirm = await fetch(`http://localhost:${EXT_PORT}/api/pair/confirm`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ code: startData.code }),
    });
    const { data: confirmData } = (await confirm.json()) as {
      data: { token: string };
    };
    token = confirmData.token;
  });

  afterAll(() => {
    inbound.stop();
    browser.stop();
  });

  function connectExtension(): Promise<WebSocket> {
    return new Promise((resolve, reject) => {
      const ws = new WebSocket(`ws://localhost:${EXT_PORT}`, [token]);
      ws.addEventListener('open', () => resolve(ws));
      ws.addEventListener('error', () => reject(new Error('connect failed')));
    });
  }

  it('routes command from CLI through bridge-core to extension', async () => {
    const ext = await connectExtension();
    // Drain any initial events the extension side sees on connect.
    await new Promise<void>((resolve) => {
      const handler = () => {
        ext.removeEventListener('message', handler);
        resolve();
      };
      ext.addEventListener('message', handler);
    });

    const cli = wsWithAuth(`ws://localhost:${WS_PORT}`, TEST_API_KEY);
    await new Promise<void>((resolve) => {
      cli.addEventListener('open', () => resolve());
    });
    // Drain welcome.
    await new Promise<void>((resolve) => {
      const handler = () => {
        cli.removeEventListener('message', handler);
        resolve();
      };
      cli.addEventListener('message', handler);
    });

    const cmdId = 'cmd-int-1';
    cli.send(
      JSON.stringify({
        id: cmdId,
        type: 'command',
        browserId: state.browserId,
        payload: {
          command: 'navigate',
          params: { url: 'https://example.com' },
        },
        timestamp: Date.now(),
      }),
    );

    // The extension should see it.
    const extMessage = await new Promise<string>((resolve) => {
      const handler = (e: MessageEvent) => {
        const data = JSON.parse(e.data as string);
        if (data.type === 'command' && data.id === cmdId) {
          ext.removeEventListener('message', handler);
          resolve(e.data as string);
        }
      };
      ext.addEventListener('message', handler);
    });

    const cmdParsed = JSON.parse(extMessage);
    expect(cmdParsed.payload.command).toBe('navigate');
    expect(cmdParsed.payload.params.url).toBe('https://example.com');

    // Extension replies; CLI should receive it via the router's id lookup.
    ext.send(
      JSON.stringify({
        id: cmdId,
        type: 'response',
        browserId: state.browserId,
        payload: {
          status: 'ok',
          data: { url: 'https://example.com', title: 'Example Domain' },
        },
        timestamp: Date.now(),
      }),
    );

    const cliResponse = await new Promise<string>((resolve) => {
      const handler = (e: MessageEvent) => {
        const data = JSON.parse(e.data as string);
        if (data.type === 'response' && data.id === cmdId) {
          cli.removeEventListener('message', handler);
          resolve(e.data as string);
        }
      };
      cli.addEventListener('message', handler);
    });

    const respParsed = JSON.parse(cliResponse);
    expect(respParsed.payload.status).toBe('ok');
    expect(respParsed.payload.data.url).toBe('https://example.com');

    cli.close();
    ext.close();
  });

  it('rejects command to unknown browser before any extension connects', async () => {
    // Note: by this point the previous test left an extension connected; that
    // means we can no longer fake an offline state without tearing down. The
    // assertion below targets a browserId that has never registered.
    const cli = wsWithAuth(`ws://localhost:${WS_PORT}`, TEST_API_KEY);
    await new Promise<void>((resolve) => {
      cli.addEventListener('open', () => resolve());
    });
    await new Promise<void>((resolve) => {
      const handler = () => {
        cli.removeEventListener('message', handler);
        resolve();
      };
      cli.addEventListener('message', handler);
    });

    cli.send(
      JSON.stringify({
        id: 'test-unknown',
        type: 'command',
        browserId: 'b-never-registered',
        payload: { command: 'navigate', params: {} },
        timestamp: Date.now(),
      }),
    );

    const response = await new Promise<string>((resolve) => {
      const handler = (e: MessageEvent) => {
        const data = JSON.parse(e.data as string);
        if (data.id === 'test-unknown') {
          cli.removeEventListener('message', handler);
          resolve(e.data as string);
        }
      };
      cli.addEventListener('message', handler);
    });

    const parsed = JSON.parse(response);
    expect(parsed.payload.status).toBe('error');
    expect(parsed.payload.error).toBe('browser_offline');

    cli.close();
  });

  it('rejects CLI connection without valid API key', async () => {
    const ws = new WebSocket(`ws://localhost:${WS_PORT}`);

    const closeEvent = await new Promise<CloseEvent>((resolve) => {
      ws.addEventListener('close', (e) => resolve(e));
    });

    expect(closeEvent.code).toBe(4001);
    expect(closeEvent.reason).toBe('unauthorized');
  });

  it('rejects CLI connection with wrong API key', async () => {
    const ws = wsWithAuth(`ws://localhost:${WS_PORT}`, 'wrong-key');

    const closeEvent = await new Promise<CloseEvent>((resolve) => {
      ws.addEventListener('close', (e) => resolve(e));
    });

    expect(closeEvent.code).toBe(4001);
    expect(closeEvent.reason).toBe('unauthorized');
  });
});