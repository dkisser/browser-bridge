import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const ScrollInputSchema = z.object({
  x: z.number().int(),
  y: z.number().int(),
  selector: z.string().min(1).optional().default('page'),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeScroll(
  context: ToolContext,
  args: z.infer<typeof ScrollInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'scroll',
    params: { selector: args.selector, x: args.x, y: args.y },
    timeoutMs,
  });

  if (result.status !== 'ok') throw new Error(result.error ?? 'Scroll failed');
  return result.message ?? `Scrolled to (${args.x}, ${args.y})`;
}

export function registerScrollTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'scroll',
    description: 'Scroll the page or an element in the selected browser.',
    parameters: ScrollInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeScroll(
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
