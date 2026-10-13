import { describe, expect, it } from 'bun:test';
import { renderToStaticMarkup } from 'react-dom/server';
import { TakeoverHero } from '../src/ui/components/TakeoverHero';

const render = (paired: boolean): string =>
  renderToStaticMarkup(
    <TakeoverHero
      takeover={false}
      permissionMode="standard"
      browserConnected
      browserId="b-6e3ea4a5"
      paired={paired}
      onTakeoverChange={() => {}}
      onPermissionModeChange={() => {}}
      onOpenSettings={() => {}}
    />,
  );

// The DOM half of the pairing-row bug. The transition itself is covered by
// pair-drawer.test.ts; this pins what the user actually sees.
describe('TakeoverHero — pair row visibility', () => {
  it('shows the pair form while unpaired, since that blocks everything', () => {
    const html = render(false);
    expect(html).toContain('Pair with bridge-core');
    expect(html).toContain('placeholder="Pairing code"');
  });

  it('hides the pair form once paired and leaves only the re-pair toggle', () => {
    const html = render(true);
    expect(html).not.toContain('Pair with bridge-core');
    expect(html).not.toContain('placeholder="Pairing code"');
    expect(html).toContain('aria-label="Re-pair"');
  });

  it('never offers re-pairing while unpaired — there is nothing to re-pair', () => {
    expect(render(false)).not.toContain('aria-label="Re-pair"');
  });
});
