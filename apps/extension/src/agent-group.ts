// Agent tab group (ADR-0014): every tab opened via tab:new is placed into a
// per-window Chrome tab group titled "browser-bridge" (orange) so the human
// can see at a glance which tabs the agent is driving. Purely visual — group
// membership never feeds policy decisions (agentTabs remains the trust
// signal). Group identity is discovered by title+color query because
// groupIds are unstable across sessions; if the user renamed, recolored, or
// ungrouped, the query misses and a fresh group is created on the next
// tab:new — the user's customization is never fought.

export const AGENT_GROUP_TITLE = 'browser-bridge';
export const AGENT_GROUP_COLOR: chrome.tabGroups.ColorEnum = 'orange';

// Per-window promise chains: concurrent tab:new calls in the same window
// serialize here, so two of them cannot both miss the query and create
// duplicate groups. Entries are dropped once settled so closed windows do
// not accumulate.
const groupOpsByWindow = new Map<number, Promise<unknown>>();

async function queryAgentGroupId(windowId: number): Promise<number | null> {
  const groups = await chrome.tabGroups.query({
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
  await chrome.tabGroups.update(groupId, {
    color: AGENT_GROUP_COLOR,
    title: AGENT_GROUP_TITLE,
  });
  return groupId;
}

// Serialized per windowId; rejects on Chrome API failure (callers that must
// not fail use addTabToAgentGroup).
export function ensureAgentGroup(
  windowId: number,
  tabId: number,
): Promise<number> {
  const previous = groupOpsByWindow.get(windowId) ?? Promise.resolve();
  const operation = previous
    .catch(() => {})
    .then(() => ensureAgentGroupInner(windowId, tabId));
  groupOpsByWindow.set(windowId, operation);
  return operation.finally(() => {
    if (groupOpsByWindow.get(windowId) === operation) {
      groupOpsByWindow.delete(windowId);
    }
  });
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
  const groups = await chrome.tabGroups.query({
    color: AGENT_GROUP_COLOR,
    title: AGENT_GROUP_TITLE,
  });
  return new Set(groups.map((group) => group.id));
}
