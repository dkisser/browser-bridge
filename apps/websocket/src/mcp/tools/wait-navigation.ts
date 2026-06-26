import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const WaitNavigationInputSchema = z.object({
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeWaitNavigation(
  context: ToolContext,
  args: z.infer<typeof WaitNavigationInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'wait:navigation',
    params: { timeout: timeoutMs },
    timeoutMs,
  });

  if (result.status !== 'ok')
    throw new Error(result.error ?? 'Wait navigation failed');
  return result.message ?? 'Navigation complete';
}

export function registerWaitNavigationTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'wait_navigation',
    description: 'Wait for navigation to complete in the selected browser.',
    parameters: WaitNavigationInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeWaitNavigation(
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
