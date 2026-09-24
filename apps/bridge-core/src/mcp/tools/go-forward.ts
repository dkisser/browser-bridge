import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { commandErrorMessage, sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';
import { TAB_ID_GUIDANCE } from '../tool-descriptions';

export const GoForwardInputSchema = z.object({
  tab_id: z.number().int().min(0),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeGoForward(
  context: ToolContext,
  args: z.infer<typeof GoForwardInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'goForward',
    params: { tabId: args.tab_id },
    timeoutMs,
  });

  if (result.status !== 'ok')
    throw new Error(commandErrorMessage(result, 'Go forward failed'));
  return result.message ?? 'Went forward';
}

export function registerGoForwardTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'go_forward',
    description:
      'Go forward one page in the browser history. ' + TAB_ID_GUIDANCE,
    parameters: GoForwardInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeGoForward(
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
