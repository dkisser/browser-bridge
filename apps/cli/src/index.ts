#!/usr/bin/env bun
import { WEBSOCKET_PORT } from '@browser-bridge/shared';
import type { CommandType } from '@browser-bridge/shared/types';
import { Command } from 'commander';
import { listBrowsers } from './commands/listBrowsers';
import { sendCommand } from './commands/sendCommand';

const program = new Command();
program.name('bridge').description('Browser Bridge CLI').version('0.0.1');

interface GlobalOptions {
  server: string;
  browser: string;
  json: boolean;
  timeout: number;
}

function getGlobalOptions(opts: Record<string, unknown>): GlobalOptions {
  return {
    server: (opts.server as string) || `ws://localhost:${WEBSOCKET_PORT}`,
    browser: opts.browser as string,
    json: opts.json as boolean,
    timeout: (opts.timeout as number) || 10000,
  };
}

function output(global: GlobalOptions, data: unknown): void {
  if (global.json) {
    console.log(JSON.stringify(data, null, 2));
  } else {
    console.log(data);
  }
}

function outputError(
  global: GlobalOptions,
  error: string,
  message: string,
): void {
  if (global.json) {
    console.log(JSON.stringify({ status: 'error', error, message }));
  } else {
    console.error(`Error: ${message}`);
  }
  process.exit(1);
}

async function dispatchCommand(
  global: GlobalOptions,
  command: CommandType,
  params: Record<string, unknown> = {},
): Promise<void> {
  try {
    const data = await sendCommand(
      {
        server: global.server,
        browser: global.browser,
        timeout: global.timeout,
      },
      command,
      params,
    );
    output(global, data);
  } catch (err) {
    outputError(global, 'command_failed', String(err));
  }
}

// Global options
program
  .option('--server <url>', 'WS Server URL', `ws://localhost:${WEBSOCKET_PORT}`)
  .option('--browser <id>', 'Target browser instance')
  .option('--json', 'Structured JSON output')
  .option('--timeout <ms>', 'Command timeout', '10000');

// Navigation commands
program
  .command('navigate <url>')
  .description('Navigate to URL')
  .action(async (url: string) => {
    const global = getGlobalOptions(program.opts());
    await dispatchCommand(global, 'navigate', { url });
  });

program
  .command('goBack')
  .description('Go back in browser history')
  .action(async () => {
    const global = getGlobalOptions(program.opts());
    await dispatchCommand(global, 'goBack');
  });

program
  .command('goForward')
  .description('Go forward in browser history')
  .action(async () => {
    const global = getGlobalOptions(program.opts());
    await dispatchCommand(global, 'goForward');
  });

program
  .command('refresh')
  .description('Refresh current page')
  .action(async () => {
    const global = getGlobalOptions(program.opts());
    await dispatchCommand(global, 'refresh');
  });

// Tab management
program
  .command('tab:list')
  .description('List all open tabs')
  .action(async () => {
    const global = getGlobalOptions(program.opts());
    await dispatchCommand(global, 'tab:list');
  });

program
  .command('tab:new [url]')
  .description('Open a new tab')
  .action(async (url: string | undefined) => {
    const global = getGlobalOptions(program.opts());
    await dispatchCommand(global, 'tab:new', { url });
  });

program
  .command('tab:close <tabId>')
  .description('Close a tab by ID')
  .action(async (tabId: string) => {
    const global = getGlobalOptions(program.opts());
    await dispatchCommand(global, 'tab:close', { tabId: Number(tabId) });
  });

program
  .command('tab:switch <tabId>')
  .description('Switch to a tab by ID')
  .action(async (tabId: string) => {
    const global = getGlobalOptions(program.opts());
    await dispatchCommand(global, 'tab:switch', { tabId: Number(tabId) });
  });

// DOM interaction
program
  .command('click <selector>')
  .description('Click an element')
  .action(async (selector: string) => {
    const global = getGlobalOptions(program.opts());
    await dispatchCommand(global, 'click', { selector });
  });

program
  .command('type <selector> <text>')
  .description('Type text into an element')
  .action(async (selector: string, text: string) => {
    const global = getGlobalOptions(program.opts());
    await dispatchCommand(global, 'type', { selector, text });
  });

program
  .command('select <selector> <value>')
  .description('Select an option in a dropdown')
  .action(async (selector: string, value: string) => {
    const global = getGlobalOptions(program.opts());
    await dispatchCommand(global, 'select', { selector, value });
  });

program
  .command('scroll <x> <y>')
  .description('Scroll page by x,y pixels')
  .action(async (x: string, y: string) => {
    const global = getGlobalOptions(program.opts());
    await dispatchCommand(global, 'scroll', {
      selector: 'page',
      x: Number(x),
      y: Number(y),
    });
  });

program
  .command('hover <selector>')
  .description('Hover over an element')
  .action(async (selector: string) => {
    const global = getGlobalOptions(program.opts());
    await dispatchCommand(global, 'hover', { selector });
  });

// Data extraction
program
  .command('gettext <selector>')
  .description('Get text content of an element')
  .action(async (selector: string) => {
    const global = getGlobalOptions(program.opts());
    await dispatchCommand(global, 'gettext', { selector });
  });

program
  .command('gethtml <selector>')
  .description('Get inner HTML of an element')
  .action(async (selector: string) => {
    const global = getGlobalOptions(program.opts());
    await dispatchCommand(global, 'gethtml', { selector });
  });

program
  .command('screenshot')
  .description('Take a screenshot')
  .action(async () => {
    const global = getGlobalOptions(program.opts());
    await dispatchCommand(global, 'screenshot', {});
  });

program
  .command('pageinfo')
  .description('Get current page info')
  .action(async () => {
    const global = getGlobalOptions(program.opts());
    await dispatchCommand(global, 'pageinfo');
  });

// Wait / utility
program
  .command('wait:element <selector>')
  .description('Wait for an element to appear')
  .option('--timeout <ms>', 'Timeout in ms', '10000')
  .action(async (selector: string, opts: Record<string, unknown>) => {
    const global = getGlobalOptions(program.opts());
    await dispatchCommand(global, 'wait:element', {
      selector,
      timeout: Number(opts.timeout || 10000),
    });
  });

program
  .command('wait:navigation')
  .description('Wait for page navigation to complete')
  .option('--timeout <ms>', 'Timeout in ms', '10000')
  .action(async (opts: Record<string, unknown>) => {
    const global = getGlobalOptions(program.opts());
    await dispatchCommand(global, 'wait:navigation', {
      timeout: Number(opts.timeout || 10000),
    });
  });

program
  .command('browser:list')
  .description('List connected browser instances')
  .action(async () => {
    const global = getGlobalOptions(program.opts());
    try {
      const browsers = await listBrowsers(global.server);
      if (global.json) {
        output(global, browsers);
        return;
      }
      if (browsers.length === 0) {
        console.log('No connected browsers.');
        return;
      }
      console.log('Connected browsers:');
      for (const browser of browsers) {
        const lastSeen = new Date(browser.lastSeen).toLocaleString();
        console.log(
          `  - ${browser.browserId} (status: ${browser.status}, lastSeen: ${lastSeen})`,
        );
      }
    } catch (err) {
      outputError(global, 'list_failed', String(err));
    }
  });

// Reserved for future distributed-mode support. Not yet implemented.
program
  .command('bridge-host')
  .description(
    'Configure CLI to point at a remote Browser Bridge server (not yet implemented)',
  )
  .action(() => {
    console.error(
      'bridge-host: not yet implemented. See docs/superpowers/specs/2026-06-15-distribution-design.md',
    );
    process.exit(1);
  });

program.parse();
