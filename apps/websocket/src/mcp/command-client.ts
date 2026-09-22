import type {
  CommandPayload,
  CommandType,
  ContentScriptUnavailableReason,
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

    const tabId = options.params.tabId;
    const payload: CommandPayload = {
      command: options.command,
      tabId: typeof tabId === 'number' ? tabId : 0,
      params: options.params,
    };

    const envelope = await client.sendCommand(options.browserId, payload, {
      timeout: options.timeoutMs,
    });

    return withRecoveryHint(
      (envelope.payload ?? {
        status: 'error',
        error: 'Empty response',
      }) as ResponsePayload,
    );
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

// Error responses may carry a human-readable `message` next to the machine
// `error` code (policy denials, relay failures). Prefer it so tool callers
// see how to recover — e.g. asking the user to approve an origin in the
// extension popup — instead of a bare reason code that invites blind retries.
export function commandErrorMessage(
  result: ResponsePayload,
  fallback: string,
): string {
  return result.message ?? result.error ?? fallback;
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

// Chrome reports unknown tab ids as "No tab with id: N". Append the recovery
// step so a wrong guess points the caller at tab_list instead of a dead end.
const NO_TAB_ID_PATTERN = /No tab with id: \d+/i;
const TAB_LIST_HINT =
  ' Call tab_list to discover valid tab ids for the selected browser.';

// Recovery hints keyed off the structured `reason` the SW sends when a
// content-script dispatch fails. The MCP tool sees a single-line error;
// the hint turns a dead end into an actionable next step.
const CONTENT_SCRIPT_RECOVERY: Record<ContentScriptUnavailableReason, string> =
  {
    tab_not_found:
      ' The tab was closed between commands — call tab_list to discover valid tab ids.',
    restricted_page:
      ' Content commands only work on http(s) pages. Navigate to a non-restricted URL first.',
    injection_failed:
      ' The extension could not inject its content script into this page. Verify host_permissions cover the origin.',
    no_listener:
      ' The content script did not respond. Reload the page or retry the command.',
  };

export function withRecoveryHint(payload: ResponsePayload): ResponsePayload {
  if (payload.status !== 'error') return payload;

  // 1. Structured reason from the SW (preferred — exact classification).
  if (payload.reason && CONTENT_SCRIPT_RECOVERY[payload.reason]) {
    const hint = CONTENT_SCRIPT_RECOVERY[payload.reason];
    if (typeof payload.error === 'string' && !payload.error.endsWith(hint)) {
      return { ...payload, error: `${payload.error}${hint}` };
    }
  }

  // 2. Legacy fallback: pattern-match the Chrome error string for tabs
  //    that pre-date this PR's structured `reason` field.
  if (
    typeof payload.error === 'string' &&
    NO_TAB_ID_PATTERN.test(payload.error) &&
    !payload.error.includes('tab_list')
  ) {
    return { ...payload, error: `${payload.error}${TAB_LIST_HINT}` };
  }
  return payload;
}
