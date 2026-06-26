import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const PageinfoInputSchema = z.object({
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executePageinfo(
  context: ToolContext,
  args: z.infer<typeof PageinfoInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'pageinfo',
    params: {},
    timeoutMs,
  });

  if (result.status !== 'ok')
    throw new Error(result.error ?? 'pageinfo failed');
  return JSON.stringify(result.data ?? {}, null, 2);
}

export function registerPageinfoTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'pageinfo',
    description: 'Get title, URL, and tab list from the selected browser.',
    parameters: PageinfoInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executePageinfo(
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
