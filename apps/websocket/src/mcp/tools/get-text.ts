import {
  type GettextResult,
  selectorMatchedButEmptyMessage,
} from '@browser-bridge/shared';
import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';
import { TAB_ID_GUIDANCE } from '../tool-descriptions';

export const GettextInputSchema = z.object({
  selector: z.string().min(1),
  tab_id: z.number().int().min(0),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeGettext(
  context: ToolContext,
  args: z.infer<typeof GettextInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'gettext',
    params: { selector: args.selector, tabId: args.tab_id },
    timeoutMs,
  });

  if (result.status !== 'ok') throw new Error(result.error ?? 'gettext failed');
  const data = result.data as GettextResult;
  const text = data.text ?? '';
  if (text.trim() === '') {
    return selectorMatchedButEmptyMessage(args.selector);
  }
  return text;
}

export function registerGettextTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'get_text',
    description:
      'Get the text content of an element by CSS selector. ' + TAB_ID_GUIDANCE,
    parameters: GettextInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeGettext(
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
