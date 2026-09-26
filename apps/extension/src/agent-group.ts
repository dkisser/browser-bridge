// Agent tab group (ADR-0014): every tab opened via tab:new is placed into a
// per-window Chrome tab group titled "browser-bridge" (orange) so the human
// can see at a glance which tabs the agent is driving. Purely visual — group
// membership never feeds policy decisions (agentTabs remains the trust
// signal). Group identity is discovered by title+color query because
// groupIds are unstable across sessions; if the user renamed, recolored, or
// ungrouped, the query misses and a fresh group is created on the next
// tab:new — the user's customization is never fought.

import { setAgentGroupAvailability } from './policy-state';

export const AGENT_GROUP_TITLE = 'browser-bridge';
export const AGENT_GROUP_COLOR: chrome.tabGroups.ColorEnum = 'orange';

// Per-window FIFO queue: concurrent tab:new calls in the same window
// serialize here so two of them cannot both miss the query and create
// duplicate groups. Each queue owns its own `tail` pointer rather than
// storing a closure-captured promise — a hung chrome.tabs.group IPC
// leaves the tail pending forever; subsequent enqueues chain on the same
// pending tail (via .then in enqueue) and wait behind it. The only
// recovery path is Chrome restarting the service worker (~30s), which
// clears the Map.
class WindowGroupQueue {
  private tail: Promise<unknown> = Promise.resolve();

  constructor(readonly windowId: number) {}

  enqueue(tabId: number): Promise<number> {
    const next = this.tail
      .catch(() => undefined)
      .then(() => ensureAgentGroupInner(this.windowId, tabId));
    // Advance the tail so the next enqueue waits on this one without
    // propagating the result/error to its consumer.
    this.tail = next.catch(() => undefined);
    return next;
  }
}

const groupQueuesByWindow = new Map<number, WindowGroupQueue>();

function getQueue(windowId: number): WindowGroupQueue {
  let queue = groupQueuesByWindow.get(windowId);
  if (!queue) {
    queue = new WindowGroupQueue(windowId);
    groupQueuesByWindow.set(windowId, queue);
  }
  return queue;
}

// Each chrome.tabGroups API call (query AND update) is routed through a
// tracked wrapper so we can flip the PolicyState.agentGroupAvailable bit
// on permission rejection. Chrome silently denies tabGroups API calls
// after an extension update if the user rejected the new permission
// prompt, and without this tracking every tab:new would silently leave
// its tab ungrouped with no UI signal. Both query and update must be
// tracked — if update is left bare, a permission revocation between a
// successful query and the subsequent update would silently fail to
// label the freshly-created group without flipping the badge.
async function trackedTabGroupsQuery(
  opts: chrome.tabGroups.QueryInfo,
): Promise<chrome.tabGroups.TabGroup[]> {
  try {
    const groups = await chrome.tabGroups.query(opts);
    await setAgentGroupAvailability(true);
    return groups;
  } catch (error) {
    await setAgentGroupAvailability(false, error);
    throw error;
  }
}

async function trackedTabGroupsUpdate(
  groupId: number,
  update: chrome.tabGroups.UpdateProperties,
): Promise<void> {
  try {
    await chrome.tabGroups.update(groupId, update);
    await setAgentGroupAvailability(true);
  } catch (error) {
    await setAgentGroupAvailability(false, error);
    throw error;
  }
}

async function queryAgentGroupId(windowId: number): Promise<number | null> {
  const groups = await trackedTabGroupsQuery({
    color: AGENT_GROUP_COLOR,
    title: AGENT_GROUP_TITLE,
    windowId,
  });
  return groups[0]?.id ?? null;
}

// Chrome cannot create an empty group — the create path groups the tab that
// triggered it and then labels the group.
async function ensureAgentGroupInner(
  windowId: number,
  tabId: number,
): Promise<number> {
  const existingId = await queryAgentGroupId(windowId);
  if (existingId !== null) {
    await chrome.tabs.group({ groupId: existingId, tabIds: [tabId] });
    return existingId;
  }
  const groupId = await chrome.tabs.group({
    createProperties: { windowId },
    tabIds: [tabId],
  });
  await trackedTabGroupsUpdate(groupId, {
    color: AGENT_GROUP_COLOR,
    title: AGENT_GROUP_TITLE,
  });
  return groupId;
}

// Serialized per windowId; rejects on Chrome API failure. Callers that must
// not fail use addTabToAgentGroup instead.
export function ensureAgentGroup(
  windowId: number,
  tabId: number,
): Promise<number> {
  return getQueue(windowId).enqueue(tabId);
}

// Group a freshly created tab into the window's agent group. Never throws:
// grouping is cosmetic, so a failure logs and leaves the tab ungrouped
// rather than failing tab:new.
export async function addTabToAgentGroup(
  tabId: number,
  windowId: number,
): Promise<void> {
  try {
    await ensureAgentGroup(windowId, tabId);
  } catch (error) {
    console.error('browser-bridge: failed to group agent tab', error);
  }
}

// Group ids of every agent group across all windows — the tab:list handler
// marks entries by membership in this set.
export async function queryAgentGroupIds(): Promise<Set<number>> {
  const groups = await trackedTabGroupsQuery({
    color: AGENT_GROUP_COLOR,
    title: AGENT_GROUP_TITLE,
  });
  return new Set(groups.map((group) => group.id));
}

// Prune queue entries for windows Chrome has closed. Without this listener
// the Map grows for the lifetime of the service worker (Chrome may keep
// the SW alive for hours, not just the documented ~30s idle); each entry
// holds a WindowGroupQueue with a tail promise — a slow leak proportional
// to the user's window turnover.
chrome.windows.onRemoved.addListener((windowId: number) => {
  groupQueuesByWindow.delete(windowId);
});
