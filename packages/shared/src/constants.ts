export const WEBSOCKET_PORT = 3001;
export const LOCAL_WS_PORT = 3002;

// MCP tool outputs land in the model's context. Reading tools refuse
// results past this size and tell the caller to narrow with a selector —
// an unguarded whole-page read dumps hundreds of KB of chrome there.
export const MAX_READ_RESULT_CHARS = 100_000;
