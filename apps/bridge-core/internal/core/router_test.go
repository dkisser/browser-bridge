package core

import (
	"encoding/json"
	"log"
	"sync"
	"testing"
	"time"
)

// fakeBrowser is the TS test's `browser` double: only the members the router
// touches.
type fakeBrowser struct {
	mu      sync.Mutex
	online  bool
	sent    []string
	deliver bool // default true; set false to simulate the extension disappearing mid-flight
}

func (f *fakeBrowser) HasExtension() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.online
}

func (f *fakeBrowser) SendToExtension(text string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, text)
	return f.deliver
}

func (f *fakeBrowser) sentMessages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sent...)
}

type fakeRegistry struct {
	mu       sync.Mutex
	statuses [][2]string
}

func (f *fakeRegistry) SetStatus(browserID string, status BrowserStatus) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses = append(f.statuses, [2]string{browserID, string(status)})
	return true
}

// fakeSender is the TS test's inbound WS double.
type fakeSender struct {
	mu   sync.Mutex
	sent []string
}

func (f *fakeSender) Send(text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, text)
}

func (f *fakeSender) sentMessages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sent...)
}

func makeRouter(t *testing.T, stateOpts ...StateOption) (*Router, *StateManager, *fakeBrowser, *fakeRegistry) {
	t.Helper()
	t.Setenv("BB_HOME", t.TempDir())
	st, err := NewStateManager(stateOpts...)
	if err != nil {
		t.Fatalf("NewStateManager: %v", err)
	}
	browser := &fakeBrowser{deliver: true}
	reg := &fakeRegistry{}
	return NewRouter(st, browser, reg, log.Default()), st, browser, reg
}

func commandEnvelope(browserID, id string) Envelope {
	return Envelope{
		ID:        id,
		Type:      TypeCommand,
		BrowserID: browserID,
		Payload:   json.RawMessage(`{"command":"navigate","params":{}}`),
		Timestamp: time.Now().UnixMilli(),
	}
}

func decodePayload(t *testing.T, text string) ResponsePayload {
	t.Helper()
	env, err := Decode(text)
	if err != nil {
		t.Fatalf("decode %q: %v", text, err)
	}
	var payload ResponsePayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("decode payload of %q: %v", text, err)
	}
	return payload
}

func TestBuffersWhileExtensionBetweenReconnects(t *testing.T) {
	r, st, _, _ := makeRouter(t)
	st.SetStatus(StatusIdleWait)
	sender := &fakeSender{}

	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c1"), sender)

	// No immediate browser_offline / cannot_buffer: the command is held for
	// a fast reconnect.
	if got := sender.sentMessages(); len(got) != 0 {
		t.Fatalf("sender got %v, want no response yet", got)
	}
	if _, ok := st.GetBufferedCommand(); !ok {
		t.Fatal("command was not buffered")
	}
}

func TestDeliversBufferedCommandOnConnect(t *testing.T) {
	r, st, browser, _ := makeRouter(t)
	st.SetStatus(StatusIdleWait)
	timedOut := make(chan struct{}, 1)
	if !st.BufferCommand(`{"id":"buffered"}`, func() { timedOut <- struct{}{} }) {
		t.Fatal("did not buffer")
	}

	r.HandleBrowserConnect()

	// Regression: flipping status before reading the buffer cleared it (and
	// canceled its timeout) on the way in, so the command was dropped with
	// no response at all.
	if got := browser.sentMessages(); len(got) != 1 || got[0] != `{"id":"buffered"}` {
		t.Fatalf("extension got %v, want the buffered command", got)
	}
	select {
	case <-timedOut:
		t.Fatal("buffer timeout fired after the flush")
	default:
	}
}

func TestConnectConsumesBufferAndFlipsOnline(t *testing.T) {
	r, st, _, reg := makeRouter(t)
	st.SetStatus(StatusIdleWait)
	if !st.BufferCommand(`{"id":"buffered"}`, func() {}) {
		t.Fatal("did not buffer")
	}

	r.HandleBrowserConnect()

	if _, ok := st.GetBufferedCommand(); ok {
		t.Fatal("buffer was not consumed")
	}
	if st.Status() != StatusOnline {
		t.Fatalf("status = %s, want online", st.Status())
	}
	if len(reg.statuses) != 1 || reg.statuses[0] != [2]string{st.BrowserID(), "online"} {
		t.Fatalf("registry statuses = %v", reg.statuses)
	}
}

func TestDisconnectReturnsToIdleWait(t *testing.T) {
	r, st, _, reg := makeRouter(t)
	st.SetStatus(StatusOnline)
	reg.SetStatus(st.BrowserID(), StatusOnline)

	r.HandleBrowserDisconnect()

	if st.Status() != StatusIdleWait {
		t.Fatalf("status = %s, want idle_wait", st.Status())
	}
	last := reg.statuses[len(reg.statuses)-1]
	if last != [2]string{st.BrowserID(), "offline"} {
		t.Fatalf("last registry status = %v, want offline", last)
	}
}

func TestForwardsToExtensionWhenConnected(t *testing.T) {
	r, st, browser, _ := makeRouter(t)
	st.SetStatus(StatusOnline)
	browser.online = true
	sender := &fakeSender{}

	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c1"), sender)

	if got := sender.sentMessages(); len(got) != 0 {
		t.Fatalf("sender got %v, want nothing yet", got)
	}
	got := browser.sentMessages()
	if len(got) != 1 {
		t.Fatalf("extension got %d messages, want 1", len(got))
	}
	env, err := Decode(got[0])
	if err != nil {
		t.Fatal(err)
	}
	if env.ID != "c1" || env.Type != TypeCommand || env.BrowserID != st.BrowserID() {
		t.Fatalf("forwarded envelope = %+v", env)
	}
}

func TestBrowserOfflineWhenStateOffline(t *testing.T) {
	r, st, _, _ := makeRouter(t)
	// Fresh state is offline.
	sender := &fakeSender{}

	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c1"), sender)

	got := sender.sentMessages()
	if len(got) != 1 {
		t.Fatalf("sender got %d messages, want 1", len(got))
	}
	payload := decodePayload(t, got[0])
	if payload.Status != "error" || payload.Error != "browser_offline" || payload.Message != "Browser is offline" {
		t.Fatalf("payload = %+v", payload)
	}
	env, _ := Decode(got[0])
	if env.ID != "c1" {
		t.Fatalf("response id = %q, want c1", env.ID)
	}
}

func TestCannotBufferWhenABufferIsAlreadyHeld(t *testing.T) {
	r, st, _, _ := makeRouter(t)
	st.SetStatus(StatusIdleWait)
	first := &fakeSender{}
	second := &fakeSender{}

	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c1"), first)
	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c2"), second)

	if got := first.sentMessages(); len(got) != 0 {
		t.Fatalf("first sender got %v, want nothing yet", got)
	}
	got := second.sentMessages()
	if len(got) != 1 {
		t.Fatalf("second sender got %d messages, want 1", len(got))
	}
	if payload := decodePayload(t, got[0]); payload.Error != "cannot_buffer" {
		t.Fatalf("payload = %+v, want cannot_buffer", payload)
	}
}

func TestBufferedCommandTimesOutIntoSWTimeout(t *testing.T) {
	r, st, _, _ := makeRouter(t, WithBufferTimeout(20*time.Millisecond))
	st.SetStatus(StatusIdleWait)

	responded := make(chan string, 1)
	sender := senderFunc(func(text string) { responded <- text })

	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c1"), sender)

	select {
	case text := <-responded:
		payload := decodePayload(t, text)
		if payload.Status != "error" || payload.Error != "sw_timeout" || payload.Message != "Service worker did not wake up" {
			t.Fatalf("payload = %+v, want sw_timeout", payload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no sw_timeout within the buffer budget")
	}
}

// TestResponseFanoutByID is the router's headline behavior: a response from
// the extension is routed to the exact inbound connection that submitted the
// command, not broadcast.
func TestResponseFanoutByID(t *testing.T) {
	r, st, browser, _ := makeRouter(t)
	st.SetStatus(StatusOnline)
	browser.online = true
	first := &fakeSender{}
	second := &fakeSender{}

	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c1"), first)
	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c2"), second)

	r.HandleBrowserResponse(Envelope{
		ID:        "c2",
		Type:      TypeResponse,
		BrowserID: st.BrowserID(),
		Payload:   json.RawMessage(`{"status":"ok","data":{"title":"t"}}`),
		Timestamp: time.Now().UnixMilli(),
	})

	if got := first.sentMessages(); len(got) != 0 {
		t.Fatalf("first sender got %v, want nothing", got)
	}
	got := second.sentMessages()
	if len(got) != 1 {
		t.Fatalf("second sender got %d messages, want 1", len(got))
	}
	env, err := Decode(got[0])
	if err != nil {
		t.Fatal(err)
	}
	if env.ID != "c2" || env.Type != TypeResponse {
		t.Fatalf("routed envelope = %+v", env)
	}

	// The id is consumed: a duplicate response for c2 routes nowhere.
	r.HandleBrowserResponse(Envelope{
		ID:        "c2",
		Type:      TypeResponse,
		BrowserID: st.BrowserID(),
		Payload:   json.RawMessage(`{"status":"ok"}`),
		Timestamp: time.Now().UnixMilli(),
	})
	if got := second.sentMessages(); len(got) != 1 {
		t.Fatalf("second sender got %d messages, want still 1", len(got))
	}
}

func TestUnknownResponseIDRoutesNowhere(t *testing.T) {
	r, _, _, _ := makeRouter(t)
	// Must not panic or block.
	r.HandleBrowserResponse(Envelope{
		ID:        "never-seen",
		Type:      TypeResponse,
		Payload:   json.RawMessage(`{"status":"ok"}`),
		Timestamp: time.Now().UnixMilli(),
	})
}

func TestBrowserEventsUpdateRegistry(t *testing.T) {
	r, st, _, reg := makeRouter(t)
	event := func(name string) Envelope {
		payload, _ := json.Marshal(map[string]string{"event": name})
		return Envelope{
			ID:        "e1",
			Type:      TypeEvent,
			Payload:   payload,
			Timestamp: time.Now().UnixMilli(),
		}
	}

	r.HandleBrowserEvent(event("register"))
	r.HandleBrowserEvent(event("online"))
	r.HandleBrowserEvent(event("offline"))
	r.HandleBrowserEvent(event("something-else"))

	// An event without browserId falls back to the state's browserId.
	want := [][2]string{
		{st.BrowserID(), "online"},
		{st.BrowserID(), "online"},
		{st.BrowserID(), "offline"},
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if len(reg.statuses) != len(want) {
		t.Fatalf("statuses = %v, want %v", reg.statuses, want)
	}
	for i := range want {
		if reg.statuses[i] != want[i] {
			t.Fatalf("statuses = %v, want %v", reg.statuses, want)
		}
	}
}

// senderFunc adapts a function to TextSender.
type senderFunc func(text string)

func (f senderFunc) Send(text string) { f(text) }

// TestHandleInboundCommandRemovesRouteWhenSendToExtensionFails closes the
// leak that existed before the fix: when HasExtension was true but
// SendToExtension returned false (extension dropped between the two checks),
// the entry stayed in inboundByID forever because the response path would
// never fire — the sender was pinned until the process restarted. The fix
// removes the route whenever SendToExtension reports it could not deliver,
// so a late response from a different code path routes nowhere.
func TestHandleInboundCommandRemovesRouteWhenSendToExtensionFails(t *testing.T) {
	r, st, browser, _ := makeRouter(t)
	st.SetStatus(StatusOnline)
	browser.online = true   // HasExtension -> true
	browser.deliver = false // SendToExtension -> false (extension disconnected)

	sender := &fakeSender{}
	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c1"), sender)

	// SendToExtension was supposed to record the frame (it does, even on
	// failure — that is intentional, for debugging) but report delivery
	// failure. The inbound route must have been removed.
	if got := r.lookupInbound("c1"); got != nil {
		t.Fatal("inboundByID[c1] is still set after a failed SendToExtension")
	}

	// A late response must route nowhere because the route was cleared at
	// dispatch time. Pre-fix this would have re-pinned the sender.
	r.HandleBrowserResponse(Envelope{
		ID:        "c1",
		Type:      TypeResponse,
		BrowserID: st.BrowserID(),
		Payload:   json.RawMessage(`{"status":"ok"}`),
		Timestamp: time.Now().UnixMilli(),
	})
	if got := sender.sentMessages(); len(got) != 0 {
		t.Fatalf("sender got %v, want nothing — route should have been cleared", got)
	}
}

// TestHandleInboundCommandHappyPathKeepsRouteUntilResponse documents the
// happy path: when SendToExtension succeeds, the route stays so the response
// can be routed back to the exact sender. This is the existing behavior and
// pins the fix to the failure path only — we are not removing the lookup on
// success.
func TestHandleInboundCommandHappyPathKeepsRouteUntilResponse(t *testing.T) {
	r, st, browser, _ := makeRouter(t)
	st.SetStatus(StatusOnline)
	browser.online = true
	browser.deliver = true

	first := &fakeSender{}
	second := &fakeSender{}
	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c1"), first)
	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c2"), second)

	// Both routes must be live while the extension has the commands.
	if r.lookupInbound("c1") == nil || r.lookupInbound("c2") == nil {
		t.Fatal("routes were cleared prematurely on the happy path")
	}

	r.HandleBrowserResponse(Envelope{
		ID:        "c1",
		Type:      TypeResponse,
		BrowserID: st.BrowserID(),
		Payload:   json.RawMessage(`{"status":"ok"}`),
		Timestamp: time.Now().UnixMilli(),
	})
	if r.lookupInbound("c1") != nil {
		t.Fatal("c1 route should be cleared after the response")
	}
	if r.lookupInbound("c2") == nil {
		t.Fatal("c2 route should still be live — only c1 was answered")
	}
}
