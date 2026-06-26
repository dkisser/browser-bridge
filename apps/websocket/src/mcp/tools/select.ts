import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const SelectInputSchema = z.object({
  selector: z.string().min(1),
  value: z.string().min(1),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeSelect(
  context: ToolContext,
  args: z.infer<typeof SelectInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'select',
    params: { selector: args.selector, value: args.value },
    timeoutMs,
  });

  if (result.status !== 'ok') throw new Error(result.error ?? 'Select failed');
  return result.message ?? `Selected ${args.value} in ${args.selector}`;
}

export function registerSelectTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'select',
    description:
      'Select an option from a dropdown in the selected browser by CSS selector.',
    parameters: SelectInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeSelect(
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
