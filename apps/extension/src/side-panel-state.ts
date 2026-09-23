import type { PolicyState } from './policy-state';

// Side panel tabs are the four policy panels the side panel exposes
// (ADR-0010). Settings is not a tab — hard configuration lives in a separate
// page opened via chrome.runtime.openOptionsPage(); a link to it sits in the
// state bar.
// Settings is not a tab — hard configuration lives in a separate page opened
// via chrome.runtime.openOptionsPage(); a link to it sits in the state bar.
export type SidePanelTab = 'approvals' | 'origins' | 'blocklist' | 'downloads';

// The default tab is the side panel's first view when the panel opens.
// Approval-worthy items are time-sensitive — the user must see them
// immediately. Denials surface on the Approvals tab; paused downloads
// surface on the Downloads tab. Routing only denials to Approvals (and
// downloads to Downloads) avoids landing the user on an empty panel.
export function selectDefaultView(state: PolicyState): SidePanelTab {
  if (state.recentDenials.length > 0) return 'approvals';
  if (state.pendingDownloads.length > 0) return 'downloads';
  return 'origins';
}
