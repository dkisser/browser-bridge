import { FastMCP } from 'fastmcp';
import { createBrowserSessionStore } from './browser-session';
import { registerClickTool } from './tools/click';
import { registerGethtmlTool } from './tools/get-html';
import { registerGettextTool } from './tools/get-text';
import { registerGoBackTool } from './tools/go-back';
import { registerGoForwardTool } from './tools/go-forward';
import { registerHoverTool } from './tools/hover';
import { registerListBrowsersTool } from './tools/list-browsers';
import { registerNavigateTool } from './tools/navigate';
import { registerPageinfoTool } from './tools/pageinfo';
import { registerRefreshTool } from './tools/refresh';
import { registerScreenshotTool } from './tools/screenshot';
import { registerScrollTool } from './tools/scroll';
import { registerSelectTool } from './tools/select';
import { registerSetBrowserTool } from './tools/set-browser';
import { registerTabCloseTool } from './tools/tab-close';
import { registerTabListTool } from './tools/tab-list';
import { registerTabNewTool } from './tools/tab-new';
import { registerTabSwitchTool } from './tools/tab-switch';
import { registerTypeTool } from './tools/type';
import { registerWaitElementTool } from './tools/wait-element';
import { registerWaitNavigationTool } from './tools/wait-navigation';

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
  registerGoBackTool(server, serverContext);
  registerGoForwardTool(server, serverContext);
  registerRefreshTool(server, serverContext);
  registerTabListTool(server, serverContext);
  registerTabNewTool(server, serverContext);
  registerTabCloseTool(server, serverContext);
  registerTabSwitchTool(server, serverContext);
  registerClickTool(server, serverContext);
  registerTypeTool(server, serverContext);
  registerSelectTool(server, serverContext);
  registerScrollTool(server, serverContext);
  registerHoverTool(server, serverContext);
  registerGettextTool(server, serverContext);
  registerGethtmlTool(server, serverContext);
  registerScreenshotTool(server, serverContext);
  registerPageinfoTool(server, serverContext);
  registerWaitElementTool(server, serverContext);
  registerWaitNavigationTool(server, serverContext);

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
