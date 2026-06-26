import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const TypeInputSchema = z.object({
  selector: z.string().min(1),
  text: z.string(),
  submit: z.boolean().optional(),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeType(
  context: ToolContext,
  args: z.infer<typeof TypeInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'type',
    params: { selector: args.selector, text: args.text, submit: args.submit },
    timeoutMs,
  });

  if (result.status !== 'ok') throw new Error(result.error ?? 'Type failed');
  return result.message ?? `Typed into ${args.selector}`;
}

export function registerTypeTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'type',
    description: 'Type text into an input element in the selected browser.',
    parameters: TypeInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeType(
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
