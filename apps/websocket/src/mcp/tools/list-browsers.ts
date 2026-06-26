import type { BrowserConnection } from '@browser-bridge/shared';
import type { FastMCP } from 'fastmcp';
import { z } from 'zod';
import { fetchBrowserList } from '../browser-lookup';
import type { ServerContext, ToolContext } from '../tool-context';

export const ListBrowsersInputSchema = z.object({
  timeout_ms: z.number().int().min(100).max(120000).optional(),
});

export async function executeListBrowsers(
  context: ToolContext,
  args: z.infer<typeof ListBrowsersInputSchema>,
): Promise<string> {
  const timeoutMs =
    args.timeout_ms ??
    context.sessions.getSession(context.sessionId).defaultTimeoutMs;

  const browsers = await fetchBrowserList(context, timeoutMs);

  if (browsers.length === 0) {
    return 'No browsers connected.';
  }

  return browsers
    .map((b: BrowserConnection) => `- ${b.browserId} (${b.status})`)
    .join('\n');
}

export function registerListBrowsersTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'list_browsers',
    description: 'List all browsers connected to Browser Bridge.',
    parameters: ListBrowsersInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeListBrowsers(
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
