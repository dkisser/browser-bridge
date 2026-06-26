import type { BrowserConnection } from '@browser-bridge/shared';
import {
  type BrowserResolutionResult,
  resolveBrowser,
} from './browser-resolver';
import { sendEvent } from './command-client';
import type { ToolContext } from './tool-context';

function isBrowserConnection(value: unknown): value is BrowserConnection {
  return (
    typeof value === 'object' &&
    value !== null &&
    'browserId' in value &&
    typeof (value as Record<string, unknown>).browserId === 'string' &&
    'userId' in value &&
    typeof (value as Record<string, unknown>).userId === 'string' &&
    'status' in value &&
    typeof (value as Record<string, unknown>).status === 'string' &&
    'lastSeen' in value &&
    typeof (value as Record<string, unknown>).lastSeen === 'number'
  );
}

export async function fetchBrowserList(
  context: ToolContext,
  timeoutMs: number,
): Promise<BrowserConnection[]> {
  const result = await sendEvent({
    serverUrl: context.websocketUrl,
    event: 'list_browsers',
    payload: {},
    timeoutMs,
  });

  if (result.status !== 'ok') {
    throw new Error(result.error ?? 'Failed to list browsers');
  }

  if (!Array.isArray(result.data)) {
    return [];
  }

  return result.data.filter(isBrowserConnection);
}

export async function resolveTargetBrowser(
  context: ToolContext,
  timeoutMs: number,
): Promise<BrowserResolutionResult> {
  const explicit = context.sessions.getSession(context.sessionId).browserId;
  try {
    const browsers = await fetchBrowserList(context, timeoutMs);
    return resolveBrowser(explicit, browsers);
  } catch (error) {
    const message =
      error instanceof Error ? error.message : 'Failed to list browsers';
    return { success: false, message };
  }
}
