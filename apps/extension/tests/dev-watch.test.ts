import { afterEach, describe, expect, it } from 'bun:test';
import { spawn } from 'node:child_process';
import { existsSync } from 'node:fs';
import { join } from 'node:path';

const REPO_ROOT = join(import.meta.dir, '..', '..', '..');
const SCRIPT = join(REPO_ROOT, 'apps/extension/scripts/dev-watch.sh');

/**
 * Round 4 CR finding: `wait -n` (bash 4.3+) silently breaks on stock macOS,
 * where /usr/bin/env bash resolves to /bin/bash 3.2.57. The reviewer traced
 * it to the dev-watch.sh `wait -n` line and showed the script dies with
 * `wait: -n: invalid option` before either watcher even runs.
 *
 * These tests exercise the bash 3.2 path explicitly:
 *   1. The script must not emit `wait: -n: invalid option` (or any variant
 *      of "invalid option" from `wait`).
 *   2. The script must exit promptly with the watcher's status, not stall
 *      or wait for both watchers to exit.
 *
 * We strip PATH so `vite` is not found; both watchers exit immediately with
 * status 127, and the script must propagate that status. With the pre-fix
 * `wait -n` line, the script would fail with the invalid-option error first.
 */
describe('apps/extension/scripts/dev-watch.sh (bash 3.2 portability)', () => {
  // Cleanup any vite processes we may have leaked across tests.
  afterEach(() => {
    spawn('pkill', ['-f', 'vite build'], { stdio: 'ignore' });
  });

  function runUnderBash32(): Promise<{
    stdout: string;
    stderr: string;
    code: number | null;
  }> {
    return new Promise((resolve) => {
      // Restrict PATH so neither `vite` invocation finds a binary: both fail
      // immediately with exit 127, giving the script something to react to.
      const proc = spawn('/bin/bash', [SCRIPT], {
        env: { PATH: '/usr/bin:/bin' },
        stdio: ['ignore', 'pipe', 'pipe'],
      });
      let stdout = '';
      let stderr = '';
      proc.stdout.on('data', (chunk) => (stdout += chunk.toString()));
      proc.stderr.on('data', (chunk) => (stderr += chunk.toString()));
      proc.on('close', (code) => resolve({ stdout, stderr, code }));
      // Safety net: the script's own trap should clean up; this is a fallback
      // for a hung test. 3s is well past the 0.2s poll interval plus startup.
      setTimeout(() => {
        if (!proc.killed) proc.kill('SIGKILL');
      }, 3000);
    });
  }

  it('runs under bash 3.2 without `wait -n: invalid option`', async () => {
    const { stdout, stderr, code } = await runUnderBash32();
    const combined = stdout + stderr;
    // The pre-fix script emitted this exact string on bash 3.2.
    expect(combined).not.toMatch(/wait:\s*-n:\s*invalid option/);
    // Belt and braces: no `invalid option` line at all from `wait`.
    expect(combined).not.toMatch(/wait.*invalid option/);
    // Both `vite` calls fail because PATH has no node_modules — script
    // should exit non-zero with the watcher's status, not 0 (which would
    // mean the script swallowed the failure).
    expect(code).not.toBe(0);
    // Sanity: the script's own "watcher exited" log line should appear.
    expect(combined).toMatch(/watcher exited/);
  });

  it('exits promptly (well under the poll interval)', async () => {
    const start = Date.now();
    const { code } = await runUnderBash32();
    const elapsedMs = Date.now() - start;
    expect(elapsedMs).toBeLessThan(2000);
    expect(code).not.toBe(0);
  });

  it('the script exists on disk (sanity check for the test setup)', () => {
    expect(existsSync(SCRIPT)).toBe(true);
  });
});
