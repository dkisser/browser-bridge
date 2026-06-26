import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const ClickInputSchema = z.object({
  selector: z.string().min(1),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeClick(
  context: ToolContext,
  args: z.infer<typeof ClickInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'click',
    params: { selector: args.selector },
    timeoutMs,
  });

  if (result.status !== 'ok') throw new Error(result.error ?? 'Click failed');
  return result.message ?? `Clicked ${args.selector}`;
}

export function registerClickTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'click',
    description: 'Click an element in the selected browser by CSS selector.',
    parameters: ClickInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeClick(
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
