#!/usr/bin/env bun
/**
 * Build the extension + runtime from this working tree and install them into
 * $BB_HOME, replacing whatever a previous `install.sh` run left there.
 *
 * Why this exists: `install/install.sh` can only fetch *released* artifacts
 * from GitHub — `download_runtime`/`download_extension` have no local-source
 * entry point — so there is no supported way to install a local build. This
 * mirrors `write_artifacts`' layout by hand instead of calling it, because
 * install.sh would re-download a release and clobber the local build.
 *
 * Layout (ADR-0017): bin/, extension/ and version are install artifacts that
 * get overwritten here; data/ is persistent and is never touched.
 *   $BB_HOME/{bin/bridge, extension/, version, data/}
 *   ~/Browser-Bridge/extension -> $BB_HOME/extension
 *   ~/.local/bin/bridge        -> $BB_HOME/bin/bridge
 *
 * Single binary (ADR-0013): bin/bridge carries the CLI, the service lifecycle
 * and the hidden `serve` control-plane subcommand.
 */
import { $ } from 'bun';

const REPO_ROOT = new URL('..', import.meta.url).pathname;

const HOME_DIR = process.env.HOME ?? '';
const BB_HOME = process.env.BB_HOME ?? `${HOME_DIR}/.browser-bridge`;
const BB_EXTENSION_DIR =
  process.env.BB_EXTENSION_DIR ?? `${HOME_DIR}/Browser-Bridge`;

const binPath = (home: string) => `${home}/bin/bridge`;
const pkgVersion = await Bun.file(`${REPO_ROOT}/package.json`).json();

/**
 * Version stamped into the binary. The `-dev+<sha>` suffix is deliberate: it
 * makes `bridge --version` and `$BB_HOME/version` visibly distinct from a
 * release build, so `bridge update` (which would overwrite this install with
 * a downloaded release) is obviously not what you want.
 */
const buildVersion = async (): Promise<string> => {
  const base = String(pkgVersion.version).replace(/^v/, '');
  const sha = (
    await $`git -C ${REPO_ROOT} rev-parse --short HEAD`.quiet()
  ).stdout
    .toString()
    .trim();
  return sha ? `${base}-dev+${sha}` : `${base}-dev`;
};

const step = (msg: string) => console.log(`\n▸ ${msg}`);
const done = (msg: string) => console.log(`  ✓ ${msg}`);

/** Compile `dist/bridge` with the version linked in (build:cli omits ldflags). */
const buildBinary = async (): Promise<string> => {
  step('Building Go runtime');
  const version = await buildVersion();
  await $`mkdir -p ${`${REPO_ROOT}/dist`}`.quiet();
  const proc = Bun.spawn(
    [
      'go',
      '-C',
      `${REPO_ROOT}/apps/bridge-core`,
      'build',
      '-ldflags',
      `-X main.version=${version}`,
      '-o',
      `${REPO_ROOT}/dist/bridge`,
      './cmd/bridge',
    ],
    { cwd: REPO_ROOT, stdout: 'inherit', stderr: 'inherit' },
  );
  if ((await proc.exited) !== 0) {
    throw new Error('go build failed');
  }
  done(`dist/bridge (version ${version})`);
  return version;
};

/** Build the extension bundle into apps/extension/dist/. */
const buildExtension = async (): Promise<void> => {
  step('Building Chrome extension');
  const proc = Bun.spawn(['bun', 'run', 'build:extension'], {
    cwd: REPO_ROOT,
    stdout: 'inherit',
    stderr: 'inherit',
  });
  if ((await proc.exited) !== 0) {
    throw new Error('extension build failed');
  }
  done('apps/extension/dist/');
};

/**
 * Re-sign the installed binary ad-hoc.
 *
 * A plain `go build` leaves a *linker-signed* signature, which macOS 27's
 * AMFI rejects for any executable path registered as a background task — and
 * $BB_HOME/bin/bridge is exactly that, because the supervisor runs it via
 * launchd. Symptoms are exit 137 (SIGKILL) with no output, and
 * `bridge` reporting "command not found" even though PATH is correct:
 *
 *   AMFI: '/Users/.../.browser-bridge/bin/bridge' has no CMS blob?
 *   AMFI: '/Users/.../.browser-bridge/bin/bridge': Unrecoverable CT signature issue
 *
 * Only registered paths are affected, which is why an identical byte-for-byte
 * copy named `bridge-test` in the same directory runs fine — so this cannot be
 * diagnosed by testing the build output in dist/. `codesign --force --sign -`
 * writes a real CMS blob and passes validation.
 *
 * Released binaries ship with a Developer ID signature, so they never need this.
 */
const signBinary = async (): Promise<void> => {
  const path = binPath(BB_HOME);
  const signed = await $`codesign --force --sign - ${path}`.quiet().nothrow();
  if (signed.exitCode !== 0) {
    throw new Error(
      `codesign failed: ${signed.stderr.toString().trim() || 'unknown error'}`,
    );
  }
  done('ad-hoc signed (required by AMFI for launchd-registered paths)');
};

/**
 * Copy build artifacts into $BB_HOME and refresh both symlinks.
 * extension/ is wiped first so removed files do not linger and get loaded
 * by Chrome as orphans.
 */
const installArtifacts = async (version: string): Promise<void> => {
  step(`Installing into ${BB_HOME}`);
  await $`mkdir -p ${`${BB_HOME}/bin`} ${`${BB_HOME}/extension`}`.quiet();

  await $`cp ${`${REPO_ROOT}/dist/bridge`} ${binPath(BB_HOME)}`.quiet();
  await $`chmod +x ${binPath(BB_HOME)}`.quiet();
  await signBinary();

  await $`rm -rf ${`${BB_HOME}/extension`}`.quiet();
  await $`mkdir -p ${`${BB_HOME}/extension`}`.quiet();
  await $`cp -R ${`${REPO_ROOT}/apps/extension/dist/.`} ${`${BB_HOME}/extension/`}`.quiet();

  await Bun.write(`${BB_HOME}/version`, `${version}\n`);

  // ADR-0017: data/ is persistent. Created if missing, never written to.
  await $`mkdir -p ${`${BB_HOME}/data`}`.quiet();
  await $`chmod 700 ${`${BB_HOME}/data`}`.quiet();

  step('Linking');
  await $`mkdir -p ${`${HOME_DIR}/.local/bin`} ${BB_EXTENSION_DIR}`.quiet();
  await $`ln -sf ${binPath(BB_HOME)} ${`${HOME_DIR}/.local/bin/bridge`}`.quiet();
  await $`ln -sfn ${`${BB_HOME}/extension`} ${`${BB_EXTENSION_DIR}/extension`}`.quiet();
  done(`~/.local/bin/bridge -> ${binPath(BB_HOME)}`);
  done(`${BB_EXTENSION_DIR}/extension -> ${BB_HOME}/extension`);
};

/** Run an installed `bridge` subcommand, capturing output and exit code. */
const bridgeRun = async (
  args: string[],
): Promise<{ code: number; out: string }> => {
  const proc = Bun.spawn([binPath(BB_HOME), ...args], {
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
 * Wait for the old supervisor to actually release its ports.
 *
 * `service down` returns before the process is gone, so a bare down→up races
 * and `up` fails with the port still held (BB-E010/E011).
 */
const waitForStop = async (timeoutMs = 10_000): Promise<boolean> => {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const { code, out } = await bridgeRun(['service', 'status']);
    if (code !== 0 || !/running/.test(out)) return true;
    await Bun.sleep(250);
  }
  return false;
};

/**
 * Wait for bridge-core to report running.
 *
 * `service up` returns once launchd has accepted the supervisor, which is
 * before `bridge serve` has bound its ports and written run/bridge-core.pid —
 * so a status check immediately after `up` races and sees "stopped" even on a
 * healthy start.
 */
const waitForStart = async (timeoutMs = 15_000): Promise<string | null> => {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const { out } = await bridgeRun(['service', 'status']);
    const line = out.split('\n').find((l) => /running/.test(l));
    if (line) return line.trim();
    await Bun.sleep(250);
  }
  return null;
};

/**
 * Restart the supervisor so it picks up the freshly installed binary.
 *
 * Verifies the service actually came up: a failed restart used to print a
 * warning and exit 0, so a broken install still looked successful.
 */
const restartService = async (): Promise<void> => {
  step('Restarting bridge services');
  await $`${binPath(BB_HOME)} service down`.quiet().nothrow();
  if (!(await waitForStop())) {
    throw new Error(
      'old supervisor did not stop within 10s — ports still held',
    );
  }

  await $`${binPath(BB_HOME)} service enable`.quiet().nothrow();
  const up = await $`${binPath(BB_HOME)} service up`.quiet().nothrow();
  if (up.exitCode !== 0) {
    throw new Error(
      `service up failed: ${up.stderr.toString().trim() || 'no output'}`,
    );
  }

  const running = await waitForStart();
  if (!running) {
    const log = await Bun.file(`${BB_HOME}/logs/bridge-core.log`).text();
    throw new Error(
      `service up succeeded but bridge-core never reported running.\n` +
        `Last log lines:\n${log.split('\n').slice(-5).join('\n')}`,
    );
  }
  done(running);
};

try {
  const version = await buildBinary();
  await buildExtension();
  await installArtifacts(version);
  await restartService();

  console.log(`\nLocal build ${version} installed.\n`);
  console.log(
    'Reload the extension in Chrome, or nothing you changed will show up:',
  );
  console.log(`  chrome://extensions/ -> "Browser Bridge" -> Reload`);
  console.log(`  (unpacked from ${BB_EXTENSION_DIR}/extension/)\n`);
  console.log('Then verify with: bun run test:smoke\n');
} catch (err) {
  console.error(`\nlocal install failed: ${(err as Error).message}`);
  process.exit(1);
}
