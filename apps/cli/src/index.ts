#!/usr/bin/env bun
import { WEBSOCKET_PORT } from '@my/shared';
import { Command } from 'commander';
import { createClient } from '@browser-bridge/websocket/client';
import type {
  CommandPayload,
  ResponsePayload,
  BrowserConnection,
} from '@my/shared/types';

const program = new Command();
program.name('mycli').description('Browser Bridge CLI').version('1.0.0');

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

async function listBrowsers(server: string): Promise<BrowserConnection[]> {
  const client = createClient({ url: server });

  await new Promise<void>((resolve, reject) => {
    const check = setInterval(() => {
      if (client.readyState === WebSocket.OPEN) {
        clearInterval(check);
        resolve();
      }
    }, 50);
    setTimeout(() => {
      clearInterval(check);
      reject(new Error('Connection timeout'));
    }, 5000);
  });

  try {
    const response = await client.request(
      'event',
      { event: 'list_browsers' },
      { timeout: 10000 },
    );
    const payload = response.payload as ResponsePayload;
    if (payload.status === 'error') {
      throw new Error(payload.message ?? payload.error ?? 'unknown');
    }
    return (payload.data as BrowserConnection[]) ?? [];
  } finally {
    client.close();
  }
}

async function sendCommand(
  global: GlobalOptions,
  command: CommandPayload['command'],
  params: Record<string, unknown> = {},
): Promise<void> {
  if (!global.browser) {
    outputError(global, 'missing_browser', 'Required: --browser <id>');
    return;
  }

  const client = createClient({ url: global.server });

  await new Promise<void>((resolve, reject) => {
    const check = setInterval(() => {
      if (client.readyState === WebSocket.OPEN) {
        clearInterval(check);
        resolve();
      }
    }, 50);
    setTimeout(() => {
      clearInterval(check);
      reject(new Error('Connection timeout'));
    }, 5000);
  });

  try {
    const response = await client.sendCommand(
      global.browser,
      { command, params },
      { timeout: global.timeout },
    );
    const payload = response.payload as ResponsePayload;

    if (payload.status === 'error') {
      outputError(
        global,
        payload.error ?? 'unknown',
        payload.message ?? 'Unknown error',
      );
      return;
    }

    output(global, payload.data ?? { status: 'ok' });
  } catch (err) {
    outputError(global, 'command_failed', String(err));
  } finally {
    client.close();
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
    await sendCommand(global, 'navigate', { url });
  });

program
  .command('goBack')
  .description('Go back in browser history')
  .action(async () => {
    const global = getGlobalOptions(program.opts());
    await sendCommand(global, 'goBack');
  });

program
  .command('goForward')
  .description('Go forward in browser history')
  .action(async () => {
    const global = getGlobalOptions(program.opts());
    await sendCommand(global, 'goForward');
  });

program
  .command('refresh')
  .description('Refresh current page')
  .action(async () => {
    const global = getGlobalOptions(program.opts());
    await sendCommand(global, 'refresh');
  });

// Tab management
program
  .command('tab:list')
  .description('List all open tabs')
  .action(async () => {
    const global = getGlobalOptions(program.opts());
    await sendCommand(global, 'tab:list');
  });

program
  .command('tab:new [url]')
  .description('Open a new tab')
  .action(async (url: string | undefined) => {
    const global = getGlobalOptions(program.opts());
    await sendCommand(global, 'tab:new', { url });
  });

program
  .command('tab:close <tabId>')
  .description('Close a tab by ID')
  .action(async (tabId: string) => {
    const global = getGlobalOptions(program.opts());
    await sendCommand(global, 'tab:close', { tabId: Number(tabId) });
  });

program
  .command('tab:switch <tabId>')
  .description('Switch to a tab by ID')
  .action(async (tabId: string) => {
    const global = getGlobalOptions(program.opts());
    await sendCommand(global, 'tab:switch', { tabId: Number(tabId) });
  });

// DOM interaction
program
  .command('click <selector>')
  .description('Click an element')
  .action(async (selector: string) => {
    const global = getGlobalOptions(program.opts());
    await sendCommand(global, 'click', { selector });
  });

program
  .command('type <selector> <text>')
  .description('Type text into an element')
  .action(async (selector: string, text: string) => {
    const global = getGlobalOptions(program.opts());
    await sendCommand(global, 'type', { selector, text });
  });

program
  .command('select <selector> <value>')
  .description('Select an option in a dropdown')
  .action(async (selector: string, value: string) => {
    const global = getGlobalOptions(program.opts());
    await sendCommand(global, 'select', { selector, value });
  });

program
  .command('scroll <x> <y>')
  .description('Scroll page by x,y pixels')
  .action(async (x: string, y: string) => {
    const global = getGlobalOptions(program.opts());
    await sendCommand(global, 'scroll', {
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
    await sendCommand(global, 'hover', { selector });
  });

// Data extraction
program
  .command('gettext <selector>')
  .description('Get text content of an element')
  .action(async (selector: string) => {
    const global = getGlobalOptions(program.opts());
    await sendCommand(global, 'gettext', { selector });
  });

program
  .command('gethtml <selector>')
  .description('Get inner HTML of an element')
  .action(async (selector: string) => {
    const global = getGlobalOptions(program.opts());
    await sendCommand(global, 'gethtml', { selector });
  });

program
  .command('screenshot')
  .description('Take a screenshot')
  .action(async () => {
    const global = getGlobalOptions(program.opts());
    await sendCommand(global, 'screenshot', {});
  });

program
  .command('pageinfo')
  .description('Get current page info')
  .action(async () => {
    const global = getGlobalOptions(program.opts());
    await sendCommand(global, 'pageinfo');
  });

// Wait / utility
program
  .command('wait:element <selector>')
  .description('Wait for an element to appear')
  .option('--timeout <ms>', 'Timeout in ms', '10000')
  .action(async (selector: string, opts: Record<string, unknown>) => {
    const global = getGlobalOptions(program.opts());
    await sendCommand(global, 'wait:element', {
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
    await sendCommand(global, 'wait:navigation', {
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

program.parse();
