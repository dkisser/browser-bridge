import { afterAll, beforeAll, describe, expect, it } from 'bun:test';
import { spawnSync } from 'node:child_process';
import {
  existsSync,
  mkdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from 'node:fs';
import { join } from 'node:path';

const TMP = `/tmp/bb-zip-test-${Date.now()}`;
const SCRIPT = join(
  import.meta.dir,
  '..',
  '..',
  '.github',
  'scripts',
  'build-extension-zip.sh',
);

beforeAll(() => {
  mkdirSync(`${TMP}/apps/extension/dist`, { recursive: true });
  writeFileSync(
    `${TMP}/apps/extension/dist/manifest.json`,
    '{"manifest_version":3}',
  );
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

    const listed = spawnSync('unzip', ['-Z1', zipPath], { encoding: 'utf8' });
    const entries = listed.stdout.split('\n').map((line) => line.trim());
    // Flat zip: files sit at the root so Chrome "Load unpacked" and install.sh
    // find manifest.json directly under ~/.browser-bridge/extension/.
    expect(entries).toContain('manifest.json');
    expect(entries).toContain('background.js');
    expect(entries).not.toContain('browser-bridge-extension-v1.2.3/');

    const expectedSha = readFileSync(shaPath, 'utf8').split(/\s+/)[0];
    const actual = spawnSync('shasum', ['-a', '256', zipPath], {
      encoding: 'utf8',
    });
    expect(actual.stdout.split(/\s+/)[0]).toBe(expectedSha);
  });
});
