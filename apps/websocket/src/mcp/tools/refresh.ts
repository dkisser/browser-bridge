import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const RefreshInputSchema = z.object({
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeRefresh(
  context: ToolContext,
  args: z.infer<typeof RefreshInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'refresh',
    params: {},
    timeoutMs,
  });

  if (result.status !== 'ok') throw new Error(result.error ?? 'Refresh failed');
  return result.message ?? 'Refreshed';
}

export function registerRefreshTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'refresh',
    description: 'Refresh the current page.',
    parameters: RefreshInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeRefresh(
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
