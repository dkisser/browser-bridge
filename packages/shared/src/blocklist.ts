// Built-in blocklist: origins the agent may never touch. Hard-denied with
// 'origin_blocked' — no approval card, no grant path.
//
// Entries are bare hostnames ('example.com' also matches subdomains) or full
// origins ('https://evil.example'). Keep this list to things that change
// browser configuration or are malicious by construction; ordinary sites are
// the user's choice via origin approval.
export const BUILT_IN_BLOCKED_ENTRIES: readonly string[] = [
  // Installing extensions reconfigures the browser; the web store is a
  // normal https page where content scripts run, so it must be denied here.
  'chromewebstore.google.com',
  'chrome.google.com',
];

export function blocklistHit(
  origin: string | null,
  extraEntries: readonly string[] = [],
): boolean {
  if (origin === null) return false;
  let host: string;
  try {
    host = new URL(origin).hostname;
  } catch {
    return false;
  }
  for (const entry of [...BUILT_IN_BLOCKED_ENTRIES, ...extraEntries]) {
    if (entry.includes('://')) {
      if (origin === entry) return true;
      continue;
    }
    if (host === entry || host.endsWith(`.${entry}`)) return true;
  }
  return false;
}
