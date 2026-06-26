import { describe, expect, it } from 'bun:test';
import { FastMCP } from 'fastmcp';
import { createBrowserSessionStore } from '../browser-session';
import { registerClickTool } from '../tools/click';
import { registerGethtmlTool } from '../tools/get-html';
import { registerGettextTool } from '../tools/get-text';
import { registerGoBackTool } from '../tools/go-back';
import { registerGoForwardTool } from '../tools/go-forward';
import { registerHoverTool } from '../tools/hover';
import { registerListBrowsersTool } from '../tools/list-browsers';
import { registerNavigateTool } from '../tools/navigate';
import { registerPageinfoTool } from '../tools/pageinfo';
import { registerRefreshTool } from '../tools/refresh';
import { registerScreenshotTool } from '../tools/screenshot';
import { registerScrollTool } from '../tools/scroll';
import { registerSelectTool } from '../tools/select';
import { registerSetBrowserTool } from '../tools/set-browser';
import { registerTabCloseTool } from '../tools/tab-close';
import { registerTabListTool } from '../tools/tab-list';
import { registerTabNewTool } from '../tools/tab-new';
import { registerTabSwitchTool } from '../tools/tab-switch';
import { registerTypeTool } from '../tools/type';
import { registerWaitElementTool } from '../tools/wait-element';
import { registerWaitNavigationTool } from '../tools/wait-navigation';

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
