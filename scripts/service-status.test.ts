import { describe, expect, it } from 'bun:test';

import { runningStatusLine, stoppedStatus } from './service-status';

// The exact text `bridge service status` produces. Taken from the shapes in
// internal/cli/commands.go: Status, not from a paraphrase of them.
const RUNNING = `bridge-core:  running  (pid 51234)
auto-start:  enabled, agent loaded (label com.browser-bridge.bridge-core)
`;

const STOPPED_BUT_ENABLED = `bridge-core:  stopped
auto-start:  enabled (starts at login; not running now)
`;

const STOPPED_AND_DISABLED = `bridge-core:  stopped
auto-start:  disabled
`;

// The whole point of the check: the auto-start line contains the word
// "running" while the service is down, wrapped in a "not".
//
// `waitForStart` used to grep for `running` anywhere in the output. It matched
// this line and returned success for a daemon that had never bound its ports
// — so a broken install exited 0 and looked fine. This is the reachable case,
// not a contrived one: `restartService` calls `service enable` before
// `service up`, so a launchd agent that is present and enabled is the normal
// state whenever the check runs.
describe('runningStatusLine', () => {
  it('reports the running service', () => {
    expect(runningStatusLine(0, RUNNING)).toBe(
      'bridge-core:  running  (pid 51234)',
    );
  });

  it('does not read "not running now" as running', () => {
    expect(runningStatusLine(0, STOPPED_BUT_ENABLED)).toBeNull();
  });

  it('reports a stopped and disabled service as not running', () => {
    expect(runningStatusLine(0, STOPPED_AND_DISABLED)).toBeNull();
  });

  it('trusts a non-zero exit even when the text says running', () => {
    // `Status` exits 1 whenever the pid is 0, so a running-looking line under
    // a failed exit is a state the control plane does not consider running.
    // Checking both is what keeps a future status format from reopening the
    // hole from the other side.
    expect(runningStatusLine(1, RUNNING)).toBeNull();
  });

  it('handles empty output', () => {
    expect(runningStatusLine(0, '')).toBeNull();
  });
});

describe('stoppedStatus', () => {
  it('is true for a stopped service whose launchd agent is enabled', () => {
    expect(stoppedStatus(0, STOPPED_BUT_ENABLED)).toBe(true);
  });

  it('is true for a non-zero exit', () => {
    expect(stoppedStatus(1, RUNNING)).toBe(true);
  });

  it('is false only for a genuinely running service', () => {
    expect(stoppedStatus(0, RUNNING)).toBe(false);
  });
});

// Both callers of `bridge service status` have to ask the same way. The rule
// lives in service-status.ts precisely because it was once written twice, and
// the copy in smoke.ts was the one that still matched
// `auto-start:  enabled (starts at login; not running now)` — so the smoke
// test, the thing whose job is to say a broken install looks fine, reported a
// stopped daemon as healthy.
describe('every caller of service status', () => {
  it('goes through runningStatusLine, not its own grep', async () => {
    const sources = await Promise.all(
      // Resolved against this file's own directory rather than the process
      // cwd, so the assertion holds wherever the suite is run from.
      ['./local-install.ts', './smoke.ts'].map((name) =>
        Bun.file(new URL(name, import.meta.url)).text(),
      ),
    );
    for (const [i, src] of sources.entries()) {
      const name = ['local-install.ts', 'smoke.ts'][i];
      if (!src.includes('runningStatusLine')) {
        throw new Error(
          `${name} does not use runningStatusLine; the rule has forked again`,
        );
      }
      if (/\/running\/\.test|\.test\(out\)/.test(src)) {
        throw new Error(
          `${name} still greps for "running" on the raw output, which ` +
            `matches the auto-start line's "not running now"`,
        );
      }
    }
  });
});
