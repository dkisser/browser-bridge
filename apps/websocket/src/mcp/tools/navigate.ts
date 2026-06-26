import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const NavigateInputSchema = z.object({
  url: z.string().url(),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeNavigate(
  context: ToolContext,
  args: z.infer<typeof NavigateInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);

  if (!resolution.success) {
    throw new Error(resolution.message);
  }

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'navigate',
    params: { url: args.url },
    timeoutMs,
  });

  if (result.status !== 'ok') {
    throw new Error(result.error ?? 'Navigation failed');
  }

  return result.message ?? `Navigated to ${args.url}`;
}

export function registerNavigateTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'navigate',
    description: 'Navigate the active tab of the selected browser to a URL.',
    parameters: NavigateInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeNavigate(
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
