export const WEBSOCKET_PORT = 3001;
export const LOCAL_WS_PORT = 3002;

// The loopback host the local proxy binds and is reached at.
//
// One declaration on purpose. The extension used to dial `localhost` while
// the settings tab displayed `127.0.0.1` — two literals for one address, in
// two files, neither referring to the other, so they drifted silently and
// the page told the user a URL the extension never opens. The server binds
// 127.0.0.1 exactly (app.DefaultBrowserHost), so that is what goes here: it
// is the address that is guaranteed to be listening, whereas `localhost` can
// resolve to ::1 first and miss an IPv4-only bind.
export const LOCAL_HOST = '127.0.0.1';

// Sentinel thrown by the content script when its execution-point recheck
// finds the target field sensitive after the policy preflight cleared it
// (TOCTOU between preflight and execution). The service worker turns this
// into an approval_required policy denial.
export const SENSITIVE_FIELD_RECHECK_ERROR = 'bb_sensitive_field_at_execution';

// MCP tool outputs land in the model's context. Reading tools refuse
// results past this size and tell the caller to narrow with a selector —
// an unguarded whole-page read dumps hundreds of KB of chrome there.
export const MAX_READ_RESULT_CHARS = 100_000;
