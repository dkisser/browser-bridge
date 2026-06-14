#!/usr/bin/env bun
import { WEBSOCKET_PORT } from '@my/shared';
import { Command } from 'commander';
import { createClient } from '@browser-bridge/websocket/client';

const program = new Command();

program.name('mycli').description('Browser Bridge CLI').version('1.0.0');

program
  .command('check-ws')
  .description('Print WebSocket server address and port')
  .action(() => {
    console.log(`WebSocket server: ws://localhost:${WEBSOCKET_PORT}`);
  });

program
  .command('send <message...>')
  .description('Send a message to the WebSocket server and print the echo')
  .action(async (messages: string[]) => {
    const message = messages.join(' ');
    const client = createClient({
      onMessage(envelope) {
        console.log(`[${envelope.type}]`, envelope.data);
        client.close();
      },
      onError(error) {
        console.error('WebSocket error:', error);
        process.exit(1);
      },
      onClose() {
        process.exit(0);
      },
    });

    client.send('message', message);
  });

program.parse();
