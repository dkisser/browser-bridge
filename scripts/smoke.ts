#!/usr/bin/env bun
/**
 * Verify a local install (scripts/local-install.ts) actually works.
 *
 * install/README.md documents a *manual* smoke test — a shell block the human
 * reads and types. This is the same sequence, scripted, so a broken local
 * build fails loudly instead of being discovered when a browser command
 * silently finds no extension.
 *
 * Checks are grouped by what breaks them: the on-disk layout (bin/extension/
 * symlinks), then the running service, then the extension handshake, which is
 * the only check that needs Chrome to be open with the extension loaded.
 */
import { $ } from 'bun';

import { runningStatusLine } from './service-status';

const HOME_DIR = process.env.HOME ?? '';
const BB_HOME = process.env.BB_HOME ?? `${HOME_DIR}/.browser-bridge`;
const BB_EXTENSION_DIR =
  process.env.BB_EXTENSION_DIR ?? `${HOME_DIR}/Browser-Bridge`;

type Verdict = 'pass' | 'fail' | 'warn';

interface Check {
  readonly name: string;
  readonly verdict: Verdict;
  readonly detail: string;
}

/** A failed check is the only thing that should set a non-zero exit code. */
const isFailure = (c: Check) => c.verdict === 'fail';

const check = async (
  name: string,
  fn: () => Promise<Omit<Check, 'name'>>,
): Promise<Check> => {
  // Each callback describes only its verdict + detail; the label is owned
  // here so a check can never render blank.
  try {
    return { name, ...(await fn()) };
  } catch (err: unknown) {
    return {
      name,
      verdict: 'fail',
      detail: err instanceof Error ? err.message : String(err),
    };
  }
};

/**
 * `test <flag> <path>` as a boolean.
 *
 * `.nothrow()` is essential: a failing `test` is the normal answer here (that
 * is how a missing file or a non-symlink is detected), and without it the
 * throw escapes as "Failed with exit code 1" instead of the diagnostic the
 * check meant to report.
 */
const test = async (
  flag: '-e' | '-L' | '-x',
  path: string,
): Promise<boolean> => {
  const proc = Bun.spawn(['test', flag, path], {
    stdout: 'ignore',
    stderr: 'ignore',
  });
  return (await proc.exited) === 0;
};

const exists = async (path: string): Promise<boolean> => test('-e', path);

/**
 * Run a `bridge` subcommand from the installed binary (not PATH — the point
 * is to test what was installed, not whatever `bridge` resolves to).
 */
const bridgeRun = async (
  args: string[],
): Promise<{ code: number; out: string }> => {
  const proc = Bun.spawn([`${BB_HOME}/bin/bridge`, ...args], {
    stdout: 'pipe',
    stderr: 'pipe',
  });
  const [out, code] = await Promise.all([
    new Response(proc.stdout).text(),
    proc.exited,
  ]);
  return { code, out };
};

/**
 * True when `browser:list` succeeded but reported nothing paired.
 *
 * The CLI prints prose ("No connected browsers."), not a stable JSON shape,
 * so this matches an empty JSON collection *or* a "no <noun>" phrasing rather
 * than pinning one exact string that a wording change would break.
 */
const isUnpaired = (out: string): boolean => {
  const trimmed = out.trim();
  const emptyJson =
    /^(\[\s*\]|\{\s*(\}|"(\w+)"\s*:\s*(\[\s*\]|\{\s*\}))\s*)$/.test(trimmed) ||
    /^null$/.test(trimmed);
  return (
    emptyJson ||
    /\bno\s+(connected|available|paired|browsers?)\b/i.test(trimmed)
  );
};

const checks: Check[] = [
  await check('binary installed and executable', async () => {
    const path = `${BB_HOME}/bin/bridge`;
    if (!(await exists(path))) {
      return { verdict: 'fail', detail: `missing ${path}` };
    }
    const runnable = await test('-x', path);
    return {
      verdict: runnable ? 'pass' : 'fail',
      detail: runnable ? path : `${path} is not executable`,
    };
  }),

  await check('~/.local/bin/bridge links into BB_HOME', async () => {
    const link = `${HOME_DIR}/.local/bin/bridge`;
    if (!(await test('-L', link))) {
      return { verdict: 'fail', detail: `${link} is not a symlink` };
    }
    const target = (await $`readlink ${link}`.quiet()).stdout.toString().trim();
    // A dangling link is the failure mode this whole script exists to catch:
    // `bridge` reports "command not found" even though PATH is correct.
    const resolves = await exists(target);
    return {
      verdict: resolves ? 'pass' : 'fail',
      detail: resolves
        ? `${link} -> ${target}`
        : `dangling: ${link} -> ${target} (target does not exist)`,
    };
  }),

  await check('extension/manifest.json at top level', async () => {
    const manifest = `${BB_HOME}/extension/manifest.json`;
    const present = await exists(manifest);
    return {
      verdict: present ? 'pass' : 'fail',
      detail: present
        ? manifest
        : `${manifest} missing — Chrome cannot load this`,
    };
  }),

  await check('~/Browser-Bridge/extension links into BB_HOME', async () => {
    const link = `${BB_EXTENSION_DIR}/extension`;
    if (!(await test('-L', link))) {
      return { verdict: 'fail', detail: `${link} is not a symlink` };
    }
    const target = (await $`readlink ${link}`.quiet()).stdout.toString().trim();
    const resolves = await exists(target);
    return {
      verdict: resolves ? 'pass' : 'fail',
      detail: resolves ? link : `dangling: ${link} -> ${target}`,
    };
  }),

  await check('version file records the local build', async () => {
    const file = `${BB_HOME}/version`;
    if (!(await exists(file))) {
      return { verdict: 'warn', detail: `${file} missing` };
    }
    const version = (await Bun.file(file).text()).trim();
    const binary = (await bridgeRun(['--version'])).out.trim();
    // `bridge --version` is the *linked-in* build version (ldflags), which can
    // legitimately differ from the stamp file, so only presence is asserted.
    return {
      verdict: 'pass',
      detail: `version file: ${version}; binary reports: ${binary}`,
    };
  }),

  await check('bridge service status', async () => {
    const { code, out } = await bridgeRun(['service', 'status']);
    // The same rule local-install.ts uses, for the same reason: `service
    // status` prints the launchd state on a second line, and the not-loaded
    // variant reads `auto-start:  enabled (starts at login; not running now)`.
    // A grep for `running` matches that line, so this check reported a stopped
    // daemon as healthy — and the smoke test exists to be the thing that says
    // a broken install looks fine.
    const line = runningStatusLine(code, out);
    return {
      verdict: line ? 'pass' : 'fail',
      detail: line ?? `not running:\n${out.trim()}`,
    };
  }),

  await check('bridge service doctor', async () => {
    const { code, out } = await bridgeRun(['service', 'doctor']);
    return {
      verdict: code === 0 ? 'pass' : 'fail',
      detail: out.trim(),
    };
  }),

  await check('extension handshake (browser:list)', async () => {
    const { code, out } = await bridgeRun(['browser:list']);
    if (code !== 0) {
      return { verdict: 'fail', detail: out.trim() };
    }
    if (isUnpaired(out)) {
      // The service is healthy but nothing paired: this is a Chrome-side
      // problem, not a build problem, so it warns rather than fails.
      return {
        verdict: 'warn',
        detail:
          'no browsers paired — open Chrome and load the unpacked extension ' +
          `from ${BB_EXTENSION_DIR}/extension/`,
      };
    }
    return { verdict: 'pass', detail: out.trim() };
  }),
];

console.log('\nSmoke test — local install\n');
for (const c of checks) {
  const icon = { pass: '✓', fail: '✗', warn: '!' }[c.verdict];
  console.log(`  ${icon} ${c.name}\n      ${c.detail}`);
}

const failures = checks.filter(isFailure);
const warnings = checks.filter((c) => c.verdict === 'warn');

console.log(
  `\n${checks.length - failures.length - warnings.length} passed, ` +
    `${warnings.length} warning(s), ${failures.length} failed\n`,
);

if (failures.length > 0) {
  process.exit(1);
}
