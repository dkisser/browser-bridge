import { WEBSOCKET_PORT } from '@browser-bridge/shared';
import { ApiKeyAuthProvider } from '@browser-bridge/shared/auth';
import pkg from '../package.json';
import { startMcpServer } from './mcp';
import { startServer } from './server';

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

try {
  startServer(port, authProvider, hostname);
  await startMcpServer({
    websocketUrl: `ws://127.0.0.1:${port ?? WEBSOCKET_PORT}`,
    port: mcpPort,
    hostname: mcpHostname,
    defaultTimeoutMs: mcpTimeout,
    version: pkg.version,
  });
} catch (error) {
  console.error('Failed to start servers:', error);
  process.exit(1);
}
