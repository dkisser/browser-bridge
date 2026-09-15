// Shared description fragments for MCP tools. Keep these short: they are
// read by the model when deciding which tool to call, and they encode the
// discovery workflow (list browsers first, then list tabs) so the first
// call lands on list_browsers/tab_list instead of a guessed id.

export const TAB_ID_GUIDANCE =
  'Requires a valid tab_id — call tab_list first to discover open tabs.';
