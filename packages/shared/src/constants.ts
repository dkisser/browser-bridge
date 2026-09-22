export const WEBSOCKET_PORT = 3001;
export const LOCAL_WS_PORT = 3002;

// Sentinel thrown by the content script when its execution-point recheck
// finds the target field sensitive after the policy preflight cleared it
// (TOCTOU between preflight and execution). The service worker turns this
// into an approval_required policy denial.
export const SENSITIVE_FIELD_RECHECK_ERROR = 'bb_sensitive_field_at_execution';

// MCP tool outputs land in the model's context. Reading tools refuse
// results past this size and tell the caller to narrow with a selector —
// an unguarded whole-page read dumps hundreds of KB of chrome there.
export const MAX_READ_RESULT_CHARS = 100_000;
