package http

import (
	"testing"

	"browser-bridge/internal/core"
)

// TestResolveBrowser is the table-driven port of resolveBrowser in
// src/mcp/browser-resolver.ts.
func TestResolveBrowser(t *testing.T) {
	tests := []struct {
		name     string
		explicit string
		browsers []core.BrowserConnection
		wantID   string
		wantFail string
	}{
		{
			name:     "explicit online match",
			explicit: "b-1",
			browsers: []core.BrowserConnection{onlineBrowser("b-1"), onlineBrowser("b-2")},
			wantID:   "b-1",
		},
		{
			name:     "explicit missing",
			explicit: "b-9",
			browsers: []core.BrowserConnection{onlineBrowser("b-1")},
			wantFail: `Browser "b-9" is not connected.`,
		},
		{
			name:     "explicit not online",
			explicit: "b-2",
			browsers: []core.BrowserConnection{
				onlineBrowser("b-1"),
				{BrowserID: "b-2", UserID: "extension", Status: core.StatusOffline, LastSeen: 1},
			},
			wantFail: `Browser "b-2" is not online (status: offline).`,
		},
		{
			name:     "implicit none online",
			browsers: []core.BrowserConnection{{BrowserID: "b-1", UserID: "extension", Status: core.StatusOffline, LastSeen: 1}},
			wantFail: "No browser connected. Start the extension/local-proxy first.",
		},
		{
			name:     "implicit single online",
			browsers: []core.BrowserConnection{onlineBrowser("b-1"), {BrowserID: "b-2", UserID: "extension", Status: core.StatusIdleWait, LastSeen: 1}},
			wantID:   "b-1",
		},
		{
			name:     "implicit multiple online",
			browsers: []core.BrowserConnection{onlineBrowser("b-1"), onlineBrowser("b-2")},
			wantFail: "Multiple browsers are online. Call set_browser with one of:\n- b-1 (online)\n- b-2 (online)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, fail := resolveBrowser(tt.explicit, tt.browsers)
			if id != tt.wantID || fail != tt.wantFail {
				t.Errorf("resolveBrowser(%q) = (%q, %q), want (%q, %q)", tt.explicit, id, fail, tt.wantID, tt.wantFail)
			}
		})
	}
}

// TestSessionStore pins and reads back per-session browserIds.
func TestSessionStore(t *testing.T) {
	store := newSessionStore()
	if got := store.get("s1"); got != "" {
		t.Fatalf("get before set = %q, want empty", got)
	}
	store.set("s1", "b-1")
	store.set("s2", "b-2")
	if got := store.get("s1"); got != "b-1" {
		t.Errorf("get s1 = %q, want b-1", got)
	}
	if got := store.get("s2"); got != "b-2" {
		t.Errorf("get s2 = %q, want b-2", got)
	}
	// Re-pinning overwrites.
	store.set("s1", "b-3")
	if got := store.get("s1"); got != "b-3" {
		t.Errorf("get s1 after re-pin = %q, want b-3", got)
	}
}

// TestWithRecoveryHint covers the hint appendix rules of
// src/mcp/command-client.ts.
func TestWithRecoveryHint(t *testing.T) {
	tests := []struct {
		name    string
		in      core.ResponsePayload
		wantMsg string
		wantErr string
	}{
		{
			name:    "ok payload untouched",
			in:      core.ResponsePayload{Status: "ok", Message: "fine"},
			wantMsg: "fine",
		},
		{
			name:    "structured reason appends to message",
			in:      core.ResponsePayload{Status: "error", Reason: "tab_not_found", Message: "gone"},
			wantMsg: "gone The tab was closed between commands — call tab_list to discover valid tab ids.",
		},
		{
			name:    "structured reason appends to error without message",
			in:      core.ResponsePayload{Status: "error", Reason: "injection_failed", Error: "cs_unavailable"},
			wantErr: "cs_unavailable The extension could not inject its content script into this page. Verify host_permissions cover the origin.",
		},
		{
			name:    "unknown reason falls through untouched",
			in:      core.ResponsePayload{Status: "error", Reason: "mystery", Error: "boom"},
			wantErr: "boom",
		},
		{
			name:    "legacy No tab with id pattern",
			in:      core.ResponsePayload{Status: "error", Error: "No tab with id: 42"},
			wantErr: "No tab with id: 42 Call tab_list to discover valid tab ids for the selected browser.",
		},
		{
			name:    "legacy pattern case-insensitive",
			in:      core.ResponsePayload{Status: "error", Error: "no tab with id: 7"},
			wantErr: "no tab with id: 7 Call tab_list to discover valid tab ids for the selected browser.",
		},
		{
			name:    "legacy pattern suppressed when tab_list already mentioned",
			in:      core.ResponsePayload{Status: "error", Error: "No tab with id: 42 — see tab_list"},
			wantErr: "No tab with id: 42 — see tab_list",
		},
		{
			name:    "hint not doubled",
			in:      core.ResponsePayload{Status: "error", Reason: "no_listener", Message: "dead The content script did not respond. Reload the page or retry the command."},
			wantMsg: "dead The content script did not respond. Reload the page or retry the command.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := withRecoveryHint(tt.in)
			if got.Message != tt.wantMsg || got.Error != tt.wantErr {
				t.Errorf("withRecoveryHint = (msg %q, err %q), want (msg %q, err %q)", got.Message, got.Error, tt.wantMsg, tt.wantErr)
			}
		})
	}
}

// TestCommandErrorMessage covers the message ?? error ?? fallback chain.
func TestCommandErrorMessage(t *testing.T) {
	tests := []struct {
		name     string
		in       core.ResponsePayload
		fallback string
		want     string
	}{
		{name: "message wins", in: core.ResponsePayload{Message: "m", Error: "e"}, fallback: "f", want: "m"},
		{name: "error next", in: core.ResponsePayload{Error: "e"}, fallback: "f", want: "e"},
		{name: "fallback last", in: core.ResponsePayload{}, fallback: "f", want: "f"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := commandErrorMessage(tt.in, tt.fallback); got != tt.want {
				t.Errorf("commandErrorMessage = %q, want %q", got, tt.want)
			}
		})
	}
}
