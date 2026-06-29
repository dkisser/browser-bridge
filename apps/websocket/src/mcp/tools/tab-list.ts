import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const TabListInputSchema = z.object({
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeTabList(
  context: ToolContext,
  args: z.infer<typeof TabListInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'tab:list',
    params: {},
    timeoutMs,
  });

  if (result.status !== 'ok')
    throw new Error(result.error ?? 'tab:list failed');
  return JSON.stringify(result.data ?? [], null, 2);
}

export function registerTabListTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'tab_list',
    description: 'List all tabs in the selected browser.',
    parameters: TabListInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeTabList(
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
