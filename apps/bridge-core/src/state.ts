import {
  chmodSync,
  existsSync,
  mkdirSync,
  readFileSync,
  writeFileSync,
} from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';
import type { BrowserStatus } from '@browser-bridge/shared/types';

const DEFAULT_CONFIG_DIR = join(homedir(), '.browser-bridge');
const BUFFER_TIMEOUT_MS = 5000;

/**
 * Resolve the state directory the same way install.sh and bridge.sh do, so a
 * custom install prefix keeps binaries and state together. Resolved per call
 * rather than frozen at import time: the installer and the test suite both set
 * BB_HOME for a process that may already have loaded this module.
 */
function configDir(): string {
  return process.env.BB_HOME || DEFAULT_CONFIG_DIR;
}

function configFile(): string {
  return join(configDir(), 'config.json');
}

interface BridgeConfig {
  browserId: string;
  extensionTokenHash?: string;
}

interface BufferedCommand {
  envelope: string;
  receivedAt: number;
  timer: ReturnType<typeof setTimeout>;
}

export class StateManager {
  private config: BridgeConfig;
  private browserStatus: BrowserStatus = 'offline';
  private bufferedCommand: BufferedCommand | null = null;

  constructor() {
    this.config = this.loadConfig();
  }

  get browserId(): string {
    return this.config.browserId;
  }

  get extensionTokenHash(): string | undefined {
    return this.config.extensionTokenHash;
  }

  setExtensionTokenHash(hash: string): void {
    this.config.extensionTokenHash = hash;
    this.saveConfig();
  }

  clearExtensionTokenHash(): void {
    this.config.extensionTokenHash = undefined;
    this.saveConfig();
  }

  get status(): BrowserStatus {
    return this.browserStatus;
  }

  set status(status: BrowserStatus) {
    this.browserStatus = status;
    if (status !== 'idle_wait' && this.bufferedCommand) {
      clearTimeout(this.bufferedCommand.timer);
      this.bufferedCommand = null;
    }
  }

  canAcceptCommand(): boolean {
    return (
      this.browserStatus === 'online' || this.browserStatus === 'idle_wait'
    );
  }

  bufferCommand(envelope: string, onTimeout: () => void): boolean {
    if (this.browserStatus !== 'idle_wait') return false;
    if (this.bufferedCommand) return false;
    this.bufferedCommand = {
      envelope,
      receivedAt: Date.now(),
      timer: setTimeout(() => {
        this.bufferedCommand = null;
        onTimeout();
      }, BUFFER_TIMEOUT_MS),
    };
    return true;
  }

  getBufferedCommand(): string | null {
    if (!this.bufferedCommand) return null;
    const cmd = this.bufferedCommand.envelope;
    clearTimeout(this.bufferedCommand.timer);
    this.bufferedCommand = null;
    return cmd;
  }

  private loadConfig(): BridgeConfig {
    const file = configFile();
    try {
      if (existsSync(file)) {
        const data = readFileSync(file, 'utf-8');
        // Tighten permissions on config files written by older versions:
        // the file holds the extension token hash.
        chmodSync(file, 0o600);
        // Destructure only known fields so pre-cleanup config files
        // (which may still carry apiToken / serverUrl from the pre-merge
        // ws-server + local-proxy design) do not silently re-serialize
        // those ghost fields on every saveConfig().
        const { browserId, extensionTokenHash } = JSON.parse(data) as {
          browserId?: unknown;
          extensionTokenHash?: unknown;
        };
        if (typeof browserId !== 'string') throw new Error('missing browserId');
        return {
          browserId,
          ...(typeof extensionTokenHash === 'string'
            ? { extensionTokenHash }
            : {}),
        };
      }
    } catch {
      // fall through to defaults
    }
    const config: BridgeConfig = {
      browserId: `b-${crypto.randomUUID().slice(0, 8)}`,
    };
    this.saveConfigSync(config);
    return config;
  }

  private saveConfig(): void {
    this.saveConfigSync(this.config);
  }

  private saveConfigSync(config: BridgeConfig): void {
    mkdirSync(configDir(), { recursive: true, mode: 0o700 });
    const file = configFile();
    writeFileSync(file, JSON.stringify(config, null, 2), {
      mode: 0o600,
    });
    chmodSync(file, 0o600);
  }
}
