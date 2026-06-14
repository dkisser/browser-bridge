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
  | 'screenshot'
  | 'pageinfo'
  | 'wait:element'
  | 'wait:navigation';

export interface CommandPayload {
  command: CommandType;
  tabId?: number;
  params: Record<string, unknown>;
}

export interface ResponsePayload {
  status: 'ok' | 'error';
  data?: unknown;
  error?: string;
  message?: string;
}

export interface BrowserConnection {
  browserId: string;
  userId: string;
  status: BrowserStatus;
  lastSeen: number;
}
