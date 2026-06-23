import type {
  BrowserConnection,
  ResponsePayload,
} from '@browser-bridge/shared/types';
import { ManagedClient } from '../managedClient';

export async function listBrowsers(
  server: string,
): Promise<BrowserConnection[]> {
  {
    using client = new ManagedClient(server);
    await client.waitForOpen(5000);

    const response = await client.request(
      'event',
      { event: 'list_browsers' },
      { timeout: 10000 },
    );
    const payload = response.payload as ResponsePayload;
    if (payload.status === 'error') {
      throw new Error(payload.message ?? payload.error ?? 'unknown');
    }
    return (payload.data as BrowserConnection[]) ?? [];
  }
}
