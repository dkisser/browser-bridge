import { describe, expect, it } from 'bun:test';
import { renderToStaticMarkup } from 'react-dom/server';
import { PermissionModeControl } from '../src/ui/components/PermissionModeControl';

const render = (mode: 'strict' | 'standard' | 'relaxed' | null): string =>
  renderToStaticMarkup(
    <PermissionModeControl mode={mode} onChange={() => {}} />,
  );

describe('PermissionModeControl — structural render', () => {
  it('offers exactly the three modes, in ascending order', () => {
    const html = render('strict');
    expect(html).toContain('role="radiogroup"');
    expect(html).toContain('aria-label="Permission mode"');
    for (const label of ['Strict', 'Standard', 'Relaxed']) {
      expect(html).toContain(`>${label}<`);
    }
    expect(html.indexOf('>Strict<')).toBeLessThan(html.indexOf('>Standard<'));
    expect(html.indexOf('>Standard<')).toBeLessThan(html.indexOf('>Relaxed<'));
  });

  it('uses native radio inputs, so the group is one tab stop with real arrow keys', () => {
    const html = render('strict');
    expect(html.match(/type="radio"/g)?.length).toBe(3);
    // One shared name is what makes them a group rather than three
    // independent controls.
    expect(html.match(/name="permission-mode"/g)?.length).toBe(3);
  });

  it('checks only the selected mode', () => {
    const html = render('standard');
    expect(html).toMatch(/checked=""[^>]*value="standard"/);
    expect(html).not.toMatch(/checked=""[^>]*value="strict"/);
    expect(html).not.toMatch(/checked=""[^>]*value="relaxed"/);
  });

  it('states what the selected mode changes, so the boundary stays visible', () => {
    expect(render('strict')).toContain(
      'Reading and writing both ask for origin approval.',
    );
    expect(render('standard')).toContain(
      'Reads run within your approved origins; writes still ask.',
    );
    expect(render('relaxed')).toContain(
      'Reads and writes run within your approved origins.',
    );
  });

  it('renders nothing as selected while the policy read is in flight', () => {
    const html = render(null);
    expect(html).not.toContain('checked=""');
    expect(html).toContain('Loading…');
    // Nothing is clickable before the real value is known, so the panel
    // cannot write a mode the user never chose.
    expect(html.match(/disabled=""/g)?.length).toBe(3);
  });
});
