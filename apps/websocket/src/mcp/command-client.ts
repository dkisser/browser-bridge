import type {
  CommandPayload,
  CommandType,
  ResponsePayload,
} from '@browser-bridge/shared';
import { createClient } from '@browser-bridge/websocket/client';

export interface SendCommandOptions {
  serverUrl: string;
  browserId: string;
  command: CommandType;
  params: Record<string, unknown>;
  timeoutMs: number;
}

export interface SendEventOptions {
  serverUrl: string;
  event: string;
  payload: Record<string, unknown>;
  timeoutMs: number;
}

export async function sendCommand(
  options: SendCommandOptions,
): Promise<ResponsePayload> {
  const client = createClient({ url: options.serverUrl });

  try {
    await waitForOpen(client, Math.min(options.timeoutMs, 5000));

    const payload: CommandPayload = {
      command: options.command,
      params: options.params,
    };

    const envelope = await client.sendCommand(options.browserId, payload, {
      timeout: options.timeoutMs,
    });

    return (envelope.payload ?? {
      status: 'error',
      error: 'Empty response',
    }) as ResponsePayload;
  } finally {
    client.close();
  }
}

export async function sendEvent(
  options: SendEventOptions,
): Promise<ResponsePayload> {
  const client = createClient({ url: options.serverUrl });

  try {
    await waitForOpen(client, Math.min(options.timeoutMs, 5000));

    const envelope = await client.request(
      'event',
      { event: options.event, ...options.payload },
      { timeout: options.timeoutMs },
    );

    return (envelope.payload ?? {
      status: 'error',
      error: 'Empty response',
    }) as ResponsePayload;
  } finally {
    client.close();
  }
}

function waitForOpen(
  client: ReturnType<typeof createClient>,
  timeoutMs: number,
): Promise<void> {
  return new Promise((resolve, reject) => {
    if (client.readyState === WebSocket.OPEN) {
      resolve();
      return;
    }

    const start = Date.now();
    const interval = setInterval(() => {
      if (client.readyState === WebSocket.OPEN) {
        clearInterval(interval);
        resolve();
        return;
      }
      if (Date.now() - start > timeoutMs) {
        clearInterval(interval);
        client.close();
        reject(new Error('Failed to connect to WebSocket server'));
      }
    }, 10);
  });
}
