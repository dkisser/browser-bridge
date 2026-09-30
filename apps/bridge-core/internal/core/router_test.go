package core

import (
	"encoding/json"
	"log"
	"strings"
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
		if payload.Status != "error" || payload.Error != "sw_timeout" || payload.Message != bufferExpiredMessage {
			t.Fatalf("payload = %+v, want sw_timeout", payload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no sw_timeout within the buffer budget")
	}
}

// TestBufferExpiryDoesNotBlameTheServiceWorker pins the *content* of the
// buffer-expiry message, not just its code.
//
// The code is a contract clients match on and has to stay sw_timeout. The
// message is the part a human reads, and it used to assert something the
// router cannot know: whether the service worker woke up. The command is
// buffered because the extension has no connection, and that connection can
// have ended for any number of reasons downstream of the service worker, so
// the sentence sent an operator to inspect the wrong subsystem. The claim
// under test is the narrower one this router can actually support.
func TestBufferExpiryDoesNotBlameTheServiceWorker(t *testing.T) {
	if strings.Contains(strings.ToLower(bufferExpiredMessage), "service worker") {
		t.Fatalf("buffer expiry message %q names a layer the router cannot "+
			"observe; it should describe the connection, not the service worker",
			bufferExpiredMessage)
	}
	// It still has to say something actionable rather than become a shrug.
	if !strings.Contains(bufferExpiredMessage, "not connected") {
		t.Fatalf("buffer expiry message %q does not say the browser was "+
			"unreachable, which is the condition the caller can act on", bufferExpiredMessage)
	}
}

// TestReplayedCommandFailsTheCallerWhenTheExtensionCannotBeReached covers
// the third site where an accepted command can be lost, and the only one
// where every safety net is already disarmed at the moment it happens.
//
// By the time HandleBrowserConnect replays the buffer, GetBufferedCommand has
// cleared the buffer and stopped its timeout — so the sw_timeout callback can
// no longer fire — and the buffered path never armed a route TTL, so nothing
// else will clean up either. The return value of the replay was discarded, so
// a send that failed left the caller waiting on its own deadline and the
// router holding an inbound route that would never be released.
func TestReplayedCommandFailsTheCallerWhenTheExtensionCannotBeReached(t *testing.T) {
	r, st, browser, _ := makeRouter(t)
	st.SetStatus(StatusIdleWait)

	responded := make(chan string, 1)
	sender := senderFunc(func(text string) { responded <- text })

	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c1"), sender)
	select {
	case text := <-responded:
		t.Fatalf("sender got %q while the command is still buffered, want nothing", text)
	default:
	}

	// The extension reconnects, but the upgrade produced no usable
	// connection — SendToExtension says so.
	browser.online = true
	browser.deliver = false
	r.HandleBrowserConnect()

	select {
	case text := <-responded:
		payload := decodePayload(t, text)
		if payload.Status != "error" || payload.Error != "extension_send_failed" {
			t.Fatalf("payload = %+v, want extension_send_failed", payload)
		}
		if payload.Message == "" {
			t.Fatal("extension_send_failed carried no message; the caller is " +
				"left with a code and no explanation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("caller was never answered after the replay failed")
	}

	// And the route is released, rather than pinned until the process exits.
	r.mu.Lock()
	_, still := r.inboundByID["c1"]
	r.mu.Unlock()
	if still {
		t.Fatal("inboundByID[c1] survived a failed replay; the route leaks")
	}
}

// The happy path: a replay that goes through must not also produce an error.
func TestReplayedCommandStaysSilentWhenItIsDelivered(t *testing.T) {
	r, st, browser, _ := makeRouter(t)
	st.SetStatus(StatusIdleWait)

	responded := make(chan string, 1)
	sender := senderFunc(func(text string) { responded <- text })

	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c1"), sender)
	browser.online = true
	browser.deliver = true
	r.HandleBrowserConnect()

	select {
	case text := <-responded:
		t.Fatalf("sender got %q; a delivered replay must leave the caller waiting "+
			"for the extension's real response", text)
	case <-time.After(50 * time.Millisecond):
	}
	if got := browser.sentMessages(); len(got) != 1 {
		t.Fatalf("extension got %d frames, want the 1 buffered command", len(got))
	}
	// The route survives: the response still has to reach this caller.
	r.mu.Lock()
	_, alive := r.inboundByID["c1"]
	r.mu.Unlock()
	if !alive {
		t.Fatal("inboundByID[c1] was removed after a successful replay")
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

// TestRemoveClientDropsAllRoutesForSender closes the leak that existed
// before the inbound-side disconnect hook: a CLI process killed between
// submitting a command and receiving the response used to pin its sender
// in inboundByID until the extension answered (which could be never).
// RemoveClient drops every route pointing at the sender.
func TestRemoveClientDropsAllRoutesForSender(t *testing.T) {
	r, st, browser, _ := makeRouter(t)
	st.SetStatus(StatusOnline)
	browser.online = true
	browser.deliver = true

	client := &fakeSender{}
	other := &fakeSender{}
	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c1"), client)
	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c2"), client)
	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c3"), other)

	r.RemoveClient(client)

	if r.lookupInbound("c1") != nil {
		t.Fatal("c1 route should be cleared after RemoveClient(client)")
	}
	if r.lookupInbound("c2") != nil {
		t.Fatal("c2 route should be cleared after RemoveClient(client)")
	}
	if r.lookupInbound("c3") == nil {
		t.Fatal("c3 belongs to a different client and must not be cleared")
	}
}

// TestRemoveRouteClearsSingleEntry verifies the single-id cleanup that
// sendCommand calls on context cancel / timeout. Pre-fix the router relied
// on HandleBrowserResponse being the only cleanup; a slow call that already
// returned left its sender pinned until the extension answered.
func TestRemoveRouteClearsSingleEntry(t *testing.T) {
	r, st, browser, _ := makeRouter(t)
	st.SetStatus(StatusOnline)
	browser.online = true
	browser.deliver = true

	sender := &fakeSender{}
	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c1"), sender)
	if r.lookupInbound("c1") == nil {
		t.Fatal("setup: c1 should be tracked")
	}

	r.RemoveRoute("c1")
	if r.lookupInbound("c1") != nil {
		t.Fatal("RemoveRoute(c1) should have cleared the entry")
	}

	// A late response now must route nowhere.
	r.HandleBrowserResponse(Envelope{
		ID:        "c1",
		Type:      TypeResponse,
		BrowserID: st.BrowserID(),
		Payload:   json.RawMessage(`{"status":"ok"}`),
		Timestamp: time.Now().UnixMilli(),
	})
	if got := sender.sentMessages(); len(got) != 0 {
		t.Fatalf("sender got %v, want nothing", got)
	}
}

// TestSuccessPathTTLTimeoutFiresSwTimeout covers the success-with-no-
// response leak: SendToExtension returns true (the frame was accepted),
// but the extension never answers. Without a per-route deadline the
// sender is pinned forever; with the deadline the sender gets a sw_timeout
// and the route is cleared.
func TestSuccessPathTTLTimeoutFiresSwTimeout(t *testing.T) {
	r, st, browser := makeRouterWithRouteTTL(t, 30*time.Millisecond)
	st.SetStatus(StatusOnline)
	browser.online = true
	browser.deliver = true

	sender := &fakeSender{}
	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c1"), sender)

	// Wait for the TTL to expire. AfterFunc runs in its own goroutine;
	// poll until the sender sees the sw_timeout or the test budget runs
	// out.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if msgs := sender.sentMessages(); len(msgs) > 0 {
			payload := decodePayload(t, msgs[0])
			if payload.Status != "error" || payload.Error != "sw_timeout" {
				t.Fatalf("payload = %+v, want sw_timeout", payload)
			}
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if r.lookupInbound("c1") != nil {
		t.Fatal("c1 route should be cleared after the TTL fired")
	}
}

// TestHandleBrowserResponseCancelsTTL verifies that a response cancels the
// pending TTL — without this the timer would still fire and try to send a
// stale sw_timeout to a sender that already received its response.
func TestHandleBrowserResponseCancelsTTL(t *testing.T) {
	r, st, browser := makeRouterWithRouteTTL(t, 50*time.Millisecond)
	st.SetStatus(StatusOnline)
	browser.online = true
	browser.deliver = true

	sender := &fakeSender{}
	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c1"), sender)

	r.HandleBrowserResponse(Envelope{
		ID:        "c1",
		Type:      TypeResponse,
		BrowserID: st.BrowserID(),
		Payload:   json.RawMessage(`{"status":"ok"}`),
		Timestamp: time.Now().UnixMilli(),
	})

	// Drain the response that the sender just got.
	got := sender.sentMessages()
	if len(got) != 1 {
		t.Fatalf("sender got %d messages, want 1 response", len(got))
	}

	// Wait past the TTL; no sw_timeout should follow.
	time.Sleep(150 * time.Millisecond)
	if msgs := sender.sentMessages(); len(msgs) != 1 {
		t.Fatalf("sender got %d messages, want only the original response (no late sw_timeout)", len(msgs))
	}
}

// makeRouterWithRouteTTL is like makeRouter but overrides the router's
// success-path TTL for tests that need a short window.
func makeRouterWithRouteTTL(t *testing.T, ttl time.Duration) (*Router, *StateManager, *fakeBrowser) {
	t.Helper()
	t.Setenv("BB_HOME", t.TempDir())
	st, err := NewStateManager()
	if err != nil {
		t.Fatalf("NewStateManager: %v", err)
	}
	browser := &fakeBrowser{deliver: true}
	return NewRouter(st, browser, &fakeRegistry{}, log.Default(), WithRouteTTL(ttl)), st, browser
}

// TestRequestedDeadlineOverridesShorterBackstop is the regression guard for
// the timeout split: the router's TTL and the caller's deadline used to be
// independent, and the router's was a flat 30s. A caller that legitimately
// waited longer — the MCP schema advertises timeout_ms up to 120s — was cut
// off mid-command and told the service worker had stopped responding, while
// the extension was still working on it.
func TestRequestedDeadlineOverridesShorterBackstop(t *testing.T) {
	r, st, browser := makeRouterWithRouteTTL(t, 30*time.Millisecond)
	st.SetStatus(StatusOnline)
	browser.online = true
	browser.deliver = true

	sender := &fakeSender{}
	// Ask for a deadline well past the router's own backstop.
	requested := 400 * time.Millisecond
	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c1"), sender,
		WithRouteDeadline(requested))

	// The 30ms backstop must not fire: a message here is the bug.
	time.Sleep(120 * time.Millisecond)
	if msgs := sender.sentMessages(); len(msgs) != 0 {
		t.Fatalf("sender got %d messages at 120ms; the 30ms backstop fired "+
			"despite a 400ms requested deadline: %q", len(msgs), msgs[0])
	}

	// The backstop must still fire eventually — the option raises it, it does
	// not disable it — and only *after* the caller's own deadline, so the
	// caller reports its own accurate timeout first. That is carried by the
	// polling loop below, not asserted here: a previous version checked
	// `(requested + margin) - requested == margin`, which is true by
	// construction in int64 nanosecond arithmetic and therefore asserted
	// nothing at all while reading as a guard on the margin.
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if msgs := sender.sentMessages(); len(msgs) > 0 {
			payload := decodePayload(t, msgs[0])
			if payload.Status != "error" || payload.Error != "sw_timeout" {
				t.Fatalf("payload = %+v, want sw_timeout", payload)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no sw_timeout after the requested deadline + backstop margin")
}

// TestBackstopNeverRacesTheCallersOwnTimeout pins the margin itself. If the
// backstop were armed at exactly the caller's deadline, the two timers would
// be due at the same instant and the router's (armed first) could win — and
// the caller would be told the service worker had stopped responding rather
// than that its own request timed out.
func TestBackstopNeverRacesTheCallersOwnTimeout(t *testing.T) {
	r, st, browser := makeRouterWithRouteTTL(t, 10*time.Millisecond)
	st.SetStatus(StatusOnline)
	browser.online = true
	browser.deliver = true

	sender := &fakeSender{}
	const requested = 60 * time.Millisecond
	start := time.Now()
	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c1"), sender,
		WithRouteDeadline(requested))

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if msgs := sender.sentMessages(); len(msgs) > 0 {
			elapsed := time.Since(start)
			if elapsed < requested+routeBackstopMargin {
				t.Fatalf("backstop fired at %v, before deadline %v + margin %v",
					elapsed, requested, routeBackstopMargin)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("backstop never fired")
}

// TestShorterRequestedDeadlineDoesNotShortenBackstop pins the other half: the
// TTL is a leak backstop, and a caller passing a small deadline must not be
// able to pull it down for its own command.
func TestShorterRequestedDeadlineDoesNotShortenBackstop(t *testing.T) {
	r, st, browser := makeRouterWithRouteTTL(t, 300*time.Millisecond)
	st.SetStatus(StatusOnline)
	browser.online = true
	browser.deliver = true

	sender := &fakeSender{}
	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c1"), sender,
		WithRouteDeadline(10*time.Millisecond))

	time.Sleep(150 * time.Millisecond)
	if msgs := sender.sentMessages(); len(msgs) != 0 {
		t.Fatalf("a 10ms request shortened the 300ms backstop: %q", msgs[0])
	}
}

// TestRouteDeadlineReadsBackWhatTheCallerAsked documents the accessor tests
// use to observe the option through the router interface.
func TestRouteDeadlineReadsBackWhatTheCallerAsked(t *testing.T) {
	if got := RouteDeadline(); got != 0 {
		t.Fatalf("RouteDeadline() with no options = %v, want 0", got)
	}
	if got := RouteDeadline(WithRouteDeadline(90 * time.Second)); got != 90*time.Second {
		t.Fatalf("RouteDeadline() = %v, want 90s", got)
	}
}

// TestCommandArrivingDuringTheDisconnectWindowBuffers covers the window the
// browser server opens on every socket close: it clears s.ext under its own
// lock, unlocks, closes the socket, and only then calls
// HandleBrowserDisconnect. A command landing in between sees the state still
// reading online with no extension behind it, so the router took the
// buffering branch — and BufferCommand used to insist on idle_wait, so the
// caller got cannot_buffer for a browser that was merely reconnecting, which
// is exactly what the 5s reconnect tolerance exists to absorb.
func TestCommandArrivingDuringTheDisconnectWindowBuffers(t *testing.T) {
	r, st, browser, _ := makeRouter(t)
	// Online, but the extension socket is already gone.
	st.SetStatus(StatusOnline)
	browser.online = false

	sender := &fakeSender{}
	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c1"), sender)

	if msgs := sender.sentMessages(); len(msgs) != 0 {
		payload := decodePayload(t, msgs[0])
		t.Fatalf("sender got %+v during the disconnect window, want the command buffered", payload)
	}
	if st.Status() != StatusIdleWait {
		t.Errorf("status = %q, want idle_wait once the disconnect is observed", st.Status())
	}

	// The browser server's own HandleBrowserDisconnect lands moments later
	// and must not drop what we just buffered. GetBufferedCommand consumes,
	// so this is the assertion that the buffer survived — check the status
	// first, then claim it last.
	r.HandleBrowserDisconnect()
	if _, ok := st.GetBufferedCommand(); !ok {
		t.Fatal("HandleBrowserDisconnect dropped the buffered command")
	}
	if st.Status() != StatusIdleWait {
		t.Errorf("status = %q, want idle_wait", st.Status())
	}
}

// TestSecondCommandDuringTheDisconnectWindowStillRejected keeps the real
// guarantee intact: the buffer holds one command, and the second one must
// still be told cannot_buffer rather than silently dropped.
func TestSecondCommandDuringTheDisconnectWindowStillRejected(t *testing.T) {
	r, st, browser, _ := makeRouter(t)
	st.SetStatus(StatusOnline)
	browser.online = false

	first := &fakeSender{}
	second := &fakeSender{}
	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c1"), first)
	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), "c2"), second)

	if msgs := first.sentMessages(); len(msgs) != 0 {
		t.Errorf("first sender got %q, want it buffered", msgs[0])
	}
	msgs := second.sentMessages()
	if len(msgs) != 1 {
		t.Fatalf("second sender got %d messages, want 1", len(msgs))
	}
	if payload := decodePayload(t, msgs[0]); payload.Error != "cannot_buffer" {
		t.Fatalf("second command error = %+v, want cannot_buffer", payload)
	}
}

// A client is allowed to omit the envelope id — Encode has always minted one
// for it. But Encode ran *after* the route was registered, so the route was
// filed under "" while the frame going to the extension carried a fresh UUID.
// The response echoes the id it was given, so `takeInbound` never matched: an
// id-less client's command was answered by a perfectly healthy extension and
// the answer was dropped, with the route pinned until the TTL fired.
//
// This is the happy path, not an error path, which is why it went unnoticed:
// nothing failed, the extension just never got its answer through.
func TestIdlessInboundCommandStillGetsItsResponse(t *testing.T) {
	r, st, browser, _ := makeRouter(t)
	st.SetStatus(StatusOnline)
	browser.online = true

	sender := &fakeSender{}
	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), ""), sender)

	sent := browser.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("extension got %d frames, want 1", len(sent))
	}
	env, err := Decode(sent[0])
	if err != nil {
		t.Fatalf("decode the frame the extension received: %v", err)
	}
	if env.ID == "" {
		t.Fatal("the frame went out with an empty id; the extension has " +
			"nothing to echo, so no response can be routed back")
	}

	// The route must be filed under the id that is actually on the wire —
	// that is the whole point, and the response below is the proof.
	r.mu.Lock()
	_, routed := r.inboundByID[env.ID]
	r.mu.Unlock()
	if !routed {
		t.Fatalf("no route under the wire id %q; the response will find "+
			"nothing to deliver to", env.ID)
	}

	r.HandleBrowserResponse(Envelope{
		ID:        env.ID,
		Type:      TypeResponse,
		BrowserID: st.BrowserID(),
		Payload:   json.RawMessage(`{"status":"ok"}`),
	})

	got := sender.sentMessages()
	if len(got) != 1 {
		t.Fatalf("sender got %d messages, want the response routed back", len(got))
	}
	routed2, err := Decode(got[0])
	if err != nil {
		t.Fatalf("decode the delivered response: %v", err)
	}
	if routed2.ID != env.ID {
		t.Errorf("response id = %q, want %q", routed2.ID, env.ID)
	}
}

// The same invariant on the buffer path: a replay that cannot be delivered has
// to find the route to fail, or the fix's own error path misses and the entry
// leaks exactly as it did before.
func TestIdlessBufferedCommandIsFailedOnAFailedReplay(t *testing.T) {
	r, st, browser, _ := makeRouter(t)
	st.SetStatus(StatusIdleWait)

	sender := &fakeSender{}
	r.HandleInboundCommand(commandEnvelope(st.BrowserID(), ""), sender)

	browser.online = true
	browser.deliver = false
	r.HandleBrowserConnect()

	got := sender.sentMessages()
	if len(got) != 1 {
		t.Fatalf("sender got %d messages, want extension_send_failed", len(got))
	}
	payload := decodePayload(t, got[0])
	if payload.Error != "extension_send_failed" {
		t.Errorf("error = %q, want extension_send_failed", payload.Error)
	}

	r.mu.Lock()
	n := len(r.inboundByID)
	r.mu.Unlock()
	if n != 0 {
		t.Errorf("%d routes left behind after a failed replay; each one pins "+
			"its sender until the TTL fires", n)
	}
}
