import type { PolicyState } from './policy-state';

// Side panel tabs are the four policy panels the side panel exposes
// (ADR-0010). Settings is not a tab — hard configuration lives in a separate
// page opened via chrome.runtime.openOptionsPage(); a link to it sits in the
// state bar.
// Settings is not a tab — hard configuration lives in a separate page opened
// via chrome.runtime.openOptionsPage(); a link to it sits in the state bar.
export type SidePanelTab = 'approvals' | 'origins' | 'blocklist' | 'downloads';

// The default tab is the side panel's first view when the panel opens.
// Approval-worthy items (denials and paused downloads) are time-sensitive —
// the user must see them immediately — so when either is present, approvals
// wins. Otherwise origins is the most common lookup target.
export function selectDefaultView(state: PolicyState): SidePanelTab {
  if (state.recentDenials.length > 0 || state.pendingDownloads.length > 0) {
    return 'approvals';
  }
  return 'origins';
}
