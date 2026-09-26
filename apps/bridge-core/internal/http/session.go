package http

import "sync"

// sessionStore is createBrowserSessionStore in src/mcp/browser-session.ts:
// it remembers which browserId a MCP session pinned with set_browser. The TS
// store also carries defaultTimeoutMs per session; here that value is
// server-wide, so only the browserId is tracked. Like the TS Map, entries
// are never evicted (sessions are short-lived HTTP connections).
type sessionStore struct {
	mu      sync.Mutex
	browser map[string]string
}

func newSessionStore() *sessionStore {
	return &sessionStore{browser: make(map[string]string)}
}

// get returns the browserId pinned for the session, or "" if none.
func (s *sessionStore) get(sessionID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.browser[sessionID]
}

// set pins browserId for the session (set_browser).
func (s *sessionStore) set(sessionID, browserID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.browser[sessionID] = browserID
}
