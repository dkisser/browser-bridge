#!/usr/bin/env bun
import { CloudClient } from './cloud-client';
import {
  DEFAULT_LOCAL_HOSTNAME,
  DEFAULT_LOCAL_PORT,
  DEFAULT_SERVER_URL,
} from './config';
import { LocalServer } from './local-server';
import { Router } from './router';
import { StateManager } from './state';

function isLocalhostUrl(url: string): boolean {
  try {
    const parsed = new URL(url);
    return (
      parsed.hostname === 'localhost' ||
      parsed.hostname === '127.0.0.1' ||
      parsed.hostname === '::1'
    );
  } catch {
    return false;
  }
}

async function main() {
  const serverUrl =
    process.env.BRIDGE_WS_URL ||
    process.env.BRIDGE_SERVER_URL ||
    DEFAULT_SERVER_URL;
  const localPort = Number(process.env.BRIDGE_LOCAL_PORT) || DEFAULT_LOCAL_PORT;
  const localHostname =
    process.env.BRIDGE_LOCAL_HOSTNAME || DEFAULT_LOCAL_HOSTNAME;
  const apiToken = process.env.BRIDGE_API_TOKEN;

  if (!apiToken && !isLocalhostUrl(serverUrl)) {
    console.error(
      'BRIDGE_API_TOKEN is required when connecting to a remote server',
    );
    process.exit(1);
  }

  if (!apiToken && isLocalhostUrl(serverUrl)) {
    console.warn(
      'Warning: BRIDGE_API_TOKEN not set — running without authentication (local development only)',
    );
  }

  const state = new StateManager();
  console.log(`Browser ID: ${state.browserId}`);
  console.log(`Cloud server: ${serverUrl}`);

  let router: Router;

  const local = new LocalServer(
    localPort,
    {
      onCommand: (envelope) => {
        if (envelope.type === 'response') {
          router.handleExtensionResponse(envelope);
        }
      },
      onConnect: () => router.handleExtensionConnect(),
      onDisconnect: () => router.handleExtensionDisconnect(),
      cloud: {
        isConnected: () => cloud.isConnected,
        isManualDisconnect: () => cloud.isManualDisconnect,
        connect: () => cloud.connect(),
        disconnect: () => cloud.close(),
        browserId: state.browserId,
        serverUrl,
      },
    },
    localHostname,
  );

  const cloud = new CloudClient({
    serverUrl,
    apiToken: apiToken || '',
    browserId: state.browserId,
    onCommand: (envelope) => router.handleCloudCommand(envelope),
  });

  router = new Router(state, cloud, local);

  local.start();

  try {
    await cloud.connect();
    console.log('Connected to cloud server');
  } catch (err) {
    console.error('Failed to connect to cloud server:', err);
    console.log('Will retry automatically...');
  }
}

main();
