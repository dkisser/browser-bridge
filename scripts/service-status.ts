/**
 * Reading `bridge service status`.
 *
 * Both the start and the stop wait need to answer the same question — is the
 * supervisor up? — and they used to answer it with two different, looser
 * greps. That is how a stopped daemon came to be reported as a working
 * install, so the rule lives here once and both call it.
 *
 * It is a separate module because local-install.ts runs its work at import
 * time: a test cannot import the script, only something it uses.
 */

/** What `bridge service status` calls the service on the first line. */
const SERVICE_LINE = /^bridge-core:\s+/;

/**
 * The running line from a `service status` result, or null if the service is
 * not running.
 *
 * Two things have to agree, and either alone is not enough:
 *
 * - The exit code. `Status` returns `pid != 0` and cobra exits 1 when that is
 *   false, so a stopped service is a non-zero exit.
 * - The service's own line. Matching `running` anywhere in the output is not
 *   the same test: `service status` prints the launchd state separately, and
 *   the not-loaded variant literally reads
 *   `auto-start:  enabled (starts at login; not running now)`. A grep for
 *   `running` matches that line — including the word "not" in front of it —
 *   which is how a daemon that never bound its ports was reported as running.
 *
 * The exit code is checked as well as the text so a future status line cannot
 * re-open the same hole from the other side.
 */
export const runningStatusLine = (code: number, out: string): string | null => {
  if (code !== 0) return null;
  const line = out
    .split('\n')
    .find((l) => SERVICE_LINE.test(l) && /running/.test(l));
  return line ? line.trim() : null;
};

/** Whether a `service status` result describes a service that is not up. */
export const stoppedStatus = (code: number, out: string): boolean =>
  code !== 0 || runningStatusLine(code, out) === null;
