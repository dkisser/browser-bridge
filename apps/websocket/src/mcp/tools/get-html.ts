import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const GethtmlInputSchema = z.object({
  selector: z.string().min(1),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeGethtml(
  context: ToolContext,
  args: z.infer<typeof GethtmlInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'gethtml',
    params: { selector: args.selector },
    timeoutMs,
  });

  if (result.status !== 'ok') throw new Error(result.error ?? 'gethtml failed');
  return String(result.data ?? '');
}

export function registerGethtmlTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'get_html',
    description: 'Get the HTML content of an element by CSS selector.',
    parameters: GethtmlInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeGethtml(
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
