import type { Denial } from './policy';
import type { SnapshotResult } from './snapshot';

export interface Envelope {
  id: string;
  type: 'command' | 'response' | 'event';
  browserId: string;
  payload: unknown;
  timestamp: number;
}

export type BrowserStatus = 'online' | 'idle_wait' | 'offline';

export type CommandType =
  | 'navigate'
  | 'goBack'
  | 'goForward'
  | 'refresh'
  | 'tab:list'
  | 'tab:new'
  | 'tab:close'
  | 'tab:switch'
  | 'click'
  | 'type'
  | 'select'
  | 'scroll'
  | 'hover'
  | 'gettext'
  | 'gethtml'
  | 'snapshot'
  | 'screenshot'
  | 'pageinfo'
  | 'wait:element'
  | 'wait:navigation';

export interface CommandPayload {
  command: CommandType;
  tabId: number;
  params: Record<string, unknown>;
}

// Machine-readable reason for a content-script dispatch failure. Defined
// here (rather than in the extension) so the MCP layer can branch on it
// without importing extension code. Keep in sync with
// `ContentScriptUnavailableReason` in `apps/extension/src/content-bridge.ts`.
export type ContentScriptUnavailableReason =
  | 'tab_not_found'
  | 'restricted_page'
  | 'injection_failed'
  | 'no_listener';

export interface ResponsePayload {
  status: 'ok' | 'error';
  data?: unknown;
  error?: string;
  message?: string;
  // Structured policy denial (ADR-0008). Present alongside status 'error'
  // when the extension's policy gate rejected the command; `error` carries
  // the machine-readable DenyReason for clients that only read strings.
  denied?: Denial;
  // Structured content-script dispatch failure. Set when the SW tried to
  // send a command to a content script and the underlying chrome.tabs API
  // rejected for one of the reasons above. `error` / `message` carry the
  // human-readable text; MCP tools should branch on `reason` for recovery
  // hints (see `withRecoveryHint` in apps/websocket/src/mcp/command-client.ts).
  reason?: ContentScriptUnavailableReason;
}

// Command result contracts. These are the single source of truth for the
// shape each command's `data` payload: the extension handlers are annotated
// against CommandResultMap, and MCP tools read `data` through these types.
// Keep them in sync with the extension handlers — tsc enforces both sides.
export interface OkResult {
  ok: true;
}

export interface UrlTitleResult {
  url?: string;
  title?: string;
}

export interface GettextResult {
  text: string | null;
}

export interface GethtmlResult {
  html: string;
}

export interface ScreenshotResult {
  dataUrl: string;
}

export interface PageinfoResult {
  id?: number;
  url?: string;
  title?: string;
  active: boolean;
}

export interface TabListResultItem {
  id?: number;
  url?: string;
  title?: string;
  active: boolean;
  windowId: number;
  // True when the tab sits in the window's "browser-bridge" tab group
  // (ADR-0014). Visual organization only — not a trust signal.
  inAgentGroup: boolean;
}

export interface TabNewResult {
  id?: number;
  url?: string;
}

export interface TabSwitchResult {
  id?: number;
  url?: string;
  title?: string;
}

export interface ClickResult {
  clicked: string;
}

export interface TypeResult {
  typed: string;
}

export interface SelectResult {
  selected: string;
}

export interface ScrollResult {
  scrolled: true;
}

export interface HoverResult {
  hovered: string;
}

export interface WaitElementResult {
  found: true;
  selector: string;
}

export interface CommandResultMap {
  navigate: UrlTitleResult;
  goBack: OkResult;
  goForward: OkResult;
  refresh: OkResult;
  'tab:list': TabListResultItem[];
  'tab:new': TabNewResult;
  'tab:close': OkResult;
  'tab:switch': TabSwitchResult;
  click: ClickResult;
  type: TypeResult;
  select: SelectResult;
  scroll: ScrollResult;
  hover: HoverResult;
  gettext: GettextResult;
  gethtml: GethtmlResult;
  snapshot: SnapshotResult;
  screenshot: ScreenshotResult;
  pageinfo: PageinfoResult;
  'wait:element': WaitElementResult;
  'wait:navigation': UrlTitleResult;
}

export type DomCommandType =
  | 'click'
  | 'type'
  | 'select'
  | 'scroll'
  | 'hover'
  | 'gettext'
  | 'gethtml'
  | 'snapshot'
  | 'wait:element';

export type DomCommandResult = CommandResultMap[DomCommandType];

export interface BrowserConnection {
  browserId: string;
  userId: string;
  status: BrowserStatus;
  lastSeen: number;
}
