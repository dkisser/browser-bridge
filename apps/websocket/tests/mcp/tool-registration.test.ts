import { describe, expect, it } from 'bun:test';
import { FastMCP } from 'fastmcp';
import { createBrowserSessionStore } from '../../src/mcp/browser-session';
import { registerClickTool } from '../../src/mcp/tools/click';
import { registerGethtmlTool } from '../../src/mcp/tools/get-html';
import { registerGettextTool } from '../../src/mcp/tools/get-text';
import { registerGoBackTool } from '../../src/mcp/tools/go-back';
import { registerGoForwardTool } from '../../src/mcp/tools/go-forward';
import { registerHoverTool } from '../../src/mcp/tools/hover';
import { registerListBrowsersTool } from '../../src/mcp/tools/list-browsers';
import { registerNavigateTool } from '../../src/mcp/tools/navigate';
import { registerPageinfoTool } from '../../src/mcp/tools/pageinfo';
import { registerRefreshTool } from '../../src/mcp/tools/refresh';
import { registerScreenshotTool } from '../../src/mcp/tools/screenshot';
import { registerScrollTool } from '../../src/mcp/tools/scroll';
import { registerSelectTool } from '../../src/mcp/tools/select';
import { registerSetBrowserTool } from '../../src/mcp/tools/set-browser';
import { registerTabCloseTool } from '../../src/mcp/tools/tab-close';
import { registerTabListTool } from '../../src/mcp/tools/tab-list';
import { registerTabNewTool } from '../../src/mcp/tools/tab-new';
import { registerTabSwitchTool } from '../../src/mcp/tools/tab-switch';
import { registerTypeTool } from '../../src/mcp/tools/type';
import { registerWaitElementTool } from '../../src/mcp/tools/wait-element';
import { registerWaitNavigationTool } from '../../src/mcp/tools/wait-navigation';

describe('tool registration', () => {
  it('registers all CLI-aligned tools', () => {
    const server = new FastMCP({
      name: 'test',
      version: '0.0.0',
    });
    const sessions = createBrowserSessionStore(10000);
    const context = {
      websocketUrl: 'ws://127.0.0.1:3001',
      sessions,
    };

    registerListBrowsersTool(server, context);
    registerSetBrowserTool(server, context);
    registerNavigateTool(server, context);
    registerGoBackTool(server, context);
    registerGoForwardTool(server, context);
    registerRefreshTool(server, context);
    registerTabListTool(server, context);
    registerTabNewTool(server, context);
    registerTabCloseTool(server, context);
    registerTabSwitchTool(server, context);
    registerClickTool(server, context);
    registerTypeTool(server, context);
    registerSelectTool(server, context);
    registerScrollTool(server, context);
    registerHoverTool(server, context);
    registerGettextTool(server, context);
    registerGethtmlTool(server, context);
    registerScreenshotTool(server, context);
    registerPageinfoTool(server, context);
    registerWaitElementTool(server, context);
    registerWaitNavigationTool(server, context);

    expect(server).toBeDefined();
  });
});
