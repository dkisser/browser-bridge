package http

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"browser-bridge/internal/core"
)

// TestCommandTools drives every command tool through a real MCP session
// (httptest StreamableHTTP) against a fake router, asserting the command
// name, the exact params map, the routed browserId, and the rendered text.
func TestCommandTools(t *testing.T) {
	ok := core.ResponsePayload{Status: "ok"}

	tests := []struct {
		name        string
		tool        string
		args        map[string]any
		respond     core.ResponsePayload
		wantCommand string
		wantParams  map[string]any
		wantText    string
	}{
		{
			name:        "navigate",
			tool:        "navigate",
			args:        map[string]any{"url": "https://example.com", "tab_id": 2},
			respond:     ok,
			wantCommand: "navigate",
			wantParams:  map[string]any{"url": "https://example.com", "tabId": float64(2)},
			wantText:    "Navigated to https://example.com in tab 2",
		},
		{
			name:        "go_back",
			tool:        "go_back",
			args:        map[string]any{"tab_id": 1},
			respond:     ok,
			wantCommand: "goBack",
			wantParams:  map[string]any{"tabId": float64(1)},
			wantText:    "Went back",
		},
		{
			name:        "go_forward",
			tool:        "go_forward",
			args:        map[string]any{"tab_id": 1},
			respond:     ok,
			wantCommand: "goForward",
			wantParams:  map[string]any{"tabId": float64(1)},
			wantText:    "Went forward",
		},
		{
			name:        "refresh",
			tool:        "refresh",
			args:        map[string]any{"tab_id": 1},
			respond:     ok,
			wantCommand: "refresh",
			wantParams:  map[string]any{"tabId": float64(1)},
			wantText:    "Refreshed",
		},
		{
			name:        "tab_close",
			tool:        "tab_close",
			args:        map[string]any{"tab_id": 7},
			respond:     ok,
			wantCommand: "tab:close",
			wantParams:  map[string]any{"tabId": float64(7)},
			wantText:    "Tab 7 closed",
		},
		{
			name:        "tab_switch",
			tool:        "tab_switch",
			args:        map[string]any{"tab_id": 7},
			respond:     ok,
			wantCommand: "tab:switch",
			wantParams:  map[string]any{"tabId": float64(7)},
			wantText:    "Switched to tab 7",
		},
		{
			name:        "tab_new with no optionals",
			tool:        "tab_new",
			args:        map[string]any{},
			respond:     ok,
			wantCommand: "tab:new",
			wantParams:  map[string]any{},
			wantText:    "New tab opened",
		},
		{
			name:        "tab_new with all optionals",
			tool:        "tab_new",
			args:        map[string]any{"url": "https://example.com", "active": true, "auto_close": true},
			respond:     ok,
			wantCommand: "tab:new",
			wantParams:  map[string]any{"url": "https://example.com", "active": true, "auto_close": true},
			wantText:    "New tab opened",
		},
		{
			name:        "click",
			tool:        "click",
			args:        map[string]any{"selector": "#a", "tab_id": 1},
			respond:     ok,
			wantCommand: "click",
			wantParams:  map[string]any{"selector": "#a", "tabId": float64(1)},
			wantText:    "Clicked #a",
		},
		{
			name:        "type without submit drops the key",
			tool:        "type",
			args:        map[string]any{"selector": "#a", "text": "hi", "tab_id": 1},
			respond:     ok,
			wantCommand: "type",
			wantParams:  map[string]any{"selector": "#a", "text": "hi", "tabId": float64(1)},
			wantText:    "Typed into #a",
		},
		{
			name:        "type with explicit submit=false keeps the key",
			tool:        "type",
			args:        map[string]any{"selector": "#a", "text": "hi", "submit": false, "tab_id": 1},
			respond:     ok,
			wantCommand: "type",
			wantParams:  map[string]any{"selector": "#a", "text": "hi", "submit": false, "tabId": float64(1)},
			wantText:    "Typed into #a",
		},
		{
			name:        "select",
			tool:        "select",
			args:        map[string]any{"selector": "#a", "value": "v", "tab_id": 1},
			respond:     ok,
			wantCommand: "select",
			wantParams:  map[string]any{"selector": "#a", "value": "v", "tabId": float64(1)},
			wantText:    "Selected v in #a",
		},
		{
			name:        "scroll defaults selector to page",
			tool:        "scroll",
			args:        map[string]any{"x": 10, "y": 20, "tab_id": 1},
			respond:     ok,
			wantCommand: "scroll",
			wantParams:  map[string]any{"selector": "page", "x": float64(10), "y": float64(20), "tabId": float64(1)},
			wantText:    "Scrolled to (10, 20)",
		},
		{
			name:        "scroll with explicit selector",
			tool:        "scroll",
			args:        map[string]any{"x": 1, "y": 2, "selector": "#el", "tab_id": 3},
			respond:     ok,
			wantCommand: "scroll",
			wantParams:  map[string]any{"selector": "#el", "x": float64(1), "y": float64(2), "tabId": float64(3)},
			wantText:    "Scrolled to (1, 2)",
		},
		{
			name:        "hover",
			tool:        "hover",
			args:        map[string]any{"selector": "#a", "tab_id": 1},
			respond:     ok,
			wantCommand: "hover",
			wantParams:  map[string]any{"selector": "#a", "tabId": float64(1)},
			wantText:    "Hovered #a",
		},
		{
			name:        "wait_element passes the effective timeout",
			tool:        "wait_element",
			args:        map[string]any{"selector": "#a", "tab_id": 1},
			respond:     ok,
			wantCommand: "wait:element",
			wantParams:  map[string]any{"selector": "#a", "timeout": float64(10000), "tabId": float64(1)},
			wantText:    "Element #a found",
		},
		{
			name:        "wait_element honors timeout_ms",
			tool:        "wait_element",
			args:        map[string]any{"selector": "#a", "tab_id": 1, "timeout_ms": 500},
			respond:     ok,
			wantCommand: "wait:element",
			wantParams:  map[string]any{"selector": "#a", "timeout": float64(500), "tabId": float64(1)},
			wantText:    "Element #a found",
		},
		{
			name:        "wait_navigation",
			tool:        "wait_navigation",
			args:        map[string]any{"tab_id": 1},
			respond:     ok,
			wantCommand: "wait:navigation",
			wantParams:  map[string]any{"timeout": float64(10000), "tabId": float64(1)},
			wantText:    "Navigation complete",
		},
		{
			name:        "pageinfo renders pretty JSON",
			tool:        "pageinfo",
			args:        map[string]any{"tab_id": 1},
			respond:     core.ResponsePayload{Status: "ok", Data: []byte(`{"title":"T","id":1}`)},
			wantCommand: "pageinfo",
			wantParams:  map[string]any{"tabId": float64(1)},
			wantText:    "{\n  \"title\": \"T\",\n  \"id\": 1\n}",
		},
		{
			name:        "pageinfo with no data renders {}",
			tool:        "pageinfo",
			args:        map[string]any{"tab_id": 1},
			respond:     ok,
			wantCommand: "pageinfo",
			wantParams:  map[string]any{"tabId": float64(1)},
			wantText:    "{}",
		},
		{
			name:        "tab_list renders the tab array",
			tool:        "tab_list",
			args:        map[string]any{},
			respond:     core.ResponsePayload{Status: "ok", Data: []byte(`[{"id":1,"title":"T"}]`)},
			wantCommand: "tab:list",
			wantParams:  map[string]any{},
			wantText:    "[\n  {\n    \"id\": 1,\n    \"title\": \"T\"\n  }\n]",
		},
		{
			name:        "tab_list with no data renders []",
			tool:        "tab_list",
			args:        map[string]any{},
			respond:     ok,
			wantCommand: "tab:list",
			wantParams:  map[string]any{},
			wantText:    "[]",
		},
		{
			name:        "get_text returns the text",
			tool:        "get_text",
			args:        map[string]any{"selector": "#a", "tab_id": 1},
			respond:     core.ResponsePayload{Status: "ok", Data: []byte(`{"text":"hello"}`)},
			wantCommand: "gettext",
			wantParams:  map[string]any{"selector": "#a", "tabId": float64(1)},
			wantText:    "hello",
		},
		{
			name:        "get_html returns the markup",
			tool:        "get_html",
			args:        map[string]any{"selector": "#a", "tab_id": 1},
			respond:     core.ResponsePayload{Status: "ok", Data: []byte(`{"html":"<p>x</p>"}`)},
			wantCommand: "gethtml",
			wantParams:  map[string]any{"selector": "#a", "tabId": float64(1)},
			wantText:    "<p>x</p>",
		},
		{
			name:        "snapshot renders stats line",
			tool:        "snapshot",
			args:        map[string]any{"tab_id": 1},
			respond:     core.ResponsePayload{Status: "ok", Data: []byte(`{"snapshot":"snap","truncated":false,"nodes_total":5,"nodes_emitted":3,"tier":1}`)},
			wantCommand: "snapshot",
			wantParams:  map[string]any{"filter": "interactive", "max_chars": float64(8000), "tabId": float64(1)},
			wantText:    "snap\n\n[3/5 nodes | tier=1]",
		},
		{
			name:        "snapshot full filter with max_chars and truncation",
			tool:        "snapshot",
			args:        map[string]any{"tab_id": 1, "filter": "full", "max_chars": 500, "selector": "#main"},
			respond:     core.ResponsePayload{Status: "ok", Data: []byte(`{"snapshot":"snap","truncated":true,"nodes_total":9,"nodes_emitted":4,"tier":2}`)},
			wantCommand: "snapshot",
			wantParams:  map[string]any{"selector": "#main", "filter": "full", "max_chars": float64(500), "tabId": float64(1)},
			wantText:    "snap\n\n[4/9 nodes | tier=2 | truncated]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := &fakeRouter{script: func(capturedCommand) (core.ResponsePayload, bool) {
				return tt.respond, true
			}}
			session := newTestClient(t, newTestServer(router, []core.BrowserConnection{onlineBrowser("b-1")}))

			result := callTool(t, session, tt.tool, tt.args)
			if result.IsError {
				t.Fatalf("%s returned an error: %s", tt.name, resultText(t, result))
			}
			if got := resultText(t, result); got != tt.wantText {
				t.Errorf("text = %q, want %q", got, tt.wantText)
			}

			commands := router.captured()
			if len(commands) != 1 {
				t.Fatalf("router saw %d commands, want 1", len(commands))
			}
			got := commands[0]
			if got.command != tt.wantCommand {
				t.Errorf("command = %q, want %q", got.command, tt.wantCommand)
			}
			if got.browserID != "b-1" {
				t.Errorf("browserId = %q, want b-1", got.browserID)
			}
			if !reflect.DeepEqual(got.params, tt.wantParams) {
				t.Errorf("params = %#v, want %#v", got.params, tt.wantParams)
			}
		})
	}
}

// TestMessagePreferredOverFallback: a result.message wins over the tool's
// built-in success text (TS `result.message ?? fallback`).
func TestMessagePreferredOverFallback(t *testing.T) {
	router := &fakeRouter{script: func(capturedCommand) (core.ResponsePayload, bool) {
		return core.ResponsePayload{Status: "ok", Message: "Navigated!"}, true
	}}
	session := newTestClient(t, newTestServer(router, []core.BrowserConnection{onlineBrowser("b-1")}))
	result := callTool(t, session, "navigate", map[string]any{"url": "https://example.com", "tab_id": 1})
	if got := resultText(t, result); got != "Navigated!" {
		t.Fatalf("text = %q, want Navigated!", got)
	}
}

// TestCommandErrorMapping covers the failure shapes: status != ok maps to the
// FastMCP tool-error text, message beats error, and recovery hints attach.
func TestCommandErrorMapping(t *testing.T) {
	tests := []struct {
		name     string
		respond  core.ResponsePayload
		wantText string
	}{
		{
			name:     "error code",
			respond:  core.ResponsePayload{Status: "error", Error: "boom"},
			wantText: "Tool 'click' execution failed: boom",
		},
		{
			name:     "message beats error",
			respond:  core.ResponsePayload{Status: "error", Error: "code", Message: "nice message"},
			wantText: "Tool 'click' execution failed: nice message",
		},
		{
			name:     "structured reason appends its recovery hint to message",
			respond:  core.ResponsePayload{Status: "error", Reason: "restricted_page", Message: "nope"},
			wantText: "Tool 'click' execution failed: nope Content commands only work on http(s) pages. Navigate to a non-restricted URL first.",
		},
		{
			name:     "structured reason appends to error when no message",
			respond:  core.ResponsePayload{Status: "error", Reason: "no_listener", Error: "content_script_unavailable"},
			wantText: "Tool 'click' execution failed: content_script_unavailable The content script did not respond. Reload the page or retry the command.",
		},
		{
			name:     "chrome unknown-tab error gains the tab_list hint",
			respond:  core.ResponsePayload{Status: "error", Error: "No tab with id: 42"},
			wantText: "Tool 'click' execution failed: No tab with id: 42 Call tab_list to discover valid tab ids for the selected browser.",
		},
		{
			name:     "unknown-tab error already mentioning tab_list stays untouched",
			respond:  core.ResponsePayload{Status: "error", Error: "No tab with id: 42 — call tab_list"},
			wantText: "Tool 'click' execution failed: No tab with id: 42 — call tab_list",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := &fakeRouter{script: func(capturedCommand) (core.ResponsePayload, bool) {
				return tt.respond, true
			}}
			session := newTestClient(t, newTestServer(router, []core.BrowserConnection{onlineBrowser("b-1")}))
			result := callTool(t, session, "click", map[string]any{"selector": "#a", "tab_id": 1})
			if !result.IsError {
				t.Fatalf("result should be isError: %s", resultText(t, result))
			}
			if got := resultText(t, result); got != tt.wantText {
				t.Errorf("text = %q, want %q", got, tt.wantText)
			}
		})
	}
}

// TestCommandTimeout: when no response arrives within the effective timeout,
// the tool fails with the TS client's timeout text.
func TestCommandTimeout(t *testing.T) {
	router := &fakeRouter{script: func(capturedCommand) (core.ResponsePayload, bool) {
		return core.ResponsePayload{}, false // never respond
	}}
	srv := newTestServer(router, []core.BrowserConnection{onlineBrowser("b-1")})
	srv.defaultTimeout = 50 * time.Millisecond
	session := newTestClient(t, srv)

	result := callTool(t, session, "click", map[string]any{"selector": "#a", "tab_id": 1})
	if !result.IsError {
		t.Fatalf("result should be isError: %s", resultText(t, result))
	}
	want := "Tool 'click' execution failed: timeout: no response for command click within 50ms"
	if got := resultText(t, result); got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

// TestBrowserResolutionErrors covers the resolveBrowser failure messages.
func TestBrowserResolutionErrors(t *testing.T) {
	tests := []struct {
		name     string
		browsers []core.BrowserConnection
		pin      string // set_browser before the command ("" = skip)
		wantText string
	}{
		{
			name:     "no browsers",
			browsers: nil,
			wantText: "Tool 'click' execution failed: No browser connected. Start the extension/local-proxy first.",
		},
		{
			name: "two online browsers",
			browsers: []core.BrowserConnection{
				onlineBrowser("b-1"),
				onlineBrowser("b-2"),
			},
			wantText: "Tool 'click' execution failed: Multiple browsers are online. Call set_browser with one of:\n- b-1 (online)\n- b-2 (online)",
		},
		{
			name:     "pinned browser not connected",
			browsers: []core.BrowserConnection{onlineBrowser("b-1")},
			pin:      "b-9",
			wantText: "Tool 'click' execution failed: Browser \"b-9\" is not connected.",
		},
		{
			name: "pinned browser not online",
			browsers: []core.BrowserConnection{
				onlineBrowser("b-1"),
				{BrowserID: "b-2", UserID: "extension", Status: core.StatusIdleWait, LastSeen: 1},
			},
			pin:      "b-2",
			wantText: "Tool 'click' execution failed: Browser \"b-2\" is not online (status: idle_wait).",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := &fakeRouter{script: func(capturedCommand) (core.ResponsePayload, bool) {
				return core.ResponsePayload{Status: "ok"}, true
			}}
			session := newTestClient(t, newTestServer(router, tt.browsers))
			if tt.pin != "" {
				setResult := callTool(t, session, "set_browser", map[string]any{"browserId": tt.pin})
				if setResult.IsError {
					t.Fatalf("set_browser failed: %s", resultText(t, setResult))
				}
			}
			result := callTool(t, session, "click", map[string]any{"selector": "#a", "tab_id": 1})
			if !result.IsError {
				t.Fatalf("result should be isError: %s", resultText(t, result))
			}
			if got := resultText(t, result); got != tt.wantText {
				t.Errorf("text = %q, want %q", got, tt.wantText)
			}
			if len(router.captured()) != 0 {
				t.Errorf("router saw %d commands, want 0 (resolution failed first)", len(router.captured()))
			}
		})
	}
}

// TestSetBrowserPinsSession: set_browser pins the browser for the calling MCP
// session only; commands then route to the pinned browserId.
func TestSetBrowserPinsSession(t *testing.T) {
	router := &fakeRouter{script: func(capturedCommand) (core.ResponsePayload, bool) {
		return core.ResponsePayload{Status: "ok"}, true
	}}
	srv := newTestServer(router, []core.BrowserConnection{onlineBrowser("b-1"), onlineBrowser("b-2")})
	session := newTestClient(t, srv)

	setResult := callTool(t, session, "set_browser", map[string]any{"browserId": "b-2"})
	if got := resultText(t, setResult); got != `Browser set to "b-2" for this session.` {
		t.Fatalf("set_browser text = %q", got)
	}
	result := callTool(t, session, "go_back", map[string]any{"tab_id": 1})
	if result.IsError {
		t.Fatalf("go_back failed: %s", resultText(t, result))
	}
	commands := router.captured()
	if len(commands) != 1 || commands[0].browserID != "b-2" {
		t.Fatalf("routed browserId = %+v, want b-2", commands)
	}

	// A second session has no pin: the multi-browser error applies.
	other := newTestClient(t, srv)
	result = callTool(t, other, "go_back", map[string]any{"tab_id": 1})
	if !result.IsError {
		t.Fatalf("unpinned session should fail with two browsers online")
	}
	if got := resultText(t, result); !strings.Contains(got, "Multiple browsers are online") {
		t.Fatalf("text = %q", got)
	}
}

// TestListBrowsers lists every browser regardless of status.
func TestListBrowsers(t *testing.T) {
	tests := []struct {
		name     string
		browsers []core.BrowserConnection
		wantText string
	}{
		{name: "none", browsers: nil, wantText: "No browsers connected."},
		{
			name: "mixed statuses",
			browsers: []core.BrowserConnection{
				onlineBrowser("b-1"),
				{BrowserID: "b-2", UserID: "extension", Status: core.StatusOffline, LastSeen: 2},
			},
			wantText: "- b-1 (online)\n- b-2 (offline)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := newTestClient(t, newTestServer(&fakeRouter{}, tt.browsers))
			result := callTool(t, session, "list_browsers", map[string]any{})
			if result.IsError {
				t.Fatalf("list_browsers failed: %s", resultText(t, result))
			}
			if got := resultText(t, result); got != tt.wantText {
				t.Errorf("text = %q, want %q", got, tt.wantText)
			}
		})
	}
}

// TestGetTextGuards covers the empty-text and oversize guards of get_text
// and get_html (MAX_READ_RESULT_CHARS, counted in UTF-16 code units).
func TestGetTextGuards(t *testing.T) {
	oversize := strings.Repeat("x", maxReadResultChars+1)
	tests := []struct {
		name      string
		tool      string
		data      string
		wantText  string
		wantError bool
	}{
		{
			name:     "whitespace-only text reads as empty",
			tool:     "get_text",
			data:     `{"text":"  "}`,
			wantText: selectorMatchedButEmptyMessage("#a"),
		},
		{
			name:     "missing text field reads as empty",
			tool:     "get_text",
			data:     `{}`,
			wantText: selectorMatchedButEmptyMessage("#a"),
		},
		{
			name:      "oversize text is refused",
			tool:      "get_text",
			data:      fmt.Sprintf(`{"text":%q}`, oversize),
			wantError: true,
			wantText: fmt.Sprintf("Tool 'get_text' execution failed: get_text matched %d chars — almost certainly "+
				"whole-page text (nav, ads, sidebars included), not the "+
				"content you want. Take a snapshot to find the content "+
				"container, then narrow with a selector.", maxReadResultChars+1),
		},
		{
			name:     "html at the limit passes",
			tool:     "get_html",
			data:     fmt.Sprintf(`{"html":%q}`, strings.Repeat("x", maxReadResultChars)),
			wantText: strings.Repeat("x", maxReadResultChars),
		},
		{
			name:      "oversize html is refused",
			tool:      "get_html",
			data:      fmt.Sprintf(`{"html":%q}`, oversize),
			wantError: true,
			wantText: fmt.Sprintf("Tool 'get_html' execution failed: get_html matched %d chars — almost certainly "+
				"whole-page chrome (nav, ads, sidebars), not the content you "+
				"want. Take a snapshot to find the content container, then "+
				"narrow with a selector.", maxReadResultChars+1),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := &fakeRouter{script: func(capturedCommand) (core.ResponsePayload, bool) {
				return core.ResponsePayload{Status: "ok", Data: []byte(tt.data)}, true
			}}
			session := newTestClient(t, newTestServer(router, []core.BrowserConnection{onlineBrowser("b-1")}))
			result := callTool(t, session, tt.tool, map[string]any{"selector": "#a", "tab_id": 1})
			if result.IsError != tt.wantError {
				t.Fatalf("isError = %v, want %v (text %.80q)", result.IsError, tt.wantError, resultText(t, result))
			}
			if got := resultText(t, result); got != tt.wantText {
				t.Errorf("text mismatch:\n got %.120q\nwant %.120q", got, tt.wantText)
			}
		})
	}
}

// TestScreenshot covers the image-content mapping: dataUrl prefix strip and
// base64 payload, plus the empty-data failure.
func TestScreenshot(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]any
		data      string
		wantMIME  string
		wantImage []byte // nil = expect an error result
		wantText  string
	}{
		{
			name:      "dataUrl renders image content",
			args:      map[string]any{"tab_id": 1, "fullPage": true},
			data:      `{"dataUrl":"data:image/png;base64,aGVsbG8="}`,
			wantMIME:  "image/png",
			wantImage: []byte("hello"),
		},
		{
			name:      "jpeg dataUrl reports image/jpeg, not image/png",
			args:      map[string]any{"tab_id": 1},
			data:      `{"dataUrl":"data:image/jpeg;base64,aGVsbG8="}`,
			wantMIME:  "image/jpeg",
			wantImage: []byte("hello"),
		},
		{
			name:      "webp dataUrl reports image/webp",
			args:      map[string]any{"tab_id": 1},
			data:      `{"dataUrl":"data:image/webp;base64,aGVsbG8="}`,
			wantMIME:  "image/webp",
			wantImage: []byte("hello"),
		},
		{
			name:     "missing dataUrl is an error",
			args:     map[string]any{"tab_id": 1},
			data:     `{}`,
			wantText: "Tool 'screenshot' execution failed: Screenshot failed: browser returned no image data",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := &fakeRouter{script: func(capturedCommand) (core.ResponsePayload, bool) {
				return core.ResponsePayload{Status: "ok", Data: []byte(tt.data)}, true
			}}
			session := newTestClient(t, newTestServer(router, []core.BrowserConnection{onlineBrowser("b-1")}))
			result := callTool(t, session, "screenshot", tt.args)
			if tt.wantImage == nil {
				if !result.IsError {
					t.Fatalf("expected error, got %+v", result.Content)
				}
				if got := resultText(t, result); got != tt.wantText {
					t.Errorf("text = %q, want %q", got, tt.wantText)
				}
				return
			}
			if result.IsError {
				t.Fatalf("screenshot failed: %s", resultText(t, result))
			}
			if len(result.Content) != 1 {
				t.Fatalf("content = %d parts, want 1", len(result.Content))
			}
			image, ok := result.Content[0].(*mcp.ImageContent)
			if !ok {
				t.Fatalf("content[0] = %T, want image", result.Content[0])
			}
			if image.MIMEType != tt.wantMIME {
				t.Errorf("mimeType = %q, want %q", image.MIMEType, tt.wantMIME)
			}
			if !reflect.DeepEqual(image.Data, tt.wantImage) {
				t.Errorf("image data = %q, want %q", image.Data, tt.wantImage)
			}
			// fullPage must reach the extension verbatim.
			commands := router.captured()
			if fullPage, ok := commands[0].params["fullPage"]; tt.args["fullPage"] == true && (!ok || fullPage != true) {
				t.Errorf("params = %#v, want fullPage true", commands[0].params)
			}
		})
	}
}
