package core

import (
	"sync"
	"time"
)

// extensionUserID is the userId stamped on lazily created entries
// ('extension' in registry.ts).
const extensionUserID = "extension"

type entry struct {
	browserID string
	userID    string
	status    BrowserStatus
	lastSeen  int64
}

// Registry tracks per-browser status — the Go port of src/server/registry.ts:
// browserId-keyed connection bookkeeping. Post-merge entries are created
// lazily on the first status update (the extension connects straight to the
// browser server, so there is no register handshake); the tracked WebSocket
// handle from the pre-merge design is gone — the extension connection lives
// in the ws package (BrowserServer). Safe for concurrent use.
type Registry struct {
	mu      sync.Mutex
	entries map[string]*entry
	order   []string // insertion order, mirroring the TS Map iteration order
}

func NewRegistry() *Registry {
	return &Registry{entries: make(map[string]*entry)}
}

// SetStatus updates the entry for browserID, creating it lazily on first
// sight (registry.ts: the bridge-core merge dropped the explicit register
// handshake, so the router writes the entry on the first status update).
func (r *Registry) SetStatus(browserID string, status BrowserStatus) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UnixMilli()
	e, ok := r.entries[browserID]
	if !ok {
		r.entries[browserID] = &entry{
			browserID: browserID,
			userID:    extensionUserID,
			status:    status,
			lastSeen:  now,
		}
		r.order = append(r.order, browserID)
		return true
	}
	e.status = status
	e.lastSeen = now
	return true
}

// GetStatus returns the status and whether the registry has ever seen the
// browser (registry.ts returns undefined for unknown ids, and the inbound
// server rejects only those).
func (r *Registry) GetStatus(browserID string) (BrowserStatus, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[browserID]
	if !ok {
		return "", false
	}
	return e.status, true
}

// ListBrowsers returns the list_browsers payload shape. The result is never
// nil so it marshals as [] rather than null.
func (r *Registry) ListBrowsers() []BrowserConnection {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]BrowserConnection, 0, len(r.entries))
	for _, id := range r.order {
		e := r.entries[id]
		out = append(out, BrowserConnection{
			BrowserID: e.browserID,
			UserID:    e.userID,
			Status:    e.status,
			LastSeen:  e.lastSeen,
		})
	}
	return out
}
