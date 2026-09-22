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

export interface ResponsePayload {
  status: 'ok' | 'error';
  data?: unknown;
  error?: string;
  message?: string;
  // Structured policy denial (ADR-0008). Present alongside status 'error'
  // when the extension's policy gate rejected the command; `error` carries
  // the machine-readable DenyReason for clients that only read strings.
  denied?: Denial;
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
