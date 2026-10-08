package core

import (
	"encoding/json"
	"testing"
)

// LandedTabID's own doc records the bug it exists to prevent: keying a
// landing's result under the addressed tab put a real tab's host under key 0,
// which nothing ever asks for, and left the tab that actually exists with no
// host at all.
//
// `tab:switch` has the identical shape and was not given the same treatment.
// The CLI puts the target in `params.tabId` while the envelope's top-level
// `tabId` stays whatever was global (`cli/send.go` builds
// `CommandPayload{Command, TabID: g.tab, Params: params}`), and pendingCallFrom
// reads only the top-level field. The extension answers with the tab it
// switched to, so the answer is right there in the payload and was ignored.
func TestLandedTabIDReadsTabSwitch(t *testing.T) {
	// Addressed tab 0 (the CLI's default global), switching to tab 5.
	payload := ResponsePayload{Data: json.RawMessage(`{"id":5,"url":"https://new-site.example/","title":"New"}`)}
	if got := LandedTabID("tab:switch", payload, 0); got != 5 {
		t.Errorf("LandedTabID(tab:switch) = %d, want 5", got)
	}
	// A tab:new must keep working, and must still fall back when the payload
	// names no tab.
	if got := LandedTabID("tab:new", payload, 0); got != 5 {
		t.Errorf("LandedTabID(tab:new) = %d, want 5", got)
	}
	if got := LandedTabID("tab:switch", ResponsePayload{}, 3); got != 3 {
		t.Errorf("LandedTabID(tab:switch) with no payload = %d, want the addressed 3", got)
	}
	// A non-landing command is never moved by its result.
	if got := LandedTabID("click", payload, 0); got != 0 {
		t.Errorf("LandedTabID(click) = %d, want the addressed 0", got)
	}
}

// A landing command that names no site must not be reported as if it had.
//
// hostAfter updated tabHost only when the payload carried a URL, but then
// returned tabHost[tab] either way — so `goBack`, `goForward` and `refresh`,
// which the extension answers with a bare `{ok:true}`, handed RecordResult the
// *previous* host. That took the `isLanding(command) && host != ""` branch:
// the old site's card was re-armed and lastDigest nil'd, so the next snapshot
// verified the old card against the new page, reported most entries missing,
// and recorded a healthy card as stale. This is the exact failure the guard at
// RecordResult was written to prevent, defeated by a landing that reports no
// URL.
//
// Returning "" lets the guard that is already there do its job, and it is also
// correct on its own terms: a landing on chrome:// or about:blank genuinely
// names no site.
func TestALandingThatNamesNoSiteDoesNotReportThePreviousOne(t *testing.T) {
	const noURL = `{"ok":true}`
	for _, command := range []string{"goBack", "goForward", "refresh"} {
		if !IsLandingCommand(command) {
			t.Errorf("%s is no longer a landing command; IsLandingCommand and the "+
				"extension's answer to it have to be changed together", command)
		}
		if h := LandingHost(ResponsePayload{Data: json.RawMessage(noURL)}); h != "" {
			t.Errorf("LandingHost(%s) = %q, want empty", command, h)
		}
	}
	// The control that actually lands still names a site.
	if h := LandingHost(ResponsePayload{Data: json.RawMessage(`{"url":"https://new-site.example/"}`)}); h != "new-site.example" {
		t.Errorf("LandingHost(navigate) = %q, want new-site.example", h)
	}
}

// hostHook captures the host the router told the memory hook about, which is
// the value hostAfter produced.
type hostHook struct {
	hosts []string
}

func (h *hostHook) RecordCommand(string, string, string, int, map[string]any) {}

func (h *hostHook) RecordRouterError(string, string, string, int, ResponsePayload) {}

func (h *hostHook) RecordResult(_, command, host string, _ int, _ ResponsePayload) {
	h.hosts = append(h.hosts, command+"="+host)
}

func (h *hostHook) TakeSiteNote(string, string, int) string { return "" }

// The end-to-end shape of the goBack bug: a tab is known to be on one site,
// a landing command comes back naming no site, and the hook is told the tab is
// still on the first one — which is what re-arms its card and invalidates the
// digest for a page the agent has already left.
func TestHostAfterDoesNotHandBackThePreviousHostForANamelessLanding(t *testing.T) {
	h := &hostHook{}
	r, _ := makeRouterWithHook(t, h)

	// A real landing: the tab is now known to be on news.example.
	r.mu.Lock()
	r.tabHost[7] = "news.example"
	r.mu.Unlock()

	if got := r.hostAfter(7, "goBack", ResponsePayload{Data: json.RawMessage(`{"ok":true}`)}); got != "" {
		t.Errorf("hostAfter(goBack) = %q, want empty — a landing that names no site "+
			"must not be reported as the previous one", got)
	}
	// The tab's own view is untouched: HostForTab still answers where the tab
	// was last known to be, which is what RecordCommand and the site note use.
	if got := r.HostForTab(7); got != "news.example" {
		t.Errorf("HostForTab(7) = %q, want news.example", got)
	}
	// A landing that does name a site still updates and reports it.
	if got := r.hostAfter(7, "navigate", ResponsePayload{Data: json.RawMessage(`{"url":"https://other.example/x"}`)}); got != "other.example" {
		t.Errorf("hostAfter(navigate) = %q, want other.example", got)
	}
}

// A closed tab's host must be forgotten, and the map must not grow forever.
//
// tabHost was only ever written to: hostAfter added an entry per landing and
// nothing removed one, and tab:close is not a landing command so it never
// reached that branch at all. A long-running session that opens and closes
// thousands of tabs accumulated an entry per id Chrome ever assigned, with the
// memory store's own tabState growing alongside it — the same class of
// unbounded state ADR-0032 added maxInflight to fix, missed in two places.
func TestAClosedTabIsForgotten(t *testing.T) {
	r, _ := makeRouterWithHook(t, &hostHook{})

	r.hostAfter(7, "navigate", ResponsePayload{Data: json.RawMessage(`{"url":"https://a.example/x"}`)})
	if got := r.HostForTab(7); got != "a.example" {
		t.Fatalf("HostForTab(7) = %q, want a.example", got)
	}

	r.hostAfter(7, "tab:close", ResponsePayload{Data: json.RawMessage(`{"ok":true}`)})
	if got := r.HostForTab(7); got != "" {
		t.Errorf("HostForTab(7) = %q after the tab closed, want empty", got)
	}

	// A close that failed leaves the tab open, so its host stays.
	r.hostAfter(8, "navigate", ResponsePayload{Data: json.RawMessage(`{"url":"https://b.example/x"}`)})
	r.hostAfter(8, "tab:close", ResponsePayload{Status: "error", Error: "no_such_tab"})
	if got := r.HostForTab(8); got != "b.example" {
		t.Errorf("HostForTab(8) = %q after a failed close, want b.example", got)
	}
}

// The bound has to hold for the closes the router never sees — a tab the user
// closed produces no tab:close at all.
func TestTabHostStaysBoundedWithoutCloses(t *testing.T) {
	r, _ := makeRouterWithHook(t, &hostHook{})
	for i := 0; i < maxTabHost+64; i++ {
		r.hostAfter(i, "navigate", ResponsePayload{Data: json.RawMessage(`{"url":"https://a.example/x"}`)})
	}
	r.mu.Lock()
	size := len(r.tabHost)
	r.mu.Unlock()
	if size > maxTabHost {
		t.Errorf("tabHost holds %d entries, over the %d bound", size, maxTabHost)
	}
}

// A tab the agent never navigated must still be known to the router.
//
// tabHost was written by landing commands alone, and the flow the MCP tools
// document is tab_list then snapshot. An agent following it reached a tab it
// never navigated, so HostForTab returned "", TakeSiteNote bailed at its own
// host == "" guard, and the learner's snapshot branch never armed either — so
// the documented path injected nothing and learned nothing, on a card that
// existed.
//
// These are the commands whose entire answer *is* a location, so they may
// correct the router's view. They are not landings, so nothing re-arms a card
// or discards a digest: the page did not change.
func TestAHostReportingCommandTeachesTheRouterWhereATabIs(t *testing.T) {
	t.Run("pageinfo names the addressed tab", func(t *testing.T) {
		r, _ := makeRouterWithHook(t, &hostHook{})
		payload := ResponsePayload{Data: json.RawMessage(`{"id":5,"url":"https://mail.example.com/u/0/","active":true}`)}
		r.hostAfter(5, "pageinfo", payload)
		if got := r.HostForTab(5); got != "mail.example.com" {
			t.Errorf("HostForTab(5) = %q after pageinfo, want mail.example.com", got)
		}
	})

	t.Run("tab:list names every tab", func(t *testing.T) {
		r, _ := makeRouterWithHook(t, &hostHook{})
		payload := ResponsePayload{Data: json.RawMessage(
			`[{"id":1,"url":"https://a.example/"},{"id":2,"url":"https://b.example/x"},{"id":3,"url":"chrome://extensions"}]`)}
		r.hostAfter(0, "tab:list", payload)
		for tab, want := range map[int]string{1: "a.example", 2: "b.example"} {
			if got := r.HostForTab(tab); got != want {
				t.Errorf("HostForTab(%d) = %q, want %q", tab, got, want)
			}
		}
		// chrome:// names no site, and must not become one.
		if got := r.HostForTab(3); got != "" {
			t.Errorf("HostForTab(3) = %q for a chrome:// tab, want empty", got)
		}
	})

	// And a host report is not a landing: the card stays armed and the digest
	// is not discarded, because the page did not change.
	t.Run("a host report is not a landing", func(t *testing.T) {
		if IsLandingCommand("pageinfo") || IsLandingCommand("tab:list") {
			t.Error("a host report was classified as a landing, so it would re-arm a card " +
				"and discard a digest for a page that did not change")
		}
		payload := ResponsePayload{Data: json.RawMessage(`{"url":"https://a.example/"}`)}
		if got := LandingHost(payload); got != "a.example" {
			t.Errorf("LandingHost on a pageinfo result = %q, want a.example", got)
		}
	})
}
