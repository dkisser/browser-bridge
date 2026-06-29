import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const WaitElementInputSchema = z.object({
  selector: z.string().min(1),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeWaitElement(
  context: ToolContext,
  args: z.infer<typeof WaitElementInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'wait:element',
    params: { selector: args.selector, timeout: timeoutMs },
    timeoutMs,
  });

  if (result.status !== 'ok')
    throw new Error(result.error ?? 'Wait element failed');
  return result.message ?? `Element ${args.selector} found`;
}

export function registerWaitElementTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'wait_element',
    description:
      'Wait for an element to appear in the selected browser by CSS selector.',
    parameters: WaitElementInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeWaitElement(
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
