import { afterEach, beforeEach, describe, expect, it } from 'bun:test';
import {
  existsSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { StateManager } from '../src/state';

describe('StateManager config location', () => {
  let dir: string;
  const originalHome = process.env.BB_HOME;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), 'bb-state-'));
    process.env.BB_HOME = dir;
  });

  afterEach(() => {
    rmSync(dir, { recursive: true, force: true });
    if (originalHome === undefined) delete process.env.BB_HOME;
    else process.env.BB_HOME = originalHome;
  });

  it('writes config.json under BB_HOME', () => {
    const state = new StateManager();

    expect(state.browserId).toMatch(/^b-[0-9a-f]{8}$/);
    expect(existsSync(join(dir, 'config.json'))).toBe(true);
  });

  it('reuses the browserId from an existing config under BB_HOME', () => {
    writeFileSync(
      join(dir, 'config.json'),
      JSON.stringify({ browserId: 'b-keepme' }),
    );

    expect(new StateManager().browserId).toBe('b-keepme');
  });

  it('does not re-serialize pre-merge ghost fields on save', () => {
    writeFileSync(
      join(dir, 'config.json'),
      JSON.stringify({
        browserId: 'b-ghosty',
        apiToken: 'ghost-token',
        serverUrl: 'ws://ghost',
      }),
    );

    const state = new StateManager();
    state.setExtensionTokenHash('hash');

    const saved = JSON.parse(
      readFileSync(join(dir, 'config.json'), 'utf-8'),
    ) as Record<string, unknown>;
    expect(saved.browserId).toBe('b-ghosty');
    expect(saved.extensionTokenHash).toBe('hash');
    expect(saved.apiToken).toBeUndefined();
    expect(saved.serverUrl).toBeUndefined();
  });
});
