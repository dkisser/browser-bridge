import { afterAll, beforeAll, describe, expect, it } from 'bun:test';
import { startServer } from '../apps/websocket/src/server';
import { ApiKeyAuthProvider } from '../packages/shared/src/auth';

const TEST_PORT = 3080;
const TEST_API_KEY = 'test-key-123';
const TEST_USER_ID = 'test-user';

describe('Integration: CLI → Server → Local Proxy', () => {
  let server: ReturnType<typeof startServer>;

  beforeAll(() => {
    server = startServer(
      TEST_PORT,
      new ApiKeyAuthProvider({ [TEST_API_KEY]: TEST_USER_ID }),
    );
  });

  afterAll(() => {
    server.stop();
  });

  it('rejects command to unregistered browser', async () => {
    const cli = new WebSocket(`ws://localhost:${TEST_PORT}`, {
      headers: { Authorization: `Bearer ${TEST_API_KEY}` },
    } as any);

    await new Promise<void>((resolve) => {
      cli.addEventListener('open', () => resolve());
    });

    const response = await new Promise<string>((resolve) => {
      cli.addEventListener('message', (e) => {
        const data = JSON.parse(e.data as string);
        if (data.type === 'response' && data.id === 'test-1') {
          resolve(e.data as string);
        }
      });
      cli.send(
        JSON.stringify({
          id: 'test-1',
          type: 'command',
          browserId: 'b-nonexistent',
          payload: {
            command: 'navigate',
            params: { url: 'https://example.com' },
          },
          timestamp: Date.now(),
        }),
      );
    });

    const parsed = JSON.parse(response);
    expect(parsed.payload.status).toBe('error');
    expect(parsed.payload.error).toBe('browser_offline');

    cli.close();
  });

  it('registers a Local Proxy and routes command to it', async () => {
    // 1. Connect a mock Local Proxy WITH auth header
    const proxy = new WebSocket(`ws://localhost:${TEST_PORT}`, {
      headers: { Authorization: `Bearer ${TEST_API_KEY}` },
    } as any);
    await new Promise<void>((resolve) => {
      proxy.addEventListener('open', () => resolve());
    });

    // Consume welcome message
    await new Promise<void>((resolve) => {
      proxy.addEventListener('message', function handler() {
        proxy.removeEventListener('message', handler);
        resolve();
      });
    });

    // 2. Register the proxy (no token in message body)
    const registerResponse = await new Promise<string>((resolve) => {
      const handler = (e: MessageEvent) => {
        const data = JSON.parse(e.data as string);
        if (data.id === 'reg-1') {
          proxy.removeEventListener('message', handler);
          resolve(e.data as string);
        }
      };
      proxy.addEventListener('message', handler);
      proxy.send(
        JSON.stringify({
          id: 'reg-1',
          type: 'event',
          browserId: 'b-integration',
          payload: { event: 'register', browserId: 'b-integration' },
          timestamp: Date.now(),
        }),
      );
    });

    const regParsed = JSON.parse(registerResponse);
    expect(regParsed.payload.status).toBe('ok');

    // 3. Report online
    const onlineResponse = await new Promise<string>((resolve) => {
      const handler = (e: MessageEvent) => {
        const data = JSON.parse(e.data as string);
        if (data.id === 'online-1') {
          proxy.removeEventListener('message', handler);
          resolve(e.data as string);
        }
      };
      proxy.addEventListener('message', handler);
      proxy.send(
        JSON.stringify({
          id: 'online-1',
          type: 'event',
          browserId: 'b-integration',
          payload: { event: 'online', browserId: 'b-integration' },
          timestamp: Date.now(),
        }),
      );
    });

    expect(JSON.parse(onlineResponse).payload.status).toBe('ok');

    // 4. Connect CLI and send command
    const cli = new WebSocket(`ws://localhost:${TEST_PORT}`, {
      headers: { Authorization: `Bearer ${TEST_API_KEY}` },
    } as any);
    await new Promise<void>((resolve) => {
      cli.addEventListener('open', () => resolve());
    });

    // Consume welcome
    await new Promise<void>((resolve) => {
      cli.addEventListener('message', function handler() {
        cli.removeEventListener('message', handler);
        resolve();
      });
    });

    // Send command
    const cmdId = 'cmd-int-1';
    cli.send(
      JSON.stringify({
        id: cmdId,
        type: 'command',
        browserId: 'b-integration',
        payload: {
          command: 'navigate',
          params: { url: 'https://example.com' },
        },
        timestamp: Date.now(),
      }),
    );

    // 5. Verify command arrives at proxy
    const proxyMessage = await new Promise<string>((resolve) => {
      const handler = (e: MessageEvent) => {
        const data = JSON.parse(e.data as string);
        if (data.type === 'command' && data.id === cmdId) {
          proxy.removeEventListener('message', handler);
          resolve(e.data as string);
        }
      };
      proxy.addEventListener('message', handler);
    });

    const cmdParsed = JSON.parse(proxyMessage);
    expect(cmdParsed.type).toBe('command');
    expect(cmdParsed.payload.command).toBe('navigate');
    expect(cmdParsed.payload.params.url).toBe('https://example.com');

    // 6. Proxy sends response back
    proxy.send(
      JSON.stringify({
        id: cmdId,
        type: 'response',
        browserId: 'b-integration',
        payload: {
          status: 'ok',
          data: { url: 'https://example.com', title: 'Example Domain' },
        },
        timestamp: Date.now(),
      }),
    );

    // 7. Verify CLI receives response
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
    expect(respParsed.payload.data.title).toBe('Example Domain');

    cli.close();
    proxy.close();
  });

  it('rejects connection without valid API key', async () => {
    const ws = new WebSocket(`ws://localhost:${TEST_PORT}`);

    const closeEvent = await new Promise<CloseEvent>((resolve) => {
      ws.addEventListener('close', (e) => resolve(e));
    });

    expect(closeEvent.code).toBe(4001);
    expect(closeEvent.reason).toBe('unauthorized');
  });

  it('rejects connection with wrong API key', async () => {
    const ws = new WebSocket(`ws://localhost:${TEST_PORT}`, {
      headers: { Authorization: 'Bearer wrong-key' },
    } as any);

    const closeEvent = await new Promise<CloseEvent>((resolve) => {
      ws.addEventListener('close', (e) => resolve(e));
    });

    expect(closeEvent.code).toBe(4001);
    expect(closeEvent.reason).toBe('unauthorized');
  });
});
