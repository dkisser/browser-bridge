package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"browser-bridge/internal/core"
)

// The command-send path: the typed wire shapes, the per-command triple that
// carries them, and the in-process send that replaces the TS WS client hop.

// commandSpec is the per-command triple the control plane sends down: the wire
// command name, the tab it targets, and the typed params payload. Grouping
// them keeps the name, the tab id and the params struct that describe the same
// command in one argument, so they cannot drift apart at a call site.
type commandSpec struct {
	// name is a CommandType from packages/shared/src/types.ts.
	name string
	// tabID lands in the envelope payload's top-level tabId, which is what
	// the extension destructures (background.ts: `const { command, tabId,
	// params } = payload`). 0 for commands with no target tab (tab:list,
	// tab:new).
	tabID int
	// params is one of the structs below; it marshals into
	// CommandPayload.params.
	params any
	// waitBudget is set only by the wait commands. It is the in-page wait
	// budget sent down in params.timeout, and it also drives the transport
	// deadline (budget + waitSlack) so the content script's own rejection —
	// "Element not found within Nms: <selector>" — always wins the race
	// against the control plane's timer and reaches the agent. 0 for every
	// other command, which uses its timeout_ms as-is.
	waitBudget time.Duration
}

// The wire shapes are declared once, in core (wire.go), because internal/cli
// decodes the same responses. These aliases keep the call sites in this file
// short without introducing a second declaration that could drift.

// --- CommandPayload.params ---

type navParams = core.NavParams
type tabParams = core.TabParams
type blankParams = core.BlankParams
type tabNewParams = core.TabNewParams
type selectorTabParams = core.SelectorTabParams
type typeParams = core.TypeParams
type selectParams = core.SelectParams
type scrollParams = core.ScrollParams
type snapshotParams = core.SnapshotParams
type screenshotParams = core.ScreenshotParams
type waitParams = core.WaitParams

// --- CommandResultMap ---

type gettextResult = core.GettextResult
type gethtmlResult = core.GethtmlResult
type snapshotResult = core.SnapshotResult
type screenshotResult = core.ScreenshotResult

// sendCommand is sendCommand in src/mcp/command-client.ts with the WS client
// hop replaced by an in-process router.HandleInboundCommand call. The router
// path preserves the browser_offline / buffer / sw_timeout behavior, and the
// timeout text matches the TS client's.
func (s *MCPServer) sendCommand(ctx context.Context, browserID string, spec commandSpec, timeout time.Duration) (core.ResponsePayload, error) {
	// Params is a struct value, so it always marshals to an object — the
	// `nil map would encode as null` hazard core.CommandPayload warns about
	// cannot arise here.
	payload, err := json.Marshal(struct {
		Command string `json:"command"`
		TabID   int    `json:"tabId"`
		Params  any    `json:"params"`
	}{
		Command: spec.name,
		TabID:   spec.tabID,
		Params:  spec.params,
	})
	if err != nil {
		return core.ResponsePayload{}, fmt.Errorf("encode %s payload: %w", spec.name, err)
	}

	ch := make(chan string, 1)
	envelope := core.Envelope{
		ID:        core.NewID(),
		Type:      core.TypeCommand,
		BrowserID: browserID,
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	// The router's TTL is a leak backstop, not this call's deadline: it must
	// never fire before `timeout`, or a caller that asked for 60s (the schema
	// advertises up to 120s) would get "Service worker did not respond in
	// time" while the extension was still working on the command.
	s.router.HandleInboundCommand(envelope, channelSender{ch: ch}, core.WithRouteDeadline(timeout))

	select {
	case text := <-ch:
		// The router has already removed the route on the response path
		// (HandleBrowserResponse calls takeInbound), so do not call
		// RemoveRoute here.
		decoded, err := core.Decode(text)
		if err != nil {
			return core.ResponsePayload{}, fmt.Errorf("decode response envelope: %w", err)
		}
		if len(decoded.Payload) == 0 || bytes.Equal(decoded.Payload, []byte("null")) {
			// envelope.payload ?? { status: 'error', error: 'Empty response' }
			return core.ResponsePayload{Status: "error", Error: "Empty response"}, nil
		}
		var result core.ResponsePayload
		if err := json.Unmarshal(decoded.Payload, &result); err != nil {
			return core.ResponsePayload{}, fmt.Errorf("decode response payload: %w", err)
		}
		return result, nil
	case <-ctx.Done():
		// The router's own TTL would eventually clean this up, but a
		// long-lived daemon that accumulates slow MCP calls while the user
		// has already given up on this one would still hold the entry for
		// the whole backstop window. Drop it here so the leak window is
		// bounded by the caller's context, not the router's policy.
		s.router.RemoveRoute(envelope.ID)
		return core.ResponsePayload{}, fmt.Errorf("command %s: %w", spec.name, ctx.Err())
	case <-time.After(timeout):
		// Same reasoning as ctx.Done: the router will clean up via its own
		// TTL, but the caller has already failed — release the slot now.
		s.router.RemoveRoute(envelope.ID)
		return core.ResponsePayload{}, fmt.Errorf("timeout: no response for command %s within %dms", spec.name, timeout.Milliseconds())
	}
}
