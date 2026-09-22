import { type GethtmlResult, MAX_READ_RESULT_CHARS } from '@browser-bridge/shared';
import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { commandErrorMessage, sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';
import { SELECTOR_GUIDANCE, TAB_ID_GUIDANCE } from '../tool-descriptions';

export const GethtmlInputSchema = z.object({
  selector: z.string().min(1),
  tab_id: z.number().int().min(0),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeGethtml(
  context: ToolContext,
  args: z.infer<typeof GethtmlInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'gethtml',
    params: { selector: args.selector, tabId: args.tab_id },
    timeoutMs,
  });

  if (result.status !== 'ok')
    throw new Error(commandErrorMessage(result, 'gethtml failed'));
  const data = result.data as GethtmlResult;
  if (data.html.length > MAX_READ_RESULT_CHARS) {
    throw new Error(
      `get_html matched ${data.html.length} chars — almost certainly ` +
        'whole-page chrome (nav, ads, sidebars), not the content you ' +
        'want. Take a snapshot to find the content container, then ' +
        'narrow with a selector.',
    );
  }
  return data.html;
}

export function registerGethtmlTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'get_html',
    description:
      'Get the raw innerHTML of an element by CSS selector. Escape hatch ' +
      'for untouched markup — prefer the snapshot tool for reading and ' +
      'understanding page content. ' +
      SELECTOR_GUIDANCE +
      ' ' +
      TAB_ID_GUIDANCE,
    parameters: GethtmlInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeGethtml(
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
