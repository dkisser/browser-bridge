package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"browser-bridge/internal/core"
)

// The wire contract, restated on the Go side.
//
// packages/shared/src/types.ts is the single source of truth for the command
// names (CommandType) and the per-command `data` shapes (CommandResultMap);
// the extension's handlers are annotated against it. Go cannot import that
// package, so this file used to restate the same shapes as bare
// `map[string]any` literals and anonymous structs. That made every wire key a
// hand-typed string duplicated from the arg struct's json tag, with nothing
// linking the two — renaming a key on one side silently broke the contract
// with no compile error and no test failure.
//
// The declarations here are that missing link: one named type per wire shape,
// so the json tag *is* the declaration of the key. params_test.go pins the
// marshaled form against the shapes the extension reads.

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

// --- CommandPayload.params, one struct per wire shape ---
//
// Optional fields are pointers with omitempty so an absent argument is
// dropped from the JSON entirely rather than sent as null or zero — the
// extension distinguishes "not provided" from "provided as false"
// (`params.submit === true`, `params.active === true`).

type navParams struct {
	URL   string `json:"url"`
	TabID int    `json:"tabId"`
}

type tabParams struct {
	TabID int `json:"tabId"`
}

// blankParams is tab:list, which the extension answers from chrome.tabs and
// reads no params from. It still marshals to `{}` rather than null.
type blankParams struct{}

type tabNewParams struct {
	URL       *string `json:"url,omitempty"`
	Active    *bool   `json:"active,omitempty"`
	AutoClose *bool   `json:"auto_close,omitempty"`
}

type selectorTabParams struct {
	Selector string `json:"selector"`
	TabID    int    `json:"tabId"`
}

type typeParams struct {
	Selector string `json:"selector"`
	Text     string `json:"text"`
	Submit   *bool  `json:"submit,omitempty"`
	TabID    int    `json:"tabId"`
}

type selectParams struct {
	Selector string `json:"selector"`
	Value    string `json:"value"`
	TabID    int    `json:"tabId"`
}

type scrollParams struct {
	Selector string `json:"selector"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	TabID    int    `json:"tabId"`
}

type snapshotParams struct {
	Selector *string `json:"selector,omitempty"`
	Filter   string  `json:"filter"`
	MaxChars int     `json:"max_chars"`
	TabID    int     `json:"tabId"`
}

type screenshotParams struct {
	FullPage *bool `json:"fullPage,omitempty"`
	TabID    int   `json:"tabId"`
}

// waitParams carries the in-page wait budget to the content script.
type waitParams struct {
	Selector *string `json:"selector,omitempty"`
	Timeout  int     `json:"timeout"`
	TabID    int     `json:"tabId"`
}

// --- CommandResultMap, the entries the control plane actually decodes ---
//
// Only the commands whose `data` this package reads into a typed field are
// materialized. pageinfo and tab:list are re-indented verbatim by indentJSON
// and need no struct; the remaining commands are rendered from
// ResponsePayload.Message by runMessageTool. Declaring shapes for those would
// be unused code, not contract coverage.

// gettextResult is GettextResult. `text` is `string | null` on the TS side, so
// a null decodes into the nil pointer rather than an empty string.
type gettextResult struct {
	Text *string `json:"text"`
}

// gethtmlResult is GethtmlResult.
type gethtmlResult struct {
	HTML string `json:"html"`
}

// snapshotResult is SnapshotResult in packages/shared/src/snapshot.ts.
type snapshotResult struct {
	Snapshot     string `json:"snapshot"`
	Truncated    bool   `json:"truncated"`
	NodesTotal   int    `json:"nodes_total"`
	NodesEmitted int    `json:"nodes_emitted"`
	Tier         int    `json:"tier"`
}

// screenshotResult is ScreenshotResult.
type screenshotResult struct {
	DataURL string `json:"dataUrl"`
}

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
