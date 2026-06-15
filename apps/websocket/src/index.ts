import { startServer } from './server';
import { ApiKeyAuthProvider } from '@my/shared/auth';

const apiKeys = process.env.BRIDGE_API_KEYS;

if (apiKeys) {
  const keys = apiKeys.split(',').map(k => k.trim()).filter(Boolean);
  startServer(undefined, new ApiKeyAuthProvider(keys));
} else {
  startServer();
}
