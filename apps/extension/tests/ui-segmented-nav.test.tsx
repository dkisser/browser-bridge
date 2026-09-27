import { describe, expect, it } from 'bun:test';
import { renderToStaticMarkup } from 'react-dom/server';
import type { SidePanelTab } from '../src/side-panel-state';
import { SegmentedNav } from '../src/ui/components/SegmentedNav';

const TABS: { id: SidePanelTab; label: string; badge?: number }[] = [
  { id: 'approvals', label: 'Approvals' },
  { id: 'origins', label: 'Origins' },
  { id: 'blocklist', label: 'Blocklist' },
  { id: 'downloads', label: 'Downloads' },
];

describe('SegmentedNav — structural render', () => {
  it('renders one button per tab with role="tab" and ARIA wiring', () => {
    const html = renderToStaticMarkup(
      <SegmentedNav tabs={TABS} activeTab="approvals" onActivate={() => {}} />,
    );
    for (const tab of TABS) {
      expect(html).toContain(`id="tab-${tab.id}"`);
      expect(html).toContain(`aria-controls="panel-${tab.id}"`);
      expect(html).toContain(`>${tab.label}<`);
    }
    expect(html).toContain('role="tablist"');
    expect(html).toContain('aria-label="Side panel sections"');
  });

  it('marks only the active tab with aria-selected="true" and tabIndex=0', () => {
    const html = renderToStaticMarkup(
      <SegmentedNav tabs={TABS} activeTab="downloads" onActivate={() => {}} />,
    );
    expect(html).toMatch(/id="tab-downloads"[^>]*aria-selected="true"/);
    expect(html).toMatch(/id="tab-downloads"[^>]*tabindex="0"/i);
    for (const tab of TABS.filter((t) => t.id !== 'downloads')) {
      expect(html).toMatch(
        new RegExp(`id="tab-${tab.id}"[^>]*aria-selected="false"`),
      );
      expect(html).toMatch(
        new RegExp(`id="tab-${tab.id}"[^>]*tabindex="-1"`, 'i'),
      );
    }
  });

  it('renders a badge count next to the tab label when badge > 0', () => {
    const html = renderToStaticMarkup(
      <SegmentedNav
        tabs={[{ id: 'approvals', label: 'Approvals', badge: 3 }]}
        activeTab="approvals"
        onActivate={() => {}}
      />,
    );
    expect(html).toContain('>3</span>');
  });

  it('omits the badge when undefined or 0', () => {
    const html = renderToStaticMarkup(
      <SegmentedNav
        tabs={[
          { id: 'approvals', label: 'Approvals' },
          { id: 'approvals', label: 'Approvals', badge: 0 },
        ]}
        activeTab="approvals"
        onActivate={() => {}}
      />,
    );
    expect(html).not.toMatch(/<span[^>]*>0<\/span>/);
  });
});
