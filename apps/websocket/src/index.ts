import { ApiKeyAuthProvider } from '@browser-bridge/shared/auth';
import { startServer } from './server';

const apiKeys = process.env.BRIDGE_API_KEYS;
const port = process.env.BRIDGE_WS_PORT
  ? Number(process.env.BRIDGE_WS_PORT)
  : undefined;

if (apiKeys) {
  const keys = apiKeys
    .split(',')
    .map((k) => k.trim())
    .filter(Boolean);
  startServer(port, new ApiKeyAuthProvider(keys));
} else {
  startServer(port);
}
