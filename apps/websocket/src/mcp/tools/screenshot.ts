import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { resolveTargetBrowser } from '../browser-lookup';
import { sendCommand } from '../command-client';
import type { ServerContext, ToolContext } from '../tool-context';

export const ScreenshotInputSchema = z.object({
  fullPage: z.boolean().optional(),
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeScreenshot(
  context: ToolContext,
  args: z.infer<typeof ScreenshotInputSchema>,
): Promise<{
  content: Array<{ type: 'image'; data: string; mimeType: 'image/png' }>;
}> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;
  const resolution = await resolveTargetBrowser(context, timeoutMs);
  if (!resolution.success) throw new Error(resolution.message);

  const result = await sendCommand({
    serverUrl: context.websocketUrl,
    browserId: resolution.browserId,
    command: 'screenshot',
    params: args.fullPage === undefined ? {} : { fullPage: args.fullPage },
    timeoutMs,
  });

  if (result.status !== 'ok')
    throw new Error(result.error ?? 'Screenshot failed');
  const data = typeof result.data === 'string' ? result.data : '';

  return {
    content: [{ type: 'image', data, mimeType: 'image/png' }],
  };
}

export function registerScreenshotTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'screenshot',
    description: 'Take a screenshot of the selected browser.',
    parameters: ScreenshotInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeScreenshot(
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
