import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const TabSwitchInputSchema = z.object({
  tabId: z.number().int().min(0),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeTabSwitch(
  context: ToolContext,
  args: z.infer<typeof TabSwitchInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'tab:switch',
    params: { tabId: args.tabId },
    timeoutMs,
  });

  if (result.status !== 'ok')
    throw new Error(result.error ?? 'tab:switch failed');
  return result.message ?? `Switched to tab ${args.tabId}`;
}

export function registerTabSwitchTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'tab_switch',
    description: 'Switch to a tab by its ID in the selected browser.',
    parameters: TabSwitchInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeTabSwitch(
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
