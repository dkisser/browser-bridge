import { describe, expect, it } from 'bun:test';
import type { PolicyState } from '../src/policy-state';
import { type SidePanelTab, selectDefaultView } from '../src/side-panel-state';

const EMPTY_STATE: PolicyState = {
  origins: {},
  deniedOrigins: {},
  grants: [],
  takeover: false,
  agentTabs: [],
  pairingToken: null,
  recentDenials: [],
  blockedOrigins: [],
  pendingDownloads: [],
};

describe('selectDefaultView', () => {
  it('returns origins when there are no denials and no paused downloads', () => {
    expect(selectDefaultView(EMPTY_STATE)).toBe<SidePanelTab>('origins');
  });

  it('returns approvals when there is a recent denial', () => {
    const state: PolicyState = {
      ...EMPTY_STATE,
      recentDenials: [
        {
          reason: 'origin_not_approved',
          command: 'click',
          origin: 'https://unapproved.example',
        },
      ],
    };
    expect(selectDefaultView(state)).toBe<SidePanelTab>('approvals');
  });

  it('returns approvals when there is a paused download', () => {
    const state: PolicyState = {
      ...EMPTY_STATE,
      pendingDownloads: [
        { id: 1, filename: 'foo.zip', url: 'https://x.example/foo.zip' },
      ],
    };
    expect(selectDefaultView(state)).toBe<SidePanelTab>('approvals');
  });

  it('returns approvals when both denials and downloads are present', () => {
    const state: PolicyState = {
      ...EMPTY_STATE,
      recentDenials: [
        {
          reason: 'origin_not_approved',
          command: 'click',
          origin: 'https://unapproved.example',
        },
      ],
      pendingDownloads: [
        { id: 1, filename: 'foo.zip', url: 'https://x.example/foo.zip' },
      ],
    };
    expect(selectDefaultView(state)).toBe<SidePanelTab>('approvals');
  });
});
