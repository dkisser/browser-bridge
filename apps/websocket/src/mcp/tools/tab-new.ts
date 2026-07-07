import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const TabNewInputSchema = z.object({
  url: z.string().url().optional(),
  active: z.boolean().optional(),
  auto_close: z.boolean().optional(),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeTabNew(
  context: ToolContext,
  args: z.infer<typeof TabNewInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'tab:new',
    params: {
      url: args.url,
      active: args.active,
      auto_close: args.auto_close,
    },
    timeoutMs,
  });

  if (result.status !== 'ok') throw new Error(result.error ?? 'tab:new failed');
  return result.message ?? 'New tab opened';
}

export function registerTabNewTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'tab_new',
    description:
      'Open a new tab in the selected browser. Defaults to opening in the background (active=false).',
    parameters: TabNewInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeTabNew(
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
