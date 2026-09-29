import { beforeAll, describe, expect, it } from 'bun:test';
import { readdirSync, readFileSync } from 'node:fs';
import { LOCAL_HOST, LOCAL_WS_PORT } from '@browser-bridge/shared';

// offscreen.ts holds the extension's persistent WebSocket to the proxy — the
// one address where a wrong host is not cosmetic. `localhost` can resolve to
// ::1 on a machine whose /etc/hosts lists it first, and the proxy binds
// 127.0.0.1 exactly, so the result is an agent that never pairs at all.
//
// Three places in the extension consume the loopback host: this one,
// ui/bridge-api.ts, and the settings tab. The settings test pins the first
// two against each other; without this file, putting `localhost` back *here*
// passes every test, which is exactly how it survived the first cleanup.

// offscreen.ts registers a chrome.runtime.onMessage listener at module scope,
// so the global has to exist before the import runs. A static import would be
// hoisted above the assignment, hence the dynamic one — the same ordering
// background-gate.test.ts uses for background.ts.
let localWsUrl: string;

beforeAll(async () => {
  (globalThis as Record<string, unknown>).chrome = {
    runtime: { onMessage: { addListener: () => {} } },
  };
  ({ LOCAL_WS_URL: localWsUrl } = await import('../src/offscreen'));
});

// The same walk the single-writer guard uses, so a source file added later is
// covered the moment it exists.
function sourceFilesIn(dir: URL): URL[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    // URL resolution treats a base without a trailing slash as a *file* and
    // replaces its last segment, so the entry lands in the parent directory
    // and the walk silently starts reading the test file itself. It failed
    // loudly here only because the test contains the very literal it hunts
    // for; in a file that did not, it would have swept a directory it was
    // never pointed at and reported success.
    const base = dir.pathname.endsWith('/') ? dir : new URL(`${dir}/`);
    const full = new URL(
      `${entry.name}${entry.isDirectory() ? '/' : ''}`,
      base,
    );
    if (entry.isDirectory()) return sourceFilesIn(full);
    return /\.(ts|tsx)$/.test(entry.name) ? [full] : [];
  });
}

describe('the offscreen WebSocket dials the bound host', () => {
  it('is built from the shared host and port', () => {
    expect(localWsUrl).toBe(`ws://${LOCAL_HOST}:${LOCAL_WS_PORT}`);
  });

  it('does not reintroduce the localhost literal', () => {
    // Agreeing with the right value is not the same as deriving from the
    // right constant, and this is the site where the difference decides
    // whether the product works at all.
    expect(localWsUrl).not.toContain('localhost');
  });

  it('no extension source builds an address from a localhost literal', () => {
    // A sweep rather than a spot check, so a fourth call site added later is
    // caught here instead of in production. Reads the sources rather than
    // importing them: offscreen.ts and the UI modules have import-time
    // side effects that make importing all three cleanly awkward.
    //
    // Comments are stripped first, because several of them *quote* the old
    // literal while explaining why it went away. Only code can still dial it.
    // Every .ts/.tsx under src/, not a list of the three known call sites. A
    // hardcoded list is a spot check wearing a sweep's name — this one passed
    // unchanged when a fourth file containing a `localhost` URL was added to
    // the directory, which is the exact regression the comment above claims
    // to prevent.
    const offenders: string[] = [];
    for (const file of sourceFilesIn(new URL('../src/', import.meta.url))) {
      const source = readFileSync(file, 'utf8');
      for (const line of source.split('\n')) {
        // Comments are stripped so an explanatory note quoting the old
        // literal does not turn the guard red — but the `//` must not be
        // taken for a comment start when it is part of `://`. A plain
        // `//.*$` erases `http://localhost:3002` down to `http:`, which is
        // precisely the line this guard exists to catch: it was vacuous for
        // every URL while looking like a sweep. The preceding-character
        // class is what keeps `://` intact.
        const code = line
          .replace(/\/\*.*?\*\//g, '')
          .replace(/(^|[^:])\/\/.*$/, '$1');
        if (code.includes('localhost')) {
          offenders.push(`${file}: ${code.trim()}`);
        }
      }
    }
    expect(offenders).toEqual([]);
  });
});
