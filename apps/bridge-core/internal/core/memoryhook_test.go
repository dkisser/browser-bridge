package core

import (
	"encoding/json"
	"io"
	"log"
	"sync"
	"testing"
	"time"
)

// recordingHook is a MemoryHook double: it captures what the router told it,
// in order, so the wiring can be asserted without a real store.
type recordingHook struct {
	mu       sync.Mutex
	commands []string
	results  []string
}

func (h *recordingHook) RecordCommand(envelopeID, command, host string, tabID int, args map[string]any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.commands = append(h.commands, command)
}

func (h *recordingHook) RecordResult(envelopeID, command, host string, tabID int, payload ResponsePayload) {
	h.mu.Lock()
	defer h.mu.Unlock()
	outcome := payload.Status
	if payload.Error != "" {
		outcome = payload.Error
	}
	h.results = append(h.results, command+":"+outcome)
}

func (h *recordingHook) TakeSiteNote(command, host string, tabID int) string {
	return ""
}

// snapshot copies what the hook was told, under one lock.
func (h *recordingHook) snapshot() (commands, results []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.commands...), append([]string(nil), h.results...)
}

func makeRouterWithHook(t *testing.T, h MemoryHook) (*Router, *StateManager) {
	t.Helper()
	t.Setenv("BB_HOME", t.TempDir())
	st, err := NewStateManager()
	if err != nil {
		t.Fatalf("NewStateManager: %v", err)
	}
	st.SetStatus(StatusOnline)
	return NewRouter(st, &fakeBrowser{deliver: true}, &fakeRegistry{}, log.New(io.Discard, "", 0), WithMemoryHook(h)), st
}

func TestRouterRecordsBothHalvesOfACall(t *testing.T) {
	h := &recordingHook{}
	r, st := makeRouterWithHook(t, h)
	sender := &fakeSender{}

	env := Envelope{
		ID:        "m1",
		Type:      TypeCommand,
		BrowserID: st.BrowserID(),
		Payload:   json.RawMessage(`{"command":"navigate","tabId":3,"params":{"url":"https://x.test/"}}`),
		Timestamp: time.Now().UnixMilli(),
	}
	r.HandleInboundCommand(env, sender)

	if got, _ := h.snapshot(); len(got) != 1 || got[0] != "navigate" {
		t.Errorf("recorded commands = %v, want [navigate]", got)
	}

	// The response path is the only place that knows the command matched up
	// with its outcome.
	r.HandleBrowserResponse(Envelope{
		ID:        "m1",
		Type:      TypeResponse,
		BrowserID: st.BrowserID(),
		Payload:   json.RawMessage(`{"status":"ok","data":{"url":"https://x.test/"}}`),
		Timestamp: time.Now().UnixMilli(),
	})

	_, results := h.snapshot()
	if len(results) != 1 || results[0] != "navigate:ok" {
		t.Errorf("recorded results = %v, want [navigate:ok]", results)
	}
}

func TestRouterRecordsAFailedExtensionCall(t *testing.T) {
	h := &recordingHook{}
	r, st := makeRouterWithHook(t, h)
	sender := &fakeSender{}

	r.HandleInboundCommand(Envelope{
		ID: "m2", Type: TypeCommand, BrowserID: st.BrowserID(),
		Payload:   json.RawMessage(`{"command":"gettext","tabId":1,"params":{"selector":".nope"}}`),
		Timestamp: time.Now().UnixMilli(),
	}, sender)

	r.HandleBrowserResponse(Envelope{
		ID: "m2", Type: TypeResponse, BrowserID: st.BrowserID(),
		Payload:   json.RawMessage(`{"status":"error","error":"selector_not_found","message":"no match"}`),
		Timestamp: time.Now().UnixMilli(),
	})

	_, results := h.snapshot()
	if len(results) != 1 || results[0] != "gettext:selector_not_found" {
		t.Errorf("recorded results = %v, want the extension's error code", results)
	}
}

// tab:new is the one command addressed to no tab that lands on one, and its
// result carries the tab it created. Keyed on the addressed tab instead, the
// new tab's host went under key 0 — a key nothing asks for — and the tab that
// actually existed resolved no host, so its first snapshot was offered no card
// and the learner had no site to attribute it to.
func TestTabNewKeysItsHostOnTheTabItCreated(t *testing.T) {
	h := &recordingHook{}
	r, st := makeRouterWithHook(t, h)
	sender := &fakeSender{}

	// The wire command name is tab:new — tab_new is the MCP tool's name, and the
	// envelope carries the wire one. No tabId either: tab:new is what opens the
	// next tab, so it cannot name one.
	r.HandleInboundCommand(Envelope{
		ID: "t1", Type: TypeCommand, BrowserID: st.BrowserID(),
		Payload:   json.RawMessage(`{"command":"tab:new","tabId":0,"params":{"url":"https://shop.test/cart"}}`),
		Timestamp: time.Now().UnixMilli(),
	}, sender)

	// The extension answers with the tab it created (background.ts).
	r.HandleBrowserResponse(Envelope{
		ID: "t1", Type: TypeResponse, BrowserID: st.BrowserID(),
		Payload:   json.RawMessage(`{"status":"ok","data":{"id":42,"url":"https://shop.test/cart"}}`),
		Timestamp: time.Now().UnixMilli(),
	})

	if got := r.HostForTab(42); got != "shop.test" {
		t.Errorf("HostForTab(42) = %q, want shop.test", got)
	}
	if got := r.HostForTab(0); got != "" {
		t.Errorf("HostForTab(0) = %q, want empty: the addressed tab is not the tab that was opened", got)
	}
}

// A router-generated failure is the most instructive kind — the agent asked a
// browser that was not there — so it has to reach the store like any other.
func TestRouterRecordsItsOwnSynthesizedErrors(t *testing.T) {
	h := &recordingHook{}
	t.Setenv("BB_HOME", t.TempDir())
	st, err := NewStateManager()
	if err != nil {
		t.Fatal(err)
	}
	// Left offline so HandleInboundCommand takes the browser_offline branch.
	r := NewRouter(st, &fakeBrowser{deliver: true}, &fakeRegistry{}, log.New(io.Discard, "", 0), WithMemoryHook(h))
	sender := &fakeSender{}

	r.HandleInboundCommand(Envelope{
		ID: "m3", Type: TypeCommand, BrowserID: st.BrowserID(),
		Payload:   json.RawMessage(`{"command":"click","tabId":1,"params":{"selector":"#a"}}`),
		Timestamp: time.Now().UnixMilli(),
	}, sender)

	_, results := h.snapshot()
	if len(results) != 1 || results[0] != "click:browser_offline" {
		t.Errorf("recorded results = %v, want [click:browser_offline]", results)
	}
}

func TestRouterWithoutHookBehavesAsBefore(t *testing.T) {
	// Memory is an opt-in capability: a control plane with no store attached
	// must not change behaviour, or every existing install would depend on it.
	r, st, _, _ := makeRouter(t)
	st.SetStatus(StatusOnline)
	sender := &fakeSender{}

	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c9"), sender)
	if got := sender.sentMessages(); len(got) != 0 {
		t.Fatalf("sender got %v before any response", got)
	}
	r.HandleBrowserResponse(Envelope{
		ID: "c9", Type: TypeResponse, BrowserID: st.BrowserID(),
		Payload:   json.RawMessage(`{"status":"ok","data":{}}`),
		Timestamp: time.Now().UnixMilli(),
	})
	msgs := sender.sentMessages()
	if len(msgs) != 1 {
		t.Fatalf("sender got %v, want exactly one response", msgs)
	}
	if p := decodePayload(t, msgs[0]); p.Status != "ok" {
		t.Errorf("status = %q, want ok", p.Status)
	}
}

// The pending map exists only to describe a response; it must not outlive the
// route or it becomes a slow leak on every disconnected client.
func TestRouterDropsPendingCallsOnEveryCleanupPath(t *testing.T) {
	r, st, _, _ := makeRouter(t)
	st.SetStatus(StatusOnline)
	sender := &fakeSender{}

	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "leak"), sender)
	r.mu.Lock()
	_, stillThere := r.pending["leak"]
	r.mu.Unlock()
	if !stillThere {
		t.Fatal("pending entry was not recorded")
	}

	r.RemoveClient(sender)
	r.mu.Lock()
	_, stillThere = r.pending["leak"]
	r.mu.Unlock()
	if stillThere {
		t.Error("RemoveClient left a pending entry behind")
	}
}
