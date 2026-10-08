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
