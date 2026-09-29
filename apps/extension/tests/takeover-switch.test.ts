import { describe, expect, it } from 'bun:test';
import { readFileSync } from 'node:fs';

// The panel's half of the takeover switch: it must route through
// takeover-sync rather than keep its own copy of the rule.
//
// The behaviour itself is in takeover-sync.test.ts, where it is executed.
// This file cannot do that job — the package has no DOM harness, and the UI
// tests use react-dom/server, which runs no effects and dispatches no
// clicks, so nothing here can observe what the switch displays after a
// click. Adding happy-dom or testing-library to watch one component react
// to one message would be a larger change than the property.
//
// So the guard is scoped to the narrowest claim that survives the constraint:
// the component calls the machine, and it does not assign the display from
// the click's own argument. That is the one thing a source read can settle.
// Everything else about what the switch shows is a behaviour question and
// lives in the other file.
describe('the panel routes the switch through takeover-sync', () => {
  const SIDE_PANEL = readFileSync(
    new URL('../src/ui/SidePanel.tsx', import.meta.url),
    'utf8',
  );

  function handler(): string {
    const start = SIDE_PANEL.indexOf('const handleTakeoverChange');
    expect(start).toBeGreaterThan(-1);
    // The next top-level const, not a fixed offset: this file's comments run
    // long, and a window that happens to cover the handler today silently
    // stops covering it the first time someone inserts an explanation.
    const end = SIDE_PANEL.indexOf('const handleOpenSettings', start);
    expect(end).toBeGreaterThan(start);
    return SIDE_PANEL.slice(start, end);
  }

  it('never sets the display from the value the click asked for', () => {
    // The optimistic render, by whatever name. `setTakeover(desired)` is the
    // form it took before the machine existed and `setTakeover(!desired)` the
    // form of the revert that followed it. A reintroduction spelled some
    // other way is the behaviour tests' problem, but these two spellings are
    // worth refusing outright.
    expect(SIDE_PANEL).not.toContain('setTakeover(desired)');
    expect(SIDE_PANEL).not.toContain('setTakeover(!desired)');
  });

  it('opens the window through click() and closes it through settle()', () => {
    const body = handler();
    expect(body).toContain('click(takeoverSync.current, desired)');

    const settles = body.match(/settle\(takeoverSync\.current/g) ?? [];
    const comparisons =
      body.match(/takeoverSync\.current\.gen !== myGen/g) ?? [];
    // One settle per reply path. The success path needs no generation check
    // because settle() performs it; the error path checks before it releases
    // the window or shows a banner. Fewer comparisons than the two settle
    // sites means an error path is acting on a window it no longer owns.
    expect(settles).toHaveLength(2);
    expect(comparisons).toHaveLength(1);
  });

  it('applies a storage arrival through observe(), not by hand', () => {
    expect(SIDE_PANEL).toContain(
      'observe(takeoverSync.current, state.takeover)',
    );
  });
});
