import { LOCAL_WS_PORT, WEBSOCKET_PORT } from '@browser-bridge/shared';

// Read-only display entry for the settings tab. No `editable` / `onChange`
// fields exist — settings is inert; the UI never writes back.
export interface HardConfigEntry {
  readonly label: string;
  readonly value: string;
}

// The five entries shown in the settings tab. Paths follow the install
// layout in `install/bridge.sh.tmpl`: BB_HOME=~/.browser-bridge, the
// LaunchAgent label is com.browser-bridge.bridge, the CLI binary lives at
// BB_HOME/bin/bridge-cmd.
const BB_HOME = '~/.browser-bridge';
const LAUNCHAGENT_LABEL = 'com.browser-bridge.bridge';

export function formatHardConfig(): HardConfigEntry[] {
  return [
    {
      label: 'Local proxy URL',
      value: `http://127.0.0.1:${LOCAL_WS_PORT}`,
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
      label: 'LaunchAgent plist',
      value: `~/Library/LaunchAgents/${LAUNCHAGENT_LABEL}.plist`,
    },
    {
      label: 'CLI binary',
      value: `${BB_HOME}/bin/bridge-cmd`,
    },
  ];
}
