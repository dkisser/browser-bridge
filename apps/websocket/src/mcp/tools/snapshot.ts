import {
  DEFAULT_SNAPSHOT_MAX_CHARS,
  type SnapshotResult,
} from '@browser-bridge/shared';
import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const SnapshotInputSchema = z.object({
  selector: z.string().min(1).optional(),
  max_chars: z.number().int().min(100).max(100000).optional(),
  tab_id: z.number().int().min(0),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeSnapshot(
  context: ToolContext,
  args: z.infer<typeof SnapshotInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'snapshot',
    params: {
      selector: args.selector,
      max_chars: args.max_chars ?? DEFAULT_SNAPSHOT_MAX_CHARS,
      tabId: args.tab_id,
    },
    timeoutMs,
  });

  if (result.status !== 'ok')
    throw new Error(result.error ?? 'snapshot failed');
  const data = result.data as SnapshotResult;
  const stats = `[${data.nodes_emitted}/${data.nodes_total} nodes${
    data.truncated ? ' | truncated' : ''
  }]`;
  return `${data.snapshot}\n\n${stats}`;
}

export function registerSnapshotTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'snapshot',
    description:
      'Take a compact, budgeted snapshot of the page: a DOM-walked pseudo-tree ' +
      'of headings, links, buttons, text boxes, checkboxes, combos, lists, ' +
      'tables and text runs, with stable @eN refs you can pass to other tools ' +
      'as selectors (e.g. "@e12"). This is the DEFAULT tool for reading and ' +
      'understanding page content. Use get_html only when you need the raw, ' +
      'unmodified HTML of an element.',
    parameters: SnapshotInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeSnapshot(
        {
          sessionId: resolvedSessionId,
          sessions: serverContext.sessions,
          websocketUrl: serverContext.websocketUrl,
        },
        args,
      );
    },
  });
}
