package core

import (
	"encoding/json"
	"log"
	"sync"
	"time"
)

// defaultRouteTTL is the default upper bound on how long an inbound route can
// sit in inboundByID after a successful SendToExtension. The TS original had
// no such bound — the response path was the only cleanup — which leaks one
// entry every time the extension fails to answer a command it acknowledged.
//
// It is a *backstop*, not the command's deadline: a sender that owns its own
// timeout (the MCP layer does) removes its own route via RemoveRoute. Callers
// that can legitimately wait longer than the backstop pass their own deadline
// with WithRouteDeadline; see armRouteTimer. 30s comfortably covers the
// daemon's MCP defaultTimeout (10s) for callers that do not.
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
	// pending mirrors inboundByID again, and holds what the *response* path
	// needs to know about the command: which command it was and which tab it
	// was aimed at. A response envelope carries the originating envelope's id
	// and nothing else, so without this the only way to know whether a
	// response was a snapshot or a get_text is to have kept the command
	// somewhere — and the response path is where the Memory hook is told
	// (ADR-0018 captures at the router, the one choke both Inbound adapters
	// pass through).
	pending map[string]pendingCall
	// mem is the optional self-learning collaborator. Nil disables every
	// memory feature; nothing below may assume it is set.
	mem MemoryHook

	// routeTTL is exposed for tests; production uses defaultRouteTTL.
	routeTTL time.Duration
}

// pendingCall is the part of a command a response needs to be understood.
type pendingCall struct {
	command string
	tabID   int
}

type RouterOption func(*Router)

// WithRouteTTL overrides defaultRouteTTL (tests).
func WithRouteTTL(d time.Duration) RouterOption {
	return func(r *Router) { r.routeTTL = d }
}

// WithMemoryHook attaches the self-learning store (ADRs 0018-0020). Without it
// the router behaves exactly as it did before: memory is an opt-in capability
// of a control plane that must keep working when nothing is learned.
func WithMemoryHook(h MemoryHook) RouterOption {
	return func(r *Router) { r.mem = h }
}

func NewRouter(st *StateManager, browser Browser, reg StatusRegistry, logger *log.Logger, opts ...RouterOption) *Router {
	r := &Router{
		state:         st,
		browser:       browser,
		registry:      reg,
		logger:        logger,
		inboundByID:   make(map[string]TextSender),
		inboundTimers: make(map[string]*time.Timer),
		pending:       make(map[string]pendingCall),
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

// InboundOption customizes how one inbound command is dispatched. Variadic so
// callers that own no deadline (the inbound WebSocket server) keep the
// defaultRouteTTL backstop without having to say so.
type InboundOption func(*inboundOptions)

type inboundOptions struct {
	// routeTTL overrides the backstop for this command only. 0 means
	// "use the router's default". A value smaller than the default is
	// raised to it (see armRouteTTL): the backstop exists to protect
	// senders that never time out, and shortening it is the caller's
	// business, not the router's.
	routeTTL time.Duration
}

// WithRouteDeadline tells the router how long the sender is willing to wait
// for this command's response, so the TTL backstop does not fire first.
//
// This exists because the two used to be independent and the router's was
// smaller: the MCP schema advertises timeout_ms up to 120s (schemas.go) and
// sendCommand waits that long, but every route was cut at a flat 30s — so a
// wait_element/wait_navigation/screenshot asking for 60s was terminated with
// "Service worker did not respond in time" while the extension was alive and
// still working on it.
func WithRouteDeadline(d time.Duration) InboundOption {
	return func(o *inboundOptions) { o.routeTTL = d }
}

// RouteDeadline reports the deadline carried by opts, or 0 when none was
// set. It exists so a test double implementing the router interface can
// observe what a caller asked for without duplicating the option's effect.
func RouteDeadline(opts ...InboundOption) time.Duration {
	var o inboundOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o.routeTTL
}

// HandleInboundCommand is router.handleInboundCommand: an inbound client
// (CLI / MCP) sent a command envelope.
func (r *Router) HandleInboundCommand(envelope Envelope, sender TextSender, opts ...InboundOption) {
	var o inboundOptions
	for _, opt := range opts {
		opt(&o)
	}

	call := pendingCallFrom(envelope)
	r.mu.Lock()
	r.inboundByID[envelope.ID] = sender
	r.pending[envelope.ID] = call
	r.mu.Unlock()

	// Recorded before any dispatch decision, so a command rejected by the
	// router is still on the record: "the agent asked a browser that was
	// offline" is a fact worth having (ADR-0018).
	if r.mem != nil {
		r.mem.RecordCommand(envelope.ID, call.command, call.tabID, commandParams(envelope))
	}

	if !r.state.CanAcceptCommand() {
		r.sendError(sender, "browser_offline", "Browser is offline", envelope.ID, envelope.BrowserID, call)
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
		r.armRouteTimer(envelope.ID, sender, envelope.BrowserID, o.routeTTL)
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
		r.sendError(target, "sw_timeout", "Service worker did not wake up", envelope.ID, envelope.BrowserID, call)
		r.removeInbound(envelope.ID)
	})
	if !buffered {
		r.sendError(sender, "cannot_buffer", "Cannot buffer command", envelope.ID, envelope.BrowserID, call)
		r.removeInbound(envelope.ID)
	}
}

// HandleBrowserResponse is router.handleBrowserResponse: the extension sent
// a response envelope; route it back to the originating inbound connection
// by envelope id.
func (r *Router) HandleBrowserResponse(envelope Envelope) {
	target, call, ok := r.takeInbound(envelope.ID)
	if !ok {
		return
	}
	if r.mem != nil {
		r.recordResult(envelope, call)
	}
	text, err := Encode(envelope.Type, envelope.Payload, envelope.ID, envelope.BrowserID)
	if err != nil {
		r.logger.Printf("encode response envelope: %v", err)
		return
	}
	target.Send(text)
}

// recordResult hands the memory hook the response payload. A payload that does
// not parse is skipped rather than dropped: the hook must never be the reason a
// response is not delivered.
func (r *Router) recordResult(envelope Envelope, call pendingCall) {
	var payload ResponsePayload
	if len(envelope.Payload) > 0 {
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			r.logger.Printf("memory: decode response payload: %v", err)
			return
		}
	}
	r.mem.RecordResult(envelope.ID, call.command, call.tabID, payload)
}

// pendingCallFrom reads the command name and target tab out of a command
// envelope's payload. The payload is `{command, tabId, params}` — the same
// shape on both adapters (internal/http/command.go builds it, and the inbound
// WebSocket relays what the CLI sent) — so one decoder serves both.
func pendingCallFrom(envelope Envelope) pendingCall {
	if len(envelope.Payload) == 0 {
		return pendingCall{}
	}
	var payload CommandPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return pendingCall{}
	}
	return pendingCall{command: payload.Command, tabID: payload.TabID}
}

func commandParams(envelope Envelope) map[string]any {
	if len(envelope.Payload) == 0 {
		return nil
	}
	var payload CommandPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return nil
	}
	return payload.Params
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
			delete(r.pending, id)
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
// Returns the sender, the command the route was created for, and ok=true on
// hit; ok=false when the id is unknown (already cleaned up by the timer or by
// RemoveRoute).
func (r *Router) takeInbound(id string) (TextSender, pendingCall, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.inboundByID[id]
	if !ok {
		return nil, pendingCall{}, false
	}
	call := r.pending[id]
	delete(r.inboundByID, id)
	delete(r.pending, id)
	if t, timerOK := r.inboundTimers[id]; timerOK {
		t.Stop()
		delete(r.inboundTimers, id)
	}
	return s, call, true
}

func (r *Router) removeInbound(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.inboundByID, id)
	delete(r.pending, id)
	if t, ok := r.inboundTimers[id]; ok {
		t.Stop()
		delete(r.inboundTimers, id)
	}
}

// routeBackstopMargin is how far past a caller's stated deadline the route
// backstop is scheduled.
//
// The backstop must not merely equal the caller's timeout, it must exceed it.
// Both timers are armed from the same goroutine with the same instant — the
// router's time.AfterFunc first, the caller's time.After second — so at
// equal deadlines the outcome is a race, and when the backstop wins the
// caller is told "Service worker did not respond in time", blaming a service
// worker that is in fact still working, instead of its own accurate
// "timeout: no response for command X within Nms". The margin makes the
// caller's own deadline deterministically win and leaves the backstop doing
// only its real job: releasing a route whose sender never cleaned up.
const routeBackstopMargin = time.Second

// armRouteTimer schedules the TTL cleanup for a successful SendToExtension.
// On fire the route is removed and the caller is told via sw_timeout, so
// the inbound client (CLI / MCP) does not hang forever.
//
// requested is the sender's own deadline (0 when it has none). The effective
// TTL is max(default, requested+margin) so the backstop can only ever be
// *longer* than what the sender asked for: a backstop that fires before the
// caller's own deadline would report a timeout for a command that is still
// running, with the wrong explanation.
func (r *Router) armRouteTimer(id string, sender TextSender, browserID string, requested time.Duration) {
	ttl := r.routeTTL
	if requested > 0 && requested+routeBackstopMargin > ttl {
		ttl = requested + routeBackstopMargin
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.inboundTimers[id]; ok {
		existing.Stop()
	}
	r.inboundTimers[id] = time.AfterFunc(ttl, func() {
		r.fireRouteTimeout(id, sender, browserID)
	})
}

// fireRouteTimeout is the AfterFunc callback. It must not hold r.mu when
// it touches sendError (which itself touches the sender and logs), so it
// drops the lock between delete + send.
func (r *Router) fireRouteTimeout(id string, sender TextSender, browserID string) {
	r.mu.Lock()
	_, stillTracked := r.inboundByID[id]
	call := r.pending[id]
	delete(r.inboundByID, id)
	delete(r.pending, id)
	delete(r.inboundTimers, id)
	r.mu.Unlock()
	if !stillTracked {
		// Someone else (RemoveRoute, HandleBrowserResponse, RemoveClient)
		// already cleaned this up.
		return
	}
	r.sendError(sender, "sw_timeout", "Service worker did not respond in time", id, browserID, call)
}

// sendError renders and sends the TS encode('response', {status, error,
// message}, {id, browserId}) shape. Field order (status, error, message)
// matches the TS object literals.
//
// A router-generated error is recorded like any other failure. The three codes
// it produces — browser_offline, cannot_buffer, sw_timeout — are exactly the
// cases where the agent asked for something the browser could not do, which is
// the pattern a Site card exists to prevent (ADR-0018).
func (r *Router) sendError(sender TextSender, errCode, message, id, browserID string, call pendingCall) {
	payload := ResponsePayload{
		Status:  "error",
		Error:   errCode,
		Message: message,
	}
	if r.mem != nil {
		r.mem.RecordResult(id, call.command, call.tabID, payload)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		r.logger.Printf("encode error payload: %v", err)
		return
	}
	text, err := Encode(TypeResponse, raw, id, browserID)
	if err != nil {
		r.logger.Printf("encode error envelope: %v", err)
		return
	}
	sender.Send(text)
}
