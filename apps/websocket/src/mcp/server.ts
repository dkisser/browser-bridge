import { FastMCP } from 'fastmcp';
import { createBrowserSessionStore } from './browser-session';
import { registerClickTool } from './tools/click';
import { registerListBrowsersTool } from './tools/list-browsers';
import { registerNavigateTool } from './tools/navigate';
import { registerPageinfoTool } from './tools/pageinfo';
import { registerScreenshotTool } from './tools/screenshot';
import { registerSetBrowserTool } from './tools/set-browser';
import { registerTypeTool } from './tools/type';

export interface McpServerOptions {
  websocketUrl: string;
  port: number;
  hostname: string;
  defaultTimeoutMs: number;
  version: string;
}

export async function startMcpServer(
  options: McpServerOptions,
): Promise<FastMCP> {
  const semverVersion =
    options.version.match(/^(\d+\.\d+\.\d+)/)?.[0] ?? '0.0.0';

  const server = new FastMCP({
    name: 'Browser Bridge',
    version: semverVersion as `${number}.${number}.${number}`,
  });

  const sessions = createBrowserSessionStore(options.defaultTimeoutMs);
  const serverContext = {
    websocketUrl: options.websocketUrl,
    sessions,
  };

  registerListBrowsersTool(server, serverContext);
  registerSetBrowserTool(server, serverContext);
  registerNavigateTool(server, serverContext);
  registerClickTool(server, serverContext);
  registerTypeTool(server, serverContext);
  registerScreenshotTool(server, serverContext);
  registerPageinfoTool(server, serverContext);

  await server.start({
    transportType: 'httpStream',
    httpStream: {
      port: options.port,
      host: options.hostname,
      endpoint: '/mcp',
    },
  });

  return server;
}
