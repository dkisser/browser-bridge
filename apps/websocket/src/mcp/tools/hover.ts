import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const HoverInputSchema = z.object({
  selector: z.string().min(1),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeHover(
  context: ToolContext,
  args: z.infer<typeof HoverInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'hover',
    params: { selector: args.selector },
    timeoutMs,
  });

  if (result.status !== 'ok') throw new Error(result.error ?? 'Hover failed');
  return result.message ?? `Hovered ${args.selector}`;
}

export function registerHoverTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'hover',
    description:
      'Hover over an element in the selected browser by CSS selector.',
    parameters: HoverInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeHover(
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
