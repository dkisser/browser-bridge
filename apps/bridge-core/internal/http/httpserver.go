package http

import (
	"net"
	nethttp "net/http"
	"sync"
)

// Tracker provides connection tracking for the control plane's HTTP servers,
// attached to a net/http Server via its ConnState hook. Server.Shutdown alone
// is not enough: it waits up to 5 seconds for never-used StateNew keep-alive
// connections (Go issue 22682) and does not touch hijacked WebSocket
// connections at all. Tracking the connections and closing them ourselves
// keeps shutdown prompt.
type Tracker struct {
	mu    sync.Mutex
	conns map[net.Conn]struct{}
}

func NewTracker() *Tracker {
	return &Tracker{conns: make(map[net.Conn]struct{})}
}

// ConnState implements the net/http Server.ConnState hook. Hijacked
// connections (the WebSocket upgrades) are untracked: their owners close
// them.
func (t *Tracker) ConnState(conn net.Conn, state nethttp.ConnState) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch state {
	case nethttp.StateNew, nethttp.StateActive:
		t.conns[conn] = struct{}{}
	case nethttp.StateHijacked, nethttp.StateClosed:
		delete(t.conns, conn)
	}
}

// CloseAll closes every tracked connection.
func (t *Tracker) CloseAll() {
	t.mu.Lock()
	conns := make([]net.Conn, 0, len(t.conns))
	for c := range t.conns {
		conns = append(conns, c)
	}
	t.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}
