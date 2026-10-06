package http

import (
	"encoding/json"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"browser-bridge/internal/core"
	"browser-bridge/internal/memory"
)

// stubHook is a core.MemoryHook double whose note is fixed text, so these tests
// assert the *plumbing* — that a card reaches a tool result and nowhere else —
// without standing up the learner.
type stubHook struct {
	note     string
	askedFor []string
	askedTab []int
}

func (h *stubHook) RecordCommand(string, string, string, int, map[string]any)      {}
func (h *stubHook) RecordResult(string, string, string, int, core.ResponsePayload) {}

func (h *stubHook) TakeSiteNote(command, _ string, tabID int) string {
	h.askedFor = append(h.askedFor, command)
	h.askedTab = append(h.askedTab, tabID)
	return h.note
}

// tab:new is addressed to no tab and lands on one, so the adapter has to ask
// about the tab the extension reports rather than the one it sent. Asking about
// the addressed tab — 0 — finds no host, and the landing silently injects
// nothing: the agent opens a known site with tab_new and is told nothing at all,
// then gets only the site map (OnlyMap) on the next snapshot, never the
// failure and procedure tiers ADR-0019 puts at the landing.
func TestTabNewAsksAboutTheTabItLandedOn(t *testing.T) {
	router := &fakeRouter{
		host: "shop.test",
		script: func(c capturedCommand) (core.ResponsePayload, bool) {
			return core.ResponsePayload{
				Status: "ok",
				Data:   json.RawMessage(`{"id":42,"url":"https://shop.test/cart"}`),
			}, true
		},
	}
	h := &stubHook{note: "a card"}
	session := newServerWithHook(t, h, router)

	callTool(t, session, "tab_new", map[string]any{"url": "https://shop.test/cart"})

	if len(h.askedTab) != 1 {
		t.Fatalf("the card was asked for %d time(s), want once", len(h.askedTab))
	}
	if h.askedTab[0] != 42 {
		t.Errorf("asked about tab %d, want 42 — the tab the extension reported", h.askedTab[0])
	}
	for _, tab := range router.askedAbout() {
		if tab == 0 {
			t.Error("the adapter resolved the host against tab 0, which tab:new was addressed to and never lands on")
		}
	}
}

func newServerWithHook(t *testing.T, h core.MemoryHook, router *fakeRouter) *mcp.ClientSession {
	t.Helper()
	srv := NewMCP(MCPOptions{
		Router:         router,
		Registry:       fakeRegistry{browsers: []core.BrowserConnection{onlineBrowser("b-1")}},
		DefaultTimeout: 10 * time.Second,
		Version:        "0.3.2",
		Logger:         log.New(io.Discard, "", 0),
		Memory:         h,
	})
	return newTestClient(t, srv)
}

// The card has to reach the agent on the call that lands it on a known host.
func TestNavigateResultCarriesTheSiteCard(t *testing.T) {
	hook := &stubHook{note: "[" + memory.InjectionLabel + "] news.example.com\n  - text container → @e1"}
	router := &fakeRouter{script: func(c capturedCommand) (core.ResponsePayload, bool) {
		return core.ResponsePayload{Status: "ok", Message: "Navigated to https://news.example.com/"}, true
	}}
	session := newServerWithHook(t, hook, router)

	text := resultText(t, callTool(t, session, "navigate", map[string]any{
		"url": "https://news.example.com/", "tab_id": 1,
	}))

	if !strings.Contains(text, memory.InjectionLabel) {
		t.Fatalf("navigate result does not carry the card:\n%s", text)
	}
	if !strings.Contains(text, "Navigated to") {
		t.Errorf("the tool's own message was lost:\n%s", text)
	}
	if strings.Index(text, "Navigated to") > strings.Index(text, memory.InjectionLabel) {
		t.Error("the card was placed before the tool's own output; it must come after")
	}
}

func TestSnapshotResultCarriesTheSiteCard(t *testing.T) {
	hook := &stubHook{note: "[" + memory.InjectionLabel + "] news.example.com\n  - text container → @e1"}
	router := &fakeRouter{script: func(c capturedCommand) (core.ResponsePayload, bool) {
		data, _ := json.Marshal(map[string]any{
			"snapshot":      "Page: T | https://news.example.com/\nlink [World] href=\"/w\" @e1",
			"truncated":     false,
			"nodes_total":   4,
			"nodes_emitted": 2,
			"tier":          0,
		})
		return core.ResponsePayload{Status: "ok", Data: data}, true
	}}
	session := newServerWithHook(t, hook, router)

	text := resultText(t, callTool(t, session, "snapshot", map[string]any{"tab_id": 1}))

	if !strings.Contains(text, memory.InjectionLabel) {
		t.Fatalf("snapshot result does not carry the card:\n%s", text)
	}
	// The snapshot's own payload — the whole point of the call — must survive.
	if !strings.Contains(text, "link [World]") {
		t.Errorf("the snapshot tree was lost:\n%s", text)
	}
	if !strings.Contains(text, "[1/4 nodes") && !strings.Contains(text, "nodes | tier=0") {
		t.Errorf("the snapshot stats line was lost:\n%s", text)
	}
}

// Asking the hook on every call and rendering it everywhere would put the card
// in the middle of unrelated output and spend the budget on every call. It is
// asked once per call and the Manager decides; here we pin that the hook is at
// least consulted for the landing commands.
func TestHookIsConsultedForLandingsAndSnapshots(t *testing.T) {
	hook := &stubHook{}
	router := &fakeRouter{script: func(c capturedCommand) (core.ResponsePayload, bool) {
		switch c.command {
		case "navigate":
			return core.ResponsePayload{Status: "ok", Message: "ok"}, true
		case "snapshot":
			data, _ := json.Marshal(map[string]any{
				"snapshot": "Page: T | https://x.test/", "nodes_total": 1, "nodes_emitted": 1,
			})
			return core.ResponsePayload{Status: "ok", Data: data}, true
		case "gettext":
			data, _ := json.Marshal(map[string]any{"text": "hello"})
			return core.ResponsePayload{Status: "ok", Data: data}, true
		}
		return core.ResponsePayload{Status: "ok", Message: "ok"}, true
	}}
	session := newServerWithHook(t, hook, router)

	callTool(t, session, "navigate", map[string]any{"url": "https://x.test/", "tab_id": 1})
	callTool(t, session, "snapshot", map[string]any{"tab_id": 1})
	callTool(t, session, "get_text", map[string]any{"selector": "#a", "tab_id": 1})

	asked := map[string]int{}
	for _, c := range hook.askedFor {
		asked[c]++
	}
	if asked["navigate"] != 1 || asked["snapshot"] != 1 {
		t.Errorf("hook asked for %v, want navigate and snapshot once each", hook.askedFor)
	}
	// The hook is keyed on the *wire* command name, not the MCP tool name: the
	// Manager's rules (snapshot, the landing set) are written in terms of the
	// commands the extension receives, so `get_text` arrives as `gettext`.
	if asked["gettext"] != 1 {
		t.Errorf("hook asked for %v; it must see every call, keyed on the wire command name", hook.askedFor)
	}
}

func TestNoCardOnAFailedCall(t *testing.T) {
	hook := &stubHook{note: "should not appear"}
	router := &fakeRouter{script: func(c capturedCommand) (core.ResponsePayload, bool) {
		return core.ResponsePayload{Status: "error", Error: "selector_not_found", Message: "no match"}, true
	}}
	session := newServerWithHook(t, hook, router)

	result := callTool(t, session, "get_text", map[string]any{"selector": ".nope", "tab_id": 1})
	if result.IsError {
		// The tool reported failure; the card must not have been attached to it.
		if strings.Contains(resultText(t, result), "should not appear") {
			t.Errorf("a card was attached to a failed call:\n%s", resultText(t, result))
		}
		return
	}
	if strings.Contains(resultText(t, result), "should not appear") {
		t.Errorf("a card was attached to a failed call:\n%s", resultText(t, result))
	}
}

// Without a hook the output must be byte-for-byte what it was before the
// feature, so an install that never learns behaves identically.
func TestNoHookLeavesOutputUnchanged(t *testing.T) {
	router := &fakeRouter{script: func(c capturedCommand) (core.ResponsePayload, bool) {
		return core.ResponsePayload{Status: "ok", Message: "Navigated to https://x.test/"}, true
	}}
	session := newTestClient(t, newTestServer(router, []core.BrowserConnection{onlineBrowser("b-1")}))

	text := resultText(t, callTool(t, session, "navigate", map[string]any{
		"url": "https://x.test/", "tab_id": 1,
	}))
	if text != "Navigated to https://x.test/" {
		t.Errorf("output changed with no hook attached: %q", text)
	}
}

func TestSiteNoteNeverReachesTheWire(t *testing.T) {
	// The card rides in-process on the payload (json:"-"). If it ever became a
	// wire field, ADR-0012's frozen envelope would change under the extension.
	raw, err := json.Marshal(core.ResponsePayload{Status: "ok", Message: "ok", SiteNote: "secret card"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret card") {
		t.Errorf("SiteNote was serialized onto the wire: %s", raw)
	}
	if strings.Contains(string(raw), "SiteNote") || strings.Contains(string(raw), "siteNote") {
		t.Errorf("a SiteNote key appeared on the wire: %s", raw)
	}
}

var _ core.MemoryHook = (*stubHook)(nil)
