import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const GoBackInputSchema = z.object({
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeGoBack(
  context: ToolContext,
  args: z.infer<typeof GoBackInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'goBack',
    params: {},
    timeoutMs,
  });

  if (result.status !== 'ok') throw new Error(result.error ?? 'Go back failed');
  return result.message ?? 'Went back';
}

export function registerGoBackTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'go_back',
    description: 'Go back one page in the browser history.',
    parameters: GoBackInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeGoBack(
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
