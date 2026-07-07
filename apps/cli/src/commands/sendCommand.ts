import type {
  CommandType,
  ResponsePayload,
} from '@browser-bridge/shared/types';
import { ManagedClient } from '../managedClient';

export interface SendCommandOptions {
  server: string;
  browser?: string;
  tabId?: number;
  timeout?: number;
}

export async function sendCommand(
  options: SendCommandOptions,
  command: CommandType,
  params: Record<string, unknown> = {},
): Promise<unknown> {
  if (!options.browser) {
    throw new Error('Required: --browser <id>');
  }

  if (options.tabId === undefined) {
    throw new Error('Required: --tab <id>');
  }

  {
    using client = new ManagedClient(options.server);
    await client.waitForOpen(5000);

    const response = await client.sendCommand(
      options.browser,
      { command, tabId: options.tabId, params },
      { timeout: options.timeout ?? 10000 },
    );
    const payload = response.payload as ResponsePayload;

    if (payload.status === 'error') {
      throw new Error(payload.message ?? 'Unknown error');
    }

    return payload.data ?? { status: 'ok' };
  }
}
