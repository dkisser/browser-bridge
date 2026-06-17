import { describe, expect, it, beforeAll, afterAll } from 'bun:test';
import { spawnSync } from 'node:child_process';
import { mkdirSync, writeFileSync, readFileSync, existsSync, rmSync } from 'node:fs';
import { join } from 'node:path';

const TMP = `/tmp/bb-zip-test-${Date.now()}`;
const SCRIPT = join(import.meta.dir, '..', '..', '.github', 'scripts', 'build-extension-zip.sh');

beforeAll(() => {
  mkdirSync(`${TMP}/apps/extension/dist`, { recursive: true });
  writeFileSync(`${TMP}/apps/extension/dist/manifest.json`, '{"manifest_version":3}');
  writeFileSync(`${TMP}/apps/extension/dist/background.js`, '// sw');
});

afterAll(() => rmSync(TMP, { recursive: true, force: true }));

describe('build-extension-zip.sh', () => {
  it('produces zip with expected internal structure and matching sha256', () => {
    const result = spawnSync('bash', [SCRIPT], {
      cwd: TMP,
      env: { ...process.env, VERSION: 'v1.2.3' },
      encoding: 'utf8',
    });
    expect(result.status).toBe(0);

    const zipPath = `${TMP}/browser-bridge-extension-v1.2.3.zip`;
    const shaPath = `${zipPath}.sha256`;
    expect(existsSync(zipPath)).toBe(true);
    expect(existsSync(shaPath)).toBe(true);

    const listed = spawnSync('unzip', ['-l', zipPath], { encoding: 'utf8' });
    expect(listed.stdout).toContain('manifest.json');
    expect(listed.stdout).toContain('background.js');

    const expectedSha = readFileSync(shaPath, 'utf8').split(/\s+/)[0];
    const actual = spawnSync('shasum', ['-a', '256', zipPath], { encoding: 'utf8' });
    expect(actual.stdout.split(/\s+/)[0]).toBe(expectedSha);
  });
});
