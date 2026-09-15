import {
  defaultMaxCharsForFilter,
  type SnapshotFilter,
  type SnapshotResult,
} from '@browser-bridge/shared';
import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';
import { TAB_ID_GUIDANCE } from '../tool-descriptions';

export const SnapshotInputSchema = z.object({
  selector: z.string().min(1).optional(),
  filter: z.enum(['interactive', 'full']).optional(),
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

  const filter: SnapshotFilter = args.filter ?? 'interactive';
  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'snapshot',
    params: {
      selector: args.selector,
      filter,
      max_chars: args.max_chars ?? defaultMaxCharsForFilter(filter),
      tabId: args.tab_id,
    },
    timeoutMs,
  });

  if (result.status !== 'ok')
    throw new Error(result.error ?? 'snapshot failed');
  const data = result.data as SnapshotResult;
  const stats = `[${data.nodes_emitted}/${data.nodes_total} nodes | tier=${data.tier}${
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
      'Take a compact, budgeted snapshot of the page: interactive ' +
      'elements (links, buttons, text boxes, checkboxes, combos) and ' +
      'headings, with stable @eN refs you can pass to other tools as ' +
      'selectors (e.g. "@e12"). Use this to see what you can ACT ON. ' +
      'Default filter is "interactive"; pass filter="full" for the ' +
      'complete pseudo-tree including text runs, images and structural ' +
      'containers. To READ page content (article body, email subjects, ' +
      'feed items), use get_text instead — it returns plain text and is ' +
      'the right tool for reading. When output is truncated, text runs ' +
      'may be replaced by placeholders (full filter) or dropped ' +
      '(interactive filter); narrow with selector or raise max_chars. ' +
      TAB_ID_GUIDANCE,
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
