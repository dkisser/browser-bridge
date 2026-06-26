import { z } from 'zod';
import type { FastMCP } from 'fastmcp';
import type { ToolContext, ServerContext } from '../tool-context';

export const SetBrowserInputSchema = z.object({
  browserId: z.string().min(1),
});

export async function executeSetBrowser(
  context: ToolContext,
  args: z.infer<typeof SetBrowserInputSchema>,
): Promise<string> {
  context.sessions.setBrowser(context.sessionId, args.browserId);
  return `Browser set to "${args.browserId}" for this session.`;
}

export function registerSetBrowserTool(
  server: FastMCP,
  serverContext: ServerContext,
): void {
  server.addTool({
    name: 'set_browser',
    description:
      'Explicitly choose which connected browser to control for this MCP session.',
    parameters: SetBrowserInputSchema,
    execute: async (args, { sessionId }) => {
      const resolvedSessionId = sessionId ?? 'anonymous';
      return executeSetBrowser(
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
