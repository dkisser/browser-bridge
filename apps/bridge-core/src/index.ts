#!/usr/bin/env bun
import { LOCAL_WS_PORT, WEBSOCKET_PORT } from '@browser-bridge/shared';
import { ApiKeyAuthProvider } from '@browser-bridge/shared/auth';
import pkg from '../package.json';
import { BrowserServer } from './browser-server';
import { startMcpServer } from './mcp';
import { PairingManager } from './pairing';
import { Router } from './router';
import { InboundServer } from './server/inbound';
import { ConnectionRegistry } from './server/registry';
import { StateManager } from './state';

const apiKeys = process.env.BRIDGE_API_KEYS;
const port = process.env.BRIDGE_WS_PORT
  ? Number(process.env.BRIDGE_WS_PORT)
  : undefined;
const hostname = process.env.BRIDGE_WS_HOSTNAME;

const authProvider = apiKeys
  ? new ApiKeyAuthProvider(
      apiKeys
        .split(',')
        .map((k) => k.trim())
        .filter(Boolean),
    )
  : undefined;

const localPort = process.env.BRIDGE_LOCAL_PORT
  ? Number(process.env.BRIDGE_LOCAL_PORT)
  : LOCAL_WS_PORT;
if (Number.isNaN(localPort)) {
  throw new Error(`Invalid BRIDGE_LOCAL_PORT: ${process.env.BRIDGE_LOCAL_PORT}`);
}
const localHostname = process.env.BRIDGE_LOCAL_HOSTNAME ?? '127.0.0.1';

const mcpPort = process.env.BRIDGE_MCP_PORT
  ? Number(process.env.BRIDGE_MCP_PORT)
  : 3003;
if (Number.isNaN(mcpPort)) {
  throw new Error(`Invalid BRIDGE_MCP_PORT: ${process.env.BRIDGE_MCP_PORT}`);
}

const mcpHostname = process.env.BRIDGE_MCP_HOSTNAME ?? '127.0.0.1';

const mcpTimeout = process.env.BRIDGE_MCP_TIMEOUT_MS
  ? Number(process.env.BRIDGE_MCP_TIMEOUT_MS)
  : 10000;
if (Number.isNaN(mcpTimeout)) {
  throw new Error(
    `Invalid BRIDGE_MCP_TIMEOUT_MS: ${process.env.BRIDGE_MCP_TIMEOUT_MS}`,
  );
}

async function main() {
  const state = new StateManager();
  console.log(`Browser ID: ${state.browserId}`);

  const registry = new ConnectionRegistry();

  const pairing = new PairingManager(
    () => state.extensionTokenHash,
    (hash) => state.setExtensionTokenHash(hash),
  );

  // BrowserServer needs a Router reference; we use a getter so the reference
  // is resolved after Router is constructed.
  let router!: Router;
  const browser = new BrowserServer({
    port: localPort,
    hostname: localHostname,
    getRouter: () => router,
    pairing,
  });

  router = new Router(state, browser, registry);

  // Start the BrowserServer (3002, accepts extension WS).
  browser.start();

  // Start the InboundServer (3001, accepts CLI/MCP).
  const inbound = new InboundServer({
    port: port ?? WEBSOCKET_PORT,
    hostname,
    authProvider,
    router,
    registry,
  });
  inbound.start();

  // Start MCP (3003, FastMCP). MCP talks to the InboundServer over
  // 127.0.0.1:3001 internally (same process, protocol-clean path).
  await startMcpServer({
    websocketUrl: `ws://127.0.0.1:${port ?? WEBSOCKET_PORT}`,
    port: mcpPort,
    hostname: mcpHostname,
    defaultTimeoutMs: mcpTimeout,
    version: pkg.version,
  });

  console.log(
    `bridge-core ready: inbound=${port ?? WEBSOCKET_PORT}, browser=${localPort}, mcp=${mcpPort}`,
  );
}

main().catch((error) => {
  console.error('Failed to start bridge-core:', error);
  process.exit(1);
});