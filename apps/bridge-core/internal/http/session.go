package http

import "sync"

// sessionStoreCap caps the number of pinned-browser entries a long-running
// launchd-supervised daemon will hold. MCP Streamable HTTP session ids are
// opaque to us; clients can mint fresh sessions in tight loops and the TS
// reference (createBrowserSessionStore in src/mcp/browser-session.ts) never
// evicted either. Without this cap an MCP client that reconnects and re-pins
// browsers for hours grows the map without bound. 1024 is well past any
// realistic per-launch session count; once exceeded we evict a single
// arbitrary entry per insert. Eviction is approximate-LRU — we do not track
// access order — but the cap only kicks in under sustained churn and the map
// is small enough that the choice is not observable to clients.
const sessionStoreCap = 1024

// sessionStore is createBrowserSessionStore in src/mcp/browser-session.ts:
// it remembers which browserId a MCP session pinned with set_browser. The TS
// store also carries defaultTimeoutMs per session; here that value is
// server-wide, so only the browserId is tracked.
type sessionStore struct {
	mu      sync.Mutex
	browser map[string]string
}

func newSessionStore() *sessionStore {
	return &sessionStore{browser: make(map[string]string, sessionStoreCap)}
}

// get returns the browserId pinned for the session, or "" if none.
func (s *sessionStore) get(sessionID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.browser[sessionID]
}

// set pins browserId for the session (set_browser). When inserting a new
// key would push the map past sessionStoreCap, one existing entry is
// evicted to make room; re-setting an existing key never evicts.
func (s *sessionStore) set(sessionID, browserID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.browser[sessionID]; !exists && len(s.browser) >= sessionStoreCap {
		for k := range s.browser {
			delete(s.browser, k)
			break
		}
	}
	s.browser[sessionID] = browserID
}
