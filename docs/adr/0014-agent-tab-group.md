# Agent tab group

Users need to see at a glance which tabs the agent is operating. The extension already tracks agent-opened tabs (`agentTabs`) for policy (ADR-0007); on top of that, every tab opened via `tab:new` is now placed into a per-window Chrome tab group titled "browser-bridge" (color orange). Group identity is discovered by `chrome.tabGroups.query({title, color, windowId})` because groupIds are unstable across sessions; a miss creates a fresh group. The group is purely visual/lifecycle organization and is explicitly not a trust signal: group membership never feeds policy decisions, and `agentTabs` is never rebuilt from group membership after a service-worker restart. User customizations are not fought — if the human renames, recolors, or ungroups, the query simply misses and the next `tab:new` creates a fresh group. No new MCP tools; `tab_list` gains an `inAgentGroup` field per entry.

## Consequences

- New `tabGroups` manifest permission. Grouping failure never fails `tab:new` — the tab is left ungrouped and the error is logged.
- Working scope (ADR-0007) remains the only trust boundary; the orange group carries no policy meaning.
- SKILL.md and the `tab_new` / `tab_list` tool descriptions document the behavior (ADR-0003).
