package http

import (
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"browser-bridge/internal/core"
)

// okScript answers every command with a bare ok payload.
func okScript(capturedCommand) (core.ResponsePayload, bool) {
	return core.ResponsePayload{Status: "ok"}, true
}

// These tests pin the control plane's side of the wire contract.
//
// The params structs in command.go replaced bare `map[string]any` literals,
// which were the only place the wire key names were written on the Go side.
// A key rename there is invisible to the compiler and to tools/list, so it
// would reach the extension as a silently missing field. The table below is
// the guard: it names, per command, the exact param keys that go on the wire
// — the same keys apps/extension/src/background.ts reads off `params`.

func TestWireParamKeys(t *testing.T) {
	tests := []struct {
		tool string
		args map[string]any
		// wantKeys is the full set of keys the extension sees in params.
		wantKeys []string
		// wantOmitted lists optional keys that must be absent when the
		// caller did not supply them, rather than sent as null/false.
		wantOmitted []string
	}{
		{
			tool:     "navigate",
			args:     map[string]any{"url": "https://example.com", "tab_id": 1},
			wantKeys: []string{"tabId", "url"},
		},
		{
			tool:     "tab_list",
			args:     map[string]any{},
			wantKeys: []string{},
		},
		{
			tool:        "tab_new",
			args:        map[string]any{},
			wantKeys:    []string{},
			wantOmitted: []string{"active", "auto_close", "url"},
		},
		{
			// Absent optionals must be dropped, not sent as false: the
			// extension reads `params.active === true`.
			tool:        "tab_new",
			args:        map[string]any{"url": "https://example.com", "active": true},
			wantKeys:    []string{"active", "url"},
			wantOmitted: []string{"auto_close"},
		},
		{
			tool:     "type",
			args:     map[string]any{"selector": "#a", "text": "hi", "tab_id": 1, "submit": true},
			wantKeys: []string{"selector", "submit", "tabId", "text"},
		},
		{
			tool:        "type",
			args:        map[string]any{"selector": "#a", "text": "hi", "tab_id": 1},
			wantKeys:    []string{"selector", "tabId", "text"},
			wantOmitted: []string{"submit"},
		},
		{
			tool:        "snapshot",
			args:        map[string]any{"tab_id": 1},
			wantKeys:    []string{"filter", "max_chars", "tabId"},
			wantOmitted: []string{"selector"},
		},
		{
			tool:        "screenshot",
			args:        map[string]any{"tab_id": 1},
			wantKeys:    []string{"tabId"},
			wantOmitted: []string{"fullPage"},
		},
		{
			tool:     "scroll",
			args:     map[string]any{"selector": "page", "x": 0, "y": 100, "tab_id": 1},
			wantKeys: []string{"selector", "tabId", "x", "y"},
		},
	}

	for _, tt := range tests {
		router := &fakeRouter{script: okScript}
		session := newTestClient(t, newTestServer(router, []core.BrowserConnection{onlineBrowser("b-1")}))
		callTool(t, session, tt.tool, tt.args)

		commands := router.captured()
		if len(commands) != 1 {
			t.Fatalf("%s: router saw %d commands, want 1", tt.tool, len(commands))
		}
		got := keysOf(commands[0].params)
		if !reflect.DeepEqual(got, tt.wantKeys) {
			t.Errorf("%s: wire param keys = %v, want %v\n"+
				"(if a key moved, update CommandPayload.params handling in "+
				"apps/extension/src/background.ts and CommandResultMap in "+
				"packages/shared/src/types.ts together with this table)",
				tt.tool, got, tt.wantKeys)
		}
		for _, omitted := range tt.wantOmitted {
			if _, present := commands[0].params[omitted]; present {
				t.Errorf("%s: params[%q] present = %#v, want omitted entirely",
					tt.tool, omitted, commands[0].params[omitted])
			}
		}
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestRouteDeadlineFollowsCallerTimeout is the P0-1 regression guard at the
// MCP layer: the router's TTL must be told the caller's deadline. Without it
// a wait_element asking for 60s was terminated by the router's flat 30s
// backstop and reported as "Service worker did not respond in time".
func TestRouteDeadlineFollowsCallerTimeout(t *testing.T) {
	router := &fakeRouter{script: okScript}
	session := newTestClient(t, newTestServer(router, []core.BrowserConnection{onlineBrowser("b-1")}))

	callTool(t, session, "click", map[string]any{
		"selector":   "#a",
		"tab_id":     1,
		"timeout_ms": 60000,
	})

	commands := router.captured()
	if len(commands) != 1 {
		t.Fatalf("router saw %d commands, want 1", len(commands))
	}
	if got := commands[0].routeDeadline; got != 60*time.Second {
		t.Errorf("route deadline = %v, want 60s (the caller's timeout_ms)", got)
	}
}

// TestWaitBudgetLeavesRoomForTheContentScriptDiagnostic: the content script
// rejects wait:element with "Element not found within Nms: <selector>" at
// exactly params.timeout. When the transport deadline equalled that budget
// the control plane's own timer — started earlier — always won, so the agent
// never saw the selector. The in-page budget stays what the caller asked for,
// and the transport gets slack on top.
func TestWaitBudgetLeavesRoomForTheContentScriptDiagnostic(t *testing.T) {
	router := &fakeRouter{script: okScript}
	session := newTestClient(t, newTestServer(router, []core.BrowserConnection{onlineBrowser("b-1")}))

	callTool(t, session, "wait_element", map[string]any{
		"selector":   "#a",
		"tab_id":     1,
		"timeout_ms": 30000,
	})

	commands := router.captured()
	if len(commands) != 1 {
		t.Fatalf("router saw %d commands, want 1", len(commands))
	}
	// The page still gets the full 30s the caller asked for.
	if got := commands[0].params["timeout"]; got != float64(30000) {
		t.Errorf("params.timeout = %v, want 30000 (the caller's full budget)", got)
	}
	if got, want := commands[0].routeDeadline, 30*time.Second+waitSlack; got != want {
		t.Errorf("route deadline = %v, want %v (budget + slack)", got, want)
	}
}

// TestShortWaitTimeoutStillGetsTransportSlack: the schema's minimum timeout_ms
// is small, so the slack has to be additive or a short wait would have a
// transport deadline shorter than its own in-page budget.
func TestShortWaitTimeoutStillGetsTransportSlack(t *testing.T) {
	router := &fakeRouter{script: okScript}
	session := newTestClient(t, newTestServer(router, []core.BrowserConnection{onlineBrowser("b-1")}))

	callTool(t, session, "wait_navigation", map[string]any{
		"tab_id":     1,
		"timeout_ms": 200,
	})

	commands := router.captured()
	if len(commands) != 1 {
		t.Fatalf("router saw %d commands, want 1", len(commands))
	}
	raw, ok := commands[0].params["timeout"].(float64)
	if !ok {
		t.Fatalf("params.timeout = %#v, want a number", commands[0].params["timeout"])
	}
	inPage := time.Duration(raw) * time.Millisecond
	if commands[0].routeDeadline <= inPage {
		t.Errorf("route deadline %v <= in-page budget %v; the content "+
			"script's rejection would never reach the agent",
			commands[0].routeDeadline, inPage)
	}
}

// TestResultDecodingMirrorsCommandResultMap: the named result structs are the
// Go mirror of CommandResultMap. A shape change on the TS side must fail here
// rather than silently decoding to zero values.
func TestResultDecodingMirrorsCommandResultMap(t *testing.T) {
	t.Run("gettext null text is not an empty string", func(t *testing.T) {
		router := &fakeRouter{script: func(capturedCommand) (core.ResponsePayload, bool) {
			return core.ResponsePayload{Status: "ok", Data: []byte(`{"text":null}`)}, true
		}}
		session := newTestClient(t, newTestServer(router, []core.BrowserConnection{onlineBrowser("b-1")}))
		result := callTool(t, session, "get_text", map[string]any{"selector": "#a", "tab_id": 1})
		// A null text must read as empty and get the empty-read guidance,
		// not the empty string silently.
		if !strings.Contains(resultText(t, result), "no text content") {
			t.Errorf("text = %q, want the empty-read guidance", resultText(t, result))
		}
	})

	t.Run("screenshot dataUrl", func(t *testing.T) {
		router := &fakeRouter{script: func(capturedCommand) (core.ResponsePayload, bool) {
			// 1x1 transparent PNG — the tool base64-decodes before rendering.
			return core.ResponsePayload{Status: "ok", Data: []byte(
				`{"dataUrl":"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII="}`,
			)}, true
		}}
		session := newTestClient(t, newTestServer(router, []core.BrowserConnection{onlineBrowser("b-1")}))
		result := callTool(t, session, "screenshot", map[string]any{"tab_id": 1})
		if result.IsError {
			t.Fatalf("screenshot returned an error: %s", resultText(t, result))
		}
		// The tool base64-decodes dataUrl and returns it as image content,
		// so reaching that content type at all means the dataUrl key was
		// decoded rather than dropped to a zero value.
		if len(result.Content) != 1 {
			t.Fatalf("content = %d items, want 1", len(result.Content))
		}
		if _, ok := result.Content[0].(*mcp.ImageContent); !ok {
			t.Fatalf("content[0] = %T, want *mcp.ImageContent", result.Content[0])
		}
	})

	t.Run("snapshot metadata", func(t *testing.T) {
		router := &fakeRouter{script: func(capturedCommand) (core.ResponsePayload, bool) {
			return core.ResponsePayload{Status: "ok", Data: []byte(
				`{"snapshot":"#tree","truncated":true,"nodes_total":10,"nodes_emitted":3,"tier":2}`,
			)}, true
		}}
		session := newTestClient(t, newTestServer(router, []core.BrowserConnection{onlineBrowser("b-1")}))
		result := callTool(t, session, "snapshot", map[string]any{"tab_id": 1})
		if result.IsError {
			t.Fatalf("snapshot returned an error: %s", resultText(t, result))
		}
		text := resultText(t, result)
		for _, want := range []string{"#tree", "truncated", "10", "3"} {
			if !strings.Contains(text, want) {
				t.Errorf("text = %q, want it to contain %q", text, want)
			}
		}
	})
}
