import type { BrowserStatus } from '@my/shared/types';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';

const CONFIG_DIR = join(homedir(), '.browser-bridge');
const CONFIG_FILE = join(CONFIG_DIR, 'config.json');
const BUFFER_TIMEOUT_MS = 5000;

interface ProxyConfig {
  browserId: string;
  serverUrl: string;
  apiToken?: string;
}

interface BufferedCommand {
  envelope: string;
  receivedAt: number;
  timer: ReturnType<typeof setTimeout>;
}

export class StateManager {
  private config: ProxyConfig;
  private browserStatus: BrowserStatus = 'offline';
  private bufferedCommand: BufferedCommand | null = null;

  constructor() {
    this.config = this.loadConfig();
  }

  get browserId(): string {
    return this.config.browserId;
  }

  get serverUrl(): string {
    return this.config.serverUrl;
  }

  get apiToken(): string | undefined {
    return this.config.apiToken;
  }

  get status(): BrowserStatus {
    return this.browserStatus;
  }

  set status(status: BrowserStatus) {
    console.log(`Browser status: ${this.browserStatus} → ${status}`);
    this.browserStatus = status;
    if (status !== 'idle_wait' && this.bufferedCommand) {
      clearTimeout(this.bufferedCommand.timer);
      this.bufferedCommand = null;
    }
  }

  canAcceptCommand(): boolean {
    return this.browserStatus === 'online' || this.browserStatus === 'idle_wait';
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

  setApiToken(token: string): void {
    this.config.apiToken = token;
    this.saveConfig();
  }

  setServerUrl(url: string): void {
    this.config.serverUrl = url;
    this.saveConfig();
  }

  private loadConfig(): ProxyConfig {
    try {
      if (existsSync(CONFIG_FILE)) {
        const data = readFileSync(CONFIG_FILE, 'utf-8');
        return JSON.parse(data) as ProxyConfig;
      }
    } catch {
      // fall through to defaults
    }
    const config: ProxyConfig = {
      browserId: `b-${crypto.randomUUID().slice(0, 8)}`,
      serverUrl: 'ws://localhost:3001',
    };
    this.saveConfigSync(config);
    return config;
  }

  private saveConfig(): void {
    this.saveConfigSync(this.config);
  }

  private saveConfigSync(config: ProxyConfig): void {
    mkdirSync(CONFIG_DIR, { recursive: true });
    writeFileSync(CONFIG_FILE, JSON.stringify(config, null, 2));
  }
}
