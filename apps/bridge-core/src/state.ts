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

const CONFIG_DIR = join(homedir(), '.browser-bridge');
const CONFIG_FILE = join(CONFIG_DIR, 'config.json');
const BUFFER_TIMEOUT_MS = 5000;

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
    try {
      if (existsSync(CONFIG_FILE)) {
        const data = readFileSync(CONFIG_FILE, 'utf-8');
        // Tighten permissions on config files written by older versions:
        // the file holds the extension token hash.
        chmodSync(CONFIG_FILE, 0o600);
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
    mkdirSync(CONFIG_DIR, { recursive: true, mode: 0o700 });
    writeFileSync(CONFIG_FILE, JSON.stringify(config, null, 2), {
      mode: 0o600,
    });
    chmodSync(CONFIG_FILE, 0o600);
  }
}
