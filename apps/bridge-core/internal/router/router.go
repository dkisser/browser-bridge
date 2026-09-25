// Package router is the Go port of src/router.ts: it coordinates the
// in-process flow between the inbound server (CLI/MCP) and the browser
// server (extension). Command envelopes from inbound clients are dispatched
// to the browser connection; responses are routed back to the original
// requester by envelope id; while the extension is between reconnects a
// command is buffered for 5 seconds so a fast reconnect picks it up.
package router

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/protocol"
	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/state"
)

// Browser is the browser-server half the router talks to (BrowserServer in
// router.ts).
type Browser interface {
	HasExtension() bool
	SendToExtension(text string) bool
}

// StatusRegistry is the slice of registry.Registry the router writes.
type StatusRegistry interface {
	SetStatus(browserID string, status protocol.BrowserStatus) bool
}

// Router routes envelopes between inbound clients and the extension. Safe
// for concurrent use (the TS original relied on the event loop).
type Router struct {
	state    *state.Manager
	browser  Browser
	registry StatusRegistry
	logger   *log.Logger

	mu sync.Mutex
	// inboundByID tracks which inbound connection submitted which envelope
	// id, so a response coming back from the extension is routed to the exact
	// caller instead of being broadcast (which is what the pre-merge
	// ws-server did).
	inboundByID map[string]protocol.TextSender
}

func New(st *state.Manager, browser Browser, reg StatusRegistry, logger *log.Logger) *Router {
	return &Router{
		state:       st,
		browser:     browser,
		registry:    reg,
		logger:      logger,
		inboundByID: make(map[string]protocol.TextSender),
	}
}

func (r *Router) BrowserID() string {
	return r.state.BrowserID()
}

// HandleInboundCommand is router.handleInboundCommand: an inbound client
// (CLI / MCP) sent a command envelope.
func (r *Router) HandleInboundCommand(envelope protocol.Envelope, sender protocol.TextSender) {
	r.mu.Lock()
	r.inboundByID[envelope.ID] = sender
	r.mu.Unlock()

	if !r.state.CanAcceptCommand() {
		r.sendError(sender, "browser_offline", "Browser is offline", envelope.ID, envelope.BrowserID)
		r.removeInbound(envelope.ID)
		return
	}

	if r.browser.HasExtension() {
		if text, err := protocol.Encode(envelope.Type, envelope.Payload, envelope.ID, envelope.BrowserID); err != nil {
			r.logger.Printf("encode command envelope: %v", err)
		} else {
			r.browser.SendToExtension(text)
		}
		return
	}

	// Buffer once; reject re-buffers within the budget.
	text, err := protocol.Encode(envelope.Type, envelope.Payload, envelope.ID, envelope.BrowserID)
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
func (r *Router) HandleBrowserResponse(envelope protocol.Envelope) {
	target := r.lookupInbound(envelope.ID)
	if target == nil {
		return
	}
	r.removeInbound(envelope.ID)
	text, err := protocol.Encode(envelope.Type, envelope.Payload, envelope.ID, envelope.BrowserID)
	if err != nil {
		r.logger.Printf("encode response envelope: %v", err)
		return
	}
	target.Send(text)
}

// HandleBrowserEvent is router.handleBrowserEvent: the extension sent an
// event envelope (register / online / offline); update the registry.
func (r *Router) HandleBrowserEvent(envelope protocol.Envelope) {
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
		r.registry.SetStatus(browserID, protocol.StatusOnline)
	case "online":
		r.registry.SetStatus(browserID, protocol.StatusOnline)
	case "offline":
		r.registry.SetStatus(browserID, protocol.StatusOffline)
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

	r.state.SetStatus(protocol.StatusOnline)
	r.registry.SetStatus(r.state.BrowserID(), protocol.StatusOnline)

	if ok {
		r.browser.SendToExtension(buffered)
	}
}

// HandleBrowserDisconnect is router.handleBrowserDisconnect: the extension
// WS closed — go back to idle_wait so the next command buffers.
func (r *Router) HandleBrowserDisconnect() {
	r.state.SetStatus(protocol.StatusIdleWait)
	r.registry.SetStatus(r.state.BrowserID(), protocol.StatusOffline)
}

func (r *Router) lookupInbound(id string) protocol.TextSender {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.inboundByID[id]
}

func (r *Router) removeInbound(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.inboundByID, id)
}

// sendError renders and sends the TS encode('response', {status, error,
// message}, {id, browserId}) shape. Field order (status, error, message)
// matches the TS object literals.
func (r *Router) sendError(sender protocol.TextSender, errCode, message, id, browserID string) {
	payload, err := json.Marshal(protocol.ResponsePayload{
		Status:  "error",
		Error:   errCode,
		Message: message,
	})
	if err != nil {
		r.logger.Printf("encode error payload: %v", err)
		return
	}
	text, err := protocol.Encode(protocol.TypeResponse, payload, id, browserID)
	if err != nil {
		r.logger.Printf("encode error envelope: %v", err)
		return
	}
	sender.Send(text)
}
