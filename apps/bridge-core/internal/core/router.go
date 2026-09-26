package core

import (
	"encoding/json"
	"log"
	"sync"
	"time"
)

// defaultRouteTTL is the upper bound on how long an inbound route can sit in
// inboundByID after a successful SendToExtension. The TS original had no such
// bound — the response path was the only cleanup — which leaks one entry
// every time the extension fails to answer a command it acknowledged. The
// 30s window matches the launchd-supervised daemon's MCP defaultTimeout (10s)
// plus a comfortable fudge for slow extensions under heavy load.
const defaultRouteTTL = 30 * time.Second

// Browser is the browser-server half the router talks to (BrowserServer in
// router.ts).
type Browser interface {
	HasExtension() bool
	SendToExtension(text string) bool
}

// StatusRegistry is the slice of core.Registry the router writes.
type StatusRegistry interface {
	SetStatus(browserID string, status BrowserStatus) bool
}

// Router routes envelopes between inbound clients and the extension — the Go
// port of src/router.ts, coordinating the in-process flow between the
// inbound server (CLI/MCP) and the browser server (extension). Command
// envelopes from inbound clients are dispatched to the browser connection;
// responses are routed back to the original requester by envelope id; while
// the extension is between reconnects a command is buffered for 5 seconds so
// a fast reconnect picks it up. Safe for concurrent use (the TS original
// relied on the event loop).
type Router struct {
	state    *StateManager
	browser  Browser
	registry StatusRegistry
	logger   *log.Logger

	mu sync.Mutex
	// inboundByID tracks which inbound connection submitted which envelope
	// id, so a response coming back from the extension is routed to the exact
	// caller instead of being broadcast (which is what the pre-merge
	// ws-server did).
	inboundByID map[string]TextSender
	// inboundTimers mirrors inboundByID: each entry's TTL fires
	// defaultRouteTTL after a successful SendToExtension so a hung
	// extension cannot pin a sender forever. HandleBrowserResponse stops
	// the timer when the response lands.
	inboundTimers map[string]*time.Timer

	// routeTTL is exposed for tests; production uses defaultRouteTTL.
	routeTTL time.Duration
}

type RouterOption func(*Router)

// WithRouteTTL overrides defaultRouteTTL (tests).
func WithRouteTTL(d time.Duration) RouterOption {
	return func(r *Router) { r.routeTTL = d }
}

func NewRouter(st *StateManager, browser Browser, reg StatusRegistry, logger *log.Logger, opts ...RouterOption) *Router {
	r := &Router{
		state:         st,
		browser:       browser,
		registry:      reg,
		logger:        logger,
		inboundByID:   make(map[string]TextSender),
		inboundTimers: make(map[string]*time.Timer),
		routeTTL:      defaultRouteTTL,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func (r *Router) BrowserID() string {
	return r.state.BrowserID()
}

// HandleInboundCommand is router.handleInboundCommand: an inbound client
// (CLI / MCP) sent a command envelope.
func (r *Router) HandleInboundCommand(envelope Envelope, sender TextSender) {
	r.mu.Lock()
	r.inboundByID[envelope.ID] = sender
	r.mu.Unlock()

	if !r.state.CanAcceptCommand() {
		r.sendError(sender, "browser_offline", "Browser is offline", envelope.ID, envelope.BrowserID)
		r.removeInbound(envelope.ID)
		return
	}

	if r.browser.HasExtension() {
		text, err := Encode(envelope.Type, envelope.Payload, envelope.ID, envelope.BrowserID)
		if err != nil {
			r.logger.Printf("encode command envelope: %v", err)
			// Without this the entry leaks forever — the response path
			// (HandleBrowserResponse) is the only normal cleanup, so an
			// Encode failure here pins a sender in inboundByID until the
			// process restarts.
			r.removeInbound(envelope.ID)
			return
		}
		if sent := r.browser.SendToExtension(text); !sent {
			// The extension disappeared between HasExtension and
			// SendToExtension (SendToExtension returns false on no tracked
			// connection). The response path will never fire, so we have to
			// drop the route ourselves or the sender is pinned forever.
			r.removeInbound(envelope.ID)
			return
		}
		// Success path: the extension accepted the frame but might never
		// answer. Arm a deadline so a hung extension cannot pin the sender
		// forever; HandleBrowserResponse cancels it on the happy path.
		r.armRouteTimer(envelope.ID, sender, envelope.BrowserID)
		return
	}

	// Buffer once; reject re-buffers within the budget.
	text, err := Encode(envelope.Type, envelope.Payload, envelope.ID, envelope.BrowserID)
	if err != nil {
		r.logger.Printf("encode command envelope: %v", err)
		r.removeInbound(envelope.ID)
		return
	}
	buffered := r.state.BufferCommand(text, func() {
		target := r.lookupInbound(envelope.ID)
		if target == nil {
			return
		}
		r.sendError(target, "sw_timeout", "Service worker did not wake up", envelope.ID, envelope.BrowserID)
		r.removeInbound(envelope.ID)
	})
	if !buffered {
		r.sendError(sender, "cannot_buffer", "Cannot buffer command", envelope.ID, envelope.BrowserID)
		r.removeInbound(envelope.ID)
	}
}

// HandleBrowserResponse is router.handleBrowserResponse: the extension sent
// a response envelope; route it back to the originating inbound connection
// by envelope id.
func (r *Router) HandleBrowserResponse(envelope Envelope) {
	target, ok := r.takeInbound(envelope.ID)
	if !ok {
		return
	}
	text, err := Encode(envelope.Type, envelope.Payload, envelope.ID, envelope.BrowserID)
	if err != nil {
		r.logger.Printf("encode response envelope: %v", err)
		return
	}
	target.Send(text)
}

// RemoveClient is the inbound-side disconnect hook: every route pointing at
// sender is dropped, plus its TTL timer is stopped. The CLI disconnect path
// used to leak one entry per kill -9 / unexpected close — the inbound
// WebSocket's defer fired but the router never knew, so the sender stayed
// pinned until the extension answered (which can be never).
func (r *Router) RemoveClient(sender TextSender) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, s := range r.inboundByID {
		if s == sender {
			delete(r.inboundByID, id)
			if t, ok := r.inboundTimers[id]; ok {
				t.Stop()
				delete(r.inboundTimers, id)
			}
		}
	}
}

// RemoveRoute is the single-id cleanup used by sendCommand when its context
// or timeout fires. Equivalent to the internal removeInbound; exported so the
// MCP layer can call it.
func (r *Router) RemoveRoute(id string) {
	r.removeInbound(id)
}

// HandleBrowserEvent is router.handleBrowserEvent: the extension sent an
// event envelope (register / online / offline); update the registry.
func (r *Router) HandleBrowserEvent(envelope Envelope) {
	var event struct {
		Event     string `json:"event"`
		BrowserID string `json:"browserId"`
	}
	if err := json.Unmarshal(envelope.Payload, &event); err != nil {
		return
	}
	browserID := event.BrowserID
	if browserID == "" {
		browserID = r.state.BrowserID()
	}
	if browserID == "" {
		return
	}

	switch event.Event {
	case "register":
		// The extension identifies itself for routing.
		r.registry.SetStatus(browserID, StatusOnline)
	case "online":
		r.registry.SetStatus(browserID, StatusOnline)
	case "offline":
		r.registry.SetStatus(browserID, StatusOffline)
	}
}

// HandleBrowserConnect is router.handleBrowserConnect: the extension WS
// upgrade succeeded.
func (r *Router) HandleBrowserConnect() {
	// Read the buffer *before* flipping status. StateManager's status setter
	// clears any buffered command and cancels its timeout as soon as the
	// status leaves idle_wait, so reading afterwards always yields nothing
	// and the command is silently dropped — neither forwarded nor answered,
	// with the caller left to time out on its own.
	buffered, ok := r.state.GetBufferedCommand()

	r.state.SetStatus(StatusOnline)
	r.registry.SetStatus(r.state.BrowserID(), StatusOnline)

	if ok {
		r.browser.SendToExtension(buffered)
	}
}

// HandleBrowserDisconnect is router.handleBrowserDisconnect: the extension
// WS closed — go back to idle_wait so the next command buffers.
func (r *Router) HandleBrowserDisconnect() {
	r.state.SetStatus(StatusIdleWait)
	r.registry.SetStatus(r.state.BrowserID(), StatusOffline)
}

func (r *Router) lookupInbound(id string) TextSender {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.inboundByID[id]
}

// takeInbound atomically reads + removes a route and cancels its TTL timer.
// Returns the sender and ok=true on hit; ok=false when the id is unknown
// (already cleaned up by the timer or by RemoveRoute).
func (r *Router) takeInbound(id string) (TextSender, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.inboundByID[id]
	if !ok {
		return nil, false
	}
	delete(r.inboundByID, id)
	if t, timerOK := r.inboundTimers[id]; timerOK {
		t.Stop()
		delete(r.inboundTimers, id)
	}
	return s, true
}

func (r *Router) removeInbound(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.inboundByID, id)
	if t, ok := r.inboundTimers[id]; ok {
		t.Stop()
		delete(r.inboundTimers, id)
	}
}

// armRouteTimer schedules the TTL cleanup for a successful SendToExtension.
// On fire the route is removed and the caller is told via sw_timeout, so
// the inbound client (CLI / MCP) does not hang forever.
func (r *Router) armRouteTimer(id string, sender TextSender, browserID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.inboundTimers[id]; ok {
		existing.Stop()
	}
	r.inboundTimers[id] = time.AfterFunc(r.routeTTL, func() {
		r.fireRouteTimeout(id, sender, browserID)
	})
}

// fireRouteTimeout is the AfterFunc callback. It must not hold r.mu when
// it touches sendError (which itself touches the sender and logs), so it
// drops the lock between delete + send.
func (r *Router) fireRouteTimeout(id string, sender TextSender, browserID string) {
	r.mu.Lock()
	_, stillTracked := r.inboundByID[id]
	delete(r.inboundByID, id)
	delete(r.inboundTimers, id)
	r.mu.Unlock()
	if !stillTracked {
		// Someone else (RemoveRoute, HandleBrowserResponse, RemoveClient)
		// already cleaned this up.
		return
	}
	r.sendError(sender, "sw_timeout", "Service worker did not respond in time", id, browserID)
}

// sendError renders and sends the TS encode('response', {status, error,
// message}, {id, browserId}) shape. Field order (status, error, message)
// matches the TS object literals.
func (r *Router) sendError(sender TextSender, errCode, message, id, browserID string) {
	payload, err := json.Marshal(ResponsePayload{
		Status:  "error",
		Error:   errCode,
		Message: message,
	})
	if err != nil {
		r.logger.Printf("encode error payload: %v", err)
		return
	}
	text, err := Encode(TypeResponse, payload, id, browserID)
	if err != nil {
		r.logger.Printf("encode error envelope: %v", err)
		return
	}
	sender.Send(text)
}
