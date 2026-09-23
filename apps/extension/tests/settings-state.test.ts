import { describe, expect, it } from 'bun:test';
import { formatHardConfig, type HardConfigEntry } from '../src/settings-state';

describe('formatHardConfig', () => {
  it('returns the canonical five entries', () => {
    const entries = formatHardConfig();
    expect(entries).toHaveLength(5);
    const labels = entries.map((entry) => entry.label);
    expect(labels).toContain('Local proxy URL');
    expect(labels).toContain('WebSocket port');
    expect(labels).toContain('Log directory');
    expect(labels).toContain('LaunchAgent plist');
    expect(labels).toContain('CLI binary');
  });

  it('every entry has a non-empty label and value', () => {
    const entries: HardConfigEntry[] = formatHardConfig();
    for (const entry of entries) {
      expect(entry.label.length).toBeGreaterThan(0);
      expect(entry.value.length).toBeGreaterThan(0);
    }
  });

  it('local proxy URL binds to 127.0.0.1 and the LOCAL_WS_PORT', () => {
    const entries = formatHardConfig();
    const url = entries.find((entry) => entry.label === 'Local proxy URL');
    expect(url?.value).toContain('127.0.0.1');
    expect(url?.value).toContain('3002');
  });

  it('LaunchAgent plist path ends with .plist and lives under ~/Library/LaunchAgents', () => {
    const entries = formatHardConfig();
    const plist = entries.find((entry) => entry.label === 'LaunchAgent plist');
    expect(plist?.value).toMatch(/\.plist$/);
    expect(plist?.value).toContain('Library/LaunchAgents');
  });

  it('log directory lives under ~/.browser-bridge', () => {
    const entries = formatHardConfig();
    const log = entries.find((entry) => entry.label === 'Log directory');
    expect(log?.value).toContain('.browser-bridge');
    expect(log?.value).toContain('logs');
  });

  it('CLI binary lives under ~/.browser-bridge/bin', () => {
    const entries = formatHardConfig();
    const cli = entries.find((entry) => entry.label === 'CLI binary');
    expect(cli?.value).toContain('.browser-bridge');
    expect(cli?.value).toContain('bin');
  });

  it('every entry is read-only — entries have no editable or onChange fields', () => {
    const entries: HardConfigEntry[] = formatHardConfig();
    for (const entry of entries) {
      expect(entry).not.toHaveProperty('editable');
      expect(entry).not.toHaveProperty('onChange');
    }
  });
});
