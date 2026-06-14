#!/usr/bin/env bun
import { DEFAULT_SERVER_URL, DEFAULT_LOCAL_PORT } from './config';
import { StateManager } from './state';
import { CloudClient } from './cloud-client';
import { LocalServer } from './local-server';
import { Router } from './router';

async function main() {
  const serverUrl = process.env.BRIDGE_SERVER_URL || DEFAULT_SERVER_URL;
  const localPort = Number(process.env.BRIDGE_LOCAL_PORT) || DEFAULT_LOCAL_PORT;
  const apiToken = process.env.BRIDGE_API_TOKEN || 'dev-token';

  const state = new StateManager();
  console.log(`Browser ID: ${state.browserId}`);
  console.log(`Cloud server: ${serverUrl}`);

  const local = new LocalServer(localPort, {
    onCommand: (envelope) => {
      if (envelope.type === 'response') {
        router.handleExtensionResponse(envelope);
      }
    },
    onConnect: () => router.handleExtensionConnect(),
    onDisconnect: () => router.handleExtensionDisconnect(),
  });

  const cloud = new CloudClient({
    serverUrl,
    apiToken,
    browserId: state.browserId,
    onCommand: (envelope) => router.handleCloudCommand(envelope),
  });

  const router = new Router(state, cloud, local);

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
