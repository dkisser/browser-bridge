import { describe, expect, it } from 'bun:test';
import type { Denial } from '@browser-bridge/shared';
import { renderToStaticMarkup } from 'react-dom/server';
import { ApprovalsPanel } from '../src/ui/panels/ApprovalsPanel';

function render(denial: Denial): string {
  return renderToStaticMarkup(
    <ApprovalsPanel denials={[denial]} onDenialAction={() => {}} />,
  );
}

const ORIGIN_REQUEST: Denial = {
  reason: 'origin_not_approved',
  command: 'tab:new',
  origin: 'https://example.com',
};

describe('approval card close affordance (ADR-0040)', () => {
  it('offers the close on an origin request', () => {
    const html = render(ORIGIN_REQUEST);
    expect(html).toContain('aria-label="Dismiss this request"');
  });

  it('names it for a screen reader, not only with a glyph', () => {
    // The control renders as a bare ×, which says nothing on its own.
    const html = render(ORIGIN_REQUEST);
    expect(html).toMatch(/aria-label="Dismiss this request"/);
  });

  it('does not add one where a Dismiss button already exists', () => {
    // approval_required already carries Dismiss as a labeled action button.
    // A second control for the same action, in two shapes, is how one button
    // ends up meaning two things.
    for (const denial of [
      {
        reason: 'approval_required',
        command: 'type',
        origin: 'https://x.test',
      },
      {
        reason: 'action_out_of_scope',
        command: 'tab:close',
        origin: 'https://x.test',
      },
    ] as Denial[]) {
      const html = render(denial);
      expect(html).toContain('Dismiss');
      expect(html).not.toContain('aria-label="Dismiss this request"');
    }
  });

  it('does not add one to cards the human cannot answer', () => {
    // origin_denied has no button set worth closing: the denial is a
    // standing answer, not a request awaiting one.
    const html = render({
      reason: 'origin_denied',
      command: 'click',
      origin: 'https://x.test',
    });
    expect(html).not.toContain('aria-label="Dismiss this request"');
  });

  it('keeps the existing three answers untouched', () => {
    // denialCardButtons is copy-frozen; the close is additive.
    const html = render(ORIGIN_REQUEST);
    for (const label of ['Session', 'Always', 'Deny']) {
      expect(html).toContain(`>${label}<`);
    }
  });
});

describe('the close never traps the human (ADR-0040)', () => {
  it('still offers every answer on the card a retry produces', () => {
    // The close records a stance, so the agent's retry comes back as
    // origin_ignored. If that card fell through to the default button set it
    // would carry Dismiss alone — and Dismiss records nothing and grants
    // nothing, so the origin could only be granted by switching permission
    // modes. That is a setting, not an answer about this origin, and it would
    // make the close a one-way door the ADR explicitly rules out.
    const html = render({
      reason: 'origin_ignored',
      command: 'click',
      origin: 'https://example.com',
    });
    for (const label of ['Session', 'Always', 'Deny']) {
      expect(html).toContain(`>${label}<`);
    }
    expect(html).not.toContain('>Dismiss<');
  });

  it('offers no close on a request that carries no origin to record', () => {
    // The gate reaches a content command with origin null when no tab is
    // targeted, and denial() then omits the origin entirely. applyDenialAction
    // records no stance without one, so a close here would promise something
    // and quietly deliver a card deletion — the rejected pure-UI outcome in a
    // better icon.
    const html = render({
      reason: 'origin_not_approved',
      command: 'gettext',
    });
    expect(html).not.toContain('aria-label="Dismiss this request"');
  });
});

describe('a dismissed card is still closable (ADR-0040)', () => {
  it('offers the close on the card a retry produces', () => {
    // Giving origin_ignored the three answers fixed one dead end — a human
    // who closed a card could no longer grant the origin — and opened
    // another: with no Dismiss and no close, the card they closed came back
    // and could never be removed, holding the badge at 1+ for as long as the
    // agent kept asking. Pressing the close here is a no-op on the stance
    // (withStance replaces an identical entry with itself); it takes the
    // card away, which is what the human asked for by pressing it.
    const html = render({
      reason: 'origin_ignored',
      command: 'click',
      origin: 'https://example.com',
    });
    expect(html).toContain('aria-label="Dismiss this request"');
  });

  it('still offers every answer on it', () => {
    const html = render({
      reason: 'origin_ignored',
      command: 'click',
      origin: 'https://example.com',
    });
    for (const label of ['Session', 'Always', 'Deny']) {
      expect(html).toContain(`>${label}<`);
    }
  });
});
