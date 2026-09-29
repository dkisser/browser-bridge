import {
  LOCAL_HOST,
  LOCAL_WS_PORT,
  WEBSOCKET_PORT,
} from '@browser-bridge/shared';

// Read-only display entry for the settings tab. No `editable` / `onChange`
// fields exist — settings is inert; the UI never writes back.
export interface HardConfigEntry {
  readonly label: string;
  readonly value: string;
}

// The six entries shown in the settings tab. Paths follow the install
// layout managed by the Go `bridge` binary (apps/bridge-core/internal/cli):
// BB_HOME=~/.browser-bridge, the LaunchAgent label is com.browser-bridge.bridge,
// the CLI binary lives at BB_HOME/bin/bridge, and persistent data (pairing
// config today, audit trail and agent memory later) lives at BB_HOME/data
// (ADR-0017).
const BB_HOME = '~/.browser-bridge';
const LAUNCHAGENT_LABEL = 'com.browser-bridge.bridge';

export function formatHardConfig(): HardConfigEntry[] {
  return [
    {
      // Built from the same shared host and port the extension actually
      // dials (ui/bridge-api.ts). This used to be a separate `127.0.0.1`
      // literal while the extension used `localhost` — the page advertised a
      // URL the extension never opens, and a user copying it to debug was
      // debugging something else.
      label: 'Local proxy URL',
      value: `http://${LOCAL_HOST}:${LOCAL_WS_PORT}`,
    },
    {
      label: 'WebSocket port',
      value: String(WEBSOCKET_PORT),
    },
    {
      label: 'Log directory',
      value: `${BB_HOME}/logs`,
    },
    {
      label: 'Data directory',
      value: `${BB_HOME}/data`,
    },
    {
      label: 'LaunchAgent plist',
      value: `~/Library/LaunchAgents/${LAUNCHAGENT_LABEL}.plist`,
    },
    {
      label: 'CLI binary',
      value: `${BB_HOME}/bin/bridge`,
    },
  ];
}
